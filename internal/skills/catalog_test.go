package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverMergesLocalAndGlobalWithStableMetadata(t *testing.T) {
	workdir := t.TempDir()
	local := filepath.Join(workdir, "skills", "review")
	global := filepath.Join(workdir, "global", "review")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(local, DescriptorName), "review", "Review code", "review, audit")
	writeSkill(t, filepath.Join(global, DescriptorName), "review", "Global review", "global")
	catalog, err := Discover(context.Background(), Options{
		Workdir:       workdir,
		IncludeLocal:  true,
		IncludeGlobal: true,
		GlobalRoots:   []string{filepath.Join(workdir, "global")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 2 {
		t.Fatalf("skills = %d, want 2", len(catalog.Skills))
	}
	matches := catalog.AutoMatches("review code", 2)
	if len(matches) != 1 || matches[0].Skill.Scope != ScopeLocal {
		t.Fatalf("auto matches = %+v, want local review only", matches)
	}
	if matches[0].Skill.DisplayPath != "skills/review/SKILL.md" {
		t.Fatalf("display path = %q", matches[0].Skill.DisplayPath)
	}
}

func TestDiscoverAcceptsPluralDescriptorAndReportsPreferredDuplicate(t *testing.T) {
	workdir := t.TempDir()
	dir := filepath.Join(workdir, "skills", "one")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(dir, LegacyDescriptorName), "one", "Legacy", "legacy")
	opts := DefaultOptions(workdir)
	opts.IncludeGlobal = false
	catalog, err := Discover(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 1 || catalog.Skills[0].Name != "one" {
		t.Fatalf("catalog = %+v", catalog.Skills)
	}
	writeSkill(t, filepath.Join(dir, DescriptorName), "one", "Preferred", "preferred")
	catalog, err = Discover(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 1 || catalog.Skills[0].Description != "Preferred" {
		t.Fatalf("preferred catalog = %+v", catalog.Skills)
	}
	if len(catalog.Diagnostics) == 0 {
		t.Fatal("expected duplicate descriptor diagnostic")
	}
}

func TestReadBodyRejectsSymlinkAndCapsContent(t *testing.T) {
	workdir := t.TempDir()
	dir := filepath.Join(workdir, "skills", "one")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, DescriptorName)
	writeSkill(t, path, "one", "Description", "one")
	opts := DefaultOptions(workdir)
	opts.IncludeGlobal = false
	catalog, err := Discover(context.Background(), opts)
	if err != nil || len(catalog.Skills) != 1 {
		t.Fatalf("discover = %+v, %v", catalog, err)
	}
	body, err := ReadBody(context.Background(), catalog.Skills[0], 4)
	if err != nil || len(body) != 4 {
		t.Fatalf("body = %q, err=%v", body, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "outside.md"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workdir, "outside.md"), path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ReadBody(context.Background(), catalog.Skills[0], 100); err == nil {
		t.Fatal("symlink descriptor was accepted")
	}
}

func writeSkill(t *testing.T, path, name, description, triggers string) {
	t.Helper()
	data := "---\nname: " + name + "\ndescription: " + description + "\ntriggers: [" + triggers + "]\n---\n\n# " + name + "\n\nUse this skill.\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
