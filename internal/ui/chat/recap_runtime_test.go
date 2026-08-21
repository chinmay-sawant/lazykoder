package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecallSearchesOnlyLocalRecapFilesWithQuotedTerms(t *testing.T) {
	workdir := t.TempDir()
	recapDir := filepath.Join(workdir, "knowledge-base", "recaps", "things-to-avoid")
	if err := os.MkdirAll(recapDir, 0o755); err != nil {
		t.Fatalf("mkdir recaps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recapDir, "avoid.md"), []byte("do not repeat parser.v2 migration\n"), 0o600); err != nil {
		t.Fatalf("write recap: %v", err)
	}
	m := New(Options{Workdir: workdir})
	got, err := m.recall(context.Background(), "session", "parser.v2 migration")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(got, "avoid.md") || !strings.Contains(got, "parser.v2") {
		t.Fatalf("recall = %q, want matching recap hit", got)
	}
}

func TestRecallIgnoresEmptyOrUnhelpfulPrompts(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	for _, prompt := range []string{"", "the with user"} {
		got, err := m.recall(context.Background(), "session", prompt)
		if err != nil {
			t.Fatalf("recall %q: %v", prompt, err)
		}
		if got != "" {
			t.Fatalf("recall %q = %q, want empty", prompt, got)
		}
	}
}
