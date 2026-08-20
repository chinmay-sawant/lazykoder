package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectInstructionsMissing(t *testing.T) {
	t.Parallel()
	content, path, ok := LoadProjectInstructions(t.TempDir())
	if ok || content != "" || path != "" {
		t.Fatalf("missing file: content=%q path=%q ok=%v", content, path, ok)
	}
	content, path, ok = LoadProjectInstructions("")
	if ok || content != "" || path != "" {
		t.Fatalf("empty workdir: content=%q path=%q ok=%v", content, path, ok)
	}
}

func TestLoadProjectInstructionsAGENTS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(p, []byte("# rules\nbe careful\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, path, ok := LoadProjectInstructions(dir)
	if !ok {
		t.Fatal("expected ok")
	}
	if path != p {
		t.Fatalf("path = %q, want %q", path, p)
	}
	if !strings.Contains(content, "be careful") {
		t.Fatalf("content = %q", content)
	}
}

func TestLoadProjectInstructionsFallbackAgents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "agents.md")
	if err := os.WriteFile(p, []byte("lowercase agents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, path, ok := LoadProjectInstructions(dir)
	if !ok || path != p || !strings.Contains(content, "lowercase agents") {
		t.Fatalf("fallback: content=%q path=%q ok=%v", content, path, ok)
	}
}

func TestLoadProjectInstructionsPrefersAGENTS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("UPPER\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents.md"), []byte("lower\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, _, ok := LoadProjectInstructions(dir)
	if !ok || !strings.Contains(content, "UPPER") || strings.Contains(content, "lower") {
		t.Fatalf("prefer AGENTS.md: %q", content)
	}
}

func TestLoadProjectInstructionsTruncate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := strings.Repeat("a", maxProjectInstructionsBytes+50)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	content, _, ok := LoadProjectInstructions(dir)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(content, "truncated: AGENTS.md exceeded") {
		t.Fatalf("missing truncate note: len=%d", len(content))
	}
	if len(content) <= maxProjectInstructionsBytes {
		t.Fatalf("truncated note should extend past cap, len=%d", len(content))
	}
}

func TestFormatProjectInstructionsMessage(t *testing.T) {
	t.Parallel()
	if FormatProjectInstructionsMessage("  ") != "" {
		t.Fatal("empty content should format empty")
	}
	got := FormatProjectInstructionsMessage("do not invent APIs")
	if !strings.HasPrefix(got, projectInstructionsHeader) {
		t.Fatalf("missing header: %q", got)
	}
	if !strings.Contains(got, "do not invent APIs") {
		t.Fatalf("missing body: %q", got)
	}
}
