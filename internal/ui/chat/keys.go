package chat

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

type errMsg struct {
	err error
}

func (m Model) updateKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	m.copyNotice = ""
	if key.Code != 'c' && key.Code != 'C' {
		m = m.clearTextSelection()
	}
	if key.Code != tea.KeyEscape {
		m.escapePending = false
	}
	if key.Mod.Contains(tea.ModCtrl) {
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
	case 'q', 'Q':
		return m.closeDone(), tea.Quit
	case tea.KeyEscape:
		if m.escapePending {
			m.prompt.SetValue("")
			m.escapePending = false
			m.historyCursor = -1
			m.historyDraft = ""
			m.promptUndo = nil
			m.slashFromPaste = false
			return m, nil
		}
		m.escapePending = true
		return m, nil
	case tea.KeyEnter:
		if m.busy {
			return m, nil
		}
		text := m.prompt.Value()
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		return m.submit(text)
	case tea.KeyBackspace:
		m.historyCursor = -1
		m.historyDraft = ""
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncPromptSlash(), cmd
	case 'c', 'C':
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
		if len(m.inputHistory) > 0 {
			return m.navigateHistory(-1), nil
		}
		m.transcript.ScrollUp(1)
		return m, nil
	case tea.KeyDown:
		if len(m.inputHistory) > 0 {
			return m.navigateHistory(1), nil
		}
		m.transcript.ScrollDown(1)
		return m, nil
	case tea.KeyPgUp:
		m.transcript.PageUp()
		return m, nil
	case tea.KeyPgDown:
		m.transcript.PageDown()
		return m, nil
	case tea.KeyHome:
		m.transcript.GotoTop()
		return m, nil
	case tea.KeyEnd:
		m.transcript.GotoBottom()
		return m, nil
	}
	m.historyCursor = -1
	m.historyDraft = ""
	m = m.rememberPrompt()
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(key)
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
	m.busy = true
	m.err = ""
	m.copyNotice = ""
	m.pendingUser = text
	m.historyCursor = -1
	m.historyDraft = ""
	m.promptUndo = nil
	m.slashFromPaste = false
	m.pendingHistoryIndex = len(m.inputHistory)
	m.inputHistory = append(m.inputHistory, inputHistoryItem{text: text})
	m.lines = append(m.lines, renderUserLine(text))
	m.syncTranscript()
	ch := make(chan agent.Event, eventChanBuffer)
	errCh := make(chan error, 1)
	ag := agent.New(m.store, m.client, m.workdir, agent.Options{
		Session:  m.session,
		MaxSteps: m.maxSteps,
		Model:    m.model,
		Confirm:  m.confirmHook,
		Ask:      m.askHook,
	})
	sendCmd := func() tea.Msg {
		go func() { errCh <- ag.Send(context.Background(), text, ch) }()
		return nil
	}
	watchCmd := func() tea.Msg {
		var evs []agent.Event
		for ev := range ch {
			evs = append(evs, ev)
		}
		return eventBatchMsg{events: evs, err: <-errCh}
	}
	return m, tea.Batch(sendCmd, watchCmd)
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
	m.slashFromPaste = state.slashFromPaste
	m.slashMode = false
	m.slashCursor = 0
	m.escapePending = false
	m.historyCursor = -1
	m.historyDraft = ""
	return m
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
			m.historyDraft = ""
			return m
		}
		m.historyCursor = next
	}
	m.prompt.SetValue(m.inputHistory[m.historyCursor].text)
	m.escapePending = false
	m.promptUndo = nil
	m.slashFromPaste = false
	return m
}

func (m Model) deleteSelectedHistory() (Model, tea.Cmd) {
	item, ok := m.selectedHistoryItem()
	if !ok {
		return m, nil
	}
	for i := len(m.lines) - 1; i >= 0; i-- {
		if !strings.Contains(m.lines[i], "user: "+item.text) {
			continue
		}
		m.lines = append(m.lines[:i], m.lines[i+1:]...)
		if m.lastTool >= i {
			m.lastTool--
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

func (m Model) confirmWatch() tea.Cmd {
	return func() tea.Msg {
		req := <-m.confirmCh
		return confirmRequestMsg{req: req}
	}
}

func (m Model) askWatch() tea.Cmd {
	return func() tea.Msg {
		req := <-m.askCh
		return askRequestMsg{req: req}
	}
}

func (m Model) confirmHook(dec policy.Decision, subject string) (bool, error) {
	resp := make(chan bool, 1)
	req := confirmRequest{dec: dec, subject: subject, resp: resp}
	select {
	case m.confirmCh <- req:
	default:
		return false, nil
	}
	select {
	case ok := <-resp:
		return ok, nil
	case <-m.doneCh:
		return false, nil
	}
}

func (m Model) askHook(q question.Question) (int, error) {
	resp := make(chan int, 1)
	req := askRequest{q: q, resp: resp}
	select {
	case m.askCh <- req:
	default:
		return 0, errors.New("chat: ask channel busy")
	}
	select {
	case idx := <-resp:
		return idx, nil
	case <-m.doneCh:
		return 0, errors.New("chat: cancelled")
	}
}

func (m Model) resolveConfirm(allow bool) Model {
	if m.pending != nil {
		select {
		case m.pending.resp <- allow:
		default:
		}
	}
	m.pending = nil
	m.confirmMode = false
	return m
}

func (m Model) resolveAsk(allow bool) Model {
	idx := 0
	if !allow {
		idx = 1
	}
	if m.pendingAsk != nil {
		select {
		case m.pendingAsk.resp <- idx:
		default:
		}
	}
	m.pendingAsk = nil
	m.confirmMode = false
	return m
}

func (m Model) closeDone() Model {
	if m.doneClosed {
		return m
	}
	m.doneClosed = true
	close(m.doneCh)
	return m
}
