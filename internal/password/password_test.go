package password

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const testPepper = "test-pepper-0123456789abcdef"

// fastParams keeps Argon2id cheap for tests while exercising the full
// code path (memory still >= 8*parallelism).
var fastParams = Params{Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func newTestService(t *testing.T, opts ...Option) *Service {
	t.Helper()
	s, err := New(testPepper, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	if p.Memory != 64*1024 || p.Iterations != 1 || p.Parallelism != 4 ||
		p.SaltLength != 16 || p.KeyLength != 32 {
		t.Errorf("DefaultParams = %+v, want OWASP values (64MiB, t=1, p=4, salt=16, key=32)", p)
	}
}

func TestHashFormat(t *testing.T) {
	s := newTestService(t)
	h, err := s.Hash("hunter22")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	// $argon2id$v=19$m=65536,t=1,p=4$<salt>$<key>
	parts := strings.Split(h, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		t.Fatalf("hash %q does not match the PHC layout", h)
	}
	if parts[2] != "v=19" {
		t.Errorf("version segment = %q, want v=19", parts[2])
	}
	if parts[3] != "m=65536,t=1,p=4" {
		t.Errorf("params segment = %q, want m=65536,t=1,p=4", parts[3])
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		t.Errorf("salt segment is not 16 raw-base64 bytes (len=%d, err=%v)", len(salt), err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != 32 {
		t.Errorf("key segment is not 32 raw-base64 bytes (len=%d, err=%v)", len(key), err)
	}
	if strings.Contains(h, testPepper) {
		t.Error("pepper leaked into the hash string")
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	s := newTestService(t, WithParams(fastParams))
	h, err := s.Hash("hunter22")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	ok, rehash, err := s.Verify(h, "hunter22")
	if err != nil || !ok || rehash {
		t.Errorf("Verify(h, correct) = ok:%v rehash:%v err:%v, want ok=true rehash=false", ok, rehash, err)
	}
	ok, _, err = s.Verify(h, "wrong-password")
	if err != nil || ok {
		t.Errorf("Verify(h, wrong) = ok:%v err:%v, want ok=false", ok, err)
	}
}

func TestVerifySaltUniqueness(t *testing.T) {
	s := newTestService(t, WithParams(fastParams))
	h1, _ := s.Hash("hunter22")
	h2, _ := s.Hash("hunter22")
	if h1 == h2 {
		t.Error("hashing the same password twice produced the same hash (salt not random)")
	}
}

func TestVerifyTamperedHash(t *testing.T) {
	s := newTestService(t, WithParams(fastParams))
	h, _ := s.Hash("hunter22")
	parts := strings.Split(h, "$")

	tampered := append([]string(nil), parts...)
	tampered[3] = "m=512,t=1,p=1" // different (but valid) params
	ok, _, err := s.Verify(strings.Join(tampered, "$"), "hunter22")
	if err != nil || ok {
		t.Errorf("tampered params: ok=%v err=%v, want ok=false", ok, err)
	}

	tampered = append([]string(nil), parts...)
	tampered[4] = base64.RawStdEncoding.EncodeToString([]byte("a-different-salt!!")) // 17 bytes
	ok, _, err = s.Verify(strings.Join(tampered, "$"), "hunter22")
	if err != nil || ok {
		t.Errorf("tampered salt: ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestVerifyParamDrift(t *testing.T) {
	// A hash created with older parameters must still verify (the stored
	// params drive derivation) and report needsRehash against the current
	// defaults.
	old := newTestService(t, WithParams(fastParams))
	h, _ := old.Hash("hunter22")

	cur := newTestService(t) // OWASP defaults
	ok, rehash, err := cur.Verify(h, "hunter22")
	if err != nil || !ok {
		t.Fatalf("verify with drifted params: ok=%v err=%v, want ok=true", ok, err)
	}
	if !rehash {
		t.Error("needsRehash=false for a hash created with non-default params")
	}
}

func TestVerifyLegacyBcrypt(t *testing.T) {
	legacy, err := bcrypt.GenerateFromPassword([]byte("hunter22"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	s := newTestService(t)
	ok, rehash, err := s.Verify(string(legacy), "hunter22")
	if err != nil || !ok || !rehash {
		t.Errorf("legacy bcrypt: ok=%v rehash=%v err=%v, want ok=true rehash=true", ok, rehash, err)
	}
	ok, _, err = s.Verify(string(legacy), "wrong-password")
	if err != nil || ok {
		t.Errorf("legacy bcrypt wrong password: ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestVerifyUnsupportedFormat(t *testing.T) {
	s := newTestService(t)
	for _, h := range []string{
		"", "plaintext", "$1$abc", "$pbkdf2$...",
		"$argon2id$v=19$m=65536,t=1,p=4$!!!!$AAAA", // invalid base64 salt
		"$argon2id$v=19$m=65536,t=1,p=4$$AAAA",     // empty salt
		"$argon2id$v=19$m=65536,t=1,p=4$AAAA$",     // empty key
	} {
		if _, _, err := s.Verify(h, "hunter22"); err == nil {
			t.Errorf("Verify(%q) = nil error, want unsupported-format error", h)
		}
	}
}

func TestVerifyMalformedArgon2id(t *testing.T) {
	s := newTestService(t, WithParams(fastParams))
	h, _ := s.Hash("hunter22")
	parts := strings.Split(h, "$")

	cases := [][]string{
		append([]string(nil), parts[:len(parts)-1]...),                    // missing key segment
		{parts[0], "argon2id", "v=18", parts[3], parts[4], parts[5]},      // old version
		{parts[0], "argon2id", "v=19", "m=1,t=1,p=1", parts[4], parts[5]}, // memory < 8*parallelism
		{parts[0], "argon2id", "v=19", "m=notanumber,t=1,p=1", parts[4], parts[5]},
		{parts[0], "argon2id", "v=19", parts[3], "!!!not-base64!!!", parts[5]},
	}
	for i, c := range cases {
		if _, _, err := s.Verify(strings.Join(c, "$"), "hunter22"); err == nil {
			t.Errorf("malformed case %d: expected an error", i)
		}
	}
}

func TestPepperSensitivity(t *testing.T) {
	a, _ := New("pepper-a-0123456789abcdef", WithParams(fastParams))
	b, _ := New("pepper-b-0123456789abcdef", WithParams(fastParams))

	h, _ := a.Hash("hunter22")
	ok, _, err := b.Verify(h, "hunter22")
	if err != nil || ok {
		t.Errorf("verify with different pepper: ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestNewValidatesPepper(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("New(\"\") = nil error, want error")
	}
	if _, err := New("short"); err == nil {
		t.Error("New(short pepper) = nil error, want error")
	}
	if _, err := New("0123456789abcdef"); err != nil {
		t.Errorf("New(16-byte pepper) = %v, want success", err)
	}
}

func TestNewValidatesParams(t *testing.T) {
	if _, err := New(testPepper, WithParams(Params{Memory: 4, Iterations: 1, Parallelism: 4, SaltLength: 16, KeyLength: 32})); err == nil {
		t.Error("New with memory < 8*parallelism = nil error, want error")
	}
	if _, err := New(testPepper, WithParams(Params{Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 4, KeyLength: 32})); err == nil {
		t.Error("New with 4-byte salt = nil error, want error")
	}
}
