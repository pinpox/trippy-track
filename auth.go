package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type User struct {
	ID          int
	OIDCSubject string
	Email       string
	Name        string
	CreatedAt   string
	LastLoginAt string
}

type AuthService struct {
	db       *sql.DB
	config   OIDCConfig
	provider *oidc.Provider
	oauth2   oauth2.Config
}

func NewAuthService(db *sql.DB, config OIDCConfig) (*AuthService, error) {
	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &AuthService{
		db:       db,
		config:   config,
		provider: provider,
		oauth2:   oauth2Config,
	}, nil
}

func (a *AuthService) GenerateState() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (a *AuthService) GenerateVerifier() string {
	return oauth2.GenerateVerifier()
}

func (a *AuthService) GetAuthURL(state, verifier string) string {
	return a.oauth2.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (a *AuthService) ExchangeCode(ctx context.Context, code, codeVerifier string) (*User, error) {
	token, err := a.oauth2.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	verifier := a.provider.Verifier(&oidc.Config{ClientID: a.config.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims struct {
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername  string `json:"preferred_username"`
	}

	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	username := claims.PreferredUsername
	if username == "" {
		username = claims.Name
	}

	user, err := a.createOrUpdateUser(claims.Subject, claims.Email, username)
	if err != nil {
		return nil, fmt.Errorf("failed to create/update user: %w", err)
	}

	return user, nil
}

func (a *AuthService) createOrUpdateUser(subject, email, username string) (*User, error) {
	var user User
	now := time.Now().UTC().Format(time.RFC3339)

	err := a.db.QueryRow(
		"SELECT id, oidc_subject, email, name, created_at, last_login_at FROM users WHERE oidc_subject = ?",
		subject,
	).Scan(&user.ID, &user.OIDCSubject, &user.Email, &user.Name, &user.CreatedAt, &user.LastLoginAt)

	if err == sql.ErrNoRows {
		result, err := a.db.Exec(
			"INSERT INTO users (oidc_subject, email, name, created_at, last_login_at) VALUES (?, ?, ?, ?, ?)",
			subject, email, username, now, now,
		)
		if err != nil {
			return nil, err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}

		user = User{
			ID:          int(id),
			OIDCSubject: subject,
			Email:       email,
			Name:        username,
		}
	} else if err != nil {
		return nil, err
	} else {
		_, err = a.db.Exec(
			"UPDATE users SET email = ?, name = ?, last_login_at = ? WHERE id = ?",
			email, username, now, user.ID,
		)
		if err != nil {
			return nil, err
		}
		user.Email = email
		user.Name = username
	}

	return &user, nil
}

func (a *AuthService) CreateSession(userID int) (string, time.Time, error) {
	sessionID, err := a.GenerateState()
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = a.db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)",
		sessionID, userID, expiresAt.UTC().Format(time.RFC3339), now,
	)
	if err != nil {
		return "", time.Time{}, err
	}

	return sessionID, expiresAt, nil
}

func (a *AuthService) GetUserFromSession(sessionID string) (*User, error) {
	var user User
	now := time.Now().UTC().Format(time.RFC3339)

	err := a.db.QueryRow(`
		SELECT u.id, u.oidc_subject, u.email, u.name, u.created_at, u.last_login_at
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.id = ? AND s.expires_at > ?
	`, sessionID, now).Scan(&user.ID, &user.OIDCSubject, &user.Email, &user.Name, &user.CreatedAt, &user.LastLoginAt)

	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (a *AuthService) DeleteSession(sessionID string) error {
	_, err := a.db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

func (a *AuthService) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for public routes (but not /t/{token}/admin)
		isPublicTripPath := strings.HasPrefix(r.URL.Path, "/t/") && !strings.Contains(r.URL.Path, "/admin")
		if strings.HasPrefix(r.URL.Path, "/login") ||
			strings.HasPrefix(r.URL.Path, "/callback") ||
			strings.HasPrefix(r.URL.Path, "/static/") ||
			strings.HasPrefix(r.URL.Path, "/uploads/") ||
			isPublicTripPath ||
			strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		sessionCookie, err := r.Cookie("session")
		if err != nil {
			redirectToLogin(w, r)
			return
		}

		user, err := a.GetUserFromSession(sessionCookie.Value)
		if err != nil {
			redirectToLogin(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), "user", user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	returnURL := r.URL.String()
	if returnURL != "" && returnURL != "/" {
		http.SetCookie(w, &http.Cookie{
			Name:     "return_url",
			Value:    url.QueryEscape(returnURL),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   300,
		})
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

func GetUserFromContext(r *http.Request) *User {
	if user, ok := r.Context().Value("user").(*User); ok {
		return user
	}
	return nil
}
