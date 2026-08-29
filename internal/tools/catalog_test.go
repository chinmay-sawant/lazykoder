package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
)

func TestLoadMergesLocalToolsAndReportsDuplicates(t *testing.T) {
	workdir := t.TempDir()
	local := filepath.Join(workdir, ".lazykoder")
	global := filepath.Join(t.TempDir(), "lazykoder")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(global, "tools.json"), `[{"name":"shared","description":"global","command":"printf global","binaries":["printf"]},{"name":"other","command":"printf other","binaries":["printf"]}]`)
	write(filepath.Join(local, "tools.json"), `[{"name":"shared","description":"local","command":"printf local","binaries":["printf"]},{"name":"shared","description":"duplicate","command":"printf duplicate","binaries":["printf"]}]`)
	t.Setenv("LAZYKODER_GLOBAL_CONFIG_DIR", global)
	catalog, err := Load(workdir, true, true, 32)
	if err != nil {
		t.Fatal(err)
	}
	shared, ok := findTool(catalog.Tools, "shared")
	if len(catalog.Tools) != 2 || !ok || shared.Description != "local" {
		t.Fatalf("tools = %+v", catalog.Tools)
	}
	if len(catalog.Diagnostics) != 1 || !strings.Contains(catalog.Diagnostics[0].Error, "duplicate") {
		t.Fatalf("diagnostics = %+v", catalog.Diagnostics)
	}
	if _, ok := toolplugin.Lookup("shared"); ok {
		t.Fatal("loading a descriptor registered executable code")
	}
}

func findTool(tools []Descriptor, name string) (Descriptor, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Descriptor{}, false
}

func TestDiscoveredToolQuotesArgumentsAndUsesPolicy(t *testing.T) {
	workdir := t.TempDir()
	descriptor := Descriptor{
		Name: "echo-value", Description: "echo", Parameters: map[string]any{"type": "object"},
		Command: "printf '%s' {value}", Binaries: []string{"printf"},
	}
	plugin := descriptor.Plugin()
	out, _, status, err := plugin.Run(context.Background(), `{"value":"ok; touch injected"}`, toolplugin.Context{Workdir: workdir})
	if err != nil || status != "completed" || out != "ok; touch injected" {
		t.Fatalf("run = output %q status %q err %v", out, status, err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "injected")); !os.IsNotExist(err) {
		t.Fatal("argument text escaped its shell quote")
	}
	dangerous := Descriptor{Name: "danger", Command: "rm -rf {path}", Binaries: []string{"rm"}}.Plugin()
	_, _, status, err = dangerous.Run(context.Background(), `{"path":"x"}`, toolplugin.Context{Workdir: workdir})
	if status != "denied" || err == nil {
		t.Fatalf("dangerous run = status %q err %v", status, err)
	}
}
