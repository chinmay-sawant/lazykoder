package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFresh(t *testing.T) {
	cwd := t.TempDir()
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()

	dir := filepath.Join(cwd, ".lazykoder")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat .lazykoder: %v", err)
	}
	if !info.IsDir() {
		t.Errorf(".lazykoder is not a dir")
	}
	if info, err := os.Stat(filepath.Join(dir, "lazykoder.db")); err != nil {
		t.Errorf("lazykoder.db: %v", err)
	} else if !info.Mode().IsRegular() {
		t.Errorf("lazykoder.db is not a regular file")
	}
	if n := countIgnoreLines(t, cwd); n != 1 {
		t.Errorf(".gitignore has %d .lazykoder/ lines, want 1", n)
	}
}

func TestInitTwice(t *testing.T) {
	cwd := t.TempDir()
	env1, err := Init(cwd)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := env1.DB.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	env2, err := Init(cwd)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if err := env2.DB.Close(); err != nil {
		t.Fatalf("close second store: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(cwd, ".lazykoder"))
	if err != nil {
		t.Fatalf("readdir .lazykoder: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lazykoder.db") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("files starting with lazykoder.db = %d, want 1", count)
	}
	if n := countIgnoreLines(t, cwd); n != 1 {
		t.Errorf(".gitignore has %d .lazykoder/ lines, want 1", n)
	}
}

func TestInitPreservesGitignore(t *testing.T) {
	cwd := t.TempDir()
	existing := "# hello\nbin/\n"
	if err := os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()

	content, err := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "# hello") || !strings.Contains(got, "bin/") {
		t.Errorf("existing content not preserved: %q", got)
	}
	if n := countIgnoreLines(t, cwd); n != 1 {
		t.Errorf(".gitignore has %d .lazykoder/ lines, want 1", n)
	}
}

func TestInitGitignoreAlreadyListed(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte("node_modules/\n.lazykoder/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()
	if n := countIgnoreLines(t, cwd); n != 1 {
		t.Errorf(".gitignore has %d .lazykoder/ lines, want 1", n)
	}
}

func TestInitTwiceReusableStore(t *testing.T) {
	cwd := t.TempDir()
	env1, err := Init(cwd)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := env1.DB.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	env2, err := Init(cwd)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	defer env2.DB.Close()

	sessions, err := env2.DB.ListSessionsByDir(context.Background(), cwd)
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(sessions))
	}
}

func countIgnoreLines(t *testing.T, cwd string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimRight(line, "\r") == ".lazykoder/" {
			n++
		}
	}
	return n
}
