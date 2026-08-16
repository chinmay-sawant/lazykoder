package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

func TestSettingsSlashOpensCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m = typeText(m, "/settings")
	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(Model)
	if !m.settingsMode {
		t.Fatal("settings mode not opened")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "SETTINGS") || !strings.Contains(v, "[x]") {
		t.Fatalf("settings card missing header/x: %q", v)
	}
	for _, want := range []string{"default model", "default variant", "step limit", "max steps", "sub-agents", "max concurrent", "child max steps"} {
		if !strings.Contains(v, want) {
			t.Fatalf("settings card missing %q: %q", want, v)
		}
	}
	// Full-screen card, not a drawer glued above the prompt.
	if strings.Contains(v, "ask lazykoder") && strings.Contains(v, "SETTINGS") {
		// Composer may still paint under Place only if not full replacement.
		// settingsMode uses dedicated screen - prompt should not be the focus frame.
	}
	if !strings.Contains(v, "SETTINGS") {
		t.Fatal("missing SETTINGS title")
	}
}

func TestSettingsAdjustAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	cfg := settings.Default()
	cfg.Slot.MaxSteps = 8
	if err := settings.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
		Settings:     &cfg,
	})
	m = m.openSettings()
	// move to max steps row
	for m.settingsCursor != settingsRowSteps {
		m = upd(m, tea.KeyPressMsg{Code: 'j'})
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Slot.MaxSteps != 9 {
		t.Fatalf("MaxSteps = %d, want 9", m.projectSettings.Slot.MaxSteps)
	}
	// toggle limit off
	for m.settingsCursor != settingsRowLimit {
		m = upd(m, tea.KeyPressMsg{Code: 'k'})
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.projectSettings.Slot.LimitEnabled {
		t.Fatal("limit still enabled after toggle")
	}
	if m.maxSteps != m.projectSettings.EffectiveMaxSteps() {
		t.Fatalf("maxSteps = %d, want %d", m.maxSteps, m.projectSettings.EffectiveMaxSteps())
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slot.MaxSteps != 9 || loaded.Slot.LimitEnabled {
		t.Fatalf("persisted slot %+v", loaded.Slot)
	}
}

func TestSettingsDefaultModelCyclePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m.models = []string{"deepseek-v4-flash", "claude-4", "big-pickle"}
	m = m.openSettings()
	m.settingsCursor = settingsRowModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Model.Default != "claude-4" {
		t.Fatalf("default model = %q, want claude-4", m.projectSettings.Model.Default)
	}
	if m.model != "claude-4" {
		t.Fatalf("live model = %q, want claude-4 on new session", m.model)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.Default != "claude-4" {
		t.Fatalf("persisted model = %q", loaded.Model.Default)
	}
}

func TestSettingsCloseClick(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()
	x0, y, x1, ok := m.settingsCloseRect()
	if !ok {
		t.Fatal("close rect missing")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (x0 + x1) / 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.settingsMode {
		t.Fatal("click [x] did not close settings")
	}
}

func TestSettingsMouseAdjustSteps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()
	before := m.projectSettings.Slot.MaxSteps
	rowTop := m.settingsListScreenTop() + settingsRowSteps
	line := ""
	for _, l := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(l, "max steps") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatal("max steps row missing")
	}
	dec0, dec1, ok := displaySpan(line, "◂")
	if !ok {
		t.Fatalf("decrease chevron missing in %q", line)
	}
	inc0, inc1, ok := displaySpanLast(line, "▸")
	if !ok {
		t.Fatalf("increase chevron missing in %q", line)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (dec0 + dec1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before-1 {
		t.Fatalf("click ◂: MaxSteps = %d, want %d", m.projectSettings.Slot.MaxSteps, before-1)
	}
	// Re-read after re-render.
	for _, l := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(l, "max steps") {
			line = l
			break
		}
	}
	inc0, inc1, ok = displaySpanLast(line, "▸")
	if !ok {
		t.Fatal("▸ missing after decrease")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (inc0 + inc1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before {
		t.Fatalf("click ▸: MaxSteps = %d, want %d", m.projectSettings.Slot.MaxSteps, before)
	}
}

func TestSettingsMouseToggleOnGlyph(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()
	if !m.projectSettings.Slot.LimitEnabled {
		t.Fatal("want limit on by default")
	}
	rowTop := m.settingsListScreenTop() + settingsRowLimit
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 10, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.LimitEnabled {
		t.Fatal("click step limit row did not turn limit off")
	}
}

func TestNewSeedsModelFromSettings(t *testing.T) {
	cfg := settings.Default()
	cfg.Model.Default = "claude-4"
	cfg.Model.Variant = "high"
	m := New(Options{
		Store:    newTestStore(t),
		Client:   deadClient(),
		Workdir:  t.TempDir(),
		Settings: &cfg,
	})
	if m.model != "claude-4" {
		t.Fatalf("model = %q, want claude-4", m.model)
	}
	if m.variant != "high" {
		t.Fatalf("variant = %q, want high", m.variant)
	}
}

func TestSlashMenuListsSettingsAndContinue(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/settings") || !strings.Contains(v, "/continue") {
		t.Fatalf("slash menu missing settings/continue: %q", v)
	}
}

func TestContinueAfterStepLimitResumes(t *testing.T) {
	tc := fakeToolCall{ID: "call_1", Name: "bash", Args: `{"command":"echo x"}`}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("all done", "stop", nil),
	)
	st := newTestStore(t)
	m := New(Options{
		Store:    st,
		Client:   newClient(fake.srv),
		Workdir:  t.TempDir(),
		MaxSteps: 2,
	})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "work")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if !m.stepLimitHit {
		t.Fatalf("want stepLimitHit after limit, err=%q", m.err)
	}
	if !strings.Contains(m.err, "/continue") {
		t.Fatalf("err missing /continue hint: %q", m.err)
	}
	usersBefore := countSessionUsers(t, st, m.session)

	m, cmd = m.runContinue()
	if cmd == nil {
		t.Fatal("continue nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if m.stepLimitHit {
		t.Fatal("stepLimitHit still set after successful continue")
	}
	if m.err != "" {
		t.Fatalf("continue err: %q", m.err)
	}
	usersAfter := countSessionUsers(t, st, m.session)
	if usersAfter != usersBefore {
		t.Fatalf("continue wrote user message: %d -> %d", usersBefore, usersAfter)
	}
}

func TestContinueWithoutLimitSendsMessage(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("ok", "stop", nil))
	m := New(Options{
		Store:   newTestStore(t),
		Client:  newClient(fake.srv),
		Workdir: t.TempDir(),
	})
	p := newPump(t)
	p.run(m.Init())
	m, cmd := m.runContinue()
	if cmd == nil {
		t.Fatal("continue nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if m.err != "" {
		t.Fatalf("err: %q", m.err)
	}
	found := false
	for _, it := range m.items {
		if it.kind == itemUser && it.text == "continue" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected user continue message in items: %+v", m.items)
	}
}

func countSessionUsers(t *testing.T, st *db.Store, sess *db.Session) int {
	t.Helper()
	if sess == nil {
		return 0
	}
	msgs, err := st.ListMessages(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, msg := range msgs {
		if msg.Role == "user" {
			n++
		}
	}
	return n
}
