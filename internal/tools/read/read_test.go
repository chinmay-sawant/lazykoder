package read

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFixture(t *testing.T) {
	root := t.TempDir()
	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	res, err := Run("hello.txt", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Output != content {
		t.Errorf("Output = %q, want %q", res.Output, content)
	}
	if !strings.Contains(res.Output, "line two") {
		t.Errorf("Output = %q, want it to contain \"line two\"", res.Output)
	}
	if res.Metadata["lines"] != 3 {
		t.Errorf("Metadata[lines] = %v, want 3", res.Metadata["lines"])
	}
	if _, ok := res.Metadata["truncated"]; ok {
		t.Errorf("Metadata[truncated] present on small file")
	}
}

func TestRunEmptyFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	res, err := Run("empty.txt", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Output != "" {
		t.Errorf("Output = %q, want empty", res.Output)
	}
	if res.Metadata["lines"] != 0 {
		t.Errorf("Metadata[lines] = %v, want 0", res.Metadata["lines"])
	}
}

func TestRunMissingFile(t *testing.T) {
	root := t.TempDir()
	_, err := Run("nope.txt", root)
	if err == nil {
		t.Fatal("Run() error = nil, want error for missing file")
	}
	if !strings.Contains(err.Error(), "nope.txt") {
		t.Errorf("error = %v, want it to name the path", err)
	}
}

func TestRunParentEscape(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(root)
	secret := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	_, err := Run("../secret.txt", root)
	if err == nil {
		t.Fatal("Run() error = nil, want escape rejected")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want it to mention the escape", err)
	}
}

func TestRunAbsoluteOutside(t *testing.T) {
	root := t.TempDir()
	_, err := Run("/etc/passwd", root)
	if err == nil {
		t.Fatal("Run() error = nil, want absolute escape rejected")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want it to mention the escape", err)
	}
}

func TestRunTruncated(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.bin")
	if err := os.WriteFile(path, make([]byte, 1<<20+100), 0o600); err != nil {
		t.Fatalf("write big file: %v", err)
	}
	res, err := Run("big.bin", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Metadata["truncated"] != true {
		t.Errorf("Metadata[truncated] = %v, want true", res.Metadata["truncated"])
	}
	if len(res.Output) != 1<<20+len("\n... truncated at 1MiB") {
		t.Errorf("Output length = %d, want capped at 1MiB plus note", len(res.Output))
	}
	if !strings.HasSuffix(res.Output, "... truncated at 1MiB") {
		t.Errorf("Output = %q, want truncation note", res.Output)
	}
}

func TestRunSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	_, err := Run("escape.txt", root)
	if err == nil {
		t.Fatal("Run() error = nil, want symlink escape rejected")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want it to mention the escape", err)
	}
}

func TestRunSymlinkInsideAllowed(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("inside"), 0o600); err != nil {
		t.Fatalf("write real file: %v", err)
	}
	if err := os.Symlink("real.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	res, err := Run("alias.txt", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Output != "inside" {
		t.Errorf("Output = %q, want \"inside\"", res.Output)
	}
}

func TestRunSymlinkEscapeNested(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "target.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Run("link/target.txt", root)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("want symlink escape rejection, got %v", err)
	}
}

func TestRunAbsoluteInsideAllowed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "absolute.txt")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Run(path, root)
	if err != nil || res.Output != "inside" {
		t.Fatalf("absolute inside should work: output=%q err=%v", res.Output, err)
	}
}
