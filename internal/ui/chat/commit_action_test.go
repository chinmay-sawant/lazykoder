package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/tools/edit"
)

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
}

func TestCommitPushButtonUsesInjectedWorktreeStatus(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	if !m.commitPushVisible() {
		t.Fatal("commit action should be visible during its window")
	}
	if !strings.Contains(stripANSI(m.commitPushRow()), "commit and push") {
		t.Fatal("commit action row missing label")
	}
	rowWidth := lipgloss.Width(m.commitPushRow())
	wantWidth := lipgloss.Width("[ commit and push ]") + 2
	if rowWidth != wantWidth {
		t.Fatalf("commit action width = %d, want compact content width %d", rowWidth, wantWidth)
	}

	m.worktreeDirty = func(context.Context, string) (WorktreeInfo, error) { return WorktreeInfo{Dirty: true}, nil }
	msg := m.checkWorktree()()
	status, ok := msg.(worktreeStatusMsg)
	if !ok || !status.info.Dirty {
		t.Fatalf("worktree status = %#v", msg)
	}
}

func TestCommitPushActivationKeepsPromptWireOnly(t *testing.T) {
	store := newTestStore(t)
	session, err := store.CreateSession(context.Background(), db.Session{Title: "action", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: store, Client: deadClient(), Session: &session, Workdir: session.Directory})
	m.pushPromptUntil = time.Now().Add(time.Minute)
	next, cmd := m.activateCommitPush()
	if cmd == nil || !next.busy || !next.pushPromptBusy {
		t.Fatalf("activation state busy=%v pushBusy=%v cmd=%v", next.busy, next.pushPromptBusy, cmd != nil)
	}
}

func TestCommitDrawerEnterActivatesFocusedAction(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 1}}
	m.commitDrawerActionFocused = true

	next, cmd := m.Update(keyMsg("enter"))
	model := next.(Model)
	if cmd == nil || !model.pushPromptBusy || model.session == nil {
		t.Fatalf("Enter did not create a session and activate the focused action: cmd=%v busy=%v session=%v", cmd != nil, model.pushPromptBusy, model.session != nil)
	}
}

func TestCommitDrawerClickActivatesAction(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 1}}
	x0, y, x1, ok := m.commitDrawerActionRect()
	if !ok {
		t.Fatal("commit action button was not hit-testable")
	}
	next, cmd := m.Update(tea.MouseClickMsg{X: (x0 + x1) / 2, Y: y, Button: tea.MouseLeft})
	model := next.(Model)
	if cmd == nil || !model.pushPromptBusy || model.session == nil {
		t.Fatalf("click did not create a session and activate the action: rect=(%d,%d,%d) cmd=%v busy=%v session=%v", x0, y, x1, cmd != nil, model.pushPromptBusy, model.session != nil)
	}
}

func TestCommitDrawerVisibleMirrorsPushWindow(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	if !m.commitDrawerVisible() {
		t.Fatal("drawer should be visible when push window active")
	}
	view := m.commitDrawerView(m.width)
	if !strings.Contains(stripANSI(view), "Diff") {
		t.Fatalf("drawer view missing header: %q", stripANSI(view))
	}
	if strings.Contains(stripANSI(view), "Commit and push") {
		t.Fatalf("drawer still uses the old title: %q", stripANSI(view))
	}
	if !strings.Contains(stripANSI(view), "commit and push") {
		t.Fatalf("drawer view missing action row: %q", stripANSI(view))
	}
	m.pushPromptUntil = time.Time{}
	if m.commitDrawerVisible() {
		t.Fatal("drawer should hide after collapse")
	}
}

func TestCommitDrawerKeyNavigation(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 1}, {Path: "b.go", Added: 2}}
	m.commitDrawerSelected = 0
	// Esc collapses
	m2, _ := m.handleCommitDrawerKey(keyMsg("esc"))
	if m2.commitDrawerVisible() {
		t.Fatal("esc should collapse drawer")
	}
	// Re-open and move down
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 1}, {Path: "b.go", Added: 2}}
	m.commitDrawerSelected = 0
	m3, _ := m.handleCommitDrawerKey(keyMsg("down"))
	if m3.commitDrawerSelected != 1 {
		t.Fatalf("down: selected=%d want 1", m3.commitDrawerSelected)
	}
	m3.commitDrawerSelected = 1
	m4, _ := m3.handleCommitDrawerKey(keyMsg("up"))
	if m4.commitDrawerSelected != 0 {
		t.Fatalf("up: selected=%d want 0", m4.commitDrawerSelected)
	}
}

func TestCommitDrawerKeyboardReturnsFromActionToFinalFile(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{
		{Path: "a.go", Added: 1, Removed: 1},
		{Path: "b.go", Added: 1, Removed: 1},
	}
	m.commitDiffPreview = map[string]string{
		"a.go": "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old-a\n+new-a\n",
		"b.go": "diff --git a/b.go b/b.go\n@@ -1 +1 @@\n-old-b\n+new-b\n",
	}

	next, _ := m.Update(keyMsg("down"))
	m = next.(Model)
	if m.commitDrawerSelected != 1 || m.commitDrawerActionFocused {
		t.Fatalf("first down should select the final file: selected=%d action=%v", m.commitDrawerSelected, m.commitDrawerActionFocused)
	}
	next, _ = m.Update(keyMsg("down"))
	m = next.(Model)
	if m.commitDrawerSelected != 1 || !m.commitDrawerActionFocused {
		t.Fatalf("down at the final file should focus the action: selected=%d action=%v", m.commitDrawerSelected, m.commitDrawerActionFocused)
	}
	next, _ = m.Update(keyMsg("up"))
	m = next.(Model)
	if m.commitDrawerSelected != 1 || m.commitDrawerActionFocused {
		t.Fatalf("up should return focus to the final file: selected=%d action=%v", m.commitDrawerSelected, m.commitDrawerActionFocused)
	}
	next, _ = m.Update(keyMsg("enter"))
	m = next.(Model)
	if !m.commitDiffDetailMode || m.commitDiffDetailPath != "b.go" {
		t.Fatalf("Enter should open the final file: detail=%v path=%q", m.commitDiffDetailMode, m.commitDiffDetailPath)
	}
}

func TestCommitDrawerEnterOpensSelectedDiff(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 1, Removed: 1}}
	m.commitDiffPreview = map[string]string{
		"a.go": "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old\n+new\n",
	}

	next, _ := m.handleCommitDrawerKey(keyMsg("enter"))
	view := stripANSI(next.frame())
	if !next.commitDiffDetailMode || !next.commitDiffHunkContextMode || !strings.Contains(view, "Diff  ·  a.go") || !strings.Contains(view, "change 1 of 1") || !strings.Contains(view, "+new") {
		t.Fatalf("enter should open the expanded diff, got %q", view)
	}
	next, _ = next.handleCommitDiffDetailKey(keyMsg("esc"))
	view = stripANSI(next.frame())
	if next.commitDiffHunkContextMode || !strings.Contains(view, "change 1 of 1") {
		t.Fatalf("escape should return to the separated change list, got %q", view)
	}
}

func TestCommitDrawerFileSelectionOpensExpandedDiff(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{
		{Path: "a.go", Added: 1, Removed: 1},
		{Path: "b.go", Added: 1, Removed: 1},
	}
	m.commitDiffPreview = map[string]string{
		"a.go": "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old-a\n+new-a\n",
		"b.go": "diff --git a/b.go b/b.go\n@@ -1 +1 @@\n-old-b\n+new-b\n",
	}

	next, _, _ := m.commitDrawerHit(1, m.commitDrawerTop()+1, tea.MouseLeft)
	if !next.commitDiffDetailMode || !next.commitDiffHunkContextMode {
		t.Fatalf("file selection should open expanded code, detail=%v context=%v", next.commitDiffDetailMode, next.commitDiffHunkContextMode)
	}
	if next.commitDiffDetailPath != "a.go" || !strings.Contains(stripANSI(next.frame()), "+new-a") {
		t.Fatalf("expanded file selection path=%q view=%q", next.commitDiffDetailPath, stripANSI(next.frame()))
	}
}

func TestCommitDiffDetailScrollbarStaysOnRightWithoutOrphanRows(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n@@ -1,30 +1,30 @@\n" + strings.Repeat(" line with enough context to overflow the detail viewport\n", 30)
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 30}}
	m.commitDiffPreview = map[string]string{"a.go": raw}
	m = m.openCommitDiffDetail()
	m = m.openCommitDiffHunkContext()

	foundScrollbar := false
	for index, line := range strings.Split(stripANSI(m.commitDiffDetailScreen()), "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "│ "))
		if trimmed == "░" || trimmed == "█" {
			t.Fatalf("orphan scrollbar row %d: %q", index, line)
		}
		track := strings.LastIndexAny(line, "░█")
		if track < 0 {
			continue
		}
		foundScrollbar = true
		rightBorder := strings.LastIndex(line, "│")
		if rightBorder < 0 || track < rightBorder-8 {
			t.Fatalf("scrollbar is not at the right edge on row %d: track=%d right=%d width=%d %q", index, track, rightBorder, lipgloss.Width(line), line)
		}
	}
	if !foundScrollbar {
		t.Fatal("expanded diff did not render a scrollbar")
	}
}

func TestParseWorktreeDiffsKeepsPerFilePayload(t *testing.T) {
	files := []WorktreeFile{{Path: "a.go"}, {Path: "b.go"}}
	raw := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/b.go b/b.go\n@@ -1 +1 @@\n-before\n+after\n"

	diffs := parseWorktreeDiffs(raw, files)
	if !strings.Contains(diffs["a.go"], "+new") {
		t.Fatalf("a.go diff = %q", diffs["a.go"])
	}
	if !strings.Contains(diffs["b.go"], "+after") {
		t.Fatalf("b.go diff = %q", diffs["b.go"])
	}
}

func TestRenderDiffKeepsMultiLineRowsAdjacent(t *testing.T) {
	raw := edit.UnifiedDiff(
		"func old() {\n\treturn 1\n}\n",
		"func new() {\n\treturn 2\n\treturn 3\n}\n",
	)
	if strings.HasSuffix(strings.TrimSuffix(raw, "\n"), " ") {
		t.Fatalf("unified diff contains a synthetic trailing context line: %q", raw)
	}
	rendered := stripANSI(renderDiff(raw, 80))
	for index, line := range strings.Split(rendered, "\n") {
		if line == "" {
			t.Fatalf("rendered diff inserted an empty row at index %d: %q\n%s", index, raw, rendered)
		}
	}
}

func TestCommitDrawerDiffDetailNavigatesWithKeyboardAndMouse(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n@@ -1,30 +1,30 @@\n" + strings.Repeat(" line\n", 30)
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 30}}
	m.commitDiffPreview = map[string]string{"a.go": raw}

	next, _ := m.Update(keyMsg("enter"))
	m = next.(Model)
	if !m.commitDiffDetailMode {
		t.Fatal("enter should open the diff detail view")
	}
	if !m.commitDiffHunkContextMode {
		t.Fatal("enter should open the selected change's code context")
	}
	before := m.commitDiffDetailVp.YOffset()
	next, _ = m.Update(keyMsg("down"))
	m = next.(Model)
	if m.commitDiffDetailVp.YOffset() <= before {
		t.Fatalf("down did not scroll diff detail: before=%d after=%d", before, m.commitDiffDetailVp.YOffset())
	}
	before = m.commitDiffDetailVp.YOffset()
	next, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	m = next.(Model)
	if m.commitDiffDetailVp.YOffset() <= before {
		t.Fatalf("mouse wheel did not scroll diff detail: before=%d after=%d", before, m.commitDiffDetailVp.YOffset())
	}
	next, _ = m.Update(keyMsg("esc"))
	m = next.(Model)
	if !m.commitDiffDetailMode || m.commitDiffHunkContextMode {
		t.Fatal("escape from code context should return to the change list")
	}
	x0, y, _, ok := m.commitDiffDetailCloseRect()
	if !ok {
		t.Fatal("diff detail close button was not hit-testable")
	}
	next, _ = m.Update(tea.MouseClickMsg{X: x0, Y: y, Button: tea.MouseLeft})
	m = next.(Model)
	if m.commitDiffDetailMode {
		t.Fatal("clicking diff detail close button should return to drawer")
	}
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 30}, {Path: "b.go", Added: 1}}
	m.commitDrawerSelected = 0
	next, _ = m.Update(tea.MouseClickMsg{X: 2, Y: m.commitDrawerTop() + 2, Button: tea.MouseRight})
	m = next.(Model)
	if m.commitDrawerSelected != 1 {
		t.Fatalf("right-click file selection = %d, want 1", m.commitDrawerSelected)
	}
}

func TestCommitDrawerDiffDetailNavigatesBetweenFiles(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{
		{Path: "a.go", Added: 1, Removed: 1},
		{Path: "b.go", Added: 1, Removed: 1},
	}
	m.commitDiffPreview = map[string]string{
		"a.go": "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old-a\n+new-a\n",
		"b.go": "diff --git a/b.go b/b.go\n@@ -1 +1 @@\n-old-b\n+new-b\n",
	}

	next, _ := m.Update(keyMsg("enter"))
	m = next.(Model)
	view := stripANSI(m.frame())
	if !strings.Contains(view, "a.go") || !strings.Contains(view, "1 of 2 files") {
		t.Fatalf("first diff detail view = %q", view)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = next.(Model)
	view = stripANSI(m.frame())
	if m.commitDrawerSelected != 1 || !strings.Contains(view, "b.go") || !strings.Contains(view, "2 of 2 files") || !strings.Contains(view, "+new-b") {
		t.Fatalf("next diff detail view selected=%d = %q", m.commitDrawerSelected, view)
	}
	x0, y, _, ok := m.commitDiffDetailNavRect(false)
	if !ok {
		t.Fatal("previous-file mouse control was not hit-testable")
	}
	next, _ = m.Update(tea.MouseClickMsg{X: x0, Y: y, Button: tea.MouseLeft})
	m = next.(Model)
	if m.commitDrawerSelected != 0 || m.commitDiffDetailPath != "a.go" {
		t.Fatalf("previous-file mouse navigation selected=%d path=%q", m.commitDrawerSelected, m.commitDiffDetailPath)
	}
}

func TestCommitDiffHunksSelectAndExpandWithMouse(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n" +
		"@@ -1,2 +1,2 @@ first change\n-old-a\n+new-a\n" +
		"@@ -10,2 +10,2 @@ second change\n-old-b\n+new-b\n"
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	m.commitFiles = []WorktreeFile{{Path: "a.go", Added: 2, Removed: 2}}
	m.commitDiffPreview = map[string]string{"a.go": raw}

	next, _ := m.Update(keyMsg("enter"))
	m = next.(Model)
	view := stripANSI(m.frame())
	if len(m.commitDiffHunks) != 2 || !m.commitDiffHunkContextMode || !strings.Contains(view, "+new-a") || !strings.Contains(view, "+new-b") || !strings.Contains(view, "─") {
		t.Fatalf("expanded first diff section = %q, hunks=%d", view, len(m.commitDiffHunks))
	}
	next, _ = m.Update(keyMsg("esc"))
	m = next.(Model)
	view = stripANSI(m.frame())
	if m.commitDiffHunkContextMode || !strings.Contains(view, "change 1 of 2") || !strings.Contains(view, "─") {
		t.Fatalf("escape should show the separated diff section list = %q", view)
	}
	next, _ = m.Update(keyMsg("down"))
	m = next.(Model)
	if m.commitDiffHunkSelected != 1 || !strings.Contains(stripANSI(m.frame()), "change 2 of 2") {
		t.Fatalf("down selected change=%d view=%q", m.commitDiffHunkSelected, stripANSI(m.frame()))
	}

	_, headerY, _, ok := m.commitDiffDetailCloseRect()
	if !ok {
		t.Fatal("diff detail header was not hit-testable")
	}
	next, _ = m.Update(tea.MouseClickMsg{X: 2, Y: headerY + commitDiffDetailBodyTop + commitDiffHunkRowSpan, Button: tea.MouseLeft})
	m = next.(Model)
	view = stripANSI(m.frame())
	if !m.commitDiffHunkContextMode || !strings.Contains(view, "change 2 of 2") || !strings.Contains(view, "+new-a") || !strings.Contains(view, "+new-b") {
		t.Fatalf("mouse should select the second change while keeping both expanded, got %q", view)
	}
}

func TestDiffSlashCommandOpensWorktreeDrawer(t *testing.T) {
	worktree := WorktreeInfo{
		Dirty:  true,
		Branch: "feature/diff-drawer",
		Files:  []WorktreeFile{{Path: "internal/ui/chat/chat.go", Added: 4, Removed: 2}},
		Diffs: map[string]string{
			"internal/ui/chat/chat.go": "diff --git a/internal/ui/chat/chat.go b/internal/ui/chat/chat.go\n@@ -1 +1 @@\n-old\n+new\n",
		},
	}
	m := New(Options{
		Store:   newTestStore(t),
		Client:  deadClient(),
		Workdir: t.TempDir(),
		WorktreeDirty: func(context.Context, string) (WorktreeInfo, error) {
			return worktree, nil
		},
	})
	m.width = 80
	m.height = 24
	for _, r := range "/diff" {
		m = typeRune(m, r)
	}
	if !m.slashMode || len(m.slashItems) != 1 || m.slashItems[0].name != "/diff" {
		t.Fatalf("slash state = mode:%v items:%+v", m.slashMode, m.slashItems)
	}

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("/diff did not start a worktree check")
	}
	m = next.(Model)
	checked, _ := m.Update(cmd())
	m = checked.(Model)
	if !m.commitDrawerVisible() {
		t.Fatal("/diff did not open the worktree drawer")
	}
	view := stripANSI(m.commitDrawerView(m.width))
	if !strings.Contains(view, "Diff  ·  feature/diff-drawer") {
		t.Fatalf("diff drawer header = %q", view)
	}
	if !strings.Contains(view, "internal/ui/chat/chat.go") || !strings.Contains(view, "+4 -2") {
		t.Fatalf("diff drawer missing file details = %q", view)
	}
	if !strings.Contains(view, "+new") {
		t.Fatalf("diff drawer missing actual diff payload = %q", view)
	}
}

func TestDiffSlashCommandShowsCleanWorktree(t *testing.T) {
	m := New(Options{
		Store:   newTestStore(t),
		Client:  deadClient(),
		Workdir: t.TempDir(),
		WorktreeDirty: func(context.Context, string) (WorktreeInfo, error) {
			return WorktreeInfo{Branch: "master"}, nil
		},
	})
	for _, r := range "/diff" {
		m = typeRune(m, r)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("/diff did not start a worktree check")
	}
	checked, _ := next.(Model).Update(cmd())
	m = checked.(Model)
	if !m.commitDrawerVisible() {
		t.Fatal("/diff should open the drawer for a clean worktree")
	}
	view := stripANSI(m.commitDrawerView(m.width))
	if !strings.Contains(view, "Diff  ·  master") || !strings.Contains(view, "no changes") {
		t.Fatalf("clean diff drawer = %q", view)
	}
}
