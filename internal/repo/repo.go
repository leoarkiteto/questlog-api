package repo

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"questlog/internal/model"
)


//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the Postgres connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a connection pool and verifies connectivity.
//
// QueryExecModeCacheDescribe makes the pool compatible with connection
// poolers in transaction mode (e.g. Neon's pooled DATABASE_URL behind
// pgbouncer) — prepared-statement caching breaks under pgbouncer, but
// cache-describe works with both pooled and direct connections.
func New(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate applies any pending embedded SQL migrations in filename order.
func (s *Store) Migrate(ctx context.Context) error {
	// Bootstrap the bookkeeping table before checking what's applied.
	if _, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := s.migrationApplied(ctx, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := s.applyMigration(ctx, name, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) migrationApplied(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&exists)
	return exists, err
}

func (s *Store) applyMigration(ctx context.Context, name, sqlText string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, sqlText); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- Games ----

const gameCols = `id, title, status, rating, platform, year, genre, cover_url, description, notes, steam_appid, time_to_beat_minutes, created_at, updated_at`

func scanGame(row pgx.Row) (*model.Game, error) {
	var g model.Game
	err := row.Scan(&g.ID, &g.Title, &g.Status, &g.Rating, &g.Platform, &g.Year,
		&g.Genre, &g.CoverURL, &g.Description, &g.Notes, &g.SteamAppID,
		&g.TimeToBeatMinutes, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// List returns all games, optionally filtered by status, newest first.
func (s *Store) List(ctx context.Context, status *model.Status) ([]model.Game, error) {
	q := `SELECT ` + gameCols + ` FROM games`
	var args []any
	if status != nil {
		q += ` WHERE status = $1`
		args = append(args, *status)
	}
	q += ` ORDER BY created_at DESC, id DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	games := []model.Game{}
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, *g)
	}
	return games, rows.Err()
}

// Get returns a single game by id, or pgx.ErrNoRows.
func (s *Store) Get(ctx context.Context, id int64) (*model.Game, error) {
	return scanGame(s.pool.QueryRow(ctx,
		`SELECT `+gameCols+` FROM games WHERE id = $1`, id))
}

// Create inserts a new game and returns it with id and timestamps.
func (s *Store) Create(ctx context.Context, g *model.Game) (*model.Game, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO games (title, status, rating, platform, year, genre, cover_url, description, notes, steam_appid, time_to_beat_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+gameCols,
		g.Title, g.Status, g.Rating, g.Platform, g.Year, g.Genre, g.CoverURL,
		g.Description, g.Notes, g.SteamAppID, g.TimeToBeatMinutes)
	return scanGame(row)
}

// Update overwrites a game by id and returns the updated row.
func (s *Store) Update(ctx context.Context, id int64, g *model.Game) (*model.Game, error) {
	// COALESCE: a PUT that omits time_to_beat_minutes (or sends null)
	// keeps the stored value instead of wiping it — no caller clears
	// the field today, and a failed HLTB refresh shouldn't erase a
	// known-good number. (This does mean PUT can never clear it.)
	row := s.pool.QueryRow(ctx, `
		UPDATE games
		SET title = $1, status = $2, rating = $3, platform = $4, year = $5,
		    genre = $6, cover_url = $7, description = $8, notes = $9,
		    steam_appid = $10,
		    time_to_beat_minutes = COALESCE($11, time_to_beat_minutes),
		    updated_at = now()
		WHERE id = $12
		RETURNING `+gameCols,
		g.Title, g.Status, g.Rating, g.Platform, g.Year, g.Genre, g.CoverURL,
		g.Description, g.Notes, g.SteamAppID, g.TimeToBeatMinutes, id)
	return scanGame(row)
}

// Delete removes a game; returns pgx.ErrNoRows if it didn't exist.
func (s *Store) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM games WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Ping is used by the health endpoint.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
