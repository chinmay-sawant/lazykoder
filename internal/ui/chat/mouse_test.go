package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestMouseWheelScrollsTranscript(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("expected viewport at bottom after sync")
	}
	mm, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	m = mm.(Model)
	if m.transcript.AtBottom() {
		t.Error("wheel up did not scroll the transcript")
	}
	mm, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	m = mm.(Model)
	if !m.transcript.AtBottom() {
		t.Error("wheel down did not return to bottom")
	}
}

func TestTranscriptDragSelectsAndCopiesText(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.items = append(m.items,
		transcriptItem{kind: itemNote, text: "first message"},
		transcriptItem{kind: itemNote, text: "second message"},
	)
	m.syncTranscript()

	top := m.transcriptTop()
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      0,
		Y:      top,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{
		X:      6,
		Y:      top + 1,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	mm, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{
		X:      6,
		Y:      top + 1,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("mouse release returned nil clipboard command")
	}
	if m.copyNotice != "text copied" {
		t.Fatalf("copy notice = %q, want %q", m.copyNotice, "text copied")
	}
	if !strings.Contains(stripANSI(viewText(m)), "text copied") {
		t.Fatalf("View() missing copy notice: %q", viewText(m))
	}

	got, ok := m.selectedText()
	if !ok {
		t.Fatal("drag did not create a text selection")
	}
	if want := "first message\nsecond"; got != want {
		t.Errorf("selected text = %q, want %q", got, want)
	}
}

func TestScrollbarClickJumpAndDrag(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 80; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("expected at bottom")
	}

	col := m.width - 1
	top := m.transcriptTop()
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: top + 3, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.dragOn {
		t.Fatal("click on scrollbar did not start a drag")
	}
	if m.transcript.AtBottom() {
		t.Error("click-jump did not scroll up")
	}

	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	topPct := m.transcript.ScrollPercent()
	if !m.transcript.AtTop() {
		t.Errorf("drag to top row did not reach top (pct %.2f)", topPct)
	}

	mm, _ = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: col, Y: top}))
	m = mm.(Model)
	if m.dragOn {
		t.Error("release did not end the drag")
	}
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.transcript.AtTop() {
		t.Error("drag continued after release")
	}
}

func TestScrollbarClickIgnoredWithoutOverflow(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	col := m.width - 1
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: 3, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.dragOn {
		t.Error("drag started without overflow")
	}
}

func TestClickTogglesToolCard(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	title := "echo hi"
	m.items = append(m.items, transcriptItem{
		kind:      itemTool,
		collapsed: true,
		tool:      db.ToolCall{Tool: "bash", Status: "completed", Title: &title},
	})
	m.syncTranscript()
	if !m.items[0].collapsed {
		t.Fatal("tool should start collapsed")
	}

	y := viewLineIndex(m, "bash")
	if y < 0 {
		t.Fatal("could not find the bash header in the view")
	}
	if idx, ok := m.itemIndexAtScreenY(y); !ok || idx != 0 {
		t.Fatalf("header row %d maps to item %d ok=%v, want 0", y, idx, ok)
	}

	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.items[0].collapsed {
		t.Fatal("click did not expand the tool card")
	}
	if m.selection.active {
		t.Fatal("click started a text selection")
	}
	if m.selectedItem != 0 {
		t.Fatalf("selectedItem = %d, want 0", m.selectedItem)
	}

	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.items[0].collapsed {
		t.Fatal("second click did not collapse the tool card")
	}
}

func TestClickTogglesThinkingHeader(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.items = append(m.items,
		transcriptItem{kind: itemUser, text: "hi", when: 1},
		transcriptItem{kind: itemReasoning, text: "secret thought", collapsed: true, when: 1},
	)
	m.syncTranscript()
	y := viewLineIndex(m, thinkingLabel)
	if y < 0 {
		t.Fatal("could not find the thinking header in the view")
	}
	if idx, ok := m.itemIndexAtScreenY(y); !ok || idx != 1 {
		t.Fatalf("thinking row %d maps to item %d ok=%v, want 1", y, idx, ok)
	}
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.items[1].collapsed {
		t.Fatal("click on thinking header did not expand")
	}
	if !strings.Contains(stripANSI(viewText(m)), "secret thought") {
		t.Fatal("expanded thinking missing from view")
	}
}

func TestReopenClickTogglesCollapsedAtBottom(t *testing.T) {
	st := newTestStore(t)
	dir := t.TempDir()
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "reopen", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
		if err != nil {
			t.Fatal(err)
		}
		ut := fmt.Sprintf("user-line-%02d", i)
		if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &ut}); err != nil {
			t.Fatal(err)
		}
		am, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "assistant"})
		if err != nil {
			t.Fatal(err)
		}
		at := fmt.Sprintf("assistant-line-%02d", i)
		if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "text", Text: &at}); err != nil {
			t.Fatal(err)
		}
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	thought := "secret-reopen-thought"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "reasoning", Text: &thought}); err != nil {
		t.Fatal(err)
	}
	name := "bash"
	status := "completed"
	callID := "call-reopen"
	toolPart, err := st.InsertPart(context.Background(), db.Part{
		MessageID: am.ID, Type: "tool", ToolName: &name, ToolCallID: &callID, ToolStatus: &status,
	})
	if err != nil {
		t.Fatal(err)
	}
	title := "echo reopen"
	if err := st.InsertToolCall(context.Background(), db.ToolCall{
		PartID: toolPart.ID, Tool: name, CallID: callID, Status: status, Title: &title,
	}); err != nil {
		t.Fatal(err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: dir, Session: &sess})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)

	yThink := lastViewLineIndex(m, thinkingLabel)
	yBash := lastViewLineIndex(m, "bash")
	if yThink < 0 || yBash < 0 {
		t.Fatalf("reopened view missing thinking/bash: %q", viewText(m))
	}
	if idx, ok := m.itemIndexAtScreenY(yThink); !ok || m.items[idx].kind != itemReasoning {
		t.Fatalf("thinking row %d maps to idx=%d ok=%v (offset=%d)", yThink, idx, ok, m.transcript.YOffset())
	}
	if idx, ok := m.itemIndexAtScreenY(yBash); !ok || m.items[idx].kind != itemTool {
		t.Fatalf("bash row %d maps to idx=%d ok=%v (offset=%d)", yBash, idx, ok, m.transcript.YOffset())
	}

	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: yThink, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !strings.Contains(stripANSI(viewText(m)), thought) {
		t.Fatalf("click on reopened thinking did not expand: %q", viewText(m))
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: lastViewLineIndex(m, "bash"), Button: tea.MouseLeft}))
	m = mm.(Model)
	found := false
	for _, it := range m.items {
		if it.kind == itemTool && !it.collapsed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("click on reopened bash did not expand: %q", viewText(m))
	}
}

func TestClickRunsSlashCommand(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash menu did not open")
	}

	y := -1
	for row := 0; row < m.height; row++ {
		idx, ok := m.slashIndexAtScreenY(row)
		if ok && idx < len(m.slashItems) && m.slashItems[idx].name == "/model" {
			y = row
			break
		}
	}
	if y < 0 {
		t.Fatal("could not map a screen row to /model")
	}

	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.slashMode {
		t.Fatal("slash menu still open after click")
	}
	if !m.pickerMode {
		t.Fatal("click on /model did not open the picker")
	}
}
