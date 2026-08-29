package roles

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestToolsForEveryRole(t *testing.T) {
	tests := []struct {
		role string
		want []string
	}{
		{role: Explore, want: []string{"bash", "read", "grep", "webfetch"}},
		{role: Plan, want: []string{"bash", "read", "grep", "webfetch"}},
		{role: General, want: []string{"bash", "read", "grep", "write", "edit", "webfetch"}},
	}
	for _, tt := range tests {
		if got := Tools(tt.role); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Tools(%q) = %v, want %v", tt.role, got, tt.want)
		}
	}
}

func TestRoleRegistryAllowsCustomSingleWriter(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	workdir := t.TempDir()
	dir := filepath.Join(workdir, ".lazykoder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "roles.json")
	body := `[{"id":"release","label":"Release","tools":["read","write"],"single_writer":true,"model_class":"pro","prompt":"prepare a release"}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(workdir)
	if err != nil {
		t.Fatal(err)
	}
	role, ok := DescriptorFor("release")
	if !ok || !role.SingleWriter || !reflect.DeepEqual(role.Tools, []string{"read", "write"}) {
		t.Fatalf("role = %+v, found=%v", role, ok)
	}
	if len(catalog.Roles) != 4 {
		t.Fatalf("catalog roles = %d", len(catalog.Roles))
	}
	if got := Normalize("release", Explore); got != "release" {
		t.Fatalf("Normalize = %q", got)
	}
}

func TestRoleLoadKeepsGoodEntriesWithBadJSONItems(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	workdir := t.TempDir()
	dir := filepath.Join(workdir, ".lazykoder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "roles.json"), []byte(`[null,{"id":"review","tools":["read"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := DescriptorFor("review"); !ok || len(catalog.Diagnostics) != 1 || !strings.Contains(catalog.Diagnostics[0].Error, "role ID") {
		t.Fatalf("catalog = %+v", catalog)
	}
}
