package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return string(data)
}

func TestRunUniqueReplace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "a\nb\na")
	res, err := Run("f.txt", "b", "B", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := readFile(t, root, "f.txt"); got != "a\nB\na" {
		t.Errorf("file content = %q, want \"a\\nB\\na\"", got)
	}
	diff, _ := res.Metadata["diff"].(string)
	if diff == "" {
		t.Error("Metadata[diff] empty, want a diff")
	}
	if !strings.Contains(diff, "-b") || !strings.Contains(diff, "+B") {
		t.Errorf("diff = %q, want it to contain \"-b\" and \"+B\"", diff)
	}
	if !strings.HasPrefix(diff, "@@") {
		t.Errorf("diff = %q, want it to start with a hunk header", diff)
	}
	if !strings.Contains(res.Output, "edited") {
		t.Errorf("Output = %q, want \"edited ...\"", res.Output)
	}
	if res.Metadata["bytes_changed"] != 0 {
		t.Errorf("Metadata[bytes_changed] = %v, want 0", res.Metadata["bytes_changed"])
	}
}

func TestRunMissingOldString(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "a\nb\na")
	_, err := Run("f.txt", "zzz", "Z", root)
	if err == nil {
		t.Fatal("Run() error = nil, want oldString not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want it to say not found", err)
	}
	if got := readFile(t, root, "f.txt"); got != "a\nb\na" {
		t.Errorf("file content changed to %q, want unchanged", got)
	}
}

func TestRunNotUnique(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "a\nb\na")
	_, err := Run("f.txt", "a", "A", root)
	if err == nil {
		t.Fatal("Run() error = nil, want not unique error")
	}
	if !strings.Contains(err.Error(), "not unique") {
		t.Errorf("error = %v, want it to say not unique", err)
	}
	if got := readFile(t, root, "f.txt"); got != "a\nb\na" {
		t.Errorf("file content changed to %q, want unchanged", got)
	}
}

func TestRunEmptyOldString(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "abc")
	if _, err := Run("f.txt", "", "X", root); err == nil {
		t.Fatal("Run() error = nil, want error for empty oldString")
	}
}

func TestRunParentEscape(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(root)
	writeFile(t, parent, "x.txt", "old")
	_, err := Run("../x.txt", "old", "new", root)
	if err == nil {
		t.Fatal("Run() error = nil, want escape rejected")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want it to mention the escape", err)
	}
	if got := readFile(t, parent, "x.txt"); got != "old" {
		t.Errorf("file outside rootDir was modified to %q", got)
	}
}

func TestRunAbsoluteOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "x.txt")
	writeFile(t, outside, "x.txt", "old")
	_, err := Run(target, "old", "new", root)
	if err == nil {
		t.Fatal("Run() error = nil, want absolute escape rejected")
	}
	if got := readFile(t, outside, "x.txt"); got != "old" {
		t.Errorf("file outside rootDir was modified to %q", got)
	}
}

func TestRunMissingFile(t *testing.T) {
	root := t.TempDir()
	_, err := Run("nope.txt", "a", "b", root)
	if err == nil {
		t.Fatal("Run() error = nil, want error for missing file")
	}
}

func TestRunTruncatesLongStringsInOutput(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 100)
	writeFile(t, root, "f.txt", long)
	res, err := Run("f.txt", long, "y", root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strings.Contains(res.Output, strings.Repeat("x", 100)) {
		t.Errorf("Output = %q, want oldString truncated to 60 runes", res.Output)
	}
	if !strings.Contains(res.Output, " -> y") {
		t.Errorf("Output = %q, want replacement present", res.Output)
	}
}

func TestDiffHunkMultipleChanges(t *testing.T) {
	a := strings.Split("one\ntwo\nthree\nfour\nfive\nsix\nseven\n", "\n")
	b := strings.Split("one\ntwo\nTHREE\nfour\nfive\nSIX\nseven\n", "\n")
	got := diffLines(a, b)
	if !strings.Contains(got, "-three") || !strings.Contains(got, "+THREE") {
		t.Errorf("diff = %q, want first change", got)
	}
	if !strings.Contains(got, "-six") || !strings.Contains(got, "+SIX") {
		t.Errorf("diff = %q, want second change", got)
	}
	if strings.Count(got, "@@") != 2 {
		t.Errorf("diff = %q, want two hunks", got)
	}
}

func TestDiffUnchanged(t *testing.T) {
	a := strings.Split("same\nlines\n", "\n")
	if got := diffLines(a, a); got != "" {
		t.Errorf("diff = %q, want empty for unchanged content", got)
	}
}

func TestDiffCap(t *testing.T) {
	var a, b []string
	for i := 0; i < 200; i++ {
		line := strings.Repeat("y", 40) + "x"
		a = append(a, line)
		b = append(b, line[:len(line)-1]+"z")
	}
	got := diffLines(a, b)
	if len(got) > maxDiffChars {
		t.Errorf("diff length = %d, want capped at %d", len(got), maxDiffChars)
	}
	if !strings.HasSuffix(got, "... diff truncated") {
		t.Errorf("diff = %q, want truncation note", got)
	}
}
