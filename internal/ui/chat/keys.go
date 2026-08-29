package chat

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
)

type errMsg struct {
	err error
}

// isPromptNewline reports keys that should insert a newline in the composer
// instead of submitting. Shift+Enter needs terminal key-disambiguation
// (Kitty protocol / bubbletea keyboard enhancements); Alt+Enter and Ctrl+J
// are reliable fallbacks on terminals that still fold Shift+Enter into Enter.
func isPromptNewline(key tea.KeyPressMsg) bool {
	if key.Code == tea.KeyEnter && (key.Mod.Contains(tea.ModShift) || key.Mod.Contains(tea.ModAlt)) {
		return true
	}
	if key.Mod.Contains(tea.ModCtrl) && (key.Code == 'j' || key.Code == 'J') {
		return true
	}
	switch strings.ToLower(key.String()) {
	case "shift+enter", "alt+enter", "ctrl+enter":
		return true
	}
	switch strings.ToLower(key.Keystroke()) {
	case "shift+enter", "alt+enter", "ctrl+enter":
		return true
	}
	return false
}

func isUndoKey(key tea.KeyPressMsg) bool {
	if key.Mod.Contains(tea.ModCtrl) && (key.Code == 'z' || key.Code == 'Z' || key.Code == 26) {
		return true
	}
	switch strings.ToLower(key.String()) {
	case "ctrl+z", "ctrl+shift+z":
		return true
	}
	switch strings.ToLower(key.Keystroke()) {
	case "ctrl+z", "ctrl+shift+z":
		return true
	}
	return false
}

func (m Model) updateKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	m.copyNotice = ""
	// Keep composer mouse selection only for ctrl+c / ctrl+a; any other key clears it.
	keepPromptSel := key.Mod.Contains(tea.ModCtrl) &&
		(key.Code == 'c' || key.Code == 'C' || key.Code == 'a' || key.Code == 'A')
	if !keepPromptSel {
		m = m.clearPromptSelection()
	}
	if key.Code != 'c' && key.Code != 'C' {
		m = m.clearTextSelection()
	}
	if key.Code != tea.KeyEscape {
		m.escapePending = false
	}

	if m.commitDrawerVisible() {
		empty := strings.TrimSpace(m.prompt.Value()) == ""
		switch key.Code {
		case tea.KeyEscape:
			return m.handleCommitDrawerKey(key)
		case tea.KeyUp, tea.KeyDown, tea.KeyEnter:
			if empty {
				if key.Code == tea.KeyEnter {
					return m.handleCommitDrawerKey(key)
				}
				if next, cmd := m.handleCommitDrawerKey(key); cmd != nil || next.commitDrawerSelected != m.commitDrawerSelected || next.pushPromptUntil != m.pushPromptUntil {
					return next, cmd
				}
			}
		case 'j', 'k':
			if empty && (key.String() == "j" || key.String() == "k") {
				if next, cmd := m.handleCommitDrawerKey(key); cmd != nil || next.commitDrawerSelected != m.commitDrawerSelected {
					return next, cmd
				}
			}
		}
	}

	// Soft newline in the composer (shift+enter / alt+enter / ctrl+j).
	// Handled before select-all is cleared and before plain Enter submits.
	if isPromptNewline(key) {
		m.quitConfirm = false
		m.historyCursor = -1
		m.historyDraft = ""
		m = m.rememberPrompt()
		if m.promptSelectAll {
			m.prompt.SetValue("\n")
			m.promptSelectAll = false
		} else {
			m.prompt.InsertString("\n")
		}
		m.prompt.SetHeight(m.promptHeight())
		return m.syncPromptSlash(), nil
	}

	// Ctrl+A selection: backspace/delete clears the whole draft; typing replaces it.
	if m.promptSelectAll && (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) {
		m.quitConfirm = false
		m.historyCursor = -1
		m.historyDraft = ""
		m.promptSelectAll = false
		m = m.rememberPrompt()
		m.prompt.SetValue("")
		m.prompt.SetHeight(m.promptHeight())
		m.slashFromPaste = false
		return m.syncPromptSlash(), nil
	}
	if m.promptSelectAll && key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		m.quitConfirm = false
		m.historyCursor = -1
		m.historyDraft = ""
		m.promptSelectAll = false
		m = m.rememberPrompt()
		m.prompt.SetValue(key.Text)
		m.prompt.SetHeight(m.promptHeight())
		return m.syncPromptSlash(), nil
	}

	// Keep the select-all highlight only for ctrl+a (set) and ctrl+c (copy).
	// Plain 'a' must clear it; the old check treated any 'a' as sticky.
	if !(key.Mod.Contains(tea.ModCtrl) && (key.Code == 'a' || key.Code == 'A' || key.Code == 'c' || key.Code == 'C')) {
		m.promptSelectAll = false
	}

	if key.Mod.Contains(tea.ModCtrl) {
		switch key.Code {
		case 'g', 'G':
			return m.activateCommitPush()
		case 'a', 'A':
			m.promptSelectAll = true
			m.quitConfirm = false
			m.prompt.CursorEnd()
			return m, nil
		case 'c', 'C':
			m.quitConfirm = false
			// Only copy when text is actively selected (via ctrl+a, mouse drag in composer,
			// transcript selection, or history browsing).
			if text, ok := m.selectedPromptText(); ok {
				m.copyNotice = "Text copied"
				return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
			}
			if text, ok := m.selectedText(); ok {
				m.copyNotice = "Text copied"
				return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
			}
			if item, ok := m.selectedHistoryItem(); ok {
				m.copyNotice = "Text copied"
				return m, tea.Batch(tea.SetClipboard(item.text), clearCopyNotice())
			}
			// When text is not selected, ctrl+c clears the composer draft.
			if m.prompt.Value() != "" {
				m = m.rememberPrompt()
				m.prompt.SetValue("")
				m.prompt.SetHeight(m.promptHeight())
				m.historyCursor = -1
				m.historyDraft = ""
				m.slashFromPaste = false
				m.promptSelectAll = false
				m = m.clearPromptSelection()
				return m.syncPromptSlash(), nil
			}
			return m, nil
		case 'e', 'E':
			// Toggle last tool card (including edit) even while the prompt has text.
			m.quitConfirm = false
			return m.toggleAllTools(), nil
		case 'p', 'P':
			m.quitConfirm = false
			return m.toggleAllReasoning(), nil
		case 'z', 'Z':
			m.quitConfirm = false
			return m.undoPrompt(), nil
		}
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m, cmd
	}
	if strings.HasPrefix(m.prompt.Value(), "/") && (key.Text != "" || key.Code == tea.KeyBackspace) {
		m.historyCursor = -1
		m.historyDraft = ""
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncPromptSlash(), cmd
	}
	switch key.Code {
	case tea.KeyEscape:
		if m.busy {
			// esc while busy = cancel in-flight turn (and live sub-agents).
			return m.cancelTurn(), nil
		}
		if m.escapePending {
			m.prompt.SetValue("")
			m.escapePending = false
			m.historyCursor = -1
			m.historyDraft = ""
			m.promptUndo = nil
			m.slashFromPaste = false
			m.promptSelectAll = false
			return m, nil
		}
		m.escapePending = true
		return m, nil
	case tea.KeyEnter:
		// Plain enter submits; shift/alt+enter already handled above.
		text := m.prompt.Value()
		if m.busy {
			// Force-send: interrupt the stuck/running turn, then send the draft.
			if strings.TrimSpace(text) == "" {
				m.copyNotice = "type a message, then enter to send now  •  esc cancel"
				return m, clearCopyNotice()
			}
			return m.forceSend(text)
		}
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		if name, extra, ok := parseCompactSubmit(text); ok {
			return m.runSlashArg(name, extra)
		}
		return m.submit(text)
	case '?':
		if m.prompt.Value() == "" {
			return m.setFocus(focusHelp), nil
		}
	case tea.KeyBackspace:
		m.historyCursor = -1
		m.historyDraft = ""
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncPromptSlash(), cmd
	case 'c', 'C':
		if text, ok := m.selectedPromptText(); ok {
			m.copyNotice = "Text copied"
			return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
		}
		if text, ok := m.selectedText(); ok {
			return m, tea.SetClipboard(text)
		}
		if item, ok := m.selectedHistoryItem(); ok {
			return m, tea.SetClipboard(item.text)
		}
	case 'd', 'D':
		if m.historyCursor >= 0 {
			return m.deleteSelectedHistory()
		}
	case tea.KeyUp:
		if m.promptCanMoveUp() {
			var cmd tea.Cmd
			m.prompt, cmd = m.prompt.Update(key)
			return m, cmd
		}
		if m.historyCursor >= 0 || !m.promptHasMultipleLines() {
			if len(m.inputHistory) > 0 {
				return m.navigateHistory(-1), nil
			}
		}
		if m.promptHasMultipleLines() {
			return m, nil
		}
		m.transcript.ScrollUp(1)
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	case tea.KeyDown:
		if m.promptCanMoveDown() {
			var cmd tea.Cmd
			m.prompt, cmd = m.prompt.Update(key)
			return m, cmd
		}
		if m.historyCursor >= 0 {
			return m.navigateHistory(1), nil
		}
		m.transcript.ScrollDown(1)
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	case tea.KeyPgUp:
		m.transcript.PageUp()
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	case tea.KeyPgDown:
		m.transcript.PageDown()
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	case tea.KeyHome:
		if m.prompt.Value() != "" {
			var cmd tea.Cmd
			m.prompt, cmd = m.prompt.Update(key)
			return m, cmd
		}
		m.transcript.GotoTop()
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	case tea.KeyEnd:
		if m.prompt.Value() != "" {
			var cmd tea.Cmd
			m.prompt, cmd = m.prompt.Update(key)
			return m, cmd
		}
		m.transcript.GotoBottom()
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	}
	m.historyCursor = -1
	m.historyDraft = ""
	m = m.rememberPrompt()
	if key.Text == "@" && !m.slashFromPaste {
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		m = m.syncPromptSlash()
		return m.openFilePicker(), cmd
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(key)
	if m.filePickerMode && key.Text != "" {
		m.filePickerFilter += key.Text
		m.filePickerItems = m.listAtPickItems(m.filePickerFilter)
		m.filePickerCursor = 0
	}
	return m.syncPromptSlash(), cmd
}

func (m Model) updateConfirmKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	cm, cmd := m.confirm.Update(key)
	m.confirm = cm
	if res := m.confirm.Result(); res != nil {
		if m.pendingAsk != nil {
			return m.resolveAsk(res.Allow), nil
		}
		return m.resolveConfirm(res.Allow), nil
	}
	if cmd != nil {
		return m.closeDone(), tea.Quit
	}
	return m, nil
}

func (m Model) submit(text string) (Model, tea.Cmd) {
	m.prompt.SetValue("")
	m.pendingUser = text
	m.turnHasNewUser = true
	m.historyCursor = -1
	m.historyDraft = ""
	m.pendingHistoryIndex = len(m.inputHistory)
	m.inputHistory = append(m.inputHistory, inputHistoryItem{text: text})
	// UI shows the typed text; the model receives @agent:… expansions.
	m.items = append(m.items, transcriptItem{kind: itemUser, text: text, when: time.Now().UnixMilli()})
	m.turnItemFrom = len(m.items)
	m.pendingCompactReason = ""
	m.compactHint = ""
	sendText := m.withMentionContext(text)
	return m.startTurn(turnStart{
		activity: "thinking",
		run: func(ctx context.Context, ag *agent.Agent, eventCh chan agent.Event) error {
			return ag.Send(ctx, sendText, eventCh)
		},
	})
}

func (m Model) runCompact(extra string) (Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	if m.session == nil || m.store == nil {
		m.err = "nothing to compact"
		return m, nil
	}
	m.prompt.SetValue("")
	m.pendingUser = ""
	m.turnHasNewUser = false
	m.turnItemFrom = len(m.items)
	m.compactHint = ""
	return m.startTurn(turnStart{
		activity:   "compacting",
		compacting: true,
		run: func(ctx context.Context, ag *agent.Agent, eventCh chan agent.Event) error {
			return ag.Compact(ctx, eventCh, agent.CompactReasonManual, extra)
		},
	})
}

// runContinue resumes after a step-limit stop, or sends a normal "continue"
// user turn when the session was not step-limited.
func (m Model) runContinue() (Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	if m.stepLimitHit && m.session != nil {
		return m.resumeAfterLimit()
	}
	return m.submit("continue")
}

// resumeAfterLimit runs another MaxSteps budget without a new user message.
func (m Model) resumeAfterLimit() (Model, tea.Cmd) {
	m.prompt.SetValue("")
	m.pendingUser = ""
	m.turnHasNewUser = false
	m.historyCursor = -1
	m.historyDraft = ""
	m.pendingHistoryIndex = -1
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "continuing…"})
	m.turnItemFrom = len(m.items)
	return m.startTurn(turnStart{
		activity: "thinking",
		run: func(ctx context.Context, ag *agent.Agent, eventCh chan agent.Event) error {
			return ag.Continue(ctx, eventCh)
		},
	})
}

func (m Model) watchEvents(seq int) tea.Cmd {
	return func() tea.Msg {
		if m.eventCh == nil {
			return eventDoneMsg{seq: seq}
		}
		ev, ok := <-m.eventCh
		if !ok {
			var err error
			if m.errCh != nil {
				err = <-m.errCh
			}
			return eventDoneMsg{seq: seq, err: err}
		}
		return eventMsg{seq: seq, ev: ev}
	}
}

func (m Model) cancelTurn() Model {
	if m.turnCancel != nil {
		m.turnCancel()
	}
	if m.subMgr != nil {
		parentID := ""
		if m.session != nil {
			parentID = m.session.ID
		}
		if parentID != "" {
			_ = m.subMgr.CancelAll(parentID)
		}
	}
	m.turnSeq++
	m.busy = false
	m.pendingUser = ""
	m.turnHasNewUser = false
	m.activity = ""
	m.pulseOn = false
	m.err = "cancelled"
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "cancelled"})
	m.syncTranscript()
	m.turnCancel = nil
	m.eventCh = nil
	m.errCh = nil
	return m
}

// forceSend interrupts the in-flight turn (if any) and immediately starts a
// new user turn with text. This is the "send now" action while busy.
func (m Model) forceSend(text string) (Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		return m, nil
	}
	if m.busy {
		// Quiet cancel: stop work without a sticky red "cancelled" error,
		// then submit the new message.
		if m.turnCancel != nil {
			m.turnCancel()
		}
		if m.subMgr != nil && m.session != nil {
			_ = m.subMgr.CancelAll(m.session.ID)
		}
		m.turnSeq++
		m.busy = false
		m.pendingUser = ""
		m.turnHasNewUser = false
		m.activity = ""
		m.pulseOn = false
		m.turnCancel = nil
		m.eventCh = nil
		m.errCh = nil
		m.err = ""
		m.items = append(m.items, transcriptItem{kind: itemNote, text: "interrupted · sending now"})
		m.syncTranscript()
	}
	return m.submit(text)
}

func parseCompactSubmit(text string) (name, extra string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "/compact" || strings.HasPrefix(trimmed, "/compact ") {
		return "/compact", strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact")), true
	}
	return "", "", false
}

func (m Model) rememberPrompt() Model {
	state := promptUndoState{value: m.prompt.Value(), slashFromPaste: m.slashFromPaste}
	if len(m.promptUndo) > 0 && m.promptUndo[len(m.promptUndo)-1] == state {
		return m
	}
	m.promptUndo = append(m.promptUndo, state)
	if len(m.promptUndo) > promptUndoLimit {
		m.promptUndo = m.promptUndo[len(m.promptUndo)-promptUndoLimit:]
	}
	return m
}

func (m Model) undoPrompt() Model {
	if len(m.promptUndo) == 0 {
		return m
	}
	state := m.promptUndo[len(m.promptUndo)-1]
	m.promptUndo = m.promptUndo[:len(m.promptUndo)-1]
	m.prompt.SetValue(state.value)
	m.prompt.CursorEnd()
	m.prompt.SetHeight(m.promptHeight())
	m.slashFromPaste = state.slashFromPaste
	m.slashMode = false
	m.slashCursor = 0
	m.escapePending = false
	m.historyCursor = -1
	m.historyDraft = ""
	m.promptSelectAll = false
	return m.syncPromptSlash()
}

func (m Model) selectedHistoryItem() (inputHistoryItem, bool) {
	if m.historyCursor < 0 || m.historyCursor >= len(m.inputHistory) {
		return inputHistoryItem{}, false
	}
	return m.inputHistory[m.historyCursor], true
}

func (m Model) navigateHistory(delta int) Model {
	if len(m.inputHistory) == 0 {
		return m
	}
	if m.historyCursor < 0 {
		if delta > 0 {
			return m
		}
		m.historyDraft = m.prompt.Value()
		m.historyCursor = len(m.inputHistory) - 1
	} else {
		next := m.historyCursor + delta
		if next < 0 {
			next = 0
		}
		if next >= len(m.inputHistory) {
			m.historyCursor = -1
			m.prompt.SetValue(m.historyDraft)
			m.prompt.SetHeight(m.promptHeight())
			m.historyDraft = ""
			m.promptSelectAll = false
			return m
		}
		m.historyCursor = next
	}
	m.prompt.SetValue(m.inputHistory[m.historyCursor].text)
	m.prompt.SetHeight(m.promptHeight())
	m.escapePending = false
	m.promptUndo = nil
	m.slashFromPaste = false
	m.promptSelectAll = false
	return m
}

func (m Model) deleteSelectedHistory() (Model, tea.Cmd) {
	item, ok := m.selectedHistoryItem()
	if !ok {
		return m, nil
	}
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].kind != itemUser || m.items[i].text != item.text {
			continue
		}
		m.items = append(m.items[:i], m.items[i+1:]...)
		if m.lastTool >= i {
			m.lastTool--
		}
		if m.selectedItem >= i {
			m.selectedItem--
		}
		break
	}
	m.inputHistory = append(m.inputHistory[:m.historyCursor], m.inputHistory[m.historyCursor+1:]...)
	m.historyCursor = -1
	draft := m.historyDraft
	m.historyDraft = ""
	m.prompt.SetValue(draft)
	m.promptUndo = nil
	m.slashFromPaste = false
	m.promptSelectAll = false
	m.syncTranscript()
	if item.messageID == "" || m.store == nil {
		return m, nil
	}
	store, messageID := m.store, item.messageID
	return m, func() tea.Msg {
		if err := store.SetMessageVisibility(context.Background(), messageID, false); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}
