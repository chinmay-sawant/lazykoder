package toolplugin

import (
	"context"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

type testTool struct{}

func (testTool) Spec() opencode.ToolSpec { return opencode.ToolSpec{Name: "custom"} }
func (testTool) Title(string) string     { return "custom" }
func (testTool) Run(context.Context, string, Context) (string, string, string, error) {
	return "ok", `{"custom":true}`, "completed", nil
}

func TestRegisterRejectsDuplicateAndSpecsOnlyRegisteredNames(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	if err := Register("custom", testTool{}); err != nil {
		t.Fatal(err)
	}
	if err := Register("custom", testTool{}); err == nil {
		t.Fatal("duplicate registration was accepted")
	}
	specs := Specs([]string{"custom", "missing"})
	if len(specs) != 1 || specs[0].Name != "custom" {
		t.Fatalf("specs = %+v", specs)
	}
}

type namedTool string

func (tool namedTool) Spec() opencode.ToolSpec { return opencode.ToolSpec{Name: string(tool)} }
func (tool namedTool) Title(string) string     { return string(tool) }
func (namedTool) Run(context.Context, string, Context) (string, string, string, error) {
	return "ok", "", "completed", nil
}

func TestReplaceDiscoveredRemovesStaleEntriesAndPreservesCompiled(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	if err := Register("compiled", namedTool("compiled")); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceDiscovered(map[string]Tool{"old": namedTool("old")}); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceDiscovered(map[string]Tool{"new": namedTool("new")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("old"); ok {
		t.Fatal("stale discovered tool was retained")
	}
	if _, ok := Lookup("compiled"); !ok {
		t.Fatal("compiled tool was removed")
	}
	if !IsDiscovered("new") || IsDiscovered("old") {
		t.Fatal("discovered state was not replaced")
	}
}
