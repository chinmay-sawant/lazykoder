package agent

import (
	"context"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestBaseToolRegistryHasMatchingSpecsAndRunners(t *testing.T) {
	for name, registration := range baseToolRegistry {
		if registration.spec.Name != name {
			t.Errorf("tool %q has spec name %q", name, registration.spec.Name)
		}
		if registration.runner == nil {
			t.Errorf("tool %q has no runner", name)
		}
	}
}

type registryTestTool struct{}

func (registryTestTool) Spec() opencode.ToolSpec {
	return opencode.ToolSpec{Name: "custom"}
}
func (registryTestTool) Run(context.Context, string, toolplugin.Context) (string, string, string, error) {
	return "ok", "", "completed", nil
}
func (registryTestTool) Title(string) string { return "custom" }

func TestToolRegistryEnablesOnlyAllowed(t *testing.T) {
	toolplugin.ResetForTest()
	defer toolplugin.ResetForTest()
	if err := Register("custom", registryTestTool{}); err != nil {
		t.Fatal(err)
	}
	if got := toolSpecsFor([]string{"custom"}, nil); len(got) != 1 || got[0].Name != "custom" {
		t.Fatalf("enabled specs = %+v", got)
	}
	if got := toolSpecsFor([]string{"read"}, nil); len(got) != 1 || got[0].Name != "read" {
		t.Fatalf("filtered specs = %+v", got)
	}
}
