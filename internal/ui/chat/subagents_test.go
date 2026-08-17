package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestSubagentPickerListsChildrenAndShowsLog(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := st.CreateSession(context.Background(), db.Session{
		Directory:       workdir,
		Title:           "agent_alpha",
		ParentSessionID: &pid,
		Kind:            db.SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: child.ID, Role: "user", Agent: "agent_alpha"})
	if err != nil {
		t.Fatalf("user msg: %v", err)
	}
	prompt := "explore the tree"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &prompt}); err != nil {
		t.Fatalf("user part: %v", err)
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: child.ID, Role: "assistant", Agent: "agent_alpha"})
	if err != nil {
		t.Fatalf("asst msg: %v", err)
	}
	thought := "scanning packages carefully"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "reasoning", Text: &thought}); err != nil {
		t.Fatalf("reasoning part: %v", err)
	}
	reply := "found three packages"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "text", Text: &reply}); err != nil {
		t.Fatalf("asst part: %v", err)
	}
	status := "completed"
	toolName := "bash"
	callID := "c1"
	toolPart, err := st.InsertPart(context.Background(), db.Part{
		MessageID: am.ID, Type: "tool", ToolName: &toolName, ToolStatus: &status, ToolCallID: &callID,
	})
	if err != nil {
		t.Fatalf("tool part: %v", err)
	}
	title := "ls -la"
	out := "ok"
	if err := st.InsertToolCall(context.Background(), db.ToolCall{
		PartID: toolPart.ID, Tool: "bash", CallID: "c1", Status: "completed",
		Title: &title, InputJSON: `{"command":"ls -la"}`, Output: &out,
	}); err != nil {
		t.Fatalf("tool call: %v", err)
	}

	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m.subMgr = subagent.NewManager(subagent.NewConfig(), nil)
	m = m.openSubagentPicker()
	if !m.subagentPickerMode {
		t.Fatal("expected subagent drawer open")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "sub-agents") {
		t.Fatalf("drawer missing header: %q", v)
	}
	if !strings.Contains(v, "agent_alpha") {
		t.Fatalf("drawer missing child name: %q", v)
	}
	if !strings.Contains(v, theme.StatusDiamond) {
		t.Fatalf("drawer missing status diamond: %q", v)
	}

	m, _ = m.openSelectedSubagentLog()
	if !m.subagentLogMode {
		t.Fatal("expected log mode")
	}
	// Full-screen: viewport height uses entire terminal minus header/footer.
	if m.subagentLogVp.Height() < m.height-3 {
		t.Fatalf("log viewport height %d too small for full screen (h=%d)", m.subagentLogVp.Height(), m.height)
	}
	logView := stripANSI(viewText(m))
	if !strings.Contains(logView, "SUB-AGENT") {
		t.Fatalf("log missing title: %q", logView)
	}
	if !strings.Contains(logView, "found three packages") {
		t.Fatalf("log missing child text: %q", logView)
	}
	if !strings.Contains(logView, "explore the tree") {
		t.Fatalf("log missing prompt: %q", logView)
	}
	// Main chat design: expanded thinking, tool diamond, work rail.
	if !strings.Contains(logView, thinkingLabel) {
		t.Fatalf("log missing thinking header: %q", logView)
	}
	if !strings.Contains(logView, "scanning packages carefully") {
		t.Fatalf("thinking should be expanded by default: %q", logView)
	}
	if !strings.Contains(logView, theme.StatusDiamond) {
		t.Fatalf("log missing tool diamond: %q", logView)
	}
	if !strings.Contains(logView, workRail) {
		t.Fatalf("log missing work rail: %q", logView)
	}
	// Collapse thinking with t.
	m, _ = m.updateSubagentLogKey(tea.KeyPressMsg{Code: 't'})
	collapsed := stripANSI(viewText(m))
	if strings.Contains(collapsed, "scanning packages carefully") {
		t.Fatalf("t should collapse thinking: %q", collapsed)
	}

	// Click the thinking header to expand again.
	thinkIdx := -1
	for i, it := range m.subagentLogItems {
		if it.kind == itemReasoning {
			thinkIdx = i
			break
		}
	}
	if thinkIdx < 0 {
		t.Fatal("missing reasoning item")
	}
	// Find a screen Y that maps to the thinking item.
	m = m.resizeSubagentLogCard()
	foundY := -1
	for y := 0; y < m.height; y++ {
		if idx, ok := m.subagentLogItemIndexAtScreenY(y); ok && idx == thinkIdx {
			foundY = y
			break
		}
	}
	if foundY < 0 {
		t.Fatal("could not map thinking item to a screen row")
	}
	next, _, hit := m.subagentLogHit(2, foundY, tea.MouseLeft)
	if !hit {
		t.Fatal("click on thinking should hit")
	}
	m = next
	if m.subagentLogItems[thinkIdx].collapsed {
		t.Fatal("click should expand collapsed thinking")
	}
	if !strings.Contains(stripANSI(viewText(m)), "scanning packages carefully") {
		t.Fatal("expanded thinking body missing after click")
	}

	// Footer chip should list total for the session.
	m = m.closeSubagentPicker()
	m = m.reloadSubagentRows()
	label := m.subsStatusLabel()
	if label != "subs:1" {
		t.Fatalf("subs label = %q, want subs:1", label)
	}
}

func TestSubagentDrawerNoLiveDBDuplicate(t *testing.T) {
	// Live job with ChildSessionID must not also appear as a second store row.
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := st.CreateSession(context.Background(), db.Session{
		Directory: workdir, Title: "agent-1", ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}

	cfg := subagent.NewConfig()
	cfg.MaxConcurrent = 4
	mgr := subagent.NewManager(cfg, &instantRunner{childID: child.ID})
	mgr.SetRuntime(subagent.Runtime{Workdir: workdir})

	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m.subMgr = mgr
	rows := m.collectSubagentRows()
	if len(rows) != 1 {
		t.Fatalf("store-only rows = %d, want 1", len(rows))
	}

	if _, err := mgr.Spawn(context.Background(), parent.ID, "prt_1", subagent.Spec{
		Name: "agent-1", Prompt: "hi", Background: false,
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	m.subMgr = mgr
	rows = m.collectSubagentRows()
	if len(rows) != 1 {
		t.Fatalf("after live+store merge rows = %d want 1: %+v", len(rows), rows)
	}
	if rows[0].ChildSessionID != child.ID && rows[0].Name != "agent-1" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

type instantRunner struct {
	childID string
}

func (r *instantRunner) Run(ctx context.Context, job subagent.Job) (subagent.Result, error) {
	if job.OnSession != nil && r.childID != "" {
		job.OnSession(r.childID)
	}
	return subagent.Result{
		ID: job.ID, Name: job.Name, Role: job.Role,
		Status: string(subagent.StatusCompleted), Summary: "hello world",
		ChildSessionID: r.childID,
	}, nil
}

func TestAgentsSlashOpensPicker(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{Store: st, Workdir: t.TempDir()})
	m, _ = m.runSlash("/agents")
	if !m.subagentPickerMode {
		t.Fatal("/agents should open subagent picker")
	}
}

func TestSubagentLogCloseXClick(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := st.CreateSession(context.Background(), db.Session{
		Directory:       workdir,
		Title:           "agent_alpha",
		ParentSessionID: &pid,
		Kind:            db.SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: child.ID, Role: "user", Agent: "agent_alpha"})
	if err != nil {
		t.Fatalf("msg: %v", err)
	}
	text := "hello from child"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("part: %v", err)
	}

	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m.width = 100
	m.height = 36
	m = m.openSubagentPicker()
	m, _ = m.openSelectedSubagentLog()
	if !m.subagentLogMode {
		t.Fatal("expected log mode")
	}
	x0, cy, x1, ok := m.subagentLogCloseRect()
	if !ok {
		t.Fatal("subagentLogCloseRect not found")
	}
	// Confirm painted [x] sits inside the hit rect.
	lines := strings.Split(stripANSI(viewText(m)), "\n")
	if cy < 0 || cy >= len(lines) {
		t.Fatalf("close Y %d out of view (%d lines)", cy, len(lines))
	}
	cx0, _, found := displaySpan(lines[cy], "[x]")
	if !found {
		t.Fatalf("[x] missing on painted line %d: %q", cy, lines[cy])
	}
	if cx0 < x0 || cx0 >= x1 {
		t.Fatalf("[x] col %d outside close rect [%d,%d)", cx0, x0, x1)
	}
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: cx0, Y: cy, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.subagentLogMode {
		t.Fatalf("click painted [x] at (%d,%d) did not leave log mode", cx0, cy)
	}
	if !m.subagentPickerMode {
		t.Fatal("click [x] should return to drawer list")
	}
}

func TestSubagentDrawerKeepsComposerVisible(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	for i := 0; i < 6; i++ {
		title := fmt.Sprintf("agent_%d", i)
		if _, err := st.CreateSession(context.Background(), db.Session{
			Directory:       workdir,
			Title:           title,
			ParentSessionID: &pid,
			Kind:            db.SessionKindSubagent,
		}); err != nil {
			t.Fatalf("child %d: %v", i, err)
		}
	}
	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m.width = 80
	m.height = 24
	m = m.openSubagentPicker()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "sub-agents") {
		t.Fatalf("missing drawer: %q", v)
	}
	// Composer chrome must still be painted (rounded input box footer).
	if !strings.Contains(v, "enter send") && !strings.Contains(v, "ask lazykoder") {
		// prompt placeholder or send hint depending on idle/busy
		if !strings.Contains(v, "╭") || !strings.Contains(v, "╰") {
			t.Fatalf("composer box missing while drawer open:\n%s", v)
		}
	}
	// Drawer + chrome must not exceed the terminal height.
	lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("view height %d > terminal %d (composer pushed off)", len(lines), m.height)
	}
}

func TestSubagentDrawerAllowsTranscriptMouse(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	for i := 0; i < 3; i++ {
		if _, err := st.CreateSession(context.Background(), db.Session{
			Directory: workdir, Title: fmt.Sprintf("child-%d", i),
			ParentSessionID: &pid, Kind: db.SessionKindSubagent,
		}); err != nil {
			t.Fatalf("child: %v", err)
		}
	}
	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	for i := 0; i < 80; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.width = 100
	m.height = 36
	m = m.openSubagentPicker()
	m.syncTranscript()
	if !m.subagentPickerMode {
		t.Fatal("drawer should be open")
	}
	// Wheel over the transcript (row 2) scrolls chat, not the drawer list.
	if !m.transcript.AtBottom() {
		t.Fatal("expected transcript at bottom before wheel")
	}
	beforeSub := m.subagentVp.YOffset()
	mm, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{X: 5, Y: 2, Button: tea.MouseWheelUp}))
	m = mm.(Model)
	if m.transcript.AtBottom() {
		t.Fatal("wheel over transcript should scroll the chat behind the drawer")
	}
	if m.subagentVp.YOffset() != beforeSub {
		t.Fatalf("wheel over transcript moved drawer offset %d -> %d", beforeSub, m.subagentVp.YOffset())
	}
	// Click in the transcript band must not map to a drawer row.
	txTop := m.transcriptTop()
	if _, ok := m.subagentIndexAtScreenY(txTop + 1); ok {
		t.Fatal("transcript Y should not hit a sub-agent row")
	}
	// Click well below the drawer list must not hit either.
	if _, ok := m.subagentIndexAtScreenY(m.height - 2); ok {
		t.Fatal("composer Y should not hit a sub-agent row")
	}
}

func TestSubagentDrawerAllowsTypingAndPaste(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	if _, err := st.CreateSession(context.Background(), db.Session{
		Directory: workdir, Title: "child-a", ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m = m.openSubagentPicker()
	if !m.subagentPickerMode {
		t.Fatal("drawer should be open")
	}
	m = typeText(m, "hello draft")
	if got := m.prompt.Value(); got != "hello draft" {
		t.Fatalf("typing with drawer open = %q, want hello draft", got)
	}
	if !m.subagentPickerMode {
		t.Fatal("typing should not close the drawer")
	}
	m = upd(m, tea.PasteMsg{Content: " and paste"})
	if got := m.prompt.Value(); got != "hello draft and paste" {
		t.Fatalf("paste with drawer open = %q", got)
	}
}

func TestSyncSubagentDrawerDoesNotForceOpen(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	if _, err := st.CreateSession(context.Background(), db.Session{
		Directory: workdir, Title: "child-a", ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}

	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m = m.syncSubagentDrawer()
	if m.subagentPickerMode {
		t.Fatal("syncSubagentDrawer must not force-open the drawer for existing children")
	}
	if len(m.subagentItems) != 1 {
		t.Fatalf("rows = %d, want 1 for footer chip", len(m.subagentItems))
	}
	if got := m.subsStatusLabel(); got != "subs:1" {
		t.Fatalf("footer label = %q, want subs:1", got)
	}
}

func TestCloseDrawerStaysClosedUntilNewSpawn(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	if _, err := st.CreateSession(context.Background(), db.Session{
		Directory: workdir, Title: "child-a", ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}

	m := New(Options{Store: st, Workdir: workdir, Session: &parent})
	m = m.openSubagentPicker()
	if !m.subagentPickerMode {
		t.Fatal("expected open")
	}
	m = m.closeSubagentPicker()
	if m.subagentPickerMode {
		t.Fatal("expected closed")
	}

	// finishTurn / sync after a plain user command must not re-open.
	m = m.syncSubagentDrawer()
	if m.subagentPickerMode {
		t.Fatal("drawer re-opened after close on sync (plain command path)")
	}

	// Same job set via openSubagentDrawerIfNew (task_wait-style) stays closed.
	m = m.openSubagentDrawerIfNew()
	if m.subagentPickerMode {
		t.Fatal("drawer re-opened when no new job ids")
	}

	// A brand-new child reloads rows but does not steal the transcript.
	if _, err := st.CreateSession(context.Background(), db.Session{
		Directory: workdir, Title: "child-b", ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	}); err != nil {
		t.Fatalf("child-b: %v", err)
	}
	m = m.openSubagentDrawerIfNew()
	if m.subagentPickerMode {
		t.Fatal("drawer should stay closed on spawn; use /agents or the subs chip")
	}
	if len(m.subagentItems) != 2 {
		t.Fatalf("rows = %d, want 2", len(m.subagentItems))
	}
	if got := m.subsStatusLabel(); !strings.Contains(got, "subs:") {
		t.Fatalf("subs chip missing after spawn: %q", got)
	}
}
