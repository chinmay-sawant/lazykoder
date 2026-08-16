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

func TestSettingsSlashOpensDrawer(t *testing.T) {
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
	if !strings.Contains(v, "slot settings") || !strings.Contains(v, "[x]") {
		t.Fatalf("settings drawer missing header/x: %q", v)
	}
	if !strings.Contains(v, "step limit") || !strings.Contains(v, "max steps") {
		t.Fatalf("settings drawer missing rows: %q", v)
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
	m = upd(m, tea.KeyPressMsg{Code: 'j'})
	if m.settingsCursor != settingsRowSteps {
		t.Fatalf("cursor = %d, want steps", m.settingsCursor)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.slotSettings.MaxSteps != 9 {
		t.Fatalf("MaxSteps = %d, want 9", m.slotSettings.MaxSteps)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'k'})
	m = upd(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.slotSettings.LimitEnabled {
		t.Fatal("limit still enabled after toggle")
	}
	wantEff := settings.Settings{Slot: m.slotSettings}.EffectiveMaxSteps()
	if m.maxSteps != wantEff {
		t.Fatalf("maxSteps = %d, want %d", m.maxSteps, wantEff)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slot.MaxSteps != 9 || loaded.Slot.LimitEnabled {
		t.Fatalf("persisted %+v", loaded.Slot)
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
	before := m.slotSettings.MaxSteps
	rowTop := m.settingsDrawerTop() + settingsHeaderLines + settingsRowSteps
	line := plainLine(m.settingsView(), settingsHeaderLines+settingsRowSteps)
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
	if m.slotSettings.MaxSteps != before-1 {
		t.Fatalf("click ◂: MaxSteps = %d, want %d", m.slotSettings.MaxSteps, before-1)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (inc0 + inc1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.slotSettings.MaxSteps != before {
		t.Fatalf("click ▸: MaxSteps = %d, want %d", m.slotSettings.MaxSteps, before)
	}
	// Clicking the label (not a control) must not change the value.
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.slotSettings.MaxSteps != before {
		t.Fatalf("label click changed MaxSteps to %d", m.slotSettings.MaxSteps)
	}
}

func TestSettingsMouseToggleOnGlyph(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()
	if !m.slotSettings.LimitEnabled {
		t.Fatal("want limit on by default")
	}
	rowTop := m.settingsDrawerTop() + settingsHeaderLines + settingsRowLimit
	line := plainLine(m.settingsView(), settingsHeaderLines+settingsRowLimit)
	x0, x1, ok := displaySpan(line, "[on]")
	if !ok {
		t.Fatalf("toggle missing in %q", line)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (x0 + x1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.slotSettings.LimitEnabled {
		t.Fatal("click [on] did not turn limit off")
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

func TestSlashMenuListsSettingsAndContinue(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/settings") || !strings.Contains(v, "/continue") {
		t.Fatalf("slash menu missing settings/continue: %q", v)
	}
}
