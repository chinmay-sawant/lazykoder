package chat

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

// Human-gate adapter: Confirm/Ask channel watch, hooks for Agent Options /
// Subagent Runtime, and resolve/cancel unblock. ui/confirm stays paint-only.

type confirmRequest struct {
	dec     policy.Decision
	subject string
	resp    chan bool
}

type confirmRequestMsg struct {
	req confirmRequest
}

type askRequest struct {
	q    question.Question
	resp chan int
}

type askRequestMsg struct {
	req askRequest
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
	case <-m.doneCh:
		return false, nil
	case <-turnCtxDone(m.turnCtx):
		return false, nil
	}
	select {
	case ok := <-resp:
		return ok, nil
	case <-m.doneCh:
		return false, nil
	case <-turnCtxDone(m.turnCtx):
		return false, nil
	}
}

func (m Model) askHook(q question.Question) (int, error) {
	resp := make(chan int, 1)
	req := askRequest{q: q, resp: resp}
	select {
	case m.askCh <- req:
	case <-m.doneCh:
		return 0, errors.New("chat: cancelled")
	case <-turnCtxDone(m.turnCtx):
		return 0, errors.New("chat: cancelled")
	}
	select {
	case idx := <-resp:
		if idx < 0 || idx >= len(q.Options) {
			return 0, errors.New("chat: cancelled")
		}
		return idx, nil
	case <-m.doneCh:
		return 0, errors.New("chat: cancelled")
	case <-turnCtxDone(m.turnCtx):
		return 0, errors.New("chat: cancelled")
	}
}

func turnCtxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

func (m Model) resolveConfirm(allow bool) Model {
	if m.pending != nil {
		select {
		case m.pending.resp <- allow:
		default:
		}
	}
	m.pending = nil
	return m.clearFocus(focusConfirm)
}

func (m Model) resolveAsk(allow bool) Model {
	idx := 0
	if !allow {
		idx = 1
	}
	return m.resolveAskIndex(idx)
}

func (m Model) resolveAskIndex(idx int) Model {
	if idx < 0 {
		// Deny-equivalent: do not invent an answer when the user cancels.
		// Esc cancels; a returned error denies the tool. Use -1 and let
		// askHook map cancel to error.
		idx = 0
	}
	if m.pendingAsk != nil {
		select {
		case m.pendingAsk.resp <- idx:
		default:
		}
	}
	m.pendingAsk = nil
	return m.clearFocus(focusAsk)
}

func (m Model) closeDone() Model {
	if m.doneClosed {
		return m
	}
	m.doneClosed = true
	close(m.doneCh)
	return m
}
