package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	return path
}

func TestLoadSetsMissingKeys(t *testing.T) {
	path := writeEnv(t, "OPENCODE_API_KEY=sk-foo\nOPENCODE_ZEN_API_KEY=sk-zen\n")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	unset := func(key string) {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetenv: %v", err)
		}
	}
	unset("OPENCODE_API_KEY")
	unset("OPENCODE_ZEN_API_KEY")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("OPENCODE_API_KEY"); got != "sk-foo" {
		t.Errorf("OPENCODE_API_KEY = %q, want sk-foo", got)
	}
	if got := os.Getenv("OPENCODE_ZEN_API_KEY"); got != "sk-zen" {
		t.Errorf("OPENCODE_ZEN_API_KEY = %q, want sk-zen", got)
	}
}

func TestLoadDoesNotOverrideProcessEnv(t *testing.T) {
	path := writeEnv(t, "OPENCODE_API_KEY=from-file\n")
	t.Setenv("OPENCODE_API_KEY", "from-shell")
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("OPENCODE_API_KEY"); got != "from-shell" {
		t.Errorf("OPENCODE_API_KEY = %q, want from-shell", got)
	}
}

func TestLoadSyntaxVariants(t *testing.T) {
	path := writeEnv(t, strings.Join([]string{
		"",
		"# comment",
		"export QUOTED=\"double value\"",
		"SINGLE='single value'",
		"UNQUOTED=plain",
		"NOSPACE= x ",
		"=bad",
		"BADLINE",
	}, "\n"))
	for _, key := range []string{"QUOTED", "SINGLE", "UNQUOTED", "NOSPACE", "=bad", "BADLINE"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetenv: %v", err)
		}
	}
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("QUOTED"); got != "double value" {
		t.Errorf("QUOTED = %q, want double value", got)
	}
	if got := os.Getenv("SINGLE"); got != "single value" {
		t.Errorf("SINGLE = %q, want single value", got)
	}
	if got := os.Getenv("UNQUOTED"); got != "plain" {
		t.Errorf("UNQUOTED = %q, want plain", got)
	}
	if got := os.Getenv("NOSPACE"); got != "x" {
		t.Errorf("NOSPACE = %q, want x", got)
	}
	if _, ok := os.LookupEnv("=bad"); ok {
		t.Errorf("=bad should not be set")
	}
	if _, ok := os.LookupEnv("BADLINE"); ok {
		t.Errorf("BADLINE should not be set")
	}
}

func TestLoadMissingFileIsNoop(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatalf("Load missing: %v", err)
	}
}
