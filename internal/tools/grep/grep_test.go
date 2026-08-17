package grep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFindsMatches(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package a\nfunc Hello() {}\n")
	write(t, root, "b.txt", "nope\n")
	write(t, root, "sub/c.go", "package sub\n// Hello there\n")

	res, err := Run(context.Background(), root, Options{
		Pattern:    "Hello",
		Glob:       "*.go",
		MaxMatches: 20,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "a.go") {
		t.Fatalf("missing a.go in %q", res.Output)
	}
	if !strings.Contains(res.Output, "c.go") && !strings.Contains(res.Output, "sub/c.go") {
		t.Fatalf("missing sub match in %q", res.Output)
	}
	if strings.Contains(res.Output, "b.txt") {
		t.Fatalf("glob should exclude b.txt: %q", res.Output)
	}
	if n, _ := res.Metadata["matches"].(int); n < 1 {
		t.Fatalf("matches metadata = %v", res.Metadata["matches"])
	}
}

func TestRunNoMatches(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package a\n")
	res, err := Run(context.Background(), root, Options{Pattern: "ZZZNOPE"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "no matches" {
		t.Fatalf("output = %q", res.Output)
	}
}

func TestRunRejectsEscape(t *testing.T) {
	root := t.TempDir()
	_, err := Run(context.Background(), root, Options{
		Pattern: "x",
		Path:    filepath.Join("..", "outside"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("want escape error, got %v", err)
	}
}

func TestRunRequiresPattern(t *testing.T) {
	_, err := Run(context.Background(), t.TempDir(), Options{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunGoFallback(t *testing.T) {
	root := t.TempDir()
	write(t, root, "x.go", "func FindMe() {}\n")
	r := &Runner{
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
	}
	res, err := Run(context.Background(), root, Options{Pattern: "FindMe"}, r)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Metadata["engine"] != "go" {
		t.Fatalf("engine = %v, want go", res.Metadata["engine"])
	}
	if !strings.Contains(res.Output, "FindMe") {
		t.Fatalf("output = %q", res.Output)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunAbsoluteInsideAllowed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "inside.txt", "needle\n")
	res, err := Run(context.Background(), root, Options{Pattern: "needle", Path: filepath.Join(root, "inside.txt")}, nil)
	if err != nil || !strings.Contains(res.Output, "needle") {
		t.Fatalf("absolute inside should work: output=%q err=%v", res.Output, err)
	}
}

func TestRunSymlinkEscapeNested(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "target.txt", "needle\n")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Run(context.Background(), root, Options{Pattern: "needle", Path: "link"}, nil)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("want symlink escape rejection, got %v", err)
	}
}
