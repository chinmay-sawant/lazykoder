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

func TestMouseWheelScrollsSessionPicker(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	for i := 0; i < 20; i++ {
		sess, err := st.CreateSession(context.Background(), db.Session{
			Title: fmt.Sprintf("sess-%02d", i), Directory: dir,
			TimeCreated: int64(i), TimeUpdated: int64(i),
		})
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
		um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
		if err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
		text := fmt.Sprintf("msg-%02d", i)
		if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
			t.Fatalf("insert part %d: %v", i, err)
		}
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	m = typeText(m, "/resume")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("resume picker not open")
	}
	m = upd(m, tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if m.sessionVp.YOffset() <= 0 {
		t.Fatalf("wheel down did not scroll the session picker (offset %d)", m.sessionVp.YOffset())
	}
	m = upd(m, tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.sessionVp.YOffset() != 0 {
		t.Fatalf("wheel up did not return to top (offset %d)", m.sessionVp.YOffset())
	}
}

func TestJumpBarClickScrollsToLatest(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	for i := 0; i < 80; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = mm.(Model)
	if !m.jumpBarVisible() {
		t.Fatal("jump bar not visible after scrolling up")
	}
	mm, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      10,
		Y:      m.jumpBarRow(),
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if cmd != nil {
		t.Fatalf("jump bar click returned a command %v", cmd)
	}
	if !m.transcript.AtBottom() {
		t.Fatal("jump bar click did not scroll to the bottom")
	}
	if m.jumpBarVisible() {
		t.Fatal("jump bar still visible after jumping to the bottom")
	}
	if strings.Contains(viewText(m), jumpDownArrow) {
		t.Fatal("jump bar arrow still rendered after jumping to the bottom")
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
	if m.copyNotice != "Text copied" {
		t.Fatalf("copy notice = %q, want %q", m.copyNotice, "Text copied")
	}
	if !strings.Contains(stripANSI(viewText(m)), "Text copied") {
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

func TestSelectedTextStripsWorkRailAndUserFrame(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 40
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello prompt", when: 1_700_000_000_000},
		{kind: itemAssistant, text: "hello reply", when: 1_700_000_001_000},
	}
	m.syncTranscript()

	// Full-width selection over every rendered row.
	rows := m.plainTranscriptRows()
	if len(rows) == 0 {
		t.Fatal("no plain transcript rows")
	}
	m.selection = textSelection{
		active: true,
		anchor: textPosition{row: 0, col: 0},
		// Wide enough to cover any rendered row (rails + body + stamp).
		focus: textPosition{row: len(rows) - 1, col: 1 << 20},
	}
	got, ok := m.selectedText()
	if !ok {
		t.Fatal("expected a selection range")
	}
	if strings.Contains(got, workRail) {
		t.Fatalf("clipboard still contains work rail: %q", got)
	}
	if strings.Contains(got, "╭") || strings.Contains(got, "╰") {
		t.Fatalf("clipboard still contains user frame curls: %q", got)
	}
	if !strings.Contains(got, "hello prompt") || !strings.Contains(got, "hello reply") {
		t.Fatalf("message text missing from clipboard: %q", got)
	}
	// Rails must still render on screen.
	body := stripANSI(strings.Join(m.renderedItems(), "\n"))
	if !strings.Contains(body, workRail) {
		t.Fatalf("work rail missing from rendered view: %q", body)
	}
	if !strings.Contains(body, "╭") {
		t.Fatalf("user frame missing from rendered view: %q", body)
	}
}

func TestStripTranscriptChrome(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{workRail + " assistant", "assistant"},
		{workRail + " hello", "hello"},
		{"╭ hello prompt", "hello prompt"},
		{"╰ last line", "last line"},
		{workRail, ""},
		{"plain text", "plain text"},
		{"  indented", "  indented"},
	}
	for _, tc := range cases {
		if got := stripTranscriptChrome(tc.in); got != tc.want {
			t.Errorf("stripTranscriptChrome(%q) = %q, want %q", tc.in, got, tc.want)
		}
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

func TestClickSelectsToolCardWithoutToggle(t *testing.T) {
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
	if m.selectedItem != 0 {
		t.Fatalf("selectedItem = %d, want 0", m.selectedItem)
	}
}

func TestClickSelectsThinkingHeaderWithoutToggle(t *testing.T) {
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
	if !m.items[1].collapsed {
		t.Fatal("click on thinking header changed collapse state")
	}
	if strings.Contains(stripANSI(viewText(m)), "secret thought") {
		t.Fatal("collapsed thinking body appeared after click")
	}
}

func TestClickChevronWithTodosHitsPaintedRow(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.todos = []db.Todo{
		{Content: "inspect layout", Status: db.TodoInProgress},
		{Content: "fix input box", Status: db.TodoPending},
		{Content: "run checks", Status: db.TodoPending},
	}
	out := "pwd output"
	m.items = []transcriptItem{
		{kind: itemUser, text: "look at the layout", when: 1},
		{kind: itemReasoning, text: "planning the click", collapsed: true, when: 1},
		{kind: itemTool, text: "bash", collapsed: true, when: 1, tool: db.ToolCall{
			Tool: "bash", Status: "completed", Output: &out,
		}},
	}
	m.syncTranscript()

	yThink := viewLineIndex(m, thinkingLabel)
	if yThink < 0 {
		t.Fatal("thinking header missing from painted view")
	}
	idx, ok := m.itemIndexAtScreenY(yThink)
	if !ok || m.items[idx].kind != itemReasoning {
		t.Fatalf("painted thinking row %d maps to idx=%d ok=%v (top=%d)", yThink, idx, ok, m.transcriptTop())
	}
	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: yThink, Button: tea.MouseLeft}))
	m = next.(Model)
	if !m.items[idx].collapsed {
		t.Fatalf("click on painted thinking row %d changed collapse state", yThink)
	}

	yBash := lastViewLineIndex(m, "bash")
	if yBash < 0 {
		t.Fatal("bash header missing from painted view")
	}
	idx, ok = m.itemIndexAtScreenY(yBash)
	if !ok || m.items[idx].kind != itemTool {
		t.Fatalf("painted bash row %d maps to idx=%d ok=%v", yBash, idx, ok)
	}
	next, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: yBash, Button: tea.MouseLeft}))
	m = next.(Model)
	if m.items[idx].collapsed {
		t.Fatalf("click on painted bash row %d did not expand the tool", yBash)
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
	if strings.Contains(stripANSI(viewText(m)), thought) {
		t.Fatalf("click on reopened thinking changed collapse state: %q", viewText(m))
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
		t.Fatalf("click on reopened bash did not expand the tool: %q", viewText(m))
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

func TestSessionPickerClickAtScrolledBottom(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	const n = 40
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("scroll-sess-%02d", i)
		if i == n-2 {
			// A wrapped title must still occupy one picker row. Extra
			// lines used to shift every hit target below this entry, so
			// a click on the third-from-last row opened the last session.
			title = "scroll-sess-38\n| Path | Purpose |\n| main.go |"
		}
		if err := insertNamedSession(t, st, dir, title, int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = typeText(m, "/resume")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("resume picker not open")
	}
	for i := 0; i < 80; i++ {
		m = upd(m, tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	}

	lines := strings.Split(stripANSI(viewText(m)), "\n")
	var visible []struct {
		y     int
		title string
	}
	for i, line := range lines {
		if strings.Contains(line, "scroll-sess-") {
			start := strings.Index(line, "scroll-sess-")
			visible = append(visible, struct {
				y     int
				title string
			}{i, line[start : start+len("scroll-sess-00")]})
		}
	}
	if len(visible) < 3 {
		t.Fatalf("expected at least 3 visible sessions, got %d: %q", len(visible), strings.Join(lines, "\n"))
	}
	for _, row := range visible {
		idx, ok := m.sessionIndexAtScreenY(row.y)
		if !ok || idx < 0 || idx >= len(m.sessionItems) {
			t.Fatalf("row %d (%s) did not map to a session", row.y, row.title)
		}
		got := sessionPickerTitle(m.sessionItems[idx])
		if !strings.Contains(got, row.title) && !strings.HasPrefix(got, row.title) {
			t.Fatalf("row %d shows %q but maps to %q", row.y, row.title, got)
		}
	}

	target := visible[len(visible)-3]
	m = upd(m, tea.MouseMotionMsg(tea.Mouse{X: 40, Y: target.y}))
	if m.sessionHover < 0 || m.sessionHover >= len(m.sessionItems) ||
		!strings.Contains(sessionPickerTitle(m.sessionItems[m.sessionHover]), target.title) {
		got := ""
		if m.sessionHover >= 0 && m.sessionHover < len(m.sessionItems) {
			got = m.sessionItems[m.sessionHover].Title
		}
		t.Fatalf("hover y=%d title %q, sessionHover=%d (%q)", target.y, target.title, m.sessionHover, got)
	}
	m = upd(m, tea.MouseClickMsg(tea.Mouse{X: 40, Y: target.y, Button: tea.MouseLeft}))
	if m.sessionPickerMode {
		t.Fatal("click did not close the picker")
	}
	if m.session == nil || !strings.Contains(sessionPickerTitle(*m.session), target.title) {
		got := ""
		if m.session != nil {
			got = m.session.Title
		}
		t.Fatalf("clicked y=%d title %q, loaded %q", target.y, target.title, got)
	}
}

func insertNamedSession(t *testing.T, st *db.Store, dir, title string, ts int64) error {
	t.Helper()
	sess, err := st.CreateSession(context.Background(), db.Session{
		Title: title, Directory: dir, TimeCreated: ts, TimeUpdated: ts,
	})
	if err != nil {
		return fmt.Errorf("create session %q: %w", title, err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		return fmt.Errorf("insert message %q: %w", title, err)
	}
	text := title
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
		return fmt.Errorf("insert part %q: %w", title, err)
	}
	return nil
}
