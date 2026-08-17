package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func fixtureTurns(m Model, n int, bodyLines int) Model {
	long := strings.Repeat("line of assistant output\n", bodyLines)
	m.items = nil
	for i := 0; i < n; i++ {
		m.items = append(m.items,
			transcriptItem{kind: itemUser, text: "user turn " + strings.Repeat("x", i+1), when: time.Now().UnixMilli()},
			transcriptItem{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
		)
	}
	m.syncTranscript()
	return m
}

func TestUserNavRailJumpAndHover(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	long := strings.Repeat("line of assistant output\n", 20)
	m.items = []transcriptItem{
		{kind: itemUser, text: "first question about setup", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long + "answer one", when: time.Now().UnixMilli()},
		{kind: itemUser, text: "why are you starting sub agents", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long + "answer two", when: time.Now().UnixMilli()},
	}
	m.syncTranscript()
	marks := m.userTurnMarks()
	if len(marks) != 2 {
		t.Fatalf("marks = %d, want 2", len(marks))
	}
	if !strings.Contains(marks[0].Label, "first question") {
		t.Fatalf("label0 = %q", marks[0].Label)
	}
	if !strings.Contains(marks[1].Label, "sub agents") {
		t.Fatalf("label1 = %q", marks[1].Label)
	}

	v := stripANSI(viewText(m))
	if !strings.Contains(v, userNavMarkIdle) && !strings.Contains(v, userNavMarkActive) && !strings.Contains(v, userNavMarkHover) {
		t.Fatalf("rail marks missing from view: %q", v)
	}

	h := m.transcriptRenderHeight()
	if marks[0].ScreenRow != 0 {
		t.Fatalf("first mark row=%d, want 0 (even spread)", marks[0].ScreenRow)
	}
	if marks[1].ScreenRow != h-1 {
		t.Fatalf("second mark row=%d, want %d (even spread)", marks[1].ScreenRow, h-1)
	}

	if col := m.userNavRailColumn(); col != m.width-1 {
		t.Fatalf("railCol=%d, want %d", col, m.width-1)
	}
	// Rail replaces scrollbar: only one chrome column.
	if m.transcriptContentWidth() != m.width-1 {
		t.Fatalf("contentW=%d, want %d", m.transcriptContentWidth(), m.width-1)
	}

	before := m.transcript.YOffset()
	m = m.jumpToUserTurn(1)
	if m.selectedItem != marks[1].ItemIdx {
		t.Fatalf("selectedItem = %d, want %d", m.selectedItem, marks[1].ItemIdx)
	}
	if m.transcript.YOffset() < before && marks[1].ContentY > before {
		t.Fatalf("YOffset moved the wrong way: before=%d after=%d target=%d", before, m.transcript.YOffset(), marks[1].ContentY)
	}
	if m.transcript.YOffset() == 0 && marks[1].ContentY > 5 {
		t.Fatalf("YOffset stayed 0, want jump toward %d", marks[1].ContentY)
	}
	if got := m.activeUserTurnIdx(m.userTurnMarks()); got != 1 {
		t.Fatalf("active after jump=%d, want 1", got)
	}
	// Click keeps the hollow mark + label bubble.
	plain := stripANSI(viewText(m))
	if !strings.Contains(plain, "why are you starting") {
		t.Fatalf("click bubble missing second label:\n%s", plain)
	}

	m.userNavHover = -1
	col := m.userNavRailColumn()
	top := m.transcriptTop()
	y := top + marks[0].ScreenRow
	idx, ok := m.userNavIndexAtScreen(col, y)
	if !ok || idx != 0 {
		t.Fatalf("hover hit idx=%d ok=%v, want 0", idx, ok)
	}
}

func TestUserNavEvenSpacingStaticOnExpand(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	m = fixtureTurns(m, 4, 8)
	// Collapse a later assistant-sized item by swapping in a huge body.
	before := m.userTurnMarks()
	rows := make([]int, len(before))
	for i, mk := range before {
		rows[i] = mk.ScreenRow
	}
	// Even gaps (integer rounding: consecutive diffs differ by at most 1).
	if len(rows) >= 3 {
		d0 := rows[1] - rows[0]
		for i := 2; i < len(rows); i++ {
			d := rows[i] - rows[i-1]
			if d < d0-1 || d > d0+1 {
				t.Fatalf("uneven gaps: %v", rows)
			}
		}
	}
	// Grow the first assistant reply a lot (like expanding a tool).
	m.items[1].text = strings.Repeat("expanded tool output line\n", 80)
	m.syncTranscript()
	after := m.userTurnMarks()
	if len(after) != len(before) {
		t.Fatalf("mark count changed %d -> %d", len(before), len(after))
	}
	for i := range after {
		if after[i].ScreenRow != rows[i] {
			t.Fatalf("mark %d moved on expand: %d -> %d (rows were %v)", i, rows[i], after[i].ScreenRow, rows)
		}
	}
}

func TestUserNavActiveTracksScrollAndShowsBubble(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	long := strings.Repeat("line of assistant output\n", 25)
	m.items = []transcriptItem{
		{kind: itemUser, text: "turn one about todos", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
		{kind: itemUser, text: "turn two about layout", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
		{kind: itemUser, text: "turn three about dots", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
	}
	m.syncTranscript()
	marks := m.userTurnMarks()
	if len(marks) != 3 {
		t.Fatalf("marks=%d", len(marks))
	}
	m.userNavHover = -1
	m.transcript.SetYOffset(0)
	if got := m.activeUserTurnIdx(marks); got != 0 {
		t.Fatalf("top active=%d, want 0", got)
	}
	plain := stripANSI(viewText(m))
	if !strings.Contains(plain, "turn one about todos") {
		t.Fatalf("top scroll bubble missing:\n%s", plain)
	}

	m.transcript.SetYOffset(marks[1].ContentY)
	if got := m.activeUserTurnIdx(marks); got != 1 {
		t.Fatalf("mid active=%d, want 1", got)
	}
	plain = stripANSI(viewText(m))
	if !strings.Contains(plain, "turn two about layout") {
		t.Fatalf("mid scroll bubble missing:\n%s", plain)
	}

	m.transcript.GotoBottom()
	if got := m.activeUserTurnIdx(marks); got != 2 {
		t.Fatalf("bottom active=%d, want 2", got)
	}
	plain = stripANSI(viewText(m))
	if !strings.Contains(plain, "turn three about dots") {
		t.Fatalf("bottom scroll bubble missing:\n%s", plain)
	}
}

func TestUserNavHitBandsPerMark(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	m = fixtureTurns(m, 3, 15)
	marks := m.userTurnMarks()
	if len(marks) != 3 {
		t.Fatalf("marks=%d", len(marks))
	}
	for i, mk := range marks {
		idx, ok := userNavHitIndex(marks, mk.ScreenRow)
		if !ok || idx != i {
			t.Fatalf("exact row %d: idx=%d ok=%v, want %d", mk.ScreenRow, idx, ok, i)
		}
	}
	if len(marks) >= 2 {
		mid := (marks[0].ScreenRow + marks[1].ScreenRow) / 2
		idx, ok := userNavHitIndex(marks, mid)
		if !ok || idx != 0 {
			t.Fatalf("mid %d: idx=%d ok=%v, want 0", mid, idx, ok)
		}
		idx, ok = userNavHitIndex(marks, mid+1)
		if !ok || idx != 1 {
			t.Fatalf("mid+1 %d: idx=%d ok=%v, want 1", mid+1, idx, ok)
		}
	}
	col := m.userNavRailColumn()
	y := m.transcriptTop() + marks[1].ScreenRow
	idx, ok := m.userNavIndexAtScreen(col, y)
	if !ok || idx != 1 {
		t.Fatalf("screen click second: idx=%d ok=%v", idx, ok)
	}
	idx, ok = m.userNavIndexAtScreen(col-1, y)
	if !ok || idx != 1 {
		t.Fatalf("near-rail click second: idx=%d ok=%v", idx, ok)
	}
}

func TestUserNavHoverTooltipAndClick(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	long := strings.Repeat("line of assistant output\n", 20)
	m.items = []transcriptItem{
		{kind: itemUser, text: "Can you create a to-do and a junior to-do", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
		{kind: itemUser, text: "second question about the rail", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
	}
	m.syncTranscript()
	marks := m.userTurnMarks()
	if len(marks) != 2 {
		t.Fatalf("marks=%d", len(marks))
	}
	col := m.userNavRailColumn()
	top := m.transcriptTop()

	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: top + marks[0].ScreenRow}))
	m = mm.(Model)
	if m.userNavHover != 0 {
		t.Fatalf("hover=%d, want 0", m.userNavHover)
	}
	plain := ansi.Strip(viewText(m))
	if !strings.Contains(plain, "Can you create a to-do") {
		t.Fatalf("hover tooltip missing first label:\n%s", plain)
	}
	if !strings.Contains(plain, userNavMarkHover) {
		t.Fatalf("hover mark missing from view")
	}

	m.transcript.GotoBottom()
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{
		X: col, Y: top + marks[1].ScreenRow, Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if m.userNavHover != 1 {
		t.Fatalf("click hover=%d, want 1", m.userNavHover)
	}
	plain = ansi.Strip(viewText(m))
	if !strings.Contains(plain, "second question about the rail") {
		t.Fatalf("click bubble missing:\n%s", plain)
	}
	if got := m.activeUserTurnIdx(m.userTurnMarks()); got != 1 {
		t.Fatalf("active after click=%d, want 1 (yOff=%d)", got, m.transcript.YOffset())
	}

	// Wheel clears hover so the bubble follows the active section.
	mm, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	m = mm.(Model)
	if m.userNavHover != -1 {
		t.Fatalf("hover after wheel=%d, want -1", m.userNavHover)
	}
}

func TestUserNavNoScrollbarWhenRailPresent(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	m = fixtureTurns(m, 2, 30)
	plain := stripANSI(viewText(m))
	if strings.Contains(plain, "░") || strings.Contains(plain, "█") {
		t.Fatalf("transcript scrollbar still shown with user-nav rail:\n%s", plain)
	}
	if _, _, _, ok := m.scrollbarRect(0); ok {
		t.Fatal("scrollbarRect reports transcript bar while rail is active")
	}
	// Marks still paint on the last column.
	found := false
	for _, line := range strings.Split(plain, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			continue
		}
		last := string(runes[len(runes)-1])
		if last == userNavMarkIdle || last == userNavMarkActive || last == userNavMarkHover {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rail mark missing from last column:\n%s", plain)
	}
}

func TestUserNavTopMarkClickable(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	long := strings.Repeat("assistant line\n", 30)
	m.items = []transcriptItem{
		{kind: itemUser, text: "Can you create a to-do", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
		{kind: itemUser, text: "second", when: time.Now().UnixMilli()},
		{kind: itemAssistant, text: long, when: time.Now().UnixMilli()},
	}
	m.syncTranscript()
	m.transcript.SetYOffset(0)
	marks := m.userTurnMarks()
	if len(marks) < 2 {
		t.Fatalf("marks=%d", len(marks))
	}
	if got := m.activeUserTurnIdx(marks); got != 0 {
		t.Fatalf("top active=%d, want 0", got)
	}
	col := m.userNavRailColumn()
	y := m.transcriptTop() + marks[0].ScreenRow
	idx, ok := m.userNavIndexAtScreen(col, y)
	if !ok || idx != 0 {
		t.Fatalf("top mark hit idx=%d ok=%v (screenRow=%d top=%d)", idx, ok, marks[0].ScreenRow, m.transcriptTop())
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.selectedItem != marks[0].ItemIdx {
		t.Fatalf("selectedItem=%d, want %d", m.selectedItem, marks[0].ItemIdx)
	}
}

func TestComposerPinnedToBottom(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = mm.(Model)
	m.items = []transcriptItem{
		{kind: itemUser, text: "hi", when: time.Now().UnixMilli()},
	}
	m.syncTranscript()
	v := stripANSI(viewText(m))
	found := -1
	for i, line := range strings.Split(strings.TrimRight(v, "\n"), "\n") {
		if strings.Contains(line, "enter send") || strings.Contains(line, "ask lazykoder") || strings.Contains(line, "╭") {
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("composer chrome missing:\n%s", v)
	}
	if found < m.height/2 {
		t.Fatalf("composer too high (line %d of height %d):\n%s", found, m.height, v)
	}
}
