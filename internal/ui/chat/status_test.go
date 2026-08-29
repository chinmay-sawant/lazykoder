package chat

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
)

func TestStatusPickerTogglesAndPersistsModelSegment(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), Model: "model-a"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: sess.Directory, Session: &sess})
	m.tokensPerSec = 80
	m = m.runSlashStatus()
	if !strings.Contains(statusDrawerText(m), "status") {
		t.Fatalf("status drawer missing: %q", statusDrawerText(m))
	}
	var cmd tea.Cmd
	m, cmd = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("status toggle returned no persistence command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("status persistence returned %v", msg)
	}
	m, _ = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	v := statusDrawerText(m)
	if m.statusSegmentEnabled("model") {
		t.Fatalf("model segment remained enabled: %q", v)
	}
	if !m.statusSegmentEnabled("tps") {
		t.Fatalf("tps segment unexpectedly disabled: %q", v)
	}
	got, err := st.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.StatusSegments[0] == "model" || !containsStatusSegment(got.StatusSegments, "tps") {
		t.Fatalf("persisted segments = %v, want model hidden and tps visible", got.StatusSegments)
	}
}

func TestStatusDrawerOwnsDetailsAndArrowToggle(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.variant = "high"
	m.modelInfos = []modelscache.Info{{ID: m.model, Context: 128000}}
	m.tokensUsed = 1200
	m.cacheHit = 800
	m.cacheMiss = 100
	m.sessionCost = 0.42
	m.tokensPerSec = 80
	m.models = []string{"deepseek-v4-flash", "claude-4"}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model)
	compact := stripANSI(viewText(m))
	if !strings.Contains(compact, "status ▾") {
		t.Fatalf("compact footer missing status control: %q", compact)
	}
	for _, detail := range []string{"deepseek-v4", "hit 800", "$0.420", "models:2"} {
		if !strings.Contains(compact, detail) {
			t.Fatalf("enabled detail %q missing from footer: %q", detail, compact)
		}
	}

	m = m.openStatusDrawer()
	drawer := statusDrawerText(m)
	for _, detail := range []string{"model", "variant", "tokens", "cache", "cost", "tokens/sec", "sub-agents", "models", "scroll", "prompt"} {
		if !strings.Contains(drawer, detail) {
			t.Fatalf("status drawer missing %q: %q", detail, drawer)
		}
	}
	if !m.statusSegmentEnabled("model") || !m.statusSegmentEnabled("variant") {
		t.Fatalf("status details are not enabled by default: %v", m.statusSegments)
	}

	m, _ = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.statusCursor != 1 {
		t.Fatalf("down moved cursor to %d, want variant row 1", m.statusCursor)
	}
	m, _ = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.statusSegmentEnabled("variant") {
		t.Fatal("enter did not hide the selected variant detail")
	}
	m, _ = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.statusMode {
		t.Fatal("left did not close the status drawer")
	}
}

func TestStatusDrawerClickTogglesRow(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model).openStatusDrawer()
	y := m.statusDrawerTop() + 1 + 1 // header plus the variant row
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.statusSegmentEnabled("variant") {
		t.Fatal("click did not hide the variant detail")
	}
}

func TestStatusDrawerClickTogglesRowWhileBusy(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model)
	m.busy = true
	m.activity = "thinking"

	left, top, right, _, ok := m.statusChipRect()
	if !ok {
		t.Fatal("status chip should remain clickable while a prompt is running")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{
		X: (left + right) / 2, Y: top, Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if !m.statusMode {
		t.Fatal("status chip click did not open drawer while busy")
	}

	y := m.statusDrawerTop() + 1 + 1
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{
		X: 4, Y: y, Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if m.statusSegmentEnabled("variant") {
		t.Fatal("busy status row click did not hide the variant detail")
	}
}

func TestStatusDrawerRemainsEditableAfterPromptSubmit(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model)
	m.prompt.SetValue("inspect the project")
	m, _ = m.submit(m.prompt.Value())

	left, top, right, _, ok := m.statusChipRect()
	if !ok {
		t.Fatal("status chip should remain clickable after prompt submit")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{
		X: (left + right) / 2, Y: top, Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if !m.statusMode {
		t.Fatal("status chip click did not open drawer after prompt submit")
	}

	y := m.statusDrawerTop() + 1 + 1
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{
		X: 4, Y: y, Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if m.statusSegmentEnabled("variant") {
		t.Fatal("status row click did not hide the variant detail after prompt submit")
	}
	if m.turnCancel != nil {
		m.turnCancel()
	}
}

func TestModelAndVariantChipsRemainEditableWhileBusy(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.model = "deepseek-v4-flash"
	m.variant = "high"
	m.busy = true
	m.activity = "thinking"

	if _, _, _, _, ok := m.modelStatusRect(); !ok {
		t.Fatal("model chip is not clickable while a prompt is running")
	}
	if _, _, _, _, ok := m.variantStatusRect(); !ok {
		t.Fatal("variant chip is not clickable while a prompt is running")
	}
}

func containsStatusSegment(segments []string, want string) bool {
	for _, segment := range segments {
		if segment == want {
			return true
		}
	}
	return false
}

func (m Model) runSlashStatus() Model {
	m, _ = m.runSlashArg("/status", "")
	return m
}
