package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMustCompactContainsHeadings(t *testing.T) {
	text := Must("compact.md")
	if text == "" {
		t.Fatal("compact.md is empty")
	}
	for _, heading := range []string{
		"## Primary request and intent",
		"## Key decisions and constraints",
		"## Files and code that matter",
		"## Errors and how they were fixed",
		"## Pending work / TODOs",
		"## Current work",
		"## Next step",
		"## All user messages",
	} {
		if !strings.Contains(text, heading) {
			t.Errorf("compact.md missing heading %q", heading)
		}
	}
	for _, rule := range []string{
		"handoff",
		"Follow the user's language",
		"Do not invent",
		"verbatim",
	} {
		if !strings.Contains(text, rule) {
			t.Errorf("compact.md missing rule %q", rule)
		}
	}
}

func TestMustUnknownPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Must(unknown) should panic")
		}
	}()
	_ = Must("missing-prompt.md")
}

func TestStorePrefersWorkspacePrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom.md"), []byte("workspace prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := NewDir(dir).Must("custom.md"); got != "workspace prompt\n" {
		t.Fatalf("custom prompt = %q", got)
	}
}

func TestStoreFallsBackForInvalidToolSpec(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "bash.json"), []byte(`{"name":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := NewDir(dir).ToolSpec("bash")
	if spec.Name != "bash" || !strings.Contains(spec.Description, "shell command") {
		t.Fatalf("fallback tool spec = %+v", spec)
	}
}

func TestDefaultFilesIncludeToolSchemas(t *testing.T) {
	files := DefaultFiles()
	if len(files) < 20 {
		t.Fatalf("default file count = %d, want prompt and tool defaults", len(files))
	}
	foundBash := false
	for _, file := range files {
		if file.Name == "tools/bash.json" {
			foundBash = true
		}
	}
	if !foundBash {
		t.Fatal("bash tool schema was not embedded")
	}
}
