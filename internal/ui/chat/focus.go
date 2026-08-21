package chat

// focusKind is the exclusive UI focus owner. Open helpers call setFocus so
// sibling *Mode bools are cleared in one place; Update / frame / mouse read
// the same state via currentFocus (or the bools it keeps in sync).
type focusKind int

const (
	focusNone focusKind = iota
	focusConfirm
	focusAsk
	focusHelp
	focusUsage
	focusSettings
	focusFilePicker
	focusStatus
	focusPicker
	focusSessions
	focusSubagents
	focusSubagentLog
	focusSlash
	focusForm
)

// setFocus enters k. Full-screen and primary drawers clear sibling
// user-navigation modes. Confirm/Ask and the @ file picker only flip their
// own flags (matching prior open helpers) so chrome under them stays put.
func (m Model) setFocus(k focusKind) Model {
	switch k {
	case focusForm:
		m.formMode = true
		return m
	case focusConfirm:
		m.confirmMode = true
		m.askMode = false
		return m
	case focusAsk:
		m.askMode = true
		m.confirmMode = false
		return m
	case focusFilePicker:
		m.filePickerMode = true
		return m
	case focusPicker:
		m.pickerMode = true
		return m
	case focusSlash:
		m.slashMode = true
		return m
	case focusNone:
		m.confirmMode = false
		m.askMode = false
		m.formMode = false
		return m
	}

	m.slashMode = false
	m.slashCursor = 0
	m.pickerMode = false
	m.pickerFiltering = false
	m.helpMode = false
	m.usageMode = false
	m.usageLoading = false
	m.settingsMode = false
	m.filePickerMode = false
	m.sessionPickerMode = false
	m.statusMode = false
	m.subagentPickerMode = false
	m.subagentLogMode = false
	m.formMode = false

	switch k {
	case focusHelp:
		m.helpMode = true
	case focusUsage:
		m.usageMode = true
	case focusSettings:
		m.settingsMode = true
	case focusStatus:
		m.statusMode = true
	case focusSessions:
		m.sessionPickerMode = true
	case focusSubagents:
		m.subagentPickerMode = true
		m.subagentLogMode = false
	case focusSubagentLog:
		m.subagentPickerMode = true
		m.subagentLogMode = true
	}
	return m
}

// clearFocus drops only k (or subagent pair). Other modes stay put.
func (m Model) clearFocus(k focusKind) Model {
	switch k {
	case focusForm:
		m.formMode = false
		m.formHost = nil
	case focusConfirm:
		m.confirmMode = false
	case focusAsk:
		m.askMode = false
	case focusHelp:
		m.helpMode = false
	case focusUsage:
		m.usageMode = false
		m.usageLoading = false
	case focusSettings:
		m.settingsMode = false
	case focusFilePicker:
		m.filePickerMode = false
	case focusStatus:
		m.statusMode = false
	case focusPicker:
		m.pickerMode = false
		m.pickerFiltering = false
		m.pickerFromPrompt = false
	case focusSessions:
		m.sessionPickerMode = false
	case focusSubagents, focusSubagentLog:
		m.subagentPickerMode = false
		m.subagentLogMode = false
		m.subagentDrawerCompact = false
	case focusSlash:
		m.slashMode = false
		m.slashCursor = 0
	}
	return m
}

// currentFocus returns the highest-priority active mode. Order matches the
// Update key cascade and frame() full-screen branches.
func (m Model) currentFocus() focusKind {
	switch {
	case m.formMode:
		return focusForm
	case m.confirmMode:
		return focusConfirm
	case m.askMode:
		return focusAsk
	case m.helpMode:
		return focusHelp
	case m.usageMode:
		return focusUsage
	case m.settingsMode:
		return focusSettings
	case m.statusMode:
		return focusStatus
	case m.filePickerMode:
		return focusFilePicker
	case m.pickerMode:
		return focusPicker
	case m.sessionPickerMode:
		return focusSessions
	case m.subagentLogMode:
		return focusSubagentLog
	case m.subagentPickerMode:
		return focusSubagents
	case m.slashMode:
		return focusSlash
	default:
		return focusNone
	}
}

// promptEditing is true when keystrokes should go to the composer.
// The sub-agent list drawer keeps the composer active; the full-screen log does not.
func (m Model) promptEditing() bool {
	switch m.currentFocus() {
	case focusNone, focusSubagents:
		return true
	default:
		return false
	}
}
