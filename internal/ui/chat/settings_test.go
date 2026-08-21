package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
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
	for _, want := range []string{"new-session model", "child timeout", "default role", "child bash confirms", "parent bash allowlist", "auto-compact", "compact at"} {
		if !strings.Contains(v, want) {
			t.Fatalf("settings card missing %q: %q", want, v)
		}
	}
	// Full-screen card, not a drawer glued above the prompt. Composer may
	// still paint under Place only if not full replacement; settingsMode uses
	// a dedicated screen so the prompt should not be the focus frame.
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
	rowTop := settingsPaintedRowY(m, "parent max steps")
	if rowTop < 0 {
		t.Fatal("parent max steps row missing")
	}
	line := settingsPaintedRow(m, "parent max steps")
	if line == "" {
		t.Fatal("parent max steps row missing")
	}
	dec0, dec1, ok := displaySpan(line, "◂")
	if !ok {
		t.Fatalf("decrease chevron missing in %q", line)
	}
	_, _, ok = displaySpanLast(line, "▸")
	if !ok {
		t.Fatalf("increase chevron missing in %q", line)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (dec0 + dec1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before-1 {
		t.Fatalf("click ◂: MaxSteps = %d, want %d", m.projectSettings.Slot.MaxSteps, before-1)
	}
	// Re-read after re-render.
	line = settingsPaintedRow(m, "parent max steps")
	inc0, inc1, ok := displaySpanLast(line, "▸")
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
	rowTop := settingsPaintedRowY(m, "step limit")
	if rowTop < 0 {
		t.Fatal("step limit row missing")
	}
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

func TestSettingsHitAllRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 48})
	m = mm.(Model)
	m.models = []string{"deepseek-v4-flash", "claude-4", "big-pickle"}
	m = m.openSettings()

	for row := 0; row < settingsRowCount; row++ {
		label := settingsRowLabel(row)
		base := m
		y := settingsPaintedRowY(base, label)
		if y < 0 {
			t.Fatalf("row %d %q not painted:\n%s", row, label, stripANSI(viewText(base)))
		}
		line := settingsPaintedRow(base, label)
		x := settingsHitX(line, row)
		next, _, hit := base.settingsHit(x, y, tea.MouseLeft)
		if !hit {
			t.Fatalf("settingsHit missed %q at (%d,%d) line %q", label, x, y, line)
		}
		if !settingsHitChanged(base, next, row) {
			t.Fatalf("settingsHit on %q did not change state or open editor (x=%d y=%d line=%q)", label, x, y, line)
		}
	}
}

func TestSettingsCompactPercentAdjustAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m = m.openSettings()
	if m.projectSettings.Compaction.Percent != agent.DefaultCompactPercent {
		t.Fatalf("default percent = %d", m.projectSettings.Compaction.Percent)
	}
	m.settingsCursor = settingsRowCompactPercent
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.projectSettings.Compaction.Percent != agent.DefaultCompactPercent-settingsCompactPercentStep {
		t.Fatalf("percent after left = %d", m.projectSettings.Compaction.Percent)
	}
	m.settingsCursor = settingsRowCompactAuto
	m = upd(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.projectSettings.Compaction.Auto {
		t.Fatal("auto-compact should toggle off")
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Compaction.Auto {
		t.Fatal("persisted auto still on")
	}
	if loaded.Compaction.Percent != agent.DefaultCompactPercent-settingsCompactPercentStep {
		t.Fatalf("persisted percent = %d", loaded.Compaction.Percent)
	}
}

func TestSettingsTimeoutRoleConfirmPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m = m.openSettings()
	m.settingsCursor = settingsRowAgentsTimeout
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.DefaultTimeoutSec != settings.DefaultAgentsTimeoutSec+settingsTimeoutStepSec {
		t.Fatalf("timeout = %d, want %d", m.projectSettings.Agents.DefaultTimeoutSec, settings.DefaultAgentsTimeoutSec+settingsTimeoutStepSec)
	}
	m.settingsCursor = settingsRowAgentsRole
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.DefaultRole != "plan" {
		t.Fatalf("role = %q, want plan", m.projectSettings.Agents.DefaultRole)
	}
	m.settingsCursor = settingsRowBashConfirm
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.BashConfirm != "deny" {
		t.Fatalf("bash confirm = %q, want deny", m.projectSettings.Agents.BashConfirm)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agents.DefaultTimeoutSec != settings.DefaultAgentsTimeoutSec+settingsTimeoutStepSec {
		t.Fatalf("persisted timeout = %d", loaded.Agents.DefaultTimeoutSec)
	}
	if loaded.Agents.DefaultRole != "plan" {
		t.Fatalf("persisted role = %q", loaded.Agents.DefaultRole)
	}
	if loaded.Agents.BashConfirm != "deny" {
		t.Fatalf("persisted confirm = %q", loaded.Agents.BashConfirm)
	}
}

func TestSettingsExploreQueueOverridePersist(t *testing.T) {
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
	m.settingsCursor = settingsRowExploreModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.ExploreModel != "deepseek-v4-flash" {
		t.Fatalf("explore model = %q, want deepseek-v4-flash", m.projectSettings.Agents.ExploreModel)
	}
	m.settingsCursor = settingsRowChildModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.ModelOverride != "deepseek-v4-flash" {
		t.Fatalf("override = %q, want deepseek-v4-flash", m.projectSettings.Agents.ModelOverride)
	}
	beforeQ := m.projectSettings.Agents.MaxQueued
	m.settingsCursor = settingsRowAgentsQueued
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.MaxQueued != beforeQ+1 {
		t.Fatalf("queued = %d, want %d", m.projectSettings.Agents.MaxQueued, beforeQ+1)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agents.ExploreModel != "deepseek-v4-flash" {
		t.Fatalf("persisted explore = %q", loaded.Agents.ExploreModel)
	}
	if loaded.Agents.ModelOverride != "deepseek-v4-flash" {
		t.Fatalf("persisted override = %q", loaded.Agents.ModelOverride)
	}
	if loaded.Agents.MaxQueued != beforeQ+1 {
		t.Fatalf("persisted queued = %d", loaded.Agents.MaxQueued)
	}
}

func TestSettingsCardMinHeight(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 48})
	m = mm.(Model)
	m = m.openSettings()
	cardLines := strings.Split(m.settingsCardView(), "\n")
	if len(cardLines) <= 20 {
		t.Fatalf("settings card height %d looks content-hugged:\n%s", len(cardLines), stripANSI(m.settingsCardView()))
	}
}

func settingsPaintedRowY(m Model, label string) int {
	for i, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if settingsLineHasLabel(line, label) {
			return i
		}
	}
	return -1
}

func settingsPaintedRow(m Model, label string) string {
	for _, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if settingsLineHasLabel(line, label) {
			return line
		}
	}
	return ""
}

func settingsHitX(line string, row int) int {
	switch row {
	case settingsRowLimit, settingsRowCompactAuto, settingsRowAgentsEnabled, settingsRowAgentsWriters, settingsRowAllowlistEnabled:
		if x0, x1, ok := displaySpan(line, "[on]"); ok {
			return (x0 + x1) / 2
		}
		if x0, x1, ok := displaySpan(line, "[off]"); ok {
			return (x0 + x1) / 2
		}
		if hitToggle(line, 10) {
			return 10
		}
	case settingsRowAllowlist:
		if x0, x1, ok := displaySpan(line, "allowed"); ok {
			return (x0 + x1) / 2
		}
	case settingsRowAgentsChildSteps:
		if x0, x1, ok := displaySpan(line, "◂"); ok {
			return (x0 + x1) / 2
		}
	default:
		if x0, x1, ok := displaySpanLast(line, "▸"); ok {
			return (x0 + x1) / 2
		}
		if x0, x1, ok := displaySpan(line, "◂"); ok {
			return (x0 + x1) / 2
		}
	}
	return max(1, lipgloss.Width(line)-2)
}

func settingsHitChanged(before, after Model, row int) bool {
	if after.settingsEdit && !before.settingsEdit {
		return true
	}
	if after.pickerMode && !before.pickerMode {
		return true
	}
	if !after.settingsMode && before.settingsMode {
		return true
	}
	ba, aa := before.projectSettings.Agents, after.projectSettings.Agents
	switch row {
	case settingsRowModel:
		return after.projectSettings.Model.Default != before.projectSettings.Model.Default
	case settingsRowVariant:
		return after.projectSettings.Model.Variant != before.projectSettings.Model.Variant
	case settingsRowChildModel:
		return aa.ModelOverride != ba.ModelOverride
	case settingsRowExploreModel:
		return aa.ExploreModel != ba.ExploreModel
	case settingsRowLimit:
		return after.projectSettings.Slot.LimitEnabled != before.projectSettings.Slot.LimitEnabled
	case settingsRowSteps:
		return after.projectSettings.Slot.MaxSteps != before.projectSettings.Slot.MaxSteps
	case settingsRowCompactAuto:
		return after.projectSettings.Compaction.Auto != before.projectSettings.Compaction.Auto
	case settingsRowCompactPercent:
		return after.projectSettings.Compaction.Percent != before.projectSettings.Compaction.Percent
	case settingsRowAgentsEnabled:
		return aa.Enabled != ba.Enabled
	case settingsRowAgentsRole:
		return aa.DefaultRole != ba.DefaultRole
	case settingsRowAgentsConcurrent:
		return aa.MaxConcurrent != ba.MaxConcurrent
	case settingsRowAgentsQueued:
		return aa.MaxQueued != ba.MaxQueued
	case settingsRowAgentsChildSteps:
		return aa.ChildMaxSteps != ba.ChildMaxSteps
	case settingsRowAgentsTimeout:
		return aa.DefaultTimeoutSec != ba.DefaultTimeoutSec
	case settingsRowAgentsWriters:
		return aa.AllowParallelWriters != ba.AllowParallelWriters
	case settingsRowBashConfirm:
		return aa.BashConfirm != ba.BashConfirm
	case settingsRowAllowlistEnabled:
		return aa.BashAllowlistEnabled != ba.BashAllowlistEnabled
	case settingsRowAllowlist:
		return after.settingsEdit
	default:
		return false
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

func TestSettingsMouseHover(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model).openSettings()

	if m.settingsHover != -1 {
		t.Fatalf("settingsHover on init = %d, want -1", m.settingsHover)
	}

	// Move mouse over row 5 (parent max steps)
	rowY := -1
	for y := 0; y < 30; y++ {
		if r, ok := m.settingsRowAtScreenY(y); ok && r == settingsRowSteps {
			rowY = y
			break
		}
	}
	if rowY < 0 {
		t.Fatal("could not locate settingsRowSteps on screen")
	}

	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: 30, Y: rowY}))
	m = mm.(Model)

	if m.settingsHover != settingsRowSteps {
		t.Fatalf("settingsHover = %d, want %d (settingsRowSteps)", m.settingsHover, settingsRowSteps)
	}
}

func TestSettingsLeftRightArrowBooleans(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = m.openSettings()

	// Navigate to Limit
	m.settingsCursor = settingsRowLimit
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.projectSettings.Slot.LimitEnabled {
		t.Fatal("Left arrow should turn LimitEnabled off")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.projectSettings.Slot.LimitEnabled {
		t.Fatal("Right arrow should turn LimitEnabled on")
	}

	// Navigate to AgentsEnabled
	m.settingsCursor = settingsRowAgentsEnabled
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.projectSettings.Agents.Enabled {
		t.Fatal("Left arrow should turn Agents.Enabled off")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.projectSettings.Agents.Enabled {
		t.Fatal("Right arrow should turn Agents.Enabled on")
	}
}
