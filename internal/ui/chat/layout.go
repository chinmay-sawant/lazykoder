package chat

import (
	"strings"

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
	settingsPaint   string
	settingsCloseX0 int
	settingsCloseY  int
	settingsCloseX1 int
	settingsCloseOK bool
	settingsRowByY  map[int]int // screen Y -> settings control row
}

// layoutKey fingerprints Model fields that change outer bands or settings paint.
type layoutKey struct {
	w, h                        int
	focus                       focusKind
	slash, picker, subs, status bool
	subLog, subCompact          bool
	hasErr, hasTodo             bool
	settingsEdit                bool
	settingsCursor              int
	todosExpanded               bool
	busy                        bool
}

func (m Model) layoutKey() layoutKey {
	return layoutKey{
		w:              m.width,
		h:              m.height,
		focus:          m.currentFocus(),
		slash:          m.slashMode,
		picker:         m.pickerMode,
		subs:           m.subagentPickerMode && !m.subagentLogMode,
		status:         m.statusMode,
		subLog:         m.subagentLogMode,
		subCompact:     m.subagentDrawerCompact,
		hasErr:         m.err != "",
		hasTodo:        len(m.todos) > 0,
		settingsEdit:   m.settingsEdit,
		settingsCursor: m.settingsCursor,
		todosExpanded:  m.todosExpanded,
		busy:           m.busy,
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
		if row, ok := settingsRowFromPaintedLine(plain); ok {
			snap.settingsRowByY[i] = row
		}
	}
	return snap
}
