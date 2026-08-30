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
	"github.com/chinmay-sawant/lazykoder/internal/prompts"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const commitActionLifetime = 90 * time.Second

type WorktreeFile struct {
	Path    string
	Added   int
	Removed int
	Binary  bool
}

type WorktreeInfo struct {
	Dirty  bool
	Branch string
	Files  []WorktreeFile
	Diffs  map[string]string
}

type worktreeDirtyChecker func(context.Context, string) (WorktreeInfo, error)

type worktreeStatusMsg struct {
	info   WorktreeInfo
	err    error
	manual bool
}

type commitActionExpiredMsg struct{}

// DefaultWorktreeDirty reads porcelain status when the application explicitly
// wires the commit action into the live UI. It also collects per-file
// +added/-removed counts and unified diff text so the drawer can render the
// worktree without running extra git commands in the view layer.
func DefaultWorktreeDirty(ctx context.Context, workdir string) (WorktreeInfo, error) {
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=all")
	statusCmd.Dir = workdir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return WorktreeInfo{}, fmt.Errorf("worktree status: %w", err)
	}
	dirty := strings.TrimSpace(string(statusOut)) != ""
	if !dirty {
		branch := worktreeBranch(ctx, workdir)
		return WorktreeInfo{Dirty: false, Branch: branch}, nil
	}
	branch := worktreeBranch(ctx, workdir)
	files := worktreeFilesFromStatus(ctx, workdir, string(statusOut))
	return WorktreeInfo{
		Dirty:  true,
		Branch: branch,
		Files:  files,
		Diffs:  worktreeDiffs(ctx, workdir, files),
	}, nil
}

func worktreeDiffs(ctx context.Context, workdir string, files []WorktreeFile) map[string]string {
	headCmd := exec.CommandContext(ctx, "git", "diff", "HEAD", "--no-ext-diff", "--no-color", "--unified=3")
	headCmd.Dir = workdir
	headOut, headErr := headCmd.Output()
	raw := string(headOut)
	if headErr != nil {
		raw = worktreeDiffOutput(ctx, workdir)
		if cached := worktreeCachedDiffOutput(ctx, workdir); cached != "" {
			raw += cached
		}
	}
	diffs := parseWorktreeDiffs(raw, files)
	for _, file := range files {
		if _, ok := diffs[file.Path]; ok {
			continue
		}
		if diff := worktreeUntrackedDiff(ctx, workdir, file.Path); diff != "" {
			diffs[file.Path] = diff
		}
	}
	return diffs
}

func worktreeDiffOutput(ctx context.Context, workdir string, revision ...string) string {
	args := []string{"diff"}
	args = append(args, revision...)
	args = append(args, "--no-ext-diff", "--no-color", "--unified=3")
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func worktreeCachedDiffOutput(ctx context.Context, workdir string) string {
	return worktreeDiffOutput(ctx, workdir, "--cached")
}

func worktreeUntrackedDiff(ctx context.Context, workdir, path string) string {
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-ext-diff", "--no-color", "--unified=3", "--", "/dev/null", path)
	cmd.Dir = workdir
	out, _ := cmd.Output()
	return string(out)
}

func parseWorktreeDiffs(raw string, files []WorktreeFile) map[string]string {
	diffs := make(map[string]string)
	var current strings.Builder
	currentPath := ""
	flush := func() {
		if currentPath == "" || current.Len() == 0 {
			return
		}
		diffs[currentPath] = current.String()
		current.Reset()
	}
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			currentPath = worktreeDiffPath(line, files)
		}
		if currentPath != "" {
			current.WriteString(line)
		}
	}
	flush()
	return diffs
}

func worktreeDiffPath(header string, files []WorktreeFile) string {
	for _, file := range files {
		if diffHeaderHasPath(header, file.Path) {
			return file.Path
		}
	}
	parts := strings.Fields(header)
	if len(parts) < 4 { //nolint:mnd
		return ""
	}
	return strings.TrimPrefix(strings.Trim(parts[len(parts)-1], `"`), "b/")
}

func diffHeaderHasPath(header, path string) bool {
	token := "b/" + path
	for start := 0; start < len(header); {
		idx := strings.Index(header[start:], token)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(token)
		if end == len(header) || strings.ContainsRune(" \"\t\r\n", rune(header[end])) {
			return true
		}
		start = end
	}
	return false
}

func worktreeBranch(ctx context.Context, workdir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func worktreeFilesFromStatus(ctx context.Context, workdir, porcelain string) []WorktreeFile {
	numstat := worktreeNumstat(ctx, workdir)
	files := parsePorcelainFiles(porcelain)
	for i := range files {
		if counts, ok := numstat[files[i].Path]; ok {
			files[i].Added = counts.added
			files[i].Removed = counts.removed
			files[i].Binary = counts.binary
		} else if alt, ok := numstat[stripRenameSource(files[i].Path)]; ok {
			files[i].Added = alt.added
			files[i].Removed = alt.removed
			files[i].Binary = alt.binary
		}
	}
	return files
}

type numStat struct {
	added   int
	removed int
	binary  bool
}

func worktreeNumstat(ctx context.Context, workdir string) map[string]numStat {
	cmd := exec.CommandContext(ctx, "git", "diff", "HEAD", "--numstat")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		cmd2 := exec.CommandContext(ctx, "git", "diff", "--numstat")
		cmd2.Dir = workdir
		out, err = cmd2.Output()
		if err != nil {
			return map[string]numStat{}
		}
	}
	m := map[string]numStat{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 { //nolint:mnd
			continue
		}
		path := parts[2]
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, "{} ")
		if strings.Contains(path, " => ") {
			if idx := strings.Index(path, " => "); idx >= 0 {
				path = path[idx+4:]
			}
		}
		added, removed, binary := 0, 0, false
		if parts[0] == "-" || parts[1] == "-" {
			binary = true
		} else {
			_, _ = fmt.Sscan(parts[0], &added)
			_, _ = fmt.Sscan(parts[1], &removed)
		}
		m[path] = numStat{added: added, removed: removed, binary: binary}
		m[stripRenameSource(path)] = numStat{added: added, removed: removed, binary: binary}
	}
	cached := exec.CommandContext(ctx, "git", "diff", "--cached", "--numstat")
	cached.Dir = workdir
	if cout, err := cached.Output(); err == nil {
		for _, line := range strings.Split(string(cout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 3 { //nolint:mnd
				continue
			}
			path := parts[2]
			if idx := strings.Index(path, " -> "); idx >= 0 {
				path = path[idx+4:]
			}
			if _, ok := m[path]; !ok {
				added, removed, binary := 0, 0, false
				if parts[0] == "-" || parts[1] == "-" {
					binary = true
				} else {
					_, _ = fmt.Sscan(parts[0], &added)
					_, _ = fmt.Sscan(parts[1], &removed)
				}
				m[path] = numStat{added: added, removed: removed, binary: binary}
			}
		}
	}
	return m
}

func stripRenameSource(p string) string {
	if idx := strings.Index(p, " -> "); idx >= 0 {
		return strings.TrimSpace(p[idx+4:])
	}
	return p
}

func parsePorcelainFiles(porcelain string) []WorktreeFile {
	seen := map[string]bool{}
	var out []WorktreeFile
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 3 { //nolint:mnd
			continue
		}
		rest := strings.TrimSpace(line[2:])
		if rest == "" {
			continue
		}
		if strings.Contains(rest, " -> ") {
			parts := strings.Split(rest, " -> ")
			rest = strings.TrimSpace(parts[len(parts)-1])
		}
		rest = strings.Trim(rest, "\"")
		if rest == "" || seen[rest] {
			continue
		}
		seen[rest] = true
		out = append(out, WorktreeFile{Path: rest})
	}
	sortFiles(out)
	return out
}

func sortFiles(files []WorktreeFile) {
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].Path < files[i].Path {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
}

func (m Model) checkWorktree() tea.Cmd {
	return m.checkWorktreeWithMode(false)
}

func (m Model) checkWorktreeForDiff() tea.Cmd {
	return m.checkWorktreeWithMode(true)
}

func (m Model) checkWorktreeWithMode(manual bool) tea.Cmd {
	checker := m.worktreeDirty
	if checker == nil {
		return func() tea.Msg { return worktreeStatusMsg{manual: manual} }
	}
	workdir := m.workdir
	return func() tea.Msg {
		info, err := checker(context.Background(), workdir)
		return worktreeStatusMsg{info: info, err: err, manual: manual}
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
	if !m.commitPushVisible() || m.client == nil {
		return m, nil
	}
	if m.store == nil {
		m.err = "cannot commit and push: session store unavailable"
		return m, nil
	}
	if m.session == nil {
		m = m.ensureSession("commit and push")
	}
	if m.session == nil {
		m.err = "cannot commit and push: failed to initialize session"
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
			return ag.SendHidden(ctx, prompts.New(m.workdir).Must("ui/commit-action.md"), eventCh)
		},
	})
}

func (m Model) scheduleCommitPushExpiry() tea.Cmd {
	return tea.Tick(commitActionLifetime, func(time.Time) tea.Msg { return commitActionExpiredMsg{} })
}
