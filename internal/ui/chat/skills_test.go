package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

func TestSkillsSlashDiscoversAndActivatesLocalSkill(t *testing.T) {
	workdir := t.TempDir()
	dir := filepath.Join(workdir, "skills", "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: review\ndescription: Review changes\ntriggers: [review, audit]\n---\n\n# Review\n\nCheck tests.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := settings.Default()
	cfg.Skills.IncludeGlobal = false
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: workdir, Settings: &cfg})
	var cmd tea.Cmd
	m, cmd = m.runSlash("/skills")
	if !m.pickerMode || m.pickerKind != pickerKindSkills || cmd == nil {
		t.Fatalf("skills picker = mode=%v kind=%q cmd=%v", m.pickerMode, m.pickerKind, cmd != nil)
	}
	msg := cmd()
	m = applyMsg(t, m, msg)
	if len(m.pickerItems) != 1 || !strings.Contains(stripANSI(viewText(m)), "review") {
		t.Fatalf("skills view = items=%v\n%s", m.pickerItems, stripANSI(viewText(m)))
	}
	m, _ = m.updatePickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.activeSkills) != 1 || m.activeSkills[0].Name != "review" {
		t.Fatalf("active skills = %+v", m.activeSkills)
	}
}

func TestSkillsSlashRespectsDisabledSetting(t *testing.T) {
	cfg := settings.Default()
	cfg.Skills.Enabled = false
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir(), Settings: &cfg})
	next, cmd := m.runSlash("/skills")
	if next.pickerMode || cmd == nil {
		t.Fatalf("disabled skills opened picker: mode=%v cmd=%v", next.pickerMode, cmd != nil)
	}
}

func applyMsg(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}
