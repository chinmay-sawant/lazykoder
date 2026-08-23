package chat

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const commitActionLifetime = 90 * time.Second

const commitActionPrompt = "Inspect the current worktree before acting. Run git status --porcelain, git diff, and git log -5. Summarize the user-scoped changes, write a detailed conventional commit message, then ask for the existing bash policy confirmation before running git add -A, git commit, and git push on the current upstream branch. Do not discard, reset, or clean unrelated changes. If status, commit, or push fails, explain the exact error and stop."

type worktreeDirtyChecker func(context.Context, string) (bool, error)

type worktreeStatusMsg struct {
	dirty bool
	err   error
}

type commitActionExpiredMsg struct{}

// DefaultWorktreeDirty reads porcelain status when the application explicitly
// wires the commit action into the live UI.
func DefaultWorktreeDirty(ctx context.Context, workdir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("worktree status: %w", err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (m Model) checkWorktree() tea.Cmd {
	checker := m.worktreeDirty
	if checker == nil {
		return func() tea.Msg { return worktreeStatusMsg{} }
	}
	workdir := m.workdir
	return func() tea.Msg {
		dirty, err := checker(context.Background(), workdir)
		return worktreeStatusMsg{dirty: dirty, err: err}
	}
}

func (m Model) commitPushVisible() bool {
	return !m.busy && !m.pushPromptBusy && time.Now().Before(m.pushPromptUntil)
}

func (m Model) commitPushRow() string {
	if !m.commitPushVisible() {
		return ""
	}
	label := "[ commit and push ]"
	return lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Background(theme.ColorBorder()).Padding(0, 1).Render(label)
}

func (m Model) commitPushRowScreenY() (int, bool) {
	if !m.commitPushVisible() {
		return 0, false
	}
	lines := strings.Split(m.chatScreen(), "\n")
	for index, line := range lines {
		if strings.Contains(ansi.Strip(line), "commit and push") {
			return index, true
		}
	}
	return 0, false
}

func (m Model) activateCommitPush() (Model, tea.Cmd) {
	if !m.commitPushVisible() || m.client == nil || m.session == nil {
		return m, nil
	}
	m.pushPromptUntil = time.Time{}
	m.pushPromptBusy = true
	m.err = ""
	m.copyNotice = ""
	m.turnHasNewUser = false
	m.turnItemFrom = len(m.items)
	return m.startTurn(turnStart{
		activity: "reviewing worktree",
		run: func(ctx context.Context, ag *agent.Agent, eventCh chan agent.Event) error {
			return ag.SendHidden(ctx, commitActionPrompt, eventCh)
		},
	})
}

func (m Model) scheduleCommitPushExpiry() tea.Cmd {
	return tea.Tick(commitActionLifetime, func(time.Time) tea.Msg { return commitActionExpiredMsg{} })
}
