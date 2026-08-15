// Package auth implements password hashing, server-side sessions, and
// CSRF protection for Questlog accounts.
//
// Sessions are opaque random tokens stored by the browser in an
// HttpOnly cookie; only their SHA-256 hash is persisted in Postgres, so
// a database leak never exposes live session tokens. Every session also
// carries a CSRF token checked on state-changing requests.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/leoarkiteto/questlog-api/internal/model"
	"github.com/leoarkiteto/questlog-api/internal/repo"
)

// SessionCookie is the name of the browser cookie holding the session
// token.
const SessionCookie = "questlog_session"

// AnonCSRFCookie is the name of the cookie carrying the CSRF token for
// the login form, which has no session yet.
const AnonCSRFCookie = "questlog_csrf"

// SessionMaxAge is how long a session lives before it expires.
const SessionMaxAge = 30 * 24 * time.Hour

// minPasswordLen is the shortest allowed password; maxPasswordLen caps
// input length for DoS protection (Argon2id has no 72-byte truncation
// like bcrypt did, so the old 72 cap was lifted).
const (
	minPasswordLen = 8
	maxPasswordLen = 1024
)

// Sentinel errors returned by the service.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrInvalidEmail       = errors.New("enter a valid email address")
	ErrWeakPassword       = errors.New("password must be between 8 and 1024 characters")
	ErrEmailTaken         = repo.ErrEmailTaken
)

// UserStore is the subset of the data store auth needs for accounts.
type UserStore interface {
	CreateUser(ctx context.Context, email, passwordHash string) (*model.User, error)
	UserByEmail(ctx context.Context, email string) (*model.User, error)
	UserCount(ctx context.Context) (int, error)
	UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error
}

// SessionStore is the subset of the data store auth needs for sessions.
type SessionStore interface {
	CreateSession(ctx context.Context, tokenHash, csrfToken string, userID int64, expiresAt time.Time) error
	SessionByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*model.User, string, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}

// PasswordHasher hashes and verifies passwords (Argon2id + pepper via
// internal/password). It is an interface so auth logic can be tested
// with a stub.
type PasswordHasher interface {
	// Hash returns the PHC string for a plaintext password.
	Hash(password string) (string, error)
	// Verify reports whether password matches the stored hash and
	// whether the stored hash should be upgraded (legacy bcrypt or
	// non-default parameters).
	Verify(hash, password string) (ok, needsRehash bool, err error)
}

// Session is a live login: the owning user and the per-session CSRF
// token used to protect state-changing requests.
type Session struct {
	User      *model.User
	CSRFToken string
}

// Service hashes passwords and manages sessions.
type Service struct {
	users    UserStore
	sessions SessionStore
	hasher   PasswordHasher
	now      func() time.Time
	maxAge   time.Duration
}

// New builds the auth service. The same *repo.Store implements both
// UserStore and SessionStore; the hasher is the Argon2id + pepper
// service from internal/password.
func New(users UserStore, sessions SessionStore, hasher PasswordHasher) *Service {
	return NewWithMaxAge(users, sessions, hasher, SessionMaxAge)
}

// NewWithMaxAge builds the auth service with a custom session lifetime
// (the SESSION_MAX_AGE env var). Values <= 0 fall back to the default.
func NewWithMaxAge(users UserStore, sessions SessionStore, hasher PasswordHasher, maxAge time.Duration) *Service {
	if maxAge <= 0 {
		maxAge = SessionMaxAge
	}
	return &Service{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		now:      time.Now,
		maxAge:   maxAge,
	}
}

// NormalizeEmail trims and lowercases an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validEmail is a deliberately simple format check: non-empty, no
// whitespace, contains exactly one @ with non-empty local and domain,
// and fits in a standard mailbox length. There is no email verification
// (the project has no SMTP), so this is the only gate besides password
// strength.
func validEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@")
}

// Register validates the email, hashes the password (Argon2id + pepper)
// and creates the account. The email is normalized first. Registration
// is closed — this is used by the CLI (cmd/user), not the web UI.
// Returns ErrEmailTaken when the address is already registered.
func (s *Service) Register(ctx context.Context, email, password string) (*model.User, error) {
	email = NormalizeEmail(email)
	if !validEmail(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return nil, ErrWeakPassword
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}
	return s.users.CreateUser(ctx, email, hash)
}

// Authenticate verifies an email + password pair, returning the user or
// ErrInvalidCredentials. The same error hides unknown emails and wrong
// passwords so accounts cannot be enumerated through the login form.
// When the stored hash is legacy (bcrypt) or uses non-default
// parameters, it is transparently upgraded to the current scheme.
func (s *Service) Authenticate(ctx context.Context, email, password string) (*model.User, error) {
	u, err := s.users.UserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, needsRehash, err := s.hasher.Verify(u.PasswordHash, password)
	if err != nil {
		// Unreadable stored hash — do not leak details to the client.
		log.Printf("auth: verifying password for %s: %v", u.Email, err)
		return nil, ErrInvalidCredentials
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	if needsRehash {
		// Upgrade legacy/non-default hashes in place; a failure here
		// must not block the login.
		if newHash, hErr := s.hasher.Hash(password); hErr == nil {
			if uErr := s.users.UpdateUserPassword(ctx, u.ID, newHash); uErr != nil {
				log.Printf("auth: upgrading password hash for %s: %v", u.Email, uErr)
			}
		}
	}
	return u, nil
}

// CreateSession issues a new session and returns the raw token that the
// browser stores in a cookie. Only the SHA-256 hash is persisted,
// together with a fresh per-session CSRF token. Expired sessions are
// swept opportunistically.
func (s *Service) CreateSession(ctx context.Context, userID int64) (string, error) {
	now := s.now()
	// Opportunistic cleanup; never worth failing login over.
	_ = s.sessions.DeleteExpiredSessions(ctx, now)

	raw, err := newToken()
	if err != nil {
		return "", err
	}
	csrf, err := newToken()
	if err != nil {
		return "", err
	}
	if err := s.sessions.CreateSession(ctx, hashToken(raw), csrf, userID, now.Add(s.maxAge)); err != nil {
		return "", err
	}
	return raw, nil
}

// SessionByToken resolves a raw token to its session, or
// ErrInvalidSession.
func (s *Service) SessionByToken(ctx context.Context, rawToken string) (*Session, error) {
	if rawToken == "" {
		return nil, ErrInvalidSession
	}
	u, csrf, err := s.sessions.SessionByTokenHash(ctx, hashToken(rawToken), s.now())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}
	return &Session{User: u, CSRFToken: csrf}, nil
}

// DestroySession invalidates the session for a raw token (logout).
func (s *Service) DestroySession(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return s.sessions.DeleteSession(ctx, hashToken(rawToken))
}

// ValidCSRF reports whether the submitted token matches the session's,
// using a constant-time comparison.
func ValidCSRF(sess *Session, submitted string) bool {
	return sess != nil && submitted != "" &&
		subtle.ConstantTimeCompare([]byte(sess.CSRFToken), []byte(submitted)) == 1
}

// NewRandomToken returns a fresh 32-byte random token, hex-encoded. Used
// for the anonymous CSRF cookie protecting the login form.
func NewRandomToken() (string, error) {
	return newToken()
}

// newToken returns a random 32-byte token as hex (64 chars).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest of a raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
