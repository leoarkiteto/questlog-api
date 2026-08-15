package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/leoarkiteto/questlog-api/internal/auth"
	"github.com/leoarkiteto/questlog-api/internal/catalog"
	"github.com/leoarkiteto/questlog-api/internal/config"
	"github.com/leoarkiteto/questlog-api/internal/hltb"
	"github.com/leoarkiteto/questlog-api/internal/igdb"
	"github.com/leoarkiteto/questlog-api/internal/password"
	"github.com/leoarkiteto/questlog-api/internal/repo"
	"github.com/leoarkiteto/questlog-api/internal/steam"
	"github.com/leoarkiteto/questlog-api/internal/web"
)

//go:embed static
var staticFS embed.FS

func main() {
	// Secrets can live in .env (gitignored); real env vars win.
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Printf("warning: could not load .env: %v", err)
	}

	dbURL := getenv("DATABASE_URL", "postgres://gamelog:gamelog@localhost:5432/gamelog")
	port := getenv("PORT", "8080")
	sessionMaxAge := sessionMaxAgeFromEnv()

	// PASSWORD_PEPPER is a required secret: it is keyed into every
	// password hash, so the server and cmd/user must share the same
	// value. Fail fast rather than silently minting unrecoverable hashes.
	pw, err := password.New(os.Getenv("PASSWORD_PEPPER"))
	if err != nil {
		log.Fatalf("password: %v (set PASSWORD_PEPPER, e.g. openssl rand -hex 32)", err)
	}

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

	mux := http.NewServeMux()
	mux.Handle("/static/", staticHandler())
	mux.Handle("/", web.New(
		store,
		auth.NewWithMaxAge(store, store, pw, sessionMaxAge),
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
	))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("questlog listening on http://localhost:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// staticHandler serves the embedded static assets (CSS, JS, logo) under
// /static/.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// sessionMaxAgeFromEnv reads SESSION_MAX_AGE as a Go duration (e.g.
// "720h"), defaulting to 30 days when unset or invalid.
func sessionMaxAgeFromEnv() time.Duration {
	if v := os.Getenv("SESSION_MAX_AGE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		log.Printf("warning: invalid SESSION_MAX_AGE %q, using default", v)
	}
	return 30 * 24 * time.Hour
}
