package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr        string
	DatabaseURL string
	UploadsDir  string
	OIDC        OIDCConfig
	VAPID       VAPIDConfig
}

type VAPIDConfig struct {
	PublicKey  string
	PrivateKey string
	Contact   string
}

type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func LoadConfig() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	return Config{
		Addr:        ":" + getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "trippy-track.db"),
		UploadsDir:  getEnv("UPLOADS_DIR", "uploads"),
		OIDC: OIDCConfig{
			IssuerURL:    getEnv("OIDC_ISSUER_URL", ""),
			ClientID:     getEnv("OIDC_CLIENT_ID", ""),
			ClientSecret: getEnv("OIDC_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("OIDC_REDIRECT_URL", "http://localhost:8080/callback"),
		},
		VAPID: VAPIDConfig{
			PublicKey:  getEnv("VAPID_PUBLIC_KEY", ""),
			PrivateKey: getEnv("VAPID_PRIVATE_KEY", ""),
			Contact:    getEnv("VAPID_CONTACT", ""),
		},
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (c *Config) ValidateOIDCConfig() error {
	if c.OIDC.IssuerURL == "" {
		return fmt.Errorf("OIDC_ISSUER_URL is required")
	}
	if c.OIDC.ClientID == "" {
		return fmt.Errorf("OIDC_CLIENT_ID is required")
	}
	if c.OIDC.ClientSecret == "" {
		return fmt.Errorf("OIDC_CLIENT_SECRET is required")
	}
	if c.OIDC.RedirectURL == "" {
		return fmt.Errorf("OIDC_REDIRECT_URL is required")
	}
	return nil
}
