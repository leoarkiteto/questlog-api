// Command migrate applies pending database migrations and exits.
// Used by CI/CD and for one-off production/QA migrations:
//
//	cd questlog-api && go run ./cmd/migrate
package main

import (
	"context"
	"log"
	"os"
	"time"

	"questlog/internal/repo"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gamelog@localhost:5432/gamelog"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := repo.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Println("migrations up to date")
}
