package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempEnv(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const sampleEnv = `
# comment line
GLCFG_EMPTY=
GLCFG_PLAIN=hello
GLCFG_QUOTED="with spaces and = equals"
GLCFG_SINGLE='single quoted'
GLCFG_EXPORTED=export me
export GLCFG_WITH_EXPORT=from env file
GLCFG_UNSET_LINE
`

func TestLoadDotEnv(t *testing.T) {
	cleanup := setTestEnv("GLCFG_PLAIN", "")
	defer cleanup()

	p := writeTempEnv(t, sampleEnv)
	if err := LoadDotEnv(p); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	cases := map[string]string{
		"GLCFG_PLAIN":       "hello",
		"GLCFG_QUOTED":      "with spaces and = equals",
		"GLCFG_SINGLE":      "single quoted",
		"GLCFG_EXPORTED":    "export me",
		"GLCFG_WITH_EXPORT": "from env file",
		"GLCFG_EMPTY":       "", // empty values are skipped
		"GLCFG_UNSET_LINE":  "", // line without '=' is skipped
	}
	for key, want := range cases {
		got := os.Getenv(key)
		if got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
}

func TestExistingEnvWins(t *testing.T) {
	cleanup := setTestEnv("GLCFG_PLAIN", "from shell")
	defer cleanup()

	p := writeTempEnv(t, sampleEnv)
	if err := LoadDotEnv(p); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("GLCFG_PLAIN"); got != "from shell" {
		t.Errorf("existing env var should win, got %q", got)
	}
}

func TestMissingFileIgnored(t *testing.T) {
	if err := LoadDotEnv("/nonexistent/path/.env", "/also/missing"); err != nil {
		t.Fatalf("missing files should be ignored, got %v", err)
	}
}

func TestMultiplePathsFirstWins(t *testing.T) {
	cleanup := setTestEnv("GLCFG_PLAIN", "")
	defer cleanup()

	first := writeTempEnv(t, "GLCFG_PLAIN=first\n")
	second := writeTempEnv(t, "GLCFG_PLAIN=second\n")
	if err := LoadDotEnv(first, second); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("GLCFG_PLAIN"); got != "first" {
		t.Errorf("first existing file should win, got %q", got)
	}
}

// setTestEnv sets an env var and returns a cleanup that restores it.
func setTestEnv(key, val string) func() {
	prev, had := os.LookupEnv(key)
	_ = os.Setenv(key, val)
	return func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}
