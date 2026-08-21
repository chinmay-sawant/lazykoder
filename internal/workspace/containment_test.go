package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEnforcesWorkspaceContainment(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	insideDir := filepath.Join(root, "inside")
	if err := os.Mkdir(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "inside", path: "inside/file.txt"},
		{name: "lexical escape", path: "../escape.txt", wantErr: "path escapes session directory"},
		{name: "symlink target", path: "outside-link/existing.txt", wantErr: "via symlink"},
		{name: "missing symlink leaf", path: "outside-link/missing/leaf.txt", wantErr: "via symlink"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.path, root)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if got != filepath.Join(root, tt.path) {
					t.Fatalf("Resolve() = %q", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
