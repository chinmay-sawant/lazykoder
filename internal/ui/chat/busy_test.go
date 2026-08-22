package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestBusyStatusShowsWorkingAndActions(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.width, m.height = 80, 24
	m.busy = true
	m.activity = "bash  sleep 30"
	m.pulseOn = true
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "working") {
		t.Fatalf("missing working label: %q", v)
	}
	if !strings.Contains(v, "esc cancel") {
		t.Fatalf("missing cancel hint: %q", v)
	}
	if !strings.Contains(v, "bash") {
		t.Fatalf("missing activity: %q", v)
	}
	// Draft ready -> send now hint.
	m.prompt.SetValue("stop and do this instead")
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "enter send now") {
		t.Fatalf("missing send now hint with draft: %q", v)
	}
}

func TestForceSendInterruptsAndSubmits(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir()})
	m.width, m.height = 80, 24
	m.busy = true
	m.activity = "stuck tool"
	m.pulseOn = true
	cancelled := false
	m.turnCancel = func() { cancelled = true }
	m.turnSeq = 2
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "old work", when: time.Now().UnixMilli()})

	next, cmd := m.forceSend("new instruction")
	if !cancelled {
		t.Fatal("expected turnCancel to run")
	}
	if !next.busy {
		t.Fatal("forceSend should start a new busy turn")
	}
	if next.turnSeq <= 2 {
		t.Fatalf("turnSeq = %d, want > 2 after interrupt+submit", next.turnSeq)
	}
	if next.prompt.Value() != "" {
		t.Fatalf("prompt should clear, got %q", next.prompt.Value())
	}
	foundUser, foundNote := false, false
	for _, it := range next.items {
		if it.kind == itemUser && it.text == "new instruction" {
			foundUser = true
		}
		if it.kind == itemNote && strings.Contains(it.text, "interrupted") {
			foundNote = true
		}
	}
	if !foundUser {
		t.Fatalf("missing new user message in items: %+v", next.items)
	}
	if !foundNote {
		t.Fatal("missing interrupted note")
	}
	if cmd == nil {
		t.Fatal("expected send cmds")
	}
}

func TestEnterWhileBusyEmptyDraftHints(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.busy = true
	m.activity = "thinking"
	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(Model)
	if m.copyNotice == "" || !strings.Contains(m.copyNotice, "send now") {
		t.Fatalf("copyNotice = %q, want send now hint", m.copyNotice)
	}
	if !m.busy {
		t.Fatal("empty enter should not cancel the turn")
	}
}

func TestEscWhileBusyCancels(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.busy = true
	m.activity = "thinking"
	cancelled := false
	m.turnCancel = func() { cancelled = true }
	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = mm.(Model)
	if !cancelled {
		t.Fatal("esc should cancel")
	}
	if m.busy {
		t.Fatal("should not be busy after cancel")
	}
	if m.err != "cancelled" {
		t.Fatalf("err = %q", m.err)
	}
}

func TestLiveWorkRailUsesPulsingVerticalMark(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.busy = true
	m.pulseOn = true
	m.pulse = 0
	railDim := m.workRailLive(true)
	m.pulse = 1
	railLit := m.workRailLive(true)
	railStatic := m.workRailLive(false)

	if stripANSI(railDim) != workRail || stripANSI(railLit) != workRail {
		t.Fatalf("live work rail should stay a vertical mark, got dim=%q lit=%q", railDim, railLit)
	}
	if railDim == railLit {
		t.Fatalf("live work rail should pulse as the frame advances: dim=%q lit=%q", railDim, railLit)
	}
	if stripANSI(railStatic) != workRail {
		t.Fatalf("workRailLive(false) should contain only %q, got %q", workRail, railStatic)
	}
}

func TestToolStatusMarkUsesGreenAnimatedBatonWhileRunning(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.pulse = 0
	first := m.toolStatusMark("running")
	m.pulse = 1
	second := m.toolStatusMark("running")
	wantFirst := lipgloss.NewStyle().Foreground(theme.ColorGood()).Render(theme.StatusBatonFrame(0))

	if first != wantFirst {
		t.Fatalf("running mark = %q, want green baton frame %q", first, wantFirst)
	}
	if first == second {
		t.Fatalf("running mark should animate until completion: first=%q second=%q", first, second)
	}
}

func TestToolStatusMarkUsesStaticRedBatonOnFailure(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.pulse = 4
	got := m.toolStatusMark("failed")
	want := lipgloss.NewStyle().Foreground(theme.ColorDanger()).Render(theme.StatusBatonFrame(0))

	if got != want {
		t.Fatalf("failed mark = %q, want static red baton frame %q", got, want)
	}
}

func TestPulseStaysOnUntilInFlightToolCompletes(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.busy = true
	m.pulseOn = true
	m.items = []transcriptItem{{
		kind: itemTool,
		tool: db.ToolCall{Tool: "reboot", Status: "running"},
	}}

	m = m.finishTurn(nil)
	if !m.pulseOn {
		t.Fatal("pulse should remain active while the reboot call is still running")
	}
	first := m.pulse
	next, _ := m.Update(pulseMsg{})
	m = next.(Model)
	if !m.pulseOn || m.pulse == first {
		t.Fatalf("pulse should advance for an in-flight reboot call: on=%v pulse=%d first=%d", m.pulseOn, m.pulse, first)
	}

	m.items[0].tool.Status = "completed"
	next, _ = m.Update(pulseMsg{})
	m = next.(Model)
	if m.pulseOn {
		t.Fatal("pulse should stop after the reboot call reaches a terminal status")
	}
}

func TestBusyStatusUsesPlasmaBlobUntilTurnEnds(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.busy = true
	m.pulseOn = true
	m.activity = "running"

	working := stripANSI(m.liveStatusView())
	if !strings.Contains(working, stripANSI(m.plasmaBlob())) {
		t.Fatalf("busy status should use plasma blob: %q", working)
	}

	m.busy = false
	finished := stripANSI(m.liveStatusView())
	if !strings.Contains(finished, workRail) {
		t.Fatalf("finished activity should use the fixed work rail: %q", finished)
	}
}

func TestPlasmaBlobAnimatesWithPulse(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.pulseOn = true

	m.pulse = 0
	first := stripANSI(m.plasmaBlob())
	m.pulse = pulseSteps / 2
	if stripANSI(m.plasmaBlob()) == first {
		t.Fatal("plasma blob did not advance with the pulse")
	}
}

func TestPlasmaBlobUsesAccentGlyphsWithoutBrackets(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	rendered := stripANSI(m.plasmaBlob())

	if strings.ContainsAny(rendered, "[]") {
		t.Fatalf("plasma blob should not contain traffic brackets: %q", rendered)
	}
	if len([]rune(rendered)) != plasmaBlobWidth {
		t.Fatalf("plasma blob width = %d, want %d: %q", len([]rune(rendered)), plasmaBlobWidth, rendered)
	}
}

func TestWorkingMarkUsesPlasmaBlock(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.pulseOn = true
	rendered := stripANSI(m.plasmaBlob())

	if strings.ContainsAny(rendered, "[]") {
		t.Fatalf("working mark should not contain traffic brackets: %q", rendered)
	}
	if !strings.ContainsAny(rendered, "░▒▓█") {
		t.Fatalf("working mark should contain plasma blocks: %q", rendered)
	}
}
