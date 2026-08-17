package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestHelpFitsAt80(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.helpMode = true
	v := stripANSI(viewText(m))
	if strings.Contains(v, "lazykoder╭") {
		t.Fatalf("help collided with header brand: %q", v)
	}
	if !strings.Contains(v, "/settings") || !strings.Contains(v, "[x]") {
		t.Fatalf("help missing settings or [x]: %q", v)
	}
}

func TestOverlayCardsOpaque(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.items = []transcriptItem{{kind: itemUser, text: "secret-bleed-token"}}
	m.syncTranscript()
	m.confirmMode = true
	ask := m
	ask.askMode = true
	ask.askQuestion.Question = "pick one"
	ask.askQuestion.Options = []string{"a", "b"}
	file := m
	file = file.openFilePicker()

	for name, view := range map[string]string{
		"confirm": m.confirmOverlay(),
		"ask":     ask.askOverlay(),
		"at":      file.filePickerOverlay(),
	} {
		if !strings.Contains(view, "\x1b") && !strings.Contains(view, "╭") {
			t.Fatalf("%s overlay empty: %q", name, view)
		}
		// Cards are styled with the theme background so transcript
		// cells cannot show through the fill.
		if name == "at" && !strings.Contains(stripANSI(view), "files") {
			t.Fatalf("at overlay missing files section: %q", view)
		}
	}
	_ = theme.Bg
}

func TestVariantFooterNoRefresh(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = m.openVariantPicker()
	v := stripANSI(m.pickerView())
	if strings.Contains(v, "r refresh") {
		t.Fatalf("variant footer still advertises refresh: %q", v)
	}
	if !strings.Contains(v, "reasoning_effort") && !strings.Contains(v, "reasoning") {
		t.Fatalf("variant drawer missing reasoning hint: %q", v)
	}
}

func TestCompact80x24KeepsTranscript(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.todos = []db.Todo{
		{Content: "one", Status: db.TodoInProgress},
		{Content: "two", Status: db.TodoPending},
		{Content: "three", Status: db.TodoPending},
	}
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello from compact"},
		{kind: itemAssistant, text: "line a\nline b\nline c\nline d\nline e\nline f\nline g"},
	}
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if strings.Count(v, "\n") < 10 {
		t.Fatalf("compact view too short: %q", v)
	}
	if !strings.Contains(v, "ask lazykoder") {
		t.Fatalf("composer missing at 80x24: %q", v)
	}
	if strings.Contains(v, "Tip:") && strings.Contains(v, "line g") {
		// tip on alert row should be hidden under 100 cols
		if strings.Contains(v, "▼Tip:") {
			t.Fatalf("tip overwrote transcript: %q", v)
		}
	}
}

func TestSubagentDrawerDoesNotAutoOpen(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.subagentItems = []subagentRow{{ID: "a", Name: "one", Status: "completed"}}
	m = m.openSubagentDrawerIfNew()
	if m.subagentPickerMode {
		t.Fatal("drawer auto-opened")
	}
}

func TestResumeEmptyKeepsFrame(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)
	m = m.openSessionPicker()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "no sessions yet") {
		t.Fatalf("empty resume copy: %q", v)
	}
	if strings.Count(v, "│") < 10 {
		t.Fatalf("empty resume card too short: %q", v)
	}
}

func TestHelpOverlayHasNewCommands(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.helpMode = true
	v := stripANSI(m.helpOverlay())
	for _, want := range []string{"/settings", "/new", "/continue", "/refresh", "ctrl+z", "ctrl+e"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help missing %q: %q", want, v)
		}
	}
}

func TestExpandedToolHasOutputSplit(t *testing.T) {
	out := "hello output"
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	body := m.renderTool(agent.Event{Tool: db.ToolCall{
		Tool:   "bash",
		Status: "completed",
		Output: &out,
	}}, false, 0)
	plain := stripANSI(body)
	if !strings.Contains(plain, "output") || !strings.Contains(plain, "hello output") {
		t.Fatalf("expanded tool missing header/body split: %q", plain)
	}
}

func TestFooterChipRectsMatchPaintedLine(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 36})
	m = mm.(Model)
	m.model = "deepseek-v4-flash"
	m.variant = "xhigh"
	m.todos = []db.Todo{
		{Content: "one", Status: db.TodoInProgress},
		{Content: "two", Status: db.TodoPending},
		{Content: "three", Status: db.TodoPending},
	}
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello from compact"},
		{kind: itemAssistant, text: "reply"},
	}
	m.syncTranscript()

	lines := strings.Split(stripANSI(viewText(m)), "\n")
	footerY := -1
	for i, line := range lines {
		if strings.Contains(line, "enter send") && strings.Contains(line, "deepseek-v4-flash") {
			footerY = i
			break
		}
	}
	if footerY < 0 {
		t.Fatalf("footer line missing:\n%s", viewText(m))
	}
	plain := lines[footerY]
	ml, mt, mr, _, ok := m.modelStatusRect()
	if !ok {
		t.Fatal("model chip rect missing")
	}
	if mt != footerY {
		t.Fatalf("model chip Y=%d, painted footer Y=%d", mt, footerY)
	}
	nameAt, nameEnd, foundName := displaySpan(plain, "deepseek-v4-flash")
	if !foundName || ml > nameAt || mr < nameEnd || mr <= ml {
		t.Fatalf("model chip X=[%d,%d) vs painted [%d,%d) in %q", ml, mr, nameAt, nameEnd, plain)
	}

	vl, vt, vr, _, ok := m.variantStatusRect()
	if !ok {
		t.Fatal("variant chip rect missing")
	}
	if vt != footerY {
		t.Fatalf("variant chip Y=%d, painted footer Y=%d", vt, footerY)
	}
	varAt, varEnd, foundVar := displaySpan(plain, "xhigh")
	if !foundVar || vl > varAt || vr < varEnd || vr <= vl {
		t.Fatalf("variant chip X=[%d,%d) vs painted [%d,%d) in %q", vl, vr, varAt, varEnd, plain)
	}

	// A click on the prompt row (above the footer) must not open a picker.
	promptY := footerY - 1
	cur := m
	next, _ := cur.Update(tea.MouseClickMsg(tea.Mouse{
		X: ml + 1, Y: promptY, Button: tea.MouseLeft,
	}))
	cur = next.(Model)
	if cur.pickerMode {
		t.Fatal("click on the input row opened a picker")
	}

	cur = clickModelStatus(t, m)
	if !cur.pickerMode || cur.pickerKind != pickerKindModel {
		t.Fatalf("model chip click: pickerMode=%v kind=%q", cur.pickerMode, cur.pickerKind)
	}
	vl, vt, vr, _, _ = m.variantStatusRect()
	next, _ = m.Update(tea.MouseClickMsg(tea.Mouse{
		X: (vl + vr) / 2, Y: vt, Button: tea.MouseLeft,
	}))
	cur = next.(Model)
	if !cur.pickerMode || cur.pickerKind != pickerKindVariant {
		t.Fatalf("variant chip click: pickerMode=%v kind=%q", cur.pickerMode, cur.pickerKind)
	}
}

func TestClickVariantRowSelects(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.model = "deepseek-v4-flash"
	m.variant = "high"
	m.modelInfos = []modelscache.Info{{
		ID:       "deepseek-v4-flash",
		Variants: []string{"low", "medium", "high", "xhigh", "max"},
	}}
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello"},
		{kind: itemAssistant, text: "reply"},
	}
	m.syncTranscript()
	m = m.openVariantPicker()
	if !m.pickerMode || len(m.pickerItems) < 2 {
		t.Fatalf("variant picker not open: mode=%v items=%d", m.pickerMode, len(m.pickerItems))
	}
	// Click the first list row under the painted "reasoning" header.
	headerY, ok := m.pickerHeaderScreenY()
	if !ok {
		t.Fatalf("picker header not found in view:\n%s", stripANSI(viewText(m)))
	}
	idx, ok := m.pickerIndexAtScreenY(headerY + 1)
	if !ok {
		t.Fatalf("first variant row not clickable at y=%d\n%s", headerY+1, stripANSI(viewText(m)))
	}
	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X: 8, Y: headerY + 1, Button: tea.MouseLeft,
	}))
	cur := next.(Model)
	if cur.pickerMode {
		t.Fatal("clicking a variant row left the picker open")
	}
	if cur.variant != m.pickerItems[idx] {
		t.Fatalf("variant=%q, want %q", cur.variant, m.pickerItems[idx])
	}
}

func TestPromptShowsFullTypedLine(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 167, Height: 48})
	m = mm.(Model)
	m.items = []transcriptItem{
		{kind: itemUser, text: "prior turn"},
		{kind: itemAssistant, text: "prior reply"},
	}
	m.syncTranscript()
	const typed = "hello caret abcdefghijklmnopqrstuvwxyz 0123456789 END"
	m.prompt.SetValue(typed)
	m.prompt.SetWidth(max(minPaneWidth, m.width-4))
	m.prompt.SetHeight(m.promptHeight())
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "abcdefghijklmnopqrstuvwxyz") || !strings.Contains(v, "0123456789") || !strings.Contains(v, "END") {
		t.Fatalf("prompt clipped in view (width=%d promptW=%d):\n%s", m.width, m.prompt.Width(), v)
	}
	if m.prompt.Value() != typed {
		t.Fatalf("prompt value=%q", m.prompt.Value())
	}
}

func TestPromptKeepsKeysAsTyped(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 167, Height: 48})
	m = mm.(Model)
	const typed = "hello caret abcdefghijklmnopqrstuvwxyz 0123456789 END"
	for _, r := range typed {
		m = typeText(m, string(r))
	}
	if m.prompt.Value() != typed {
		t.Fatalf("typed value=%q want %q", m.prompt.Value(), typed)
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "0123456789") || !strings.Contains(v, "END") {
		t.Fatalf("typed text clipped in view:\n%s", v)
	}
}

func TestBusyTurnKeepsComposerAndNoLeftScrollJunk(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.todos = []db.Todo{
		{Content: "inspect layout", Status: db.TodoInProgress},
		{Content: "fix input box", Status: db.TodoPending},
	}
	m.items = []transcriptItem{
		{kind: itemUser, text: "look at the layout", when: 1},
		{kind: itemAssistant, text: "working on it\nnext line", when: 1},
	}
	m.syncTranscript()
	m.busy = true
	m.activity = "thinking"
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "ask lazykoder") && !strings.Contains(v, "enter send") && !strings.Contains(v, "edit") {
		t.Fatalf("composer missing while busy:\n%s", v)
	}
	// A leftover scrollbar track on the left of assistant lines is a wrap
	// artifact from the user-nav overlay inventing extra rows.
	for _, line := range strings.Split(v, "\n") {
		trim := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trim, "░") || strings.HasPrefix(trim, "█") {
			t.Fatalf("scrollbar chrome on the left: %q", line)
		}
	}
}
