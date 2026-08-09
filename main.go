package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/leoarkiteto/questlog-api/internal/api"
	"github.com/leoarkiteto/questlog-api/internal/catalog"
	"github.com/leoarkiteto/questlog-api/internal/config"
	"github.com/leoarkiteto/questlog-api/internal/hltb"
	"github.com/leoarkiteto/questlog-api/internal/igdb"
	"github.com/leoarkiteto/questlog-api/internal/repo"
	"github.com/leoarkiteto/questlog-api/internal/steam"
)

func main() {
	// Secrets can live in backend/.env (gitignored); real env vars win.
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Printf("warning: could not load .env: %v", err)
	}

	dbURL := getenv("DATABASE_URL", "postgres://gamelog:gamelog@localhost:5432/gamelog")
	port := getenv("PORT", "8080")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := repo.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Println("database ready")

	srv := &http.Server{
		Addr: ":" + port,
		Handler: api.New(
			store,
			catalog.New(
				steam.New(getenv("STEAM_API_KEY", "")),
				igdb.New(
					getenv("IGDB_CLIENT_ID", ""),
					getenv("IGDB_CLIENT_SECRET", ""),
					getenv("IGDB_BASE_URL", ""),
					getenv("IGDB_TOKEN_URL", ""),
				),
				hltb.New(),
			),
		),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("questlog API listening on http://localhost:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
