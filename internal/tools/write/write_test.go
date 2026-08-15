package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWriteAndReadBack(t *testing.T) {
	root := t.TempDir()
	res, err := Run("hello.txt", "hello", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("file content = %q, want \"hello\"", string(got))
	}
	if res.Metadata["bytes"] != 5 {
		t.Errorf("Metadata[bytes] = %v, want 5", res.Metadata["bytes"])
	}
	if !strings.HasPrefix(res.Output, "wrote 5 bytes to ") {
		t.Errorf("Output = %q, want \"wrote 5 bytes to ...\"", res.Output)
	}
	if p, _ := res.Metadata["path"].(string); !strings.HasPrefix(p, root) {
		t.Errorf("Metadata[path] = %v, want it inside %q", res.Metadata["path"], root)
	}
}

func TestRunParentDirMissing(t *testing.T) {
	root := t.TempDir()
	_, err := Run("no/such/dir/file.txt", "x", root)
	if err == nil {
		t.Fatal("Run() error = nil, want error for missing parent dir")
	}
	if !strings.Contains(err.Error(), "parent directory") {
		t.Errorf("error = %v, want it to mention the missing parent", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "no")); !os.IsNotExist(statErr) {
		t.Errorf("no directory tree may be created, got stat error %v", statErr)
	}
}

func TestRunParentEscape(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(root)
	_, err := Run("../escape.txt", "x", root)
	if err == nil {
		t.Fatal("Run() error = nil, want escape rejected")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want it to mention the escape", err)
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(statErr) {
		t.Errorf("file must not be created outside rootDir, got stat error %v", statErr)
	}
}

func TestRunAbsoluteOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "escape.txt")
	_, err := Run(target, "x", root)
	if err == nil {
		t.Fatal("Run() error = nil, want absolute escape rejected")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("file must not be created outside rootDir, got stat error %v", statErr)
	}
}

func TestRunOverwrite(t *testing.T) {
	root := t.TempDir()
	if _, err := Run("over.txt", "first", root); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := Run("over.txt", "second", root); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "over.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("file content = %q, want \"second\"", string(got))
	}
}

func TestRunNestedExistingDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := Run("sub/nested.txt", "deep", root); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "sub", "nested.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "deep" {
		t.Errorf("file content = %q, want \"deep\"", string(got))
	}
}
