// Package chat implements the chat TUI model: transcript, prompt, status and confirm flow.
package chat

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/ui/confirm"
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
