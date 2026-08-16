// Package chat implements the chat TUI model: transcript, prompt, status and confirm flow.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/tips"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/ui/confirm"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
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
	pickerMaxRows = 16

	// titleBlockRows are the fixed rows above the transcript: the title
	// line and one blank line.
	titleBlockRows = 2
	// centerDiv splits the leftover space for centering the overlay card.
	centerDiv = 2
	// paneDivider is the width of the " │ " separator between panes.
	paneDivider = 3
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
	// jumpDownArrow is the faint centered icon on the alert row above the
	// input box that returns the transcript to the latest output.
	jumpDownArrow = "▼"
	// tipLabel is the bold prefix that marks the rotating idle hint on the
	// alert row.
	tipLabel = "Tip:"

	// cardBorder is the two columns of border/margin chrome on each side
	// of the overlay card content.
	cardBorder = 2
	// cardPad is the horizontal padding inside overlay cards (help, pickers).
	cardPad = 2
	// percentBase converts a percentage to a fraction.
	percentBase = 100
	// paneCount is the number of overlay columns (left, divider, right).
	paneCount = 3
	// pickerDrawerChrome is the models-drawer header and filter rows.
	pickerDrawerChrome = 2
	pickerKindModel    = "model"
	pickerKindVariant  = "variant"
	// pulseInterval and pulseSteps throb the in-progress reply rail.
	pulseInterval = 70 * time.Millisecond
	pulseSteps    = 16
)

var (
	errStyle       = lipgloss.NewStyle().Foreground(theme.ColorDanger())
	busyStyle      = lipgloss.NewStyle().Foreground(theme.ColorAccent())
	hintStyle      = lipgloss.NewStyle().Foreground(theme.ColorMute())
	userStyle      = lipgloss.NewStyle().Foreground(theme.ColorAccent())
	roleStyle      = lipgloss.NewStyle().Foreground(theme.ColorMute()).Bold(true)
	reasoningStyle = lipgloss.NewStyle().Foreground(theme.ColorMute())
	toolCardStyle  = lipgloss.NewStyle().
			Background(theme.ColorBg()).
			Foreground(theme.ColorText())
	toolOutputStyle = lipgloss.NewStyle().
			Background(theme.ColorBg()).
			Foreground(theme.ColorMute())
	selectionStyle = lipgloss.NewStyle().
			Background(theme.ColorAccent()).
			Foreground(theme.ColorBg())
	diffAddStyle = lipgloss.NewStyle().Foreground(theme.ColorGood())
	diffDelStyle = lipgloss.NewStyle().Foreground(theme.ColorDanger())
)

// Options configures the chat model.
type Options struct {
	Store        *db.Store
	Client       *opencode.Client
	Workdir      string
	Session      *db.Session
	MaxSteps     int // ignored when Settings is set; kept for tests
	InitialErr   string
	CachePath    string // optional models cache file; empty disables caching
	SettingsPath string // .lazykoder/settings.json; empty skips persistence
	Settings     *settings.Settings
}

// Model is the chat screen: title, transcript, prompt, status and confirm flow.
type Model struct {
	store        *db.Store
	client       *opencode.Client
	workdir      string
	session      *db.Session
	maxSteps     int
	settingsPath string
	slotSettings settings.Slot

	width  int
	height int

	items               []transcriptItem
	selectedItem        int
	transcript          viewport.Model
	prompt              textarea.Model
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

	model         string // current model; "" = provider default
	variant       string // current reasoning variant; "" = provider default
	models        []string
	modelInfos    []modelscache.Info
	modelsErr     string
	modelsCached  bool // models came from the cache, not a live fetch
	cachePath     string
	activity      string
	tokensUsed    int64
	sessionCost   float64
	tokensPerSec  float64
	cacheHit      int64
	cacheMiss     int64
	turnStarted   time.Time
	turnGenTokens int64
	turnItemFrom  int

	confirmCh   chan confirmRequest
	askCh       chan askRequest
	doneCh      chan struct{}
	doneClosed  bool
	pending     *confirmRequest
	pendingAsk  *askRequest
	confirm     confirm.Model
	confirmMode bool
	askMode     bool
	askQuestion question.Question
	askCursor   int

	helpMode bool

	settingsMode   bool
	settingsCursor int
	stepLimitHit   bool // last turn stopped on agent step limit

	filePickerMode   bool
	filePickerItems  []string
	filePickerCursor int
	filePickerFilter string
	filePickerAt     int

	pulse     int
	pulseOn   bool
	railInset int

	pickerVp         viewport.Model
	pickerItems      []string
	pickerCursor     int
	pickerFilter     string
	pickerFiltering  bool
	pickerBuilt      bool
	pickerMode       bool
	pickerKind       string
	pickerFromPrompt bool

	sessionPickerMode bool
	sessionItems      []db.Session
	sessionCursor     int
	sessionHover      int
	sessionVp         viewport.Model
	sessionBuilt      bool

	slashMode       bool
	slashCursor     int
	slashItems      []slashCmd
	slashFromPaste  bool
	selection       textSelection
	copyNotice      string
	promptSelectAll bool
	tipsIndex       int

	dragTarget int // -1 none, 0 transcript, 1 picker
	dragOn     bool

	renderCache *renderCache

	turnSeq    int
	turnCancel context.CancelFunc
	turnCtx    context.Context
	eventCh    chan agent.Event
	errCh      chan error
}

// slashCmd is one entry of the slash command menu. aliases are extra
// prefixes that match the same command (e.g. /session finds /resume).
type slashCmd struct {
	name        string
	description string
	aliases     []string
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
	{name: "/resume", description: "resume a previous session", aliases: []string{"sessions"}},
	{name: "/model", description: "search and switch the chat model"},
	{name: "/variant", description: "switch the model variant (low, medium, high)"},
	{name: "/refresh", description: "reload the model list from the server"},
	{name: "/settings", description: "slot settings (step limit)", aliases: []string{"slot"}},
	{name: "/continue", description: "continue after a step limit or keep going"},
	{name: "/help", description: "show the keyboard shortcuts"},
}

type modelsMsg struct {
	list      []string
	infos     []modelscache.Info
	err       error
	fromCache bool
	notice    string
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

type eventMsg struct {
	seq int
	ev  agent.Event
}

type eventDoneMsg struct {
	seq int
	err error
}

type pulseMsg struct{}

type tipsTickMsg struct{}

// New returns a chat model for the given options.
func New(opts Options) Model {
	slot := settings.Default().Slot
	if opts.Settings != nil {
		slot = opts.Settings.Slot
	} else if opts.MaxSteps > 0 {
		slot.MaxSteps = opts.MaxSteps
		slot.LimitEnabled = true
	}
	eff := settings.Settings{Slot: slot}.EffectiveMaxSteps()
	m := Model{
		store:               opts.Store,
		client:              opts.Client,
		workdir:             opts.Workdir,
		session:             opts.Session,
		maxSteps:            eff,
		settingsPath:        opts.SettingsPath,
		slotSettings:        slot,
		err:                 opts.InitialErr,
		width:               defaultWidth,
		height:              defaultHeight,
		confirmCh:           make(chan confirmRequest, 1),
		askCh:               make(chan askRequest, 1),
		doneCh:              make(chan struct{}),
		lastTool:            -1,
		selectedItem:        -1,
		historyCursor:       -1,
		pendingHistoryIndex: -1,
		cachePath:           opts.CachePath,
		transcript:          viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(defaultHeight-chromeLines)),
		prompt:              newPromptArea(defaultWidth),
		renderCache:         &renderCache{},
	}
	if m.cachePath != "" {
		if infos, _, err := modelscache.Load(m.cachePath, time.Now(), 0); err == nil && len(infos) > 0 {
			m.modelInfos = infos
			m.models = modelscache.IDs(infos)
		}
	}
	if m.session != nil && m.store != nil {
		m.model = m.session.Model
		if m.session.Variant != nil {
			m.variant = *m.session.Variant
		}
		m.replay(m.session.ID)
	}
	return m
}

// Init starts the fetch and watcher commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.confirmWatch(), m.askWatch(), m.fetchModels, tipsTick())
}

func (m Model) fetchModels() tea.Msg {
	if m.cachePath != "" {
		if infos, fresh, err := modelscache.Load(m.cachePath, time.Now(), modelscache.DefaultTTL); err == nil && fresh && len(infos) > 0 && modelscache.HasContext(infos) {
			return modelsMsg{list: modelscache.IDs(infos), infos: infos, fromCache: true}
		}
	}
	return m.refreshModels()
}

// refreshModels fetches the model list from the API, rewrites the cache, and
// falls back to a stale cache when the fetch fails.
func (m Model) refreshModels() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
	defer cancel()
	infos, err := m.client.ModelInfos(ctx)
	cached := toCacheInfos(infos)
	if extras, xerr := m.client.FreeModelInfos(ctx); xerr == nil && len(extras) > 0 {
		cached = modelscache.MergeByID(cached, toCacheInfos(extras))
	}
	if live, lerr := m.fetchLiveCatalog(ctx); lerr == nil {
		cached = modelscache.ApplyLive(cached, live)
	}
	list := modelscache.IDs(cached)
	if err == nil {
		if m.cachePath != "" {
			if serr := modelscache.Save(m.cachePath, cached, time.Now()); serr != nil {
				return modelsMsg{list: list, infos: cached, err: fmt.Errorf("models cache: %w", serr)}
			}
		}
		return modelsMsg{list: list, infos: cached, notice: fmt.Sprintf("models updated (%d)", len(list))}
	}
	if m.cachePath != "" {
		if stale, _, lerr := modelscache.Load(m.cachePath, time.Now(), modelscache.DefaultTTL); lerr == nil && len(stale) > 0 {
			return modelsMsg{list: modelscache.IDs(stale), infos: stale, fromCache: true, err: err}
		}
	}
	return modelsMsg{list: list, infos: cached, err: err}
}

func (m Model) fetchLiveCatalog(ctx context.Context) (map[string]modelscache.Info, error) {
	if m.client == nil || !strings.Contains(m.client.BaseURL(), "opencode.ai") {
		return nil, nil
	}
	return modelscache.Fetch(ctx, m.client.HTTP())
}

func toCacheInfos(infos []opencode.ModelInfo) []modelscache.Info {
	out := make([]modelscache.Info, 0, len(infos))
	for _, info := range infos {
		row := modelscache.Info{
			ID:             info.ID,
			Provider:       info.Provider,
			Endpoint:       info.Endpoint,
			Context:        info.Context,
			InputPerM:      info.InputPerM,
			OutputPerM:     info.OutputPerM,
			CacheReadPerM:  info.CacheReadPerM,
			CacheWritePerM: info.CacheWritePerM,
			Variants:       append([]string(nil), info.Variants...),
			Free:           info.Free,
		}
		if modelscache.IsFree(row) {
			row.Free = true
		}
		if row.Provider == "" {
			row.Provider = modelscache.ProviderFromEndpoint(row.Endpoint, row.ID)
		}
		out = append(out, row)
	}
	return out
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
		m.askQuestion = msg.req.q
		m.askCursor = 0
		m.askMode = true
		return m, m.askWatch()
	case eventMsg:
		if msg.seq != m.turnSeq {
			return m, nil
		}
		m = m.applyEvent(msg.ev)
		return m, m.watchEvents(msg.seq)
	case eventDoneMsg:
		if msg.seq != m.turnSeq {
			return m, nil
		}
		return m.finishTurn(msg.err), nil
	case pulseMsg:
		if !m.busy {
			m.pulseOn = false
			return m, nil
		}
		m.pulse = (m.pulse + 1) % pulseSteps
		return m, pulseTick()
	case tipsTickMsg:
		m.tipsIndex++
		return m, tipsTick()
	case modelsMsg:
		m.models = msg.list
		if len(msg.infos) > 0 {
			m.modelInfos = msg.infos
			m.recomputeSessionCost()
		}
		m.modelsCached = msg.fromCache
		if msg.err != nil {
			m.modelsErr = msg.err.Error()
		} else {
			m.modelsErr = ""
		}
		if msg.notice != "" {
			m.copyNotice = msg.notice
			return m, clearCopyNotice()
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
		m.prompt.SetWidth(max(minPaneWidth, msg.Width))
		m.prompt.SetHeight(m.promptHeight())
		m.transcript.SetHeight(max(minPaneHeight, m.transcriptRenderHeight()))
		m.syncTranscript()
		if m.pickerBuilt {
			m = m.resizePicker()
		}
		if m.sessionBuilt {
			m = m.resizeSessionPicker()
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
		m.promptSelectAll = false
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
			if m.promptEditing() && m.prompt.Value() != "" {
				// Fall through to updateKey: copy the input box content.
			} else if m.quitConfirm {
				return m.closeDone(), tea.Quit
			} else {
				m.quitConfirm = true
				return m, nil
			}
		}
		if m.quitConfirm {
			if msg.Code == tea.KeyEscape {
				m.quitConfirm = false
			}
			return m, nil
		}
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 'z' && !m.confirmMode && !m.pickerMode && !m.sessionPickerMode {
			return m.undoPrompt(), nil
		}
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 's' && !m.confirmMode && !m.pickerMode && !m.sessionPickerMode && !m.busy {
			return m.openSessionPicker(), nil
		}
		if m.confirmMode {
			return m.updateConfirmKey(msg)
		}
		if m.askMode {
			return m.updateAskKey(msg)
		}
		if m.helpMode {
			return m.updateHelpKey(msg)
		}
		if m.settingsMode {
			return m.updateSettingsKey(msg)
		}
		if m.filePickerMode {
			return m.updateFilePickerKey(msg)
		}
		if m.pickerMode {
			return m.updatePickerKey(msg)
		}
		if m.sessionPickerMode {
			return m.updateSessionPickerKey(msg)
		}
		if m.slashMode {
			return m.updateSlashKey(msg)
		}
		return m.updateKey(msg)
	case tea.MouseWheelMsg:
		if m.sessionPickerMode {
			vp, _ := m.sessionVp.Update(msg)
			m.sessionVp = vp
			return m, nil
		}
		if m.pickerMode {
			vp, _ := m.pickerVp.Update(msg)
			m.pickerVp = vp
			return m, nil
		}
		vp, _ := m.transcript.Update(msg)
		m.transcript = vp
		return m, nil
	case tea.MouseClickMsg:
		return m.mousePress(msg)
	case tea.MouseMotionMsg:
		if m.sessionPickerMode {
			m.sessionHover = -1
			if idx, ok := m.sessionIndexAtScreenY(msg.Mouse().Y); ok {
				m.sessionHover = idx
			}
			return m.refreshSessionHover(), nil
		}
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
		m.copyNotice = "Text copied"
		return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
	}
	return m, nil
}

func (m Model) applyEvent(ev agent.Event) Model {
	switch ev.Kind {
	case agent.EventSessionCreated:
		m = m.adoptSession(ev.SessionID)
	case agent.EventMessage:
		if ev.Role == "user" && m.pendingHistoryIndex >= 0 && m.pendingHistoryIndex < len(m.inputHistory) {
			m.inputHistory[m.pendingHistoryIndex].messageID = ev.MessageID
			m.pendingHistoryIndex = -1
		}
	case agent.EventPart:
		m.applyPart(ev.Part)
		m = m.noteActivityFromPart(ev.Part)
	case agent.EventTool:
		m.applyTool(ev)
		m.activity = toolActivity(ev.Tool)
	case agent.EventError:
		if ev.Err != nil && !isCancelErr(ev.Err) {
			m.err = ev.Err.Error()
			if isStepLimitErr(ev.Err) {
				m.stepLimitHit = true
				m.err = fmt.Sprintf("%s  ·  /continue to keep going", ev.Err.Error())
			}
		}
	}
	return m
}

func (m Model) adoptSession(id string) Model {
	if id == "" || m.store == nil {
		return m
	}
	sess, err := m.store.GetSession(context.Background(), id)
	if err != nil {
		m.session = &db.Session{ID: id, Model: m.model, Directory: m.workdir}
		return m
	}
	m.session = &sess
	return m
}

func (m Model) finishTurn(err error) Model {
	if err != nil && m.err == "" && !isCancelErr(err) {
		m.err = err.Error()
	}
	if isStepLimitErr(err) {
		m.stepLimitHit = true
		if !strings.Contains(m.err, "/continue") {
			m.err = fmt.Sprintf("%s  ·  /continue to keep going", err.Error())
		}
	} else if err == nil {
		m.stepLimitHit = false
	}
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	m.busy = false
	m.pendingUser = ""
	m.pending = nil
	m.pendingAsk = nil
	m.confirmMode = false
	m.askMode = false
	m.eventCh = nil
	m.errCh = nil
	m.pulseOn = false
	m.activity = ""
	m.collapseLiveReasoning()
	m.syncTranscript()
	if !m.turnStarted.IsZero() {
		m.tokensPerSec = tokensPerSec(m.generatedThisTurn(), time.Since(m.turnStarted))
	}
	m.bumpTokenFloor()
	return m
}

func isStepLimitErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "step limit reached")
}

func tokensPerSec(generated int64, elapsed time.Duration) float64 {
	sec := elapsed.Seconds()
	if generated <= 0 || sec < 0.05 {
		return 0
	}
	return float64(generated) / sec
}

func (m Model) displayTPS() float64 {
	if m.busy && !m.turnStarted.IsZero() {
		return tokensPerSec(m.generatedThisTurn(), time.Since(m.turnStarted))
	}
	return m.tokensPerSec
}

func (m Model) generatedThisTurn() int64 {
	if m.turnGenTokens > 0 {
		return m.turnGenTokens
	}
	if m.turnItemFrom >= 0 && m.turnItemFrom < len(m.items) {
		return estimateTokens(m.items[m.turnItemFrom:])
	}
	return 0
}

func (m Model) noteActivityFromPart(p db.Part) Model {
	switch p.Type {
	case "reasoning":
		if p.Text != nil && *p.Text != "" {
			m.activity = "thinking  " + firstLine(*p.Text, 48)
		} else {
			m.activity = "thinking"
		}
	case "text":
		if p.Text != nil && *p.Text != "" {
			m.activity = "writing  " + firstLine(*p.Text, 48)
		}
	case "step-start":
		if m.activity == "" {
			m.activity = "thinking"
		}
	}
	return m
}

func toolActivity(tc db.ToolCall) string {
	name := tc.Tool
	if name == "" {
		name = "tool"
	}
	cmd := toolCommand(tc)
	switch tc.Status {
	case "completed":
		if cmd != "" {
			return name + " done  " + truncateRunes(cmd, 40)
		}
		return name + " done"
	case "error", "denied":
		return name + " " + tc.Status
	default:
		if cmd != "" {
			return name + "  " + truncateRunes(cmd, 48)
		}
		return name
	}
}

func firstLine(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return truncateRunes(s, maxRunes)
}

func formatTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

func pulseTick() tea.Cmd {
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg { return pulseMsg{} })
}

// tipsTick advances the idle tip rotation every tips.Rotation.
func tipsTick() tea.Cmd {
	return tea.Tick(tips.Rotation, func(time.Time) tea.Msg { return tipsTickMsg{} })
}

func (m Model) pulseT() float64 {
	step := m.pulse
	if step > pulseSteps/2 {
		step = pulseSteps - step
	}
	if pulseSteps < 2 {
		return 0
	}
	return float64(step) / float64(pulseSteps/2)
}

func isCancelErr(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled"))
}

const (
	promptMaxRows = 6
	promptMinRows = 1
)

func newPromptArea(width int) textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Placeholder = "ask lazykoder"
	ta.CharLimit = 0
	ta.DynamicHeight = true
	ta.MinHeight = promptMinRows
	ta.MaxHeight = promptMaxRows
	ta.SetWidth(max(minPaneWidth, width))
	ta.SetHeight(promptMinRows)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"))
	ta.KeyMap.WordBackward = key.NewBinding(key.WithKeys("alt+left", "ctrl+left", "alt+b"))
	ta.KeyMap.WordForward = key.NewBinding(key.WithKeys("alt+right", "ctrl+right", "alt+f"))
	ta.KeyMap.LinePrevious = key.NewBinding(key.WithKeys("up", "ctrl+p", "ctrl+up"))
	ta.KeyMap.LineNext = key.NewBinding(key.WithKeys("down", "ctrl+n", "ctrl+down"))
	plain := lipgloss.NewStyle().Background(theme.ColorBg()).Foreground(theme.ColorText())
	mute := lipgloss.NewStyle().Background(theme.ColorBg()).Foreground(theme.ColorMute())
	ta.SetStyles(textarea.Styles{
		Focused: textarea.StyleState{
			Base:        plain,
			Text:        plain,
			CursorLine:  plain,
			Placeholder: mute,
			Prompt:      plain,
			EndOfBuffer: plain,
		},
		Blurred: textarea.StyleState{
			Base:        plain,
			Text:        plain,
			CursorLine:  plain,
			Placeholder: mute,
			Prompt:      plain,
			EndOfBuffer: plain,
		},
		Cursor: textarea.CursorStyle{Color: theme.ColorText(), Blink: true},
	})
	ta.Focus()
	return ta
}

func (m Model) promptHeight() int {
	n := m.visualPromptLines()
	if n < promptMinRows {
		n = promptMinRows
	}
	if n > promptMaxRows {
		n = promptMaxRows
	}
	return n
}

func (m Model) visualPromptLines() int {
	w := m.prompt.Width()
	if w < 1 {
		w = max(minPaneWidth, m.width-4)
	}
	if w < 1 {
		w = 40
	}
	n := 0
	for _, line := range strings.Split(m.prompt.Value(), "\n") {
		lw := lipgloss.Width(line)
		if lw == 0 {
			n++
			continue
		}
		n += (lw + w - 1) / w
	}
	if n < 1 {
		return 1
	}
	return n
}

func (m Model) promptCanMoveUp() bool {
	return m.prompt.Line() > 0 || m.prompt.LineInfo().RowOffset > 0
}

func (m Model) promptCanMoveDown() bool {
	last := m.prompt.LineCount() - 1
	if last < 0 {
		last = 0
	}
	if m.prompt.Line() < last {
		return true
	}
	li := m.prompt.LineInfo()
	return li.Height > 0 && li.RowOffset+1 < li.Height
}

func (m Model) promptHasMultipleLines() bool {
	if m.prompt.LineCount() > 1 {
		return true
	}
	return m.prompt.LineInfo().Height > 1
}

func (m Model) chromeHeight() int {
	h := lipgloss.Height(m.headerView()) + 1 + lipgloss.Height(m.composerBlock())
	if m.slashMode {
		h += 1 + lipgloss.Height(m.slashView())
	}
	if m.pickerMode {
		h += 1 + lipgloss.Height(m.pickerView())
	}
	if m.err != "" {
		h += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return max(chromeLines, h)
}

func (m Model) modelLabel() string {
	label := m.model
	if label == "" && m.client != nil {
		label = m.client.Model()
	}
	if label == "" {
		label = "default"
	}
	return label
}

// modelEndpoint is the chat-completions URL for the selected model.
// models.json is the source of truth; free models without a stored
// endpoint fall back to the Zen sibling of the Go client base.
func (m Model) modelEndpoint() string {
	id := m.model
	if id == "" && m.client != nil {
		id = m.client.Model()
	}
	if ep := modelscache.EndpointOf(m.modelInfos, id); ep != "" {
		return ep
	}
	if m.client == nil {
		return ""
	}
	return opencode.ChatURLForModel(m.client.BaseURL(), id)
}

func (m Model) modelDisplayLabel() string {
	label := m.modelLabel()
	if m.variant != "" {
		return label + "  " + m.variant
	}
	return label
}

func (m Model) sessionTitle() string {
	if m.session != nil && strings.TrimSpace(m.session.Title) != "" {
		return m.session.Title
	}
	return "new session"
}

func (m Model) updateAskKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	opts := m.askQuestion.Options
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q':
		return m.resolveAskIndex(-1), nil
	case tea.KeyEnter:
		return m.resolveAskIndex(m.askCursor), nil
	case 'j', tea.KeyDown:
		if m.askCursor < len(opts)-1 {
			m.askCursor++
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.askCursor > 0 {
			m.askCursor--
		}
		return m, nil
	}
	if key.Text >= "1" && key.Text <= "9" {
		idx := int(key.Text[0] - '1')
		if idx >= 0 && idx < len(opts) {
			return m.resolveAskIndex(idx), nil
		}
	}
	return m, nil
}

func (m Model) updateHelpKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', '?':
		m.helpMode = false
	}
	return m, nil
}

func (m Model) resolveAskIndex(idx int) Model {
	if idx < 0 {
		idx = 0
		if m.pendingAsk != nil && len(m.askQuestion.Options) > 1 {
			// deny-equivalent: do not invent an answer; send 0 only if one option
			// out of range is rejected by the tool, so pick 0 as the first option
			// when the user cancels? Plan: esc cancels. Agent ask returning error
			// denies the tool. Use -1 and let askHook map cancel to error.
		}
	}
	if m.pendingAsk != nil {
		select {
		case m.pendingAsk.resp <- idx:
		default:
		}
	}
	m.pendingAsk = nil
	m.askMode = false
	return m
}
