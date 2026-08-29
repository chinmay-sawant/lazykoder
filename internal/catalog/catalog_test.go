package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRootsPrioritizesLocalAndRejectsSymlinks(t *testing.T) {
	workdir := t.TempDir()
	local := filepath.Join(workdir, ".lazykoder")
	global := filepath.Join(t.TempDir(), "lazykoder")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(global, link); err != nil {
		t.Fatal(err)
	}
	roots, diagnostics := ResolveRoots(workdir, true, true, []string{global, link})
	if len(roots) != 2 || roots[0].Scope != ScopeLocal || roots[1].Path != global {
		t.Fatalf("roots = %+v", roots)
	}
	if len(diagnostics) != 1 || diagnostics[0].Path != link {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestReadBoundedFileRejectsOversizeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "descriptor.json")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBoundedFile(path, 4); err == nil {
		t.Fatal("oversize descriptor was accepted")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBoundedFile(link, 64); err == nil {
		t.Fatal("symlink descriptor was accepted")
	}
}
