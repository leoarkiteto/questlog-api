// Package password hashes and verifies user passwords with Argon2id and
// a server-side pepper.
//
// The pepper (PASSWORD_PEPPER) is an application secret mixed into every
// hash via HMAC-SHA256 BEFORE Argon2id, so a database leak alone is not
// enough to brute-force passwords. The pepper is never stored in the
// hash string — it lives only in the environment and must be shared by
// every process that hashes or verifies passwords. Rotating it
// invalidates every stored password.
//
// Stored hashes use the PHC string format:
//
//	$argon2id$v=19$m=65536,t=1,p=4$<salt-b64>$<hash-b64>
//
// with the OWASP-recommended parameters (64 MiB memory, 1 iteration,
// parallelism 4, 16-byte salt, 32-byte key). Verification re-derives the
// key from the parameters embedded in the stored hash and compares in
// constant time, so hashes keep working if the defaults change — Verify
// reports needsRehash so callers can upgrade old hashes in place. Legacy
// bcrypt hashes are still accepted (and flagged for upgrade) so accounts
// created before Argon2id keep working.
package password

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Version is the Argon2 version this package emits (PHC v=19).
const Version = argon2.Version

// minPepperLen is the shortest acceptable pepper; anything shorter is
// easily brute-forced if the database leaks.
const minPepperLen = 16

// Params holds the Argon2id parameters. Memory is expressed in KiB.
type Params struct {
	Memory      uint32 // KiB (64 MiB = 65536)
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams returns the OWASP-recommended Argon2id parameters:
// 64 MiB memory, 1 iteration, parallelism 4, 16-byte salt, 32-byte key.
func DefaultParams() Params {
	return Params{
		Memory:      64 * 1024, // 64 MiB
		Iterations:  1,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// valid sanity-checks the parameters before hashing or parsing.
func (p Params) valid() error {
	switch {
	case p.Iterations == 0:
		return errors.New("password: iterations must be > 0")
	case p.Parallelism == 0:
		return errors.New("password: parallelism must be > 0")
	case p.Memory < 8*uint32(p.Parallelism):
		return fmt.Errorf("password: memory must be at least 8*parallelism (%d KiB)", 8*p.Parallelism)
	case p.SaltLength < 8:
		return errors.New("password: salt must be at least 8 bytes")
	case p.KeyLength < 16:
		return errors.New("password: key length must be at least 16 bytes")
	}
	return nil
}

// Service hashes and verifies passwords. Build one per process with the
// application's pepper; every process must use the same pepper.
type Service struct {
	pepper []byte
	params Params
}

// Option customizes a Service.
type Option func(*Service)

// WithParams overrides the Argon2id parameters (defaults to the OWASP
// recommendations).
func WithParams(p Params) Option {
	return func(s *Service) { s.params = p }
}

// New builds a password Service with the given pepper, which is
// required and must be at least minPepperLen bytes.
func New(pepper string, opts ...Option) (*Service, error) {
	if len(pepper) < minPepperLen {
		return nil, fmt.Errorf("password: pepper must be at least %d bytes", minPepperLen)
	}
	s := &Service{pepper: []byte(pepper), params: DefaultParams()}
	for _, o := range opts {
		o(s)
	}
	if err := s.params.valid(); err != nil {
		return nil, err
	}
	return s, nil
}

// Hash returns the PHC string for password: the password is first keyed
// with the pepper via HMAC-SHA256, then hashed with Argon2id using a
// fresh random salt.
func (s *Service) Hash(password string) (string, error) {
	salt := make([]byte, s.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey(
		s.peppered(password),
		salt,
		s.params.Iterations,
		s.params.Memory,
		s.params.Parallelism,
		s.params.KeyLength,
	)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		Version,
		s.params.Memory, s.params.Iterations, s.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches a stored hash, and whether
// the stored hash should be upgraded (legacy bcrypt, or parameters that
// differ from the current defaults). The key comparison is constant
// time. Errors only for hashes in an unknown format — a wrong password
// is a mismatch, not an error.
func (s *Service) Verify(stored, password string) (ok, needsRehash bool, err error) {
	switch {
	case strings.HasPrefix(stored, "$argon2id$"):
		return s.verifyArgon2id(stored, password)
	case isBcrypt(stored):
		// Legacy pre-Argon2id hash: accept it so existing accounts keep
		// working, but flag it so the caller upgrades the record.
		if bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) != nil {
			return false, false, nil
		}
		return true, true, nil
	default:
		return false, false, errors.New("password: unsupported hash format")
	}
}

func (s *Service) verifyArgon2id(stored, password string) (bool, bool, error) {
	p, salt, want, err := parsePHC(stored)
	if err != nil {
		return false, false, err
	}
	// Re-derive the key using the parameters embedded in the stored hash
	// (not the current defaults) so old hashes keep verifying.
	got := argon2.IDKey(s.peppered(password), salt, p.Iterations, p.Memory, p.Parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	// Only the parameters that are actually encoded in the PHC string
	// (m, t, p) drive the "should we rehash" signal; salt/key lengths
	// are implied by the decoded values.
	needsRehash := p.Memory != s.params.Memory ||
		p.Iterations != s.params.Iterations ||
		p.Parallelism != s.params.Parallelism
	return true, needsRehash, nil
}

// isBcrypt reports whether hash is a legacy bcrypt hash ($2a/$2b/$2y).
func isBcrypt(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") ||
		strings.HasPrefix(hash, "$2b$") ||
		strings.HasPrefix(hash, "$2y$")
}

// parsePHC decodes "$argon2id$v=19$m=...,t=...,p=...$<salt>$<hash>".
func parsePHC(s string) (Params, []byte, []byte, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("password: malformed argon2id hash")
	}
	if parts[2] != fmt.Sprintf("v=%d", Version) {
		return Params{}, nil, nil, fmt.Errorf("password: unsupported argon2 version %q", parts[2])
	}
	p := Params{}
	for _, kv := range strings.Split(parts[3], ",") {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			return Params{}, nil, nil, errors.New("password: malformed parameters")
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return Params{}, nil, nil, errors.New("password: malformed parameters")
		}
		switch name {
		case "m":
			p.Memory = uint32(n)
		case "t":
			p.Iterations = uint32(n)
		case "p":
			p.Parallelism = uint8(n)
		default:
			return Params{}, nil, nil, fmt.Errorf("password: unknown parameter %q", name)
		}
	}
	// Only m, t and p are encoded in the PHC string; salt/key lengths are
	// implied by the decoded values, so validate just those three here.
	if p.Iterations == 0 || p.Parallelism == 0 || p.Memory < 8*uint32(p.Parallelism) {
		return Params{}, nil, nil, errors.New("password: malformed parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Params{}, nil, nil, errors.New("password: malformed salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, errors.New("password: malformed hash")
	}
	return p, salt, key, nil
}

// peppered returns HMAC-SHA256(pepper, password): the pepper is keyed
// into the password before Argon2id and never persisted.
func (s *Service) peppered(password string) []byte {
	mac := hmac.New(sha256.New, s.pepper)
	_, _ = mac.Write([]byte(password))
	return mac.Sum(nil)
}
