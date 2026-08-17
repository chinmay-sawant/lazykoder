package chat

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestBareTETypesIntoPrompt(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "test")
	if got := m.prompt.Value(); got != "test" {
		t.Fatalf("prompt = %q, want %q", got, "test")
	}
}

func TestCtrlETogglesAllTools(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = []transcriptItem{
		{kind: itemTool, collapsed: true, tool: db.ToolCall{Tool: "bash"}},
		{kind: itemTool, collapsed: false, tool: db.ToolCall{Tool: "read"}},
		{kind: itemTool, collapsed: true, tool: db.ToolCall{Tool: "edit"}},
	}
	m.syncTranscript()
	m = upd(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	for i, it := range m.items {
		if it.kind == itemTool && !it.collapsed {
			t.Fatalf("tool %d stayed open after bulk collapse", i)
		}
	}
	m = upd(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	for i, it := range m.items {
		if it.kind == itemTool && it.collapsed {
			t.Fatalf("tool %d stayed closed after bulk expand", i)
		}
	}
}

func TestCtrlPTogglesAllThinking(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = []transcriptItem{
		{kind: itemReasoning, collapsed: true, text: "one"},
		{kind: itemReasoning, collapsed: true, text: "two"},
	}
	m.syncTranscript()
	m = upd(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for i, it := range m.items {
		if it.kind == itemReasoning && it.collapsed {
			t.Fatalf("thinking %d stayed closed after bulk expand", i)
		}
	}
	m.prompt.SetValue("draft")
	m = upd(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for i, it := range m.items {
		if it.kind == itemReasoning && !it.collapsed {
			t.Fatalf("thinking %d stayed open after bulk collapse", i)
		}
	}
	if got := m.prompt.Value(); got != "draft" {
		t.Fatalf("ctrl+p changed prompt: %q", got)
	}
}

func TestClickMetaHeaderTogglesToolsOnly(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = []transcriptItem{
		{kind: itemUser, text: "prompt"},
		{kind: itemReasoning, text: "thought", collapsed: true},
		{kind: itemTool, collapsed: true, tool: db.ToolCall{Tool: "bash", Status: "completed"}},
	}
	m.syncTranscript()
	for _, tc := range []struct {
		needle  string
		expects bool
	}{
		{needle: thinkingLabel, expects: true},
		{needle: "bash", expects: false},
	} {
		needle := tc.needle
		y := viewLineIndex(m, needle)
		if y < 0 {
			t.Fatalf("missing %q header", needle)
		}
		idx, ok := m.itemIndexAtScreenY(y)
		if !ok {
			t.Fatalf("header %q did not map to an item", needle)
		}
		m = upd(m, tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
		if got := m.items[idx].collapsed; got != tc.expects {
			t.Fatalf("click on %q collapsed = %v, want %v", needle, got, tc.expects)
		}
	}
}

func TestHelpMetaKeys(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	text := stripANSI(m.helpOverlay())
	if !strings.Contains(text, "ctrl+e") || !strings.Contains(text, "ctrl+p") {
		t.Fatalf("help missing bulk meta keys: %q", text)
	}
	if strings.Contains(text, "t / e") || strings.Contains(text, "expand last tool") {
		t.Fatalf("help still advertises removed shortcuts: %q", text)
	}
}

func TestSubagentArrowNavigation(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.subagentPickerMode = true
	m.subagentItems = []subagentRow{
		{ID: "agent-1", Name: "agent one", Status: "completed"},
		{ID: "agent-2", Name: "agent two", Status: "completed"},
	}
	m.subagentCursor = 0
	m = m.ensureSubagentBuilt()

	m, _ = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.subagentCursor != 1 {
		t.Fatalf("down arrow cursor=%d, want 1", m.subagentCursor)
	}
	m, _ = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.subagentCursor != 0 {
		t.Fatalf("up arrow cursor=%d, want 0", m.subagentCursor)
	}

	var cmd tea.Cmd
	m, cmd = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if cmd != nil {
		t.Fatal("right arrow returned an unexpected command")
	}
	if !m.subagentLogMode || m.subagentSelected.Name != "agent one" {
		t.Fatalf("right arrow did not open selected log: mode=%v selected=%q", m.subagentLogMode, m.subagentSelected.Name)
	}

	m, _ = m.updateSubagentLogKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.subagentLogMode || m.subagentSelected.Name != "agent two" || m.subagentCursor != 1 {
		t.Fatalf("second right arrow did not open next log: mode=%v selected=%q cursor=%d", m.subagentLogMode, m.subagentSelected.Name, m.subagentCursor)
	}

	m, _ = m.updateSubagentLogKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.subagentLogMode || !m.subagentPickerMode {
		t.Fatalf("left arrow did not return to drawer: log=%v picker=%v", m.subagentLogMode, m.subagentPickerMode)
	}
}

func TestTruncateToolOutputForView(t *testing.T) {
	short := "one\ntwo"
	if got, omitted := truncateToolOutputForView(short); got != short || omitted {
		t.Fatalf("short output = %q, omitted=%v", got, omitted)
	}
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%03d", i)
	}
	got, omitted := truncateToolOutputForView(strings.Join(lines, "\n"))
	if !omitted {
		t.Fatal("line cap did not report omission")
	}
	if !strings.Contains(got, "lines omitted") || !strings.Contains(got, "line-000") || !strings.Contains(got, "line-199") {
		t.Fatalf("head/tail output missing evidence: %q", got)
	}
	if gotLines := len(strings.Split(got, "\n")); gotLines != maxToolBodyLines {
		t.Fatalf("line count = %d, want %d", gotLines, maxToolBodyLines)
	}
	long := strings.Repeat("x", maxToolBodyRunes+100)
	got, omitted = truncateToolOutputForView(long)
	if !omitted || len([]rune(got)) > maxToolBodyRunes || !strings.Contains(got, "output truncated") {
		t.Fatalf("rune cap result invalid: omitted=%v runes=%d output=%q", omitted, len([]rune(got)), got)
	}
}

func TestExpandedBashOutputCapped(t *testing.T) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("bash-line-%03d", i)
	}
	out := strings.Join(lines, "\n")
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	body := stripANSI(m.renderTool(agent.Event{Tool: db.ToolCall{
		Tool: "bash", Status: "completed", Output: &out,
	}}, false, 0))
	if !strings.Contains(body, "lines omitted") || !strings.Contains(body, "bash-line-499") {
		t.Fatalf("capped body missing truncation evidence: %q", body)
	}
}

func TestSubagentLogToolOutputFull(t *testing.T) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("child-line-%03d", i)
	}
	out := strings.Join(lines, "\n")
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.subagentLogItems = []transcriptItem{{kind: itemTool, collapsed: false, tool: db.ToolCall{
		Tool: "bash", Status: "completed", Output: &out,
	}}}
	content := stripANSI(m.renderSubagentLogContent())
	if strings.Contains(content, "lines omitted") || !strings.Contains(content, "child-line-499") {
		t.Fatalf("sub-agent log was capped: %q", content)
	}
}

func TestBulkExpandRespectsUIBudget(t *testing.T) {
	out := strings.Repeat("large output\n", 2000)
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	for i := 0; i < 20; i++ {
		m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: false, tool: db.ToolCall{
			Tool: "bash", Status: "completed", Output: &out,
		}})
	}
	m.syncTranscript()
	content := m.transcriptContent()
	plain := stripANSI(content)
	if got := strings.Count(plain, "lines omitted"); got != 20 {
		t.Fatalf("bulk expansion omission notes = %d, want 20", got)
	}
	if got := strings.Count(plain, "large output"); got > 20*maxToolBodyLines {
		t.Fatalf("expanded transcript painted %d output lines, want at most %d", got, 20*maxToolBodyLines)
	}
}

func TestRenderFingerprintCollapse(t *testing.T) {
	open := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	open.items = []transcriptItem{{kind: itemTool, collapsed: false, tool: db.ToolCall{Tool: "bash"}}}
	open.syncTranscript()
	collapsed := open
	collapsed.items = append([]transcriptItem(nil), open.items...)
	collapsed.items[0].collapsed = true
	if open.renderFingerprint() == collapsed.renderFingerprint() {
		t.Fatal("collapsed and expanded tools shared a render fingerprint")
	}
	same := open
	if open.renderFingerprint() != same.renderFingerprint() {
		t.Fatal("identical models produced different render fingerprints")
	}
}

func TestRenderMemoReusesUnchangedRows(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = []transcriptItem{
		{kind: itemAssistant, text: "historical reply"},
		{kind: itemAssistant, text: "live reply"},
	}
	m.syncTranscript()
	initial := m.renderCache.itemRenders
	if initial != len(m.items) {
		t.Fatalf("initial item renders = %d, want %d", initial, len(m.items))
	}
	m.items[1].text = "live reply changed"
	m.syncTranscript()
	if got := m.renderCache.itemRenders - initial; got != 1 {
		t.Fatalf("changed %d rows after tail update, want 1", got)
	}
}
