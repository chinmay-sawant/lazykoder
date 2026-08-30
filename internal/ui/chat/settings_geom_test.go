package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSettingsGeomMatchesPaintedView ensures mouse hit targets line up with
// the rows actually painted on the full-screen settings card.
func TestSettingsGeomMatchesPaintedView(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()

	view := stripANSI(viewText(m))
	lines := strings.Split(view, "\n")

	var xLine = -1
	for i, line := range lines {
		if strings.Contains(line, "SETTINGS") && strings.Contains(line, "[x]") {
			xLine = i
		}
	}
	if xLine < 0 {
		t.Fatalf("controls missing in view:\n%s", view)
	}

	x0, cy, x1, ok := m.settingsCloseRect()
	if !ok {
		t.Fatal("close rect missing")
	}
	if cy != xLine {
		t.Fatalf("close Y: computed=%d painted=%d listTop=%d", cy, xLine, m.settingsListScreenTop())
	}
	cx0, _, ok := displaySpan(lines[xLine], "[x]")
	if !ok {
		t.Fatal("[x] missing on painted line")
	}
	if cx0 < x0 || cx0 >= x1 {
		t.Fatalf("[x] col %d outside close rect [%d,%d)", cx0, x0, x1)
	}

	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: cx0, Y: xLine, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.settingsMode {
		t.Fatalf("click painted [x] at (%d,%d) did not close", cx0, xLine)
	}

	m = m.openSettingsAt(settingsSectionAgentLoop, settingsRowSteps)
	before := m.projectSettings.Slot.MaxSteps
	stepsLine, decX := paintedControlCol(stripANSI(viewText(m)), "parent max steps", "◂")
	if stepsLine < 0 || decX < 0 {
		t.Fatal("parent max steps / ◂ not painted")
	}
	gotRow, ok := m.settingsRowAtScreenY(stepsLine)
	if !ok || gotRow != settingsRowSteps {
		t.Fatalf("steps Y: painted=%d row=%d ok=%v", stepsLine, gotRow, ok)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: decX, Y: stepsLine, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before-1 {
		t.Fatalf("click ◂ at (%d,%d): got %d want %d", decX, stepsLine, m.projectSettings.Slot.MaxSteps, before-1)
	}
	stepsLine, incX := paintedControlCol(stripANSI(viewText(m)), "parent max steps", "▸")
	if stepsLine < 0 || incX < 0 {
		t.Fatal("parent max steps / ▸ not painted after decrease")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: incX, Y: stepsLine, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before {
		t.Fatalf("click ▸ at (%d,%d): got %d want %d", incX, stepsLine, m.projectSettings.Slot.MaxSteps, before)
	}

	// Model row y is the first focusable painted row.
	m = m.openSettingsAt(settingsSectionModel, settingsRowModel)
	if modelLine := findLineLabel(stripANSI(viewText(m)), "new-session model"); modelLine != m.settingsListScreenTop() {
		t.Fatalf("model Y: painted=%d computed=%d", modelLine, m.settingsListScreenTop())
	}
}

func findLineLabel(view, label string) int {
	for i, line := range strings.Split(view, "\n") {
		if settingsLineHasLabel(line, label) {
			return i
		}
	}
	return -1
}

// paintedControlCol returns the 0-based row and display column of token on
// the first painted line that contains needle. For "▸", the last match is
// used so the value chevron wins over the row selection marker.
func paintedControlCol(view, needle, token string) (row, col int) {
	for i, line := range strings.Split(view, "\n") {
		if !settingsLineHasLabel(line, needle) && !strings.Contains(line, needle) {
			continue
		}
		if needle == "parent max steps" && !settingsLineHasLabel(line, needle) {
			continue
		}
		var start int
		var ok bool
		if token == "▸" {
			start, _, ok = displaySpanLast(line, token)
		} else {
			start, _, ok = displaySpan(line, token)
		}
		if !ok {
			return i, -1
		}
		return i, start
	}
	return -1, -1
}
