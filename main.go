package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "trippy-track.db", "SQLite database path")
	flag.Parse()

	// Ensure uploads directory exists
	if err := os.MkdirAll("uploads", 0o755); err != nil {
		log.Fatalf("create uploads dir: %v", err)
	}

	db, err := initDB(*dbPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	srv, err := newServer(db, *addr)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, srv.routes()); err != nil {
		log.Fatalf("server: %v", err)
	}
}
