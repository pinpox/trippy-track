package main

import (
	"log"
	"net/http"
	"net/url"
)

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if sessionCookie, err := r.Cookie("session"); err == nil {
		if user, err := s.auth.GetUserFromSession(sessionCookie.Value); err == nil && user != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	s.tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	state, err := s.auth.GenerateState()
	if err != nil {
		log.Printf("Error generating state: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})

	verifier := s.auth.GenerateVerifier()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_verifier",
		Value:    verifier,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})

	authURL := s.auth.GetAuthURL(state, verifier)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		log.Printf("No state cookie found: %v", err)
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" || state != stateCookie.Value {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name: "oauth_state", Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})

	verifierCookie, err := r.Cookie("oauth_verifier")
	if err != nil {
		http.Error(w, "Invalid PKCE verifier", http.StatusBadRequest)
		return
	}
	verifier := verifierCookie.Value
	http.SetCookie(w, &http.Cookie{
		Name: "oauth_verifier", Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		log.Printf("OIDC error: %s - %s", errMsg, r.URL.Query().Get("error_description"))
		http.Error(w, "Authentication failed", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "No authorization code received", http.StatusBadRequest)
		return
	}

	log.Printf("Exchanging code for tokens...")
	user, err := s.auth.ExchangeCode(r.Context(), code, verifier)
	if err != nil {
		log.Printf("Error exchanging code: %v", err)
		http.Error(w, "Authentication failed", http.StatusInternalServerError)
		return
	}
	log.Printf("User authenticated: %s (%s)", user.Name, user.Email)

	sessionID, _, err := s.auth.CreateSession(user.ID)
	if err != nil {
		log.Printf("Error creating session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	log.Printf("Session created: %s", sessionID[:8]+"...")

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 60 * 60,
	})

	returnURL := "/"
	if returnCookie, err := r.Cookie("return_url"); err == nil {
		if decodedURL, err := url.QueryUnescape(returnCookie.Value); err == nil {
			returnURL = decodedURL
		}
		http.SetCookie(w, &http.Cookie{
			Name: "return_url", Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
		})
	}

	log.Printf("User %s (%s) logged in", user.Name, user.Email)
	http.Redirect(w, r, returnURL, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sessionCookie, err := r.Cookie("session"); err == nil {
		s.auth.DeleteSession(sessionCookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})

	http.Redirect(w, r, "/login", http.StatusFound)
}
