package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
