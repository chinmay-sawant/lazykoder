// Package chat implements the chat TUI model: transcript, prompt, status and confirm flow.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/ui/confirm"
	"github.com/chinmay-sawant/lazykoder/internal/ui/markdown"
)

const (
	idleHint      = "enter to send  •  q to quit"
	busyHint      = "sending..."
	defaultWidth  = 80
	defaultHeight = 24
	cardWidthPct  = 80
	// chromeLines are the fixed rows around the transcript: title, blank,
	// status and prompt lines.
	chromeLines = 5

	// modelsTimeout bounds the model-list API call.
	modelsTimeout = 10 * time.Second

	// Layout floors: the transcript, prompt and status bar never shrink
	// below these sizes, and overlay panes keep a minimum width.
	minPaneWidth  = 20
	minPaneHeight = 3
	minLeftPane   = 4
	maxLeftPane   = 24
	minRightPane  = 8
	pickerMaxRows = 12

	// titleBlockRows are the fixed rows above the transcript: the title
	// line and one blank line.
	titleBlockRows = 2
	// centerDiv splits the leftover space for centering the overlay card.
	centerDiv = 2
	// paneDivider is the width of the " │ " separator between panes.
	paneDivider = 3
	// listInsetRows is the picker list offset below the card top border
	// (border + header row).
	listInsetRows = 2
	// pickerVpMinWidth is the floor for the picker list viewport.
	pickerVpMinWidth = 12
	// slashQueryMinWidth is the floor for the slash menu query row.
	slashQueryMinWidth = 16
	// pickerVpDefaultW/H seed the picker viewport before the first resize.
	pickerVpDefaultW = 58
	pickerVpDefaultH = 10
	// eventChanBuffer is the capacity of the per-turn event channel.
	eventChanBuffer = 64
	// promptUndoLimit bounds the in-memory prompt edit history.
	promptUndoLimit = 32
	// copyNoticeDuration controls how long the clipboard confirmation stays visible.
	copyNoticeDuration = 2 * time.Second

	// cardBorder is the two columns of border/margin chrome on each side
	// of the overlay card content.
	cardBorder = 2
	// percentBase converts a percentage to a fraction.
	percentBase = 100
	// paneCount is the number of overlay columns (left, divider, right).
	paneCount = 3
	// pickerFixedRows are the card rows outside the list (borders, header,
	// footer) that reduce the available list height.
	pickerFixedRows = 7
)

var (
	errStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	busyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	hintStyle      = lipgloss.NewStyle().Faint(true)
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	reasoningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	toolCardStyle  = lipgloss.NewStyle().
			Background(lipgloss.Color("#262626")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
	toolOutputStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1c1c1c")).
			Foreground(lipgloss.Color("#a0a0a0")).
			Padding(0, 1)
	selectionStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("7")).
			Foreground(lipgloss.Color("0"))
)

// Options configures the chat model.
type Options struct {
	Store      *db.Store
	Client     *opencode.Client
	Workdir    string
	Session    *db.Session
	MaxSteps   int
	InitialErr string
	CachePath  string // optional models cache file; empty disables caching
}

// Model is the chat screen: title, transcript, prompt, status and confirm flow.
type Model struct {
	store    *db.Store
	client   *opencode.Client
	workdir  string
	session  *db.Session
	maxSteps int

	width  int
	height int

	lines               []string
	transcript          viewport.Model
	prompt              textinput.Model
	escapePending       bool
	quitConfirm         bool
	promptUndo          []promptUndoState
	inputHistory        []inputHistoryItem
	historyCursor       int
	historyDraft        string
	pendingHistoryIndex int
	busy                bool
	err                 string
	pendingUser         string
	lastTool            int

	model        string // current model; "" = provider default
	models       []string
	modelsErr    string
	modelsCached bool // models came from the cache, not a live fetch
	cachePath    string

	confirmCh   chan confirmRequest
	askCh       chan askRequest
	doneCh      chan struct{}
	doneClosed  bool
	pending     *confirmRequest
	pendingAsk  *askRequest
	confirm     confirm.Model
	confirmMode bool

	pickerVp        viewport.Model
	pickerItems     []string
	pickerCursor    int
	pickerFilter    string
	pickerFiltering bool
	pickerBuilt     bool
	pickerMode      bool

	slashMode      bool
	slashCursor    int
	slashItems     []slashCmd
	slashFromPaste bool
	selection      textSelection
	copyNotice     string

	dragTarget int // -1 none, 0 transcript, 1 picker
	dragOn     bool
}

// slashCmd is one entry of the slash command menu.
type slashCmd struct {
	name        string
	description string
}

type inputHistoryItem struct {
	messageID string
	text      string
}

type promptUndoState struct {
	value          string
	slashFromPaste bool
}

type textPosition struct {
	row int
	col int
}

type textSelection struct {
	anchor   textPosition
	focus    textPosition
	active   bool
	dragging bool
}

type copyNoticeMsg struct{}

var slashCommands = []slashCmd{
	{name: "/new", description: "start a new session and clear the transcript"},
	{name: "/model", description: "switch the chat model"},
	{name: "/refresh", description: "reload the model list from the server"},
	{name: "/help", description: "show the keyboard shortcuts"},
}

type modelsMsg struct {
	list      []string
	err       error
	fromCache bool
}

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

type eventBatchMsg struct {
	events []agent.Event
	err    error
}

// New returns a chat model for the given options.
func New(opts Options) Model {
	m := Model{
		store:               opts.Store,
		client:              opts.Client,
		workdir:             opts.Workdir,
		session:             opts.Session,
		maxSteps:            opts.MaxSteps,
		err:                 opts.InitialErr,
		width:               defaultWidth,
		height:              defaultHeight,
		confirmCh:           make(chan confirmRequest, 1),
		askCh:               make(chan askRequest, 1),
		doneCh:              make(chan struct{}),
		lastTool:            -1,
		historyCursor:       -1,
		pendingHistoryIndex: -1,
		cachePath:           opts.CachePath,
		transcript:          viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(defaultHeight-chromeLines)),
		prompt:              textinput.New(),
	}
	m.prompt.Placeholder = "ask lazykoder... (type / for commands)"
	m.prompt.Prompt = "▏"
	m.prompt.SetWidth(m.width)
	m.prompt.SetStyles(textinput.Styles{
		Focused: textinput.StyleState{
			Text:        lipgloss.NewStyle().Foreground(lipgloss.Color("#e6e6e6")),
			Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("#8a8a8a")),
			Prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		},
	})
	m.prompt.Focus()
	if m.session != nil && m.store != nil {
		m.model = m.session.Model
		m.replay(m.session.ID)
	}
	return m
}

// Init starts the fetch and watcher commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.confirmWatch(), m.askWatch(), m.fetchModels)
}

func (m Model) fetchModels() tea.Msg {
	if m.cachePath != "" {
		if models, fresh, err := modelscache.Load(m.cachePath, time.Now(), modelscache.DefaultTTL); err == nil && fresh && len(models) > 0 {
			return modelsMsg{list: models, fromCache: true}
		}
	}
	return m.refreshModels()
}

// refreshModels fetches the model list from the API, rewrites the cache, and
// falls back to a stale cache when the fetch fails.
func (m Model) refreshModels() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
	defer cancel()
	list, err := m.client.Models(ctx)
	if err == nil && m.cachePath != "" {
		_ = modelscache.Save(m.cachePath, list, time.Now())
	}
	if err != nil && m.cachePath != "" {
		if models, _, lerr := modelscache.Load(m.cachePath, time.Now(), modelscache.DefaultTTL); lerr == nil && len(models) > 0 {
			return modelsMsg{list: models, fromCache: true}
		}
	}
	return modelsMsg{list: list, err: err}
}

// Update routes keys and streamed events through the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case confirmRequestMsg:
		m.pending = &msg.req
		qualifier := "rm"
		if msg.req.dec.Destructive {
			qualifier = "rm -rf"
		}
		m.confirm = confirm.New(msg.req.subject, qualifier)
		m.confirmMode = true
		return m, m.confirmWatch()
	case askRequestMsg:
		m.pendingAsk = &msg.req
		qualifier := msg.req.q.Header
		if qualifier == "" {
			qualifier = "question"
		}
		m.confirm = confirm.New(msg.req.q.Question, qualifier)
		m.confirmMode = true
		return m, m.askWatch()
	case eventBatchMsg:
		for _, ev := range msg.events {
			switch ev.Kind {
			case agent.EventMessage:
				if ev.Role == "user" && m.pendingHistoryIndex >= 0 && m.pendingHistoryIndex < len(m.inputHistory) {
					m.inputHistory[m.pendingHistoryIndex].messageID = ev.MessageID
					m.pendingHistoryIndex = -1
				}
			case agent.EventPart:
				m.applyPart(ev.Part)
			case agent.EventTool:
				m.applyTool(ev)
			case agent.EventError:
				if ev.Err != nil {
					m.err = ev.Err.Error()
				}
			}
		}
		if m.err == "" && msg.err != nil {
			m.err = msg.err.Error()
		}
		m.busy = false
		m.pendingUser = ""
		m.pending = nil
		m.pendingAsk = nil
		m.confirmMode = false
		return m, nil
	case modelsMsg:
		m.models = msg.list
		m.modelsCached = msg.fromCache
		if msg.err != nil {
			m.modelsErr = msg.err.Error()
		} else {
			m.modelsErr = ""
		}
		return m, nil
	case errMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		return m, nil
	case copyNoticeMsg:
		m.copyNotice = ""
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.transcript.SetWidth(max(minPaneWidth, msg.Width-1))
		m.transcript.SetHeight(max(minPaneHeight, msg.Height-chromeLines))
		m.prompt.SetWidth(max(minPaneWidth, msg.Width))
		if m.pickerBuilt {
			m = m.resizePicker()
		}
		return m, nil
	case tea.PasteMsg:
		if m.confirmMode || m.pickerMode {
			return m, nil
		}
		m.escapePending = false
		m.historyCursor = -1
		m.historyDraft = ""
		m.copyNotice = ""
		m = m.clearTextSelection()
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		m.slashMode = false
		m.slashCursor = 0
		m.slashFromPaste = strings.HasPrefix(m.prompt.Value(), "/")
		return m, cmd
	case tea.KeyPressMsg:
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 'c' {
			if m.quitConfirm {
				return m.closeDone(), tea.Quit
			}
			m.quitConfirm = true
			return m, nil
		}
		if m.quitConfirm {
			if msg.Code == tea.KeyEscape {
				m.quitConfirm = false
			}
			return m, nil
		}
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 'z' && !m.confirmMode && !m.pickerMode {
			return m.undoPrompt(), nil
		}
		if m.confirmMode {
			return m.updateConfirmKey(msg)
		}
		if m.pickerMode {
			return m.updatePickerKey(msg)
		}
		if m.slashMode {
			return m.updateSlashKey(msg)
		}
		return m.updateKey(msg)
	case tea.MouseWheelMsg:
		if m.pickerMode {
			vp, _ := m.pickerVp.Update(msg)
			m.pickerVp = vp
			return m, nil
		}
		vp, _ := m.transcript.Update(msg)
		m.transcript = vp
		return m, nil
	case tea.MouseClickMsg:
		return m.mousePress(msg), nil
	case tea.MouseMotionMsg:
		if m.selection.dragging {
			return m.updateTextSelection(msg), nil
		}
		if !m.dragOn {
			return m, nil
		}
		return m.mouseDrag(msg), nil
	case tea.MouseReleaseMsg:
		m.selection.dragging = false
		m.dragOn = false
		text, ok := m.selectedText()
		if !ok {
			return m, nil
		}
		m.copyNotice = "text copied"
		return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
	}
	return m, nil
}

// mousePress starts a scrollbar drag when the click lands on a scrollbar
// column, and jumps the viewport to the clicked position.
func (m Model) mousePress(msg tea.MouseClickMsg) Model {
	mu := msg.Mouse()
	m.copyNotice = ""
	if !m.pickerMode && !m.slashMode {
		if _, top, right, bottom, ok := m.modelStatusRect(); ok && mu.X < right && mu.Y >= top && mu.Y < bottom {
			m = m.clearTextSelection()
			return m.openPicker()
		}
	}
	if m.pickerMode {
		if x, y, ok := m.pickerCloseRect(); ok && mu.X == x && mu.Y == y {
			return m.closePicker()
		}
	}
	for _, target := range []int{0, 1} {
		if m.dragTarget == 1 && target == 0 {
			continue
		}
		if !m.pickerMode && target == 1 {
			continue
		}
		top, bottom, col, ok := m.scrollbarRect(target)
		if !ok || mu.X != col || mu.Y < top || mu.Y >= bottom {
			continue
		}
		m = m.clearTextSelection()
		m = m.applyJump(target, mu.Y)
		m.dragTarget = target
		m.dragOn = true
		return m
	}
	if mu.Button == tea.MouseLeft {
		if pos, ok := m.transcriptPosition(mu); ok {
			m.selection = textSelection{
				anchor:   pos,
				focus:    pos,
				active:   true,
				dragging: true,
			}
			m.syncTranscript()
		}
	}
	return m
}

// mouseDrag keeps the viewport following the pointer while a scrollbar
// drag is in progress.
func (m Model) mouseDrag(msg tea.MouseMotionMsg) Model {
	mu := msg.Mouse()
	top, bottom, col, ok := m.scrollbarRect(m.dragTarget)
	if !ok || mu.X != col {
		return m
	}
	if mu.Y < top {
		mu.Y = top
	}
	if mu.Y >= bottom {
		mu.Y = bottom - 1
	}
	return m.applyJump(m.dragTarget, mu.Y)
}

// applyJump scrolls the target viewport so the scrollbar position matches
// the given pointer row within the scrollbar column.
func (m Model) applyJump(target, y int) Model {
	var vp *viewport.Model
	if target == 0 {
		vp = &m.transcript
	} else {
		vp = &m.pickerVp
	}
	top, _, _, _ := m.scrollbarRect(target)
	height := vp.Height()
	maxY := vp.TotalLineCount() - height
	if maxY <= 0 {
		return m
	}
	frac := float64(y-top) / float64(max(1, height-1))
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	vp.SetYOffset(int(math.Round(frac * float64(maxY))))
	return m
}

func (m Model) transcriptPosition(mu tea.Mouse) (textPosition, bool) {
	if m.pickerMode || m.slashMode {
		return textPosition{}, false
	}
	top := titleBlockRows
	height := m.transcriptRenderHeight()
	if mu.Y < top || mu.Y >= top+height || mu.X < 0 || mu.X >= m.transcript.Width() {
		return textPosition{}, false
	}
	rows := m.plainTranscriptRows()
	row := mu.Y - top + m.transcript.YOffset()
	if row < 0 || row >= len(rows) {
		return textPosition{}, false
	}
	col := mu.X + m.transcript.XOffset()
	return textPosition{row: row, col: min(col, lipgloss.Width(rows[row]))}, true
}

func (m Model) updateTextSelection(msg tea.MouseMotionMsg) Model {
	if pos, ok := m.transcriptPosition(msg.Mouse()); ok {
		m.selection.focus = pos
		m.syncTranscript()
	}
	return m
}

func (m Model) clearTextSelection() Model {
	if !m.selection.active {
		return m
	}
	m.selection = textSelection{}
	m.syncTranscript()
	return m
}

func clearCopyNotice() tea.Cmd {
	return tea.Tick(copyNoticeDuration, func(time.Time) tea.Msg {
		return copyNoticeMsg{}
	})
}

// View renders the picker card, slash menu, confirm view, or the chat layout.
func (m Model) View() tea.View {
	if m.quitConfirm {
		v := tea.NewView(m.quitScreen())
		v.AltScreen = true
		return v
	}
	if m.confirmMode {
		v := tea.NewView(m.confirm.View())
		v.AltScreen = true
		return v
	}
	if m.pickerMode {
		v := tea.NewView(m.pickerScreen())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteString("\n\n")
	b.WriteString(m.transcriptView())
	b.WriteString("\n")
	b.WriteString(m.statusLine())
	b.WriteString("\n")
	if m.slashMode {
		b.WriteString(m.slashView())
		b.WriteString("\n")
	}
	b.WriteString(m.promptLine())
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) quitScreen() string {
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("3")).
		Padding(1, cardBorder).
		Render("Press Ctrl+C again to close lazyKoder\nEsc cancel")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

// promptLine renders the prompt inside a subtle translucent-looking panel:
// a dark background with bright text so the input stays clearly readable.
// A bottom margin lifts it one row above the bottom edge.
func (m Model) promptLine() string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#262626")).
		Padding(0, 1).
		MarginBottom(1).
		Width(m.width).
		Render(m.prompt.View())
}

// transcriptRenderHeight returns the transcript height for rendering, shrinking
// it when the slash popover needs space above the prompt.
func (m Model) transcriptRenderHeight() int {
	fixedRows := titleBlockRows + lipgloss.Height(m.statusLine()) + lipgloss.Height(m.promptLine())
	if m.slashMode {
		fixedRows += 1 + lipgloss.Height(m.slashView())
	}
	return max(minPaneHeight, m.height-fixedRows)
}

// transcriptView renders the transcript viewport with a right-edge scrollbar.
func (m Model) transcriptView() string {
	atBottom := m.transcript.AtBottom()
	vp := m.transcript
	h := m.transcriptRenderHeight()
	vp.SetHeight(h)
	if atBottom {
		vp.GotoBottom()
	}
	width := vp.Width()
	return withScrollbar(vp.View(), width, h, vp.ScrollPercent(), vp.TotalLineCount() > h)
}

// titleLine renders the static white app title at the top of the chat view.
func (m Model) titleLine() string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("lazykoder")
}

// scrollbarRect returns the screen rectangle (top row, bottom row, column)
// of a rendered scrollbar column for the given target (0 = transcript,
// 1 = picker). ok is false when no scrollbar is shown.
func (m Model) scrollbarRect(target int) (top, bottom, col int, ok bool) {
	if target == 0 {
		if m.pickerMode {
			return 0, 0, 0, false
		}
		h := m.transcriptRenderHeight()
		if m.transcript.TotalLineCount() <= h {
			return 0, 0, 0, false
		}
		return titleBlockRows, titleBlockRows + h, m.width - 1, true
	}
	vpH := m.pickerVPHeight()
	if len(m.pickerItems) <= vpH {
		return 0, 0, 0, false
	}
	card := m.pickerView()
	cardTop := max(0, (m.height-lipgloss.Height(card))/centerDiv)
	cardLeft := max(0, (m.width-lipgloss.Width(card))/centerDiv)
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder)
	leftW, _ := splitPaneWidths(innerW)
	listTop := cardTop + listInsetRows
	listCol := cardLeft + 1 + leftW + paneDivider + m.pickerVp.Width()
	return listTop, listTop + vpH, listCol, true
}

func (m Model) pickerVPHeight() int {
	available := max(minPaneHeight, m.height*70/percentBase-pickerFixedRows)
	return min(max(minPaneHeight, len(m.pickerItems)), min(pickerMaxRows, available))
}

// withScrollbar appends a scrollbar column at the right edge of a rendered
// viewport when its content overflows. The thumb tracks the scroll percent.
func withScrollbar(v string, width, height int, percent float64, overflow bool) string {
	if !overflow {
		return v
	}
	lines := strings.Split(v, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	thumb := int(percent * float64(height-1))
	track := lipgloss.NewStyle().Faint(true).Render("░")
	thumbCell := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("█")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		if w := width - lipgloss.Width(line); w > 0 {
			b.WriteString(line + strings.Repeat(" ", w))
		} else {
			b.WriteString(line)
		}
		if i == thumb {
			b.WriteString(thumbCell)
		} else {
			b.WriteString(track)
		}
	}
	return b.String()
}

// slashView renders the slash command menu as a prompt-anchored two-pane card:
// the query is shown in an input-like row, followed by commands and details.
func (m Model) slashView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder)

	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dim := lipgloss.NewStyle().Faint(true)
	leftW, rightW := splitPaneWidths(innerW)

	var leftB strings.Builder
	for i, cmd := range m.slashItems {
		if i > 0 {
			leftB.WriteString("\n")
		}
		if i == m.slashCursor {
			leftB.WriteString(sel.Render("▸ " + cmd.name))
		} else {
			leftB.WriteString(dim.Render("  " + cmd.name))
		}
	}
	left := lipgloss.NewStyle().Width(leftW).Render(leftB.String())

	detail := "no matching command"
	if len(m.slashItems) > 0 && m.slashCursor < len(m.slashItems) {
		detail = m.slashItems[m.slashCursor].description
	}
	right := lipgloss.NewStyle().Faint(true).Width(rightW).Render(detail)

	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" │ ")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	query := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(max(slashQueryMinWidth, innerW)).
		Render(m.prompt.Value() + "▏")
	footer := hintStyle.Width(innerW).Render("↑/↓ select  •  enter run  •  esc close")
	content := lipgloss.JoinVertical(lipgloss.Left, query, body, footer)
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(cardW).
		Render(content)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, card)
}

// updateSlash handles keys while the slash menu is open.
func (m Model) updateSlashKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	m = m.clearTextSelection()
	switch key.Code {
	case tea.KeyEscape:
		m.slashMode = false
		m.slashCursor = 0
		m.prompt.SetValue("/")
		return m, nil
	case tea.KeyEnter:
		if m.slashCursor >= 0 && m.slashCursor < len(m.slashItems) {
			name := m.slashItems[m.slashCursor].name
			m.slashMode = false
			m.slashCursor = 0
			m.slashFromPaste = false
			m.prompt.SetValue("")
			m.promptUndo = nil
			return m.runSlash(name)
		}
		return m, nil
	case tea.KeyBackspace:
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncSlash(m.prompt.Value()), cmd
	case 'j', tea.KeyDown:
		if m.slashCursor < len(m.slashItems)-1 {
			m.slashCursor++
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.slashCursor > 0 {
			m.slashCursor--
		}
		return m, nil
	}
	if key.Text != "" {
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncSlash(m.prompt.Value()), cmd
	}
	return m, nil
}

// runSlash executes a chosen slash command.
func (m Model) runSlash(name string) (Model, tea.Cmd) {
	switch name {
	case "/new":
		m.lines = nil
		m.lastTool = -1
		m.session = nil
		m.pendingUser = ""
		m.inputHistory = nil
		m.historyCursor = -1
		m.historyDraft = ""
		m.pendingHistoryIndex = -1
		m.promptUndo = nil
		m.slashFromPaste = false
		m.syncTranscript()
	case "/model":
		return m.openPicker(), nil
	case "/refresh":
		return m, m.refreshModels
	case "/help":
		m.lines = append(m.lines,
			"help: enter send  •  click model  •  / slash commands  •  ↑/↓ history  •  q quit")
		m.syncTranscript()
	}
	return m, nil
}

// syncSlash recomputes the slash menu from the prompt text. The menu opens
// when the prompt starts with "/" and closes when it no longer does.
func (m Model) syncSlash(value string) Model {
	if !strings.HasPrefix(value, "/") {
		m.slashMode = false
		m.slashCursor = 0
		m.slashFromPaste = false
		return m
	}
	partial := strings.ToLower(strings.TrimPrefix(value, "/"))
	m.slashItems = nil
	for _, cmd := range slashCommands {
		if strings.HasPrefix(strings.TrimPrefix(cmd.name, "/"), partial) {
			m.slashItems = append(m.slashItems, cmd)
		}
	}
	if m.slashCursor >= len(m.slashItems) {
		m.slashCursor = max(0, len(m.slashItems)-1)
	}
	m.slashMode = true
	return m
}

func (m Model) syncPromptSlash() Model {
	if m.slashFromPaste {
		if !strings.HasPrefix(m.prompt.Value(), "/") {
			m.slashFromPaste = false
		}
		m.slashMode = false
		m.slashCursor = 0
		return m
	}
	return m.syncSlash(m.prompt.Value())
}

// pickerView renders the model settings card with a label rail on the left,
// the selectable model list on the right, and a filter prompt at the bottom.
func (m Model) pickerView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder)
	leftW, rightW := splitPaneWidths(innerW)

	current := m.model
	if current == "" && m.client != nil {
		current = m.client.Model()
	}
	if current == "" {
		current = "provider default"
	}
	left := lipgloss.NewStyle().Width(leftW).Render(strings.Join([]string{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render("MODEL"),
		hintStyle.Render("Selected"),
		current,
		"",
		hintStyle.Render("Choose the model used for\nthe next chat turn."),
	}, "\n"))

	vpH := m.pickerVPHeight()
	rightHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render("AVAILABLE MODELS")
	rightBody := ""
	if m.modelsErr != "" {
		rightBody = errStyle.Render("models unavailable: " + m.modelsErr)
	} else if len(m.pickerItems) == 0 {
		if len(m.models) == 0 {
			rightBody = hintStyle.Render("no models loaded")
		} else {
			rightBody = hintStyle.Render("no models match \"" + m.pickerFilter + "\"")
		}
	} else {
		vpW := m.pickerVp.Width()
		rightBody = withScrollbar(m.pickerVp.View(), vpW, vpH,
			m.pickerVp.ScrollPercent(), m.pickerVp.TotalLineCount() > vpH)
	}
	right := lipgloss.NewStyle().Width(rightW).Render(
		lipgloss.JoinVertical(lipgloss.Left, rightHeader, rightBody),
	)
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" │ ")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)

	filter := "filter /  •  r refresh  •  enter select  •  esc cancel"
	if m.pickerFiltering {
		filter = "filter: " + m.pickerFilter + "▏"
	} else if m.pickerFilter != "" {
		filter = "filter: " + m.pickerFilter + "  •  enter select"
	}
	footer := hintStyle.Width(innerW).Render(filter)
	content := lipgloss.JoinVertical(lipgloss.Left,
		pickerHeader(innerW),
		body,
		footer,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(cardW).
		Render(content)
}

func pickerHeader(width int) string {
	title := " SETTINGS  /  MODEL"
	if lipgloss.Width(title)+1 > width {
		title = " SETTINGS / MODEL"
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Width(width).Render(
		title + strings.Repeat(" ", max(0, width-lipgloss.Width(title)-1)) + "X",
	)
}

func (m Model) pickerCloseRect() (x, y int, ok bool) {
	if !m.pickerMode {
		return 0, 0, false
	}
	card := m.pickerView()
	cardW, cardH := lipgloss.Width(card), lipgloss.Height(card)
	left := max(0, (m.width-cardW)/centerDiv)
	top := max(0, (m.height-cardH)/centerDiv)
	// The close marker sits one row below the top border and two columns
	// from the right border.
	return left + cardW - cardBorder, top + 1, true
}

func (m Model) pickerScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.pickerView())
}

func (m Model) overlayWidth() int {
	available := max(minPaneWidth, m.width-cardBorder)
	desired := max(minPaneWidth, m.width*cardWidthPct/percentBase)
	return min(available, desired)
}

func splitPaneWidths(total int) (left, right int) {
	left = max(minLeftPane, min(maxLeftPane, total/paneCount))
	right = total - left - paneDivider
	if right < minRightPane {
		right = minRightPane
		left = max(minLeftPane, total-right-paneDivider)
	}
	return left, right
}

// pickerContent renders the filtered model list with the cursor marker.
func (m Model) pickerContent(width int) string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	normal := lipgloss.NewStyle().Faint(true)
	var b strings.Builder
	for i, id := range m.pickerItems {
		if i > 0 {
			b.WriteString("\n")
		}
		line := "  " + id
		if i == m.pickerCursor {
			b.WriteString(sel.Render("▸ " + id))
			continue
		}
		b.WriteString(normal.Render(line))
	}
	return b.String()
}

func (m Model) resizePicker() Model {
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder)
	_, rightW := splitPaneWidths(innerW)
	vpW := max(pickerVpMinWidth, rightW-1)
	m.pickerVp.SetWidth(vpW)
	m.pickerVp.SetHeight(m.pickerVPHeight())
	return m
}

func (m Model) updatePickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	if m.pickerFiltering {
		switch key.Code {
		case tea.KeyEscape, tea.KeyEnter:
			m.pickerFiltering = false
			return m, nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.pickerFilter) > 0 {
				m.pickerFilter = m.pickerFilter[:len(m.pickerFilter)-1]
				m.applyFilter()
			}
			return m, nil
		}
		if key.Text != "" {
			m.pickerFilter += key.Text
			m.applyFilter()
		}
		return m, nil
	}
	switch key.Code {
	case 'q', 'Q', 'x', 'X':
		m = m.closePicker()
		return m, nil
	case 'r', 'R':
		m = m.closePicker()
		return m, m.refreshModels
	case tea.KeyEscape:
		m = m.closePicker()
		return m, nil
	case '/':
		m.pickerFiltering = true
		return m, nil
	case tea.KeyEnter:
		if m.pickerBuilt && len(m.pickerItems) > 0 && m.pickerCursor < len(m.pickerItems) {
			m.model = m.pickerItems[m.pickerCursor]
			m.pickerMode = false
			return m, m.persistModel()
		}
		return m, nil
	case 'j', tea.KeyDown:
		if m.pickerCursor < len(m.pickerItems)-1 {
			m.pickerCursor++
			m = m.refreshPickerCursor()
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m = m.refreshPickerCursor()
		}
		return m, nil
	case tea.KeyPgDown:
		m.pickerVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.pickerVp.PageUp()
		return m, nil
	}
	return m, nil
}

func (m Model) closePicker() Model {
	m.pickerMode = false
	m.pickerFiltering = false
	m.dragOn = false
	return m
}

func (m Model) refreshPickerCursor() Model {
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
	return m
}

func (m *Model) applyFilter() {
	m.pickerItems = nil
	needle := strings.ToLower(m.pickerFilter)
	for _, id := range m.models {
		if needle == "" || strings.Contains(strings.ToLower(id), needle) {
			m.pickerItems = append(m.pickerItems, id)
		}
	}
	if m.pickerCursor >= len(m.pickerItems) {
		m.pickerCursor = max(0, len(m.pickerItems)-1)
	}
	m.pickerVp.SetHeight(m.pickerVPHeight())
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
}

func (m Model) openPicker() Model {
	if !m.pickerBuilt {
		m.pickerVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
		m.pickerBuilt = true
		m = m.resizePicker()
	}
	m.pickerFilter = ""
	m.pickerFiltering = false
	m.pickerCursor = 0
	m.applyFilter()
	current := m.model
	if current == "" && m.client != nil {
		current = m.client.Model()
	}
	for i, id := range m.pickerItems {
		if id == current {
			m.pickerCursor = i
			break
		}
	}
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
	m.pickerMode = true
	return m
}

func (m Model) persistModel() tea.Cmd {
	if m.session == nil || m.store == nil {
		return nil
	}
	sid, model := m.session.ID, m.model
	return func() tea.Msg {
		if err := m.store.UpdateSessionModel(context.Background(), sid, model); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

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

func renderUserLine(text string) string {
	return userStyle.Render("user: " + text)
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

func (m *Model) replay(sessionID string) {
	ctx := context.Background()
	msgs, err := m.store.ListMessages(ctx, sessionID)
	if err != nil {
		m.err = "chat: " + err.Error()
		return
	}
	tcs, err := m.store.ListToolCalls(ctx, sessionID)
	if err != nil {
		m.err = "chat: " + err.Error()
		return
	}
	toolCalls := make(map[string]db.ToolCall, len(tcs))
	for _, tc := range tcs {
		toolCalls[tc.PartID] = tc
	}
	for _, msg := range msgs {
		if !msg.Visible {
			continue
		}
		parts, err := m.store.ListParts(ctx, msg.ID)
		if err != nil {
			m.err = "chat: " + err.Error()
			return
		}
		for _, p := range parts {
			switch p.Type {
			case "text":
				if p.Text != nil {
					if msg.Role == "user" {
						m.inputHistory = append(m.inputHistory, inputHistoryItem{messageID: msg.ID, text: *p.Text})
						m.lines = append(m.lines, renderUserLine(*p.Text))
					} else {
						m.lines = append(m.lines, renderAssistantLine(*p.Text))
					}
				}
			case "reasoning":
				if p.Text != nil {
					m.lines = append(m.lines, renderReasoningLine(*p.Text))
				}
			case "tool":
				tool := db.ToolCall{PartID: p.ID}
				if stored, ok := toolCalls[p.ID]; ok {
					tool = stored
				} else {
					tool.Tool = "tool"
					if p.ToolName != nil {
						tool.Tool = *p.ToolName
					}
					if p.ToolStatus != nil {
						tool.Status = *p.ToolStatus
					}
				}
				m.lines = append(m.lines, m.renderTool(agent.Event{Part: p, Tool: tool}))
			}
		}
	}
	m.syncTranscript()
}

func (m *Model) syncTranscript() {
	atBottom := m.transcript.AtBottom()
	yOffset := m.transcript.YOffset()
	m.transcript.SetContent(m.transcriptContent())
	if atBottom {
		m.transcript.GotoBottom()
		return
	}
	m.transcript.SetYOffset(yOffset)
}

func (m Model) transcriptContent() string {
	content := strings.Join(m.lines, "\n")
	if !m.selection.active || !m.selection.hasRange() {
		return content
	}
	rows := strings.Split(content, "\n")
	start, end := m.selection.bounds()
	for row := start.row; row <= end.row && row < len(rows); row++ {
		from := 0
		to := lipgloss.Width(ansi.Strip(rows[row]))
		if row == start.row {
			from = start.col
		}
		if row == end.row {
			to = end.col
		}
		if from < to {
			rows[row] = lipgloss.StyleRanges(rows[row], lipgloss.NewRange(from, to, selectionStyle))
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) plainTranscriptRows() []string {
	content := ansi.Strip(strings.Join(m.lines, "\n"))
	return strings.Split(content, "\n")
}

func (s textSelection) hasRange() bool {
	if !s.active {
		return false
	}
	start, end := s.bounds()
	return start != end
}

func (s textSelection) bounds() (textPosition, textPosition) {
	if s.anchor.row < s.focus.row || (s.anchor.row == s.focus.row && s.anchor.col <= s.focus.col) {
		return s.anchor, s.focus
	}
	return s.focus, s.anchor
}

func (m Model) selectedText() (string, bool) {
	if !m.selection.hasRange() {
		return "", false
	}
	rows := m.plainTranscriptRows()
	start, end := m.selection.bounds()
	if start.row < 0 || end.row >= len(rows) {
		return "", false
	}
	selected := make([]string, 0, end.row-start.row+1)
	for row := start.row; row <= end.row; row++ {
		from := 0
		to := lipgloss.Width(rows[row])
		if row == start.row {
			from = start.col
		}
		if row == end.row {
			to = end.col
		}
		selected = append(selected, ansi.Cut(rows[row], from, to))
	}
	return strings.Join(selected, "\n"), true
}

func (m *Model) applyPart(p db.Part) {
	switch p.Type {
	case "text":
		if p.Text == nil {
			return
		}
		if m.pendingUser != "" && *p.Text == m.pendingUser {
			m.pendingUser = ""
			return
		}
		m.lines = append(m.lines, renderAssistantLine(*p.Text))
	case "reasoning":
		if p.Text != nil {
			m.lines = append(m.lines, renderReasoningLine(*p.Text))
		}
	}
	m.syncTranscript()
}

func renderReasoningLine(text string) string {
	return reasoningStyle.Render("reasoning: " + text)
}

func renderAssistantLine(text string) string {
	rendered := markdown.Render(text)
	if strings.Contains(rendered, "\n") {
		return "assistant:\n" + rendered
	}
	return "assistant: " + rendered
}

func (m *Model) applyTool(ev agent.Event) {
	if ev.Tool.Tool == "" && ev.Part.ToolName != nil {
		ev.Tool.Tool = *ev.Part.ToolName
	}
	if ev.Tool.Tool == "" {
		return
	}
	status := ev.Tool.Status
	if status == "" && ev.Part.ToolStatus != nil {
		status = *ev.Part.ToolStatus
		ev.Tool.Status = status
	}
	if status == "" || status == "pending" {
		m.lines = append(m.lines, m.renderTool(ev))
		m.lastTool = len(m.lines) - 1
		m.syncTranscript()
		return
	}
	if m.lastTool >= 0 && m.lastTool < len(m.lines) {
		m.lines[m.lastTool] = m.renderTool(ev)
	}
	m.syncTranscript()
}

func (m Model) renderTool(ev agent.Event) string {
	name := ev.Tool.Tool
	if name == "" && ev.Part.ToolName != nil {
		name = *ev.Part.ToolName
	}
	status := ev.Tool.Status
	if status == "" && ev.Part.ToolStatus != nil {
		status = *ev.Part.ToolStatus
	}
	if status == "" {
		status = "pending"
	}

	bodyWidth := max(minPaneWidth, m.toolCardWidth()-cardBorder*2)
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(name + ": " + status)
	body := []string{header}
	if command := toolCommand(ev.Tool); command != "" {
		body = append(body, hintStyle.Width(bodyWidth).Render("$ "+command))
	}
	if ev.Tool.Output != nil && *ev.Tool.Output != "" {
		output := strings.TrimSuffix(*ev.Tool.Output, "\n")
		outputLabel := hintStyle.Width(bodyWidth).Render("output")
		outputBox := toolOutputStyle.Width(bodyWidth).Render(output)
		body = append(body, outputLabel, outputBox)
	}
	return toolCardStyle.Width(m.toolCardWidth()).Render(strings.Join(body, "\n"))
}

func (m Model) toolCardWidth() int {
	return max(minPaneWidth, m.width-cardBorder*2)
}

func toolCommand(tc db.ToolCall) string {
	if tc.Tool == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(tc.InputJSON), &args); err == nil && args.Command != "" {
			return args.Command
		}
	}
	if tc.Title != nil {
		return *tc.Title
	}
	return ""
}

func (m Model) statusLine() string {
	var line string
	switch {
	case m.err != "":
		line = m.wrapStatus(errStyle.Render(m.err))
	case m.busy:
		line = m.wrapStatus(busyStyle.Render(busyHint))
	default:
		label := m.model
		if label == "" && m.client != nil {
			label = m.client.Model()
		}
		if label == "" {
			label = "default"
		}
		var b strings.Builder
		b.WriteString(hintStyle.Render(strings.Join([]string{
			"model " + label,
			"click model to switch",
			"/ commands",
			"enter to send",
			"q to quit",
		}, "  •  ")))
		if _, ok := m.selectedHistoryItem(); ok {
			b.WriteString("\n")
			b.WriteString(hintStyle.Render("history: ↑/↓ previous/next  •  c copy  •  d delete"))
		}
		if m.transcript.TotalLineCount() > m.transcript.Height() {
			b.WriteString("\n")
			b.WriteString(hintStyle.Render("scroll: ↑/↓, page up/down or mouse wheel"))
		}
		if m.modelsErr != "" {
			b.WriteString("\n")
			b.WriteString(errStyle.Render("models: " + m.modelsErr))
		} else if len(m.models) > 0 {
			b.WriteString("\n")
			count := fmt.Sprintf("models: %d available", len(m.models))
			if m.modelsCached {
				count += " (cached)"
			}
			b.WriteString(hintStyle.Render(count))
		}
		line = m.wrapStatus(b.String())
	}
	if m.copyNotice == "" {
		return line
	}
	notice := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true).
		Width(m.width).
		Align(lipgloss.Right).
		Render(m.copyNotice)
	return line + "\n" + notice
}

func (m Model) modelStatusRect() (left, top, right, bottom int, ok bool) {
	if m.busy || m.err != "" || m.pickerMode || m.slashMode {
		return 0, 0, 0, 0, false
	}
	label := m.model
	if label == "" && m.client != nil {
		label = m.client.Model()
	}
	if label == "" {
		label = "default"
	}
	statusTop := titleBlockRows + m.transcriptRenderHeight()
	return 0, statusTop, min(m.width, lipgloss.Width("model "+label)), statusTop + 1, true
}

func (m Model) wrapStatus(status string) string {
	return lipgloss.NewStyle().Width(max(minPaneWidth, m.width)).Render(status)
}
