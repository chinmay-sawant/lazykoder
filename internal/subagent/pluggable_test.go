package subagent

import (
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/roles"
)

func TestManagerBuildJobUsesCustomRoleCapabilities(t *testing.T) {
	roles.ResetForTest()
	defer roles.ResetForTest()
	if err := roles.Register(roles.Role{
		ID: "reviewer", Label: "Reviewer", Tools: []string{"read", "grep"},
		SingleWriter: true, DefaultModelClass: "flash",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	cfg.DefaultRole = "reviewer"
	cfg.Roles = roles.Roles()
	m := NewManager(cfg, nil)
	job := m.buildJob("job", "parent", "part", "review", "reviewer", Spec{}, 0, Runtime{}, "", false)
	if len(job.Tools) != 2 || job.Tools[0] != "read" || job.Tools[1] != "grep" {
		t.Fatalf("custom role tools = %v", job.Tools)
	}
	role, ok := roles.DescriptorFor(job.Role)
	if !ok || !role.SingleWriter {
		t.Fatalf("custom role descriptor = %+v, found=%v", role, ok)
	}
}
