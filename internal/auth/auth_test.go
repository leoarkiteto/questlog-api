package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/leoarkiteto/questlog-api/internal/model"
	"github.com/leoarkiteto/questlog-api/internal/repo"
)

// ---- fakes ----

// stubHasher is a deterministic PasswordHasher so auth tests exercise
// the flow (register hashes, login verifies, upgrade-on-rehash) without
// paying for Argon2id.
type stubHasher struct {
	rehashOnVerify bool
}

func (h stubHasher) Hash(password string) (string, error) {
	return "hash:" + password, nil
}

func (h stubHasher) Verify(hash, password string) (bool, bool, error) {
	if hash != "hash:"+password {
		return false, false, nil
	}
	return true, h.rehashOnVerify, nil
}

type fakeUserStore struct {
	users  []*model.User
	nextID int64
	// records of UpdateUserPassword calls
	updates     int
	updatedHash string
	updatedUser int64
}

func (f *fakeUserStore) CreateUser(_ context.Context, email, passwordHash string) (*model.User, error) {
	for _, u := range f.users {
		if strings.EqualFold(u.Email, email) {
			return nil, repo.ErrEmailTaken
		}
	}
	f.nextID++
	u := &model.User{ID: f.nextID, Email: email, PasswordHash: passwordHash, CreatedAt: time.Now()}
	f.users = append(f.users, u)
	return u, nil
}

func (f *fakeUserStore) UserByEmail(_ context.Context, email string) (*model.User, error) {
	for _, u := range f.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (f *fakeUserStore) UserCount(_ context.Context) (int, error) {
	return len(f.users), nil
}

func (f *fakeUserStore) UpdateUserPassword(_ context.Context, userID int64, passwordHash string) error {
	f.updates++
	f.updatedUser = userID
	f.updatedHash = passwordHash
	return nil
}

type storedSession struct {
	userID  int64
	csrf    string
	expires time.Time
}

type fakeSessionStore struct {
	sessions map[string]storedSession
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]storedSession{}}
}

func (f *fakeSessionStore) CreateSession(_ context.Context, tokenHash, csrf string, userID int64, expires time.Time) error {
	f.sessions[tokenHash] = storedSession{userID: userID, csrf: csrf, expires: expires}
	return nil
}

func (f *fakeSessionStore) SessionByTokenHash(_ context.Context, tokenHash string, now time.Time) (*model.User, string, error) {
	s, ok := f.sessions[tokenHash]
	if !ok || !s.expires.After(now) {
		return nil, "", pgx.ErrNoRows
	}
	return &model.User{ID: s.userID}, s.csrf, nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, tokenHash string) error {
	delete(f.sessions, tokenHash)
	return nil
}

func (f *fakeSessionStore) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	for h, s := range f.sessions {
		if !s.expires.After(now) {
			delete(f.sessions, h)
		}
	}
	return nil
}

// ---- service helpers ----

func newTestService() (*Service, *fakeUserStore, *fakeSessionStore) {
	return newTestServiceWithHasher(stubHasher{})
}

func newTestServiceWithHasher(h PasswordHasher) (*Service, *fakeUserStore, *fakeSessionStore) {
	users := &fakeUserStore{}
	sessions := newFakeSessionStore()
	return New(users, sessions, h), users, sessions
}

// ---- email normalization ----

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"  Me@Example.COM ": "me@example.com",
		"me@example.com":    "me@example.com",
		"":                  "",
	}
	for in, want := range cases {
		if got := NormalizeEmail(in); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	valid := []string{"me@example.com", "a.b+c@sub.example.co", "x@y"}
	for _, e := range valid {
		if !validEmail(e) {
			t.Errorf("validEmail(%q) = false, want true", e)
		}
	}
	invalid := []string{"", "not-an-email", "a@", "@b", "a b@c.com", "a@b@c.com",
		"a@b c.com", strings.Repeat("a", 250) + "@x.com"}
	for _, e := range invalid {
		if validEmail(e) {
			t.Errorf("validEmail(%q) = true, want false", e)
		}
	}
}

// ---- registration ----

func TestRegisterNormalizesAndHashes(t *testing.T) {
	svc, users, _ := newTestService()
	u, err := svc.Register(context.Background(), "  Me@Example.COM ", "hunter22")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Email != "me@example.com" {
		t.Errorf("email = %q, want normalized %q", u.Email, "me@example.com")
	}
	// The password goes through the hasher (Argon2id + pepper in
	// production); the raw value is never stored.
	if u.PasswordHash != "hash:hunter22" {
		t.Errorf("stored hash = %q, want the hasher output (not the raw password)", u.PasswordHash)
	}
	if len(users.users) != 1 {
		t.Errorf("store has %d users, want 1", len(users.users))
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "me@example.com", "hunter22"); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if _, err := svc.Register(ctx, "ME@example.com", "other123"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate Register err = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterInvalidInputs(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "not-an-email", "hunter22"); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad email err = %v, want ErrInvalidEmail", err)
	}
	if _, err := svc.Register(ctx, "me@example.com", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password err = %v, want ErrWeakPassword", err)
	}
	if _, err := svc.Register(ctx, "me@example.com", strings.Repeat("x", 1025)); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("oversized password err = %v, want ErrWeakPassword", err)
	}
}

// ---- set password ----

func TestSetPassword(t *testing.T) {
	svc, users, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "me@example.com", "hunter22"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.SetPassword(ctx, "  ME@example.com ", "new-pass-1"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if users.updates != 1 || users.updatedUser != 1 || users.updatedHash != "hash:new-pass-1" {
		t.Errorf("update = (%d, %d, %q), want (1, 1, %q)",
			users.updates, users.updatedUser, users.updatedHash, "hash:new-pass-1")
	}
}

func TestSetPasswordInvalid(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()

	if err := svc.SetPassword(ctx, "not-an-email", "hunter22"); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad email err = %v, want ErrInvalidEmail", err)
	}
	if err := svc.SetPassword(ctx, "me@example.com", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password err = %v, want ErrWeakPassword", err)
	}
	if err := svc.SetPassword(ctx, "nobody@example.com", "hunter22"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("unknown email err = %v, want pgx.ErrNoRows", err)
	}
}

// ---- authentication ----

func TestAuthenticate(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "me@example.com", "hunter22"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, err := svc.Authenticate(ctx, "  ME@example.com ", "hunter22")
	if err != nil {
		t.Fatalf("Authenticate with correct password: %v", err)
	}
	if u.Email != "me@example.com" {
		t.Errorf("Authenticate returned email %q", u.Email)
	}

	if _, err := svc.Authenticate(ctx, "me@example.com", "wrong-pass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Authenticate(ctx, "nobody@example.com", "hunter22"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown email err = %v, want ErrInvalidCredentials", err)
	}
}

// TestAuthenticateUpgradesLegacyHash checks the rehash-on-login path:
// when the hasher reports needsRehash, a successful login rewrites the
// stored hash; a failed login never does.
func TestAuthenticateUpgradesLegacyHash(t *testing.T) {
	svc, users, _ := newTestServiceWithHasher(stubHasher{rehashOnVerify: true})
	ctx := context.Background()
	if _, err := svc.Register(ctx, "me@example.com", "hunter22"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := svc.Authenticate(ctx, "me@example.com", "hunter22"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if users.updates != 1 || users.updatedUser != 1 || users.updatedHash != "hash:hunter22" {
		t.Errorf("expected one password upgrade for user 1 to %q, got updates=%d user=%d hash=%q",
			"hash:hunter22", users.updates, users.updatedUser, users.updatedHash)
	}

	// A wrong password must not trigger an upgrade.
	users.updates = 0
	if _, err := svc.Authenticate(ctx, "me@example.com", "wrong-pass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if users.updates != 0 {
		t.Errorf("wrong password triggered %d upgrade(s), want 0", users.updates)
	}
}

// ---- sessions ----

func TestSessionRoundTrip(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	raw, err := svc.CreateSession(ctx, 42)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(raw) != 64 {
		t.Errorf("raw token length = %d, want 64 hex chars", len(raw))
	}

	sess, err := svc.SessionByToken(ctx, raw)
	if err != nil {
		t.Fatalf("SessionByToken: %v", err)
	}
	if sess.User.ID != 42 {
		t.Errorf("session user id = %d, want 42", sess.User.ID)
	}
	if len(sess.CSRFToken) != 64 {
		t.Errorf("csrf token length = %d, want 64 hex chars", len(sess.CSRFToken))
	}
	if !ValidCSRF(sess, sess.CSRFToken) {
		t.Error("ValidCSRF rejected the session's own token")
	}
	if ValidCSRF(sess, "attacker-token") || ValidCSRF(sess, "") {
		t.Error("ValidCSRF accepted a wrong or empty token")
	}
}

func TestSessionDestroyedAndInvalid(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	raw, err := svc.CreateSession(ctx, 7)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := svc.DestroySession(ctx, raw); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}
	if _, err := svc.SessionByToken(ctx, raw); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("after logout err = %v, want ErrInvalidSession", err)
	}

	if _, err := svc.SessionByToken(ctx, ""); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("empty token err = %v, want ErrInvalidSession", err)
	}
	if _, err := svc.SessionByToken(ctx, strings.Repeat("0", 64)); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("unknown token err = %v, want ErrInvalidSession", err)
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	svc, _, sessions := newTestService()
	ctx := context.Background()
	raw, err := svc.CreateSession(ctx, 7)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Force the stored session into the past.
	for h := range sessions.sessions {
		s := sessions.sessions[h]
		s.expires = time.Now().Add(-time.Minute)
		sessions.sessions[h] = s
	}
	if _, err := svc.SessionByToken(ctx, raw); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("expired session err = %v, want ErrInvalidSession", err)
	}
}
