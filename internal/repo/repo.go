// Package repo is shaping ourt DB queries
package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leoarkiteto/questlog-api/internal/model"
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

// ---- Users ----

const userCols = `id, email, password_hash, created_at`

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// ErrEmailTaken reports that the email is already registered.
var ErrEmailTaken = errors.New("email already registered")

// CreateUser inserts a new account and returns it with id and timestamps.
// email must already be normalized (trimmed + lowercased by the caller).
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*model.User, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING `+userCols, email, passwordHash)
	u, err := scanUser(row)
	if isUniqueViolation(err) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// UserByEmail returns the user with the given (normalized) email, or
// pgx.ErrNoRows when there is none.
func (s *Store) UserByEmail(ctx context.Context, email string) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE lower(email) = $1`, email))
}

// UpdateUserPassword replaces a user's stored password hash (used to
// upgrade legacy or non-default hashes on login). Returns pgx.ErrNoRows
// when the user does not exist.
func (s *Store) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, passwordHash, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// FirstUser returns the account with the lowest id (the owner, who
// adopted the pre-account games), or pgx.ErrNoRows when no accounts
// exist.
func (s *Store) FirstUser(ctx context.Context) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users ORDER BY id ASC LIMIT 1`))
}

// UserCount returns how many accounts exist (used to detect the first
// user, who adopts the orphan games from before accounts existed).
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// ---- Sessions ----

// CreateSession stores a session: the SHA-256 hash of the raw token
// (never the token itself), the per-session CSRF token, and its expiry.
func (s *Store) CreateSession(ctx context.Context, tokenHash, csrfToken string, userID int64, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at)
		VALUES ($1, $2, $3, $4)`, tokenHash, userID, csrfToken, expiresAt)
	return err
}

// SessionByTokenHash returns the user and CSRF token for a live session,
// or pgx.ErrNoRows when the token is unknown or expired.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*model.User, string, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.password_hash, u.created_at, s.csrf_token
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > $2`, tokenHash, now)
	var u model.User
	var csrf string
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &csrf); err != nil {
		return nil, "", err
	}
	return &u, csrf, nil
}

// DeleteSession removes a session (logout).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteExpiredSessions removes stale sessions; call it opportunistically.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	return err
}

// AdoptOrphanGames assigns ownerless games (created before accounts
// existed) to the given user. Returns the number of adopted rows.
func (s *Store) AdoptOrphanGames(ctx context.Context, userID int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE games SET user_id = $1 WHERE user_id IS NULL`, userID)
	return tag.RowsAffected(), err
}

// ---- Games ----

// ErrDuplicate reports that a game is already in the collection (same
// normalized title or same Steam app id). Existing holds the conflicting
// card so callers can surface its current status/list.
type ErrDuplicate struct {
	Existing model.Game
}

func (e *ErrDuplicate) Error() string {
	return fmt.Sprintf(
		"game %q already exists (id %d, status %s)",
		e.Existing.Title,
		e.Existing.ID,
		e.Existing.Status,
	)
}

// normalizeTitle reduces a title to its comparison key: lowercase,
// trimmed, internal whitespace runs collapsed to single spaces. Must stay
// in sync with migration 0005, which applies the same rule in SQL
// (lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g')))).
func normalizeTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// findDuplicate returns the first card that would collide with g inside
// the user's collection: same normalized title, or same non-null Steam
// app id. excludeID skips the card being updated. Returns nil when there
// is no conflict.
func (s *Store) findDuplicate(
	ctx context.Context,
	userID, excludeID int64,
	g *model.Game,
	norm string,
) (*model.Game, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+gameCols+` FROM games
		WHERE user_id = $1 AND id <> $2
		  AND (normalized_title = $3 OR ($4::bigint IS NOT NULL AND steam_appid = $4))
		ORDER BY id DESC
		LIMIT 1`,
		userID, excludeID, norm, g.SteamAppID)
	existing, err := scanGame(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return existing, nil
}

const gameCols = `id, user_id, title, status, rating, platform, year, genre, cover_url, description, notes, steam_appid, time_to_beat_minutes, created_at, updated_at`

func scanGame(row pgx.Row) (*model.Game, error) {
	var g model.Game
	err := row.Scan(&g.ID, &g.UserID, &g.Title, &g.Status, &g.Rating, &g.Platform,
		&g.Year, &g.Genre, &g.CoverURL, &g.Description, &g.Notes, &g.SteamAppID,
		&g.TimeToBeatMinutes, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// List returns all of a user's games, optionally filtered by status,
// newest first.
func (s *Store) List(ctx context.Context, userID int64, status *model.Status) ([]model.Game, error) {
	q := `SELECT ` + gameCols + ` FROM games WHERE user_id = $1`
	var args []any = []any{userID}
	if status != nil {
		q += ` AND status = $2`
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

// ListPage returns one page of a user's games matching the library
// filters, plus whether more rows exist beyond it. sortKey is "recent"
// (default), "title", or "rating". limit/offset paginate; LIMIT is
// fetched as limit+1 so hasMore is decided without a second query.
func (s *Store) ListPage(
	ctx context.Context,
	userID int64,
	status *model.Status,
	platform, sortKey string,
	limit, offset int,
) ([]model.Game, bool, error) {
	q := `SELECT ` + gameCols + ` FROM games`
	conds := []string{fmt.Sprintf("user_id = $%d", 1)}
	args := []any{userID}
	if status != nil {
		args = append(args, *status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if platform != "" {
		args = append(args, platform)
		conds = append(conds, fmt.Sprintf("platform = $%d", len(args)))
	}
	q += ` WHERE ` + strings.Join(conds, " AND ")
	switch sortKey {
	case "title":
		q += ` ORDER BY lower(title) ASC, id ASC`
	case "rating":
		q += ` ORDER BY rating DESC, lower(title) ASC, id ASC`
	default:
		q += ` ORDER BY created_at DESC, id DESC`
	}
	args = append(args, limit+1, offset)
	q += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	games := []model.Game{}
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, false, err
		}
		games = append(games, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(games) > limit
	if hasMore {
		games = games[:limit]
	}
	return games, hasMore, nil
}

// Count returns the total number of games in a user's collection.
func (s *Store) Count(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM games WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// CountFiltered returns how many of a user's games match the library
// status/platform filters (used for the "N games" label, which must not
// be the page size).
func (s *Store) CountFiltered(ctx context.Context, userID int64, status *model.Status, platform string) (int, error) {
	q := `SELECT count(*) FROM games`
	var conds []string = []string{fmt.Sprintf("user_id = $%d", 1)}
	args := []any{userID}
	if status != nil {
		args = append(args, *status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if platform != "" {
		args = append(args, platform)
		conds = append(conds, fmt.Sprintf("platform = $%d", len(args)))
	}
	q += ` WHERE ` + strings.Join(conds, " AND ")
	var n int
	err := s.pool.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

// PlatformCounts returns a user's distinct non-empty platforms and how
// many games each has, for the library filter panel. When status is
// non-nil, counts are scoped to that status so the panel reflects the
// currently selected Status filter.
func (s *Store) PlatformCounts(ctx context.Context, userID int64, status *model.Status) ([]string, map[string]int, error) {
	args := []any{userID}
	cond := `user_id = $1 AND platform <> ''`
	if status != nil {
		args = append(args, *status)
		cond += fmt.Sprintf(" AND status = $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT platform, count(*)
		FROM games
		WHERE `+cond+`
		GROUP BY platform
		ORDER BY platform`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	platforms := []string{}
	counts := map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			return nil, nil, err
		}
		platforms = append(platforms, p)
		counts[p] = n
	}
	return platforms, counts, rows.Err()
}

// CountByStatus returns how many of a user's games are in each status,
// for the profile page. Statuses without games are absent from the map.
func (s *Store) CountByStatus(ctx context.Context, userID int64) (map[model.Status]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status, count(*)
		FROM games
		WHERE user_id = $1
		GROUP BY status`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[model.Status]int{}
	for rows.Next() {
		var st model.Status
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		counts[st] = n
	}
	return counts, rows.Err()
}

// Get returns a single game by id, or pgx.ErrNoRows. A game belonging
// to another user is indistinguishable from a missing one.
func (s *Store) Get(ctx context.Context, userID, id int64) (*model.Game, error) {
	return scanGame(s.pool.QueryRow(ctx,
		`SELECT `+gameCols+` FROM games WHERE id = $1 AND user_id = $2`, id, userID))
}

// Create inserts a new game in the user's collection and returns it with
// id and timestamps. Returns *ErrDuplicate when a card with the same
// normalized title or Steam app id already exists in that collection — a
// game may only appear once per user.
func (s *Store) Create(ctx context.Context, userID int64, g *model.Game) (*model.Game, error) {
	norm := normalizeTitle(g.Title)
	existing, err := s.findDuplicate(ctx, userID, 0, g, norm)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, &ErrDuplicate{Existing: *existing}
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO games (user_id, title, status, rating, platform, year, genre, cover_url, description, notes, steam_appid, time_to_beat_minutes, normalized_title)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+gameCols,
		userID, g.Title, g.Status, g.Rating, g.Platform, g.Year, g.Genre, g.CoverURL,
		g.Description, g.Notes, g.SteamAppID, g.TimeToBeatMinutes, norm)
	created, err := scanGame(row)
	if isUniqueViolation(err) {
		// Lost a race — the unique indexes are the backstop. Re-query to
		// report the existing card instead of a raw constraint error.
		if existing, findErr := s.findDuplicate(ctx, userID, 0, g, norm); findErr == nil &&
			existing != nil {
			return nil, &ErrDuplicate{Existing: *existing}
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update overwrites a game by id (only within the user's collection) and
// returns the updated row. Returns *ErrDuplicate when the new title or
// Steam app id collides with another card in that collection.
func (s *Store) Update(ctx context.Context, userID, id int64, g *model.Game) (*model.Game, error) {
	norm := normalizeTitle(g.Title)
	existing, err := s.findDuplicate(ctx, userID, id, g, norm)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, &ErrDuplicate{Existing: *existing}
	}

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
		    normalized_title = $12,
		    updated_at = now()
		WHERE id = $13 AND user_id = $14
		RETURNING `+gameCols,
		g.Title, g.Status, g.Rating, g.Platform, g.Year, g.Genre, g.CoverURL,
		g.Description, g.Notes, g.SteamAppID, g.TimeToBeatMinutes, norm, id, userID)
	updated, err := scanGame(row)
	if isUniqueViolation(err) {
		if existing, findErr := s.findDuplicate(ctx, userID, id, g, norm); findErr == nil &&
			existing != nil {
			return nil, &ErrDuplicate{Existing: *existing}
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete removes a game from the user's collection; returns pgx.ErrNoRows
// if it didn't exist (or belonged to another user).
func (s *Store) Delete(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM games WHERE id = $1 AND user_id = $2`, id, userID)
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
