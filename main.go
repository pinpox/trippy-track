package main

import (
	"log"
	"net/http"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	config := LoadConfig()

	if err := config.ValidateOIDCConfig(); err != nil {
		log.Fatalf("OIDC config: %v", err)
	}

	if err := os.MkdirAll("uploads", 0o755); err != nil {
		log.Fatalf("create uploads dir: %v", err)
	}

	db, err := initDB(config.DatabaseURL)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	auth, err := NewAuthService(db, config.OIDC)
	if err != nil {
		log.Fatalf("init auth: %v", err)
	}

	srv, err := newServer(db, config.Addr, auth)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	log.Printf("listening on %s", config.Addr)
	if err := http.ListenAndServe(config.Addr, srv.routes()); err != nil {
		log.Fatalf("server: %v", err)
	}
}
