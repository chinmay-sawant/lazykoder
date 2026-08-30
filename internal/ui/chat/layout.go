package chat

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// layoutSnap is one frame of outer geometry shared by View paint and mouse
// hit-test. Build via ensureLayout; do not hand-edit fields.
type layoutSnap struct {
	key layoutKey

	transcriptTop int
	transcriptH   int
	composerTop   int
	jumpBarRow    int

	// settings card (only meaningful when settingsMode)
	settingsPaint      string
	settingsCloseX0    int
	settingsCloseY     int
	settingsCloseX1    int
	settingsCloseOK    bool
	settingsRowByY     map[int]int // screen Y -> settings control row
	settingsSectionByY map[int]settingsSection
	settingsFilterY    int
	settingsRailX0     int
	settingsRailX1     int
	settingsContentX0  int
}

// layoutKey fingerprints Model fields that change outer bands or settings paint.
type layoutKey struct {
	w, h                          int
	focus                         focusKind
	slash, picker, subs, status   bool
	subLog, subCompact, history   bool
	hasErr, hasTodo               bool
	settingsEdit                  bool
	settingsEditValue             string
	settingsCursor, settingsHover int
	todosExpanded                 bool
	busy                          bool
	pushPrompt                    bool
}

func (m Model) layoutKey() layoutKey {
	return layoutKey{
		w:                 m.width,
		h:                 m.height,
		focus:             m.currentFocus(),
		slash:             m.slashMode,
		picker:            m.pickerMode,
		subs:              m.subagentPickerMode && !m.subagentLogMode,
		status:            m.statusMode,
		subLog:            m.subagentLogMode,
		subCompact:        m.subagentDrawerCompact,
		history:           m.memoryHistoryMode,
		hasErr:            m.err != "",
		hasTodo:           len(m.todos) > 0,
		settingsEdit:      m.settingsEdit,
		settingsEditValue: m.settingsEditValue,
		settingsCursor:    m.settingsCursor,
		settingsHover:     m.settingsHover,
		todosExpanded:     m.todosExpanded,
		busy:              m.busy,
		pushPrompt:        m.commitPushVisible(),
	}
}

// ensureLayout returns m with a fresh layoutSnap when the fingerprint changed.
func (m Model) ensureLayout() Model {
	key := m.layoutKey()
	if m.layout.key == key {
		return m
	}
	m.layout = m.buildLayout(key)
	return m
}

func (m Model) buildLayout(key layoutKey) layoutSnap {
	snap := layoutSnap{
		key:           key,
		transcriptTop: m.transcriptTop(),
		transcriptH:   m.transcriptRenderHeight(),
		composerTop:   m.composerTop(),
	}
	snap.jumpBarRow = snap.transcriptTop + snap.transcriptH

	if !m.settingsMode {
		return snap
	}
	paint := m.settingsScreen()
	snap.settingsPaint = paint
	snap.settingsRowByY = make(map[int]int)
	snap.settingsSectionByY = make(map[int]settingsSection)
	cardW := lipgloss.Width(m.settingsCardView())
	cardX := max(0, (m.width-cardW)/centerDiv)
	railW, _ := m.settingsWorkspaceWidths()
	snap.settingsRailX0 = cardX + 1 + settingsCardHorzPad
	snap.settingsRailX1 = snap.settingsRailX0 + railW
	snap.settingsContentX0 = snap.settingsRailX1 + 1 + settingsWorkspaceGap
	for i, line := range strings.Split(paint, "\n") {
		plain := ansi.Strip(line)
		if !snap.settingsCloseOK && strings.Contains(plain, "SETTINGS") && strings.Contains(plain, "[x]") {
			if start, end, found := displaySpan(plain, "[x]"); found {
				snap.settingsCloseX0 = max(0, start-1)
				snap.settingsCloseX1 = end + 1
				snap.settingsCloseY = i
				snap.settingsCloseOK = true
			}
		}
		if strings.Contains(plain, "filter settings [/]") {
			snap.settingsFilterY = i
		}
		if section, ok := settingsSectionFromPaintedLine(plain); ok {
			snap.settingsSectionByY[i] = section
		}
		if row, ok := settingsRowFromPaintedLine(plainFromDisplayColumn(plain, snap.settingsContentX0)); ok {
			snap.settingsRowByY[i] = row
		}
	}
	return snap
}

func plainFromDisplayColumn(line string, column int) string {
	if column <= 0 {
		return line
	}
	width := 0
	for index, r := range line {
		runeWidth := lipgloss.Width(string(r))
		if width+runeWidth > column {
			return line[index:]
		}
		width += runeWidth
		if width == column {
			return line[index+utf8.RuneLen(r):]
		}
	}
	return ""
}
