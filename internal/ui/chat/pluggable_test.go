package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
	"github.com/chinmay-sawant/lazykoder/internal/roles"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

func TestToolsSlashDiscoversAndTogglesLocalTool(t *testing.T) {
	toolplugin.ResetForTest()
	defer toolplugin.ResetForTest()
	workdir := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "global")
	if err := os.MkdirAll(filepath.Join(workdir, ".lazykoder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, ".lazykoder", "tools.json"), []byte(`[{"name":"hello-tool","description":"say hello","command":"printf hello","binaries":["printf"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYKODER_GLOBAL_CONFIG_DIR", configDir)
	cfg := settings.Default()
	cfg.Tools.AllowDiscovered = true
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: workdir, Settings: &cfg})
	m, cmd := m.runSlashArg("/tools", "")
	if !m.pickerMode || m.pickerKind != pickerKindTools || cmd == nil {
		t.Fatalf("tools picker = mode=%v kind=%q cmd=%v", m.pickerMode, m.pickerKind, cmd != nil)
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !containsString(m.pickerItems, "hello-tool") {
		t.Fatalf("tool picker items = %v", m.pickerItems)
	}
	m.pickerFilter = "hello-tool"
	m.applyFilter()
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !m.projectSettings.EffectiveTools().Enabled["hello-tool"] {
		t.Fatalf("tool was not enabled: %+v", m.projectSettings.EffectiveTools())
	}
}

func TestRolesSlashDiscoversAndSelectsLocalRole(t *testing.T) {
	roles.ResetForTest()
	defer roles.ResetForTest()
	workdir := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "global")
	if err := os.MkdirAll(filepath.Join(workdir, ".lazykoder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, ".lazykoder", "roles.json"), []byte(`[{"id":"reviewer","label":"Reviewer","tools":["read"],"single_writer":true,"model_class":"flash"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYKODER_GLOBAL_CONFIG_DIR", configDir)
	cfg := settings.Default()
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: workdir, Settings: &cfg})
	m, cmd := m.runSlashArg("/roles", "")
	if !m.pickerMode || m.pickerKind != pickerKindRoles || cmd == nil {
		t.Fatalf("roles picker = mode=%v kind=%q cmd=%v", m.pickerMode, m.pickerKind, cmd != nil)
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !containsString(m.pickerItems, "reviewer") || !strings.Contains(stripANSI(viewText(m)), "Reviewer") {
		t.Fatalf("role picker items = %v\n%s", m.pickerItems, stripANSI(viewText(m)))
	}
	m.pickerFilter = "reviewer"
	m.applyFilter()
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.projectSettings.Agents.DefaultRole != "reviewer" {
		t.Fatalf("default role = %q", m.projectSettings.Agents.DefaultRole)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
