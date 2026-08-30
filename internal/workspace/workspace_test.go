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

func TestInitCreatesEmptyCatalogFiles(t *testing.T) {
	cwd := t.TempDir()
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()

	for _, name := range []string{"settings.json", "providers.json", "tools.json", "roles.json"} {
		path := filepath.Join(env.Dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, got)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if name == "providers.json" {
			if !strings.Contains(string(body), `"id": "opencode"`) || !strings.Contains(string(body), `"id": "opencode-team"`) {
				t.Errorf("providers.json missing default entries: %q", body)
			}
		} else if name != "settings.json" && string(body) != "[]\n" {
			t.Errorf("%s = %q, want empty array", name, body)
		}
	}
}

func TestInitCreatesPromptFiles(t *testing.T) {
	cwd := t.TempDir()
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()

	promptDir := filepath.Join(env.Dir, "prompts")
	for _, name := range []string{"compact.md", "agent/recall-header.md", "tools/bash.json", "tools/task.json"} {
		path := filepath.Join(promptDir, filepath.FromSlash(name))
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, info.Mode().Perm())
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestInitDoesNotOverwriteExistingPrompt(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".lazykoder", "prompts", "compact.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("local compact prompt\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("compact.md changed to %q", got)
	}
}

func TestInitDoesNotOverwriteExistingProviders(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".lazykoder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "providers.json")
	want := []byte(`[{"id":"local","base_url":"https://example.test/v1"}]`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := Init(cwd)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer env.DB.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("providers.json changed to %q", got)
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
	if err := os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte(existing), 0o600); err != nil {
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
	if err := os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte("node_modules/\n.lazykoder/\n"), 0o600); err != nil {
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
