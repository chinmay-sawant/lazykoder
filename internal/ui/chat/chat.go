// Package chat implements the chat TUI model: transcript, prompt, status and confirm flow.
package chat

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/tips"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/ui/confirm"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// confirmQueueSize buffers concurrent child/parent confirm requests.
const confirmQueueSize = 32

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

	// centerDiv splits the leftover space for centering the overlay card.
	centerDiv = 2
	// pickerVpMinWidth is the floor for the picker list viewport.
	pickerVpMinWidth = 12
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
	// askCardContentTop is the rounded border row plus top padding row.
	askCardContentTop = 2
	// cardPad is the horizontal padding inside overlay cards (help, pickers).
	cardPad = 2
	// percentBase converts a percentage to a fraction.
	percentBase = 100
	// pickerDrawerChrome is the models-drawer header and filter rows.
	pickerDrawerChrome = 2
	pickerKindModel    = "model"
	pickerKindVariant  = "variant"
	// pulseInterval and pulseSteps throb the in-progress reply rail.
	pulseInterval = 70 * time.Millisecond
	pulseSteps    = 16
	// tpsWindowDuration bounds the live rolling rate calculation.
	tpsWindowDuration = 2 * time.Second

	// activityMaxRunes caps the activity line preview for reasoning/text.
	activityMaxRunes = 48
	// doneMaxRunes caps the tool command shown after a completed call.
	doneMaxRunes = 40
	// countKilo and countTenKilo are the token-count formatting thresholds.
	countKilo    = 1000
	countTenKilo = 10_000
	// pulseMinSteps guards the pulse ratio when the step count is tiny.
	pulseMinSteps = 2
	// composerPad is the prompt textarea border/padding width per side.
	composerPad = 2
)

var (
	errStyle       = lipgloss.NewStyle().Foreground(theme.ColorDanger())
	busyStyle      = lipgloss.NewStyle().Foreground(theme.ColorAccent())
	hintStyle      = lipgloss.NewStyle().Foreground(theme.ColorMute())
	userStyle      = lipgloss.NewStyle().Foreground(theme.ColorAccent())
	roleStyle      = lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true)
	reasoningStyle = lipgloss.NewStyle().Foreground(theme.ColorMute())
	toolCardStyle  = lipgloss.NewStyle().
			Background(theme.ColorBg()).
			Foreground(theme.ColorText())
	toolOutputStyle = lipgloss.NewStyle().
			Background(theme.ColorBg()).
			Foreground(theme.ColorMute())
	// editCardStyle: soft greenish chrome for the edit card header/body shell.
	editCardStyle = lipgloss.NewStyle().
			Background(theme.ColorEditPanel()).
			Foreground(theme.ColorText())
	selectionStyle = lipgloss.NewStyle().
			Background(theme.ColorAccent()).
			Foreground(theme.ColorBg())
	// Full-width soft tints: light greenish + rows, light reddish - rows.
	diffAddStyle = lipgloss.NewStyle().
			Foreground(theme.ColorGood()).
			Background(theme.ColorEditAddBg())
	diffDelStyle = lipgloss.NewStyle().
			Foreground(theme.ColorDanger()).
			Background(theme.ColorEditDelBg())
	diffMetaStyle = lipgloss.NewStyle().
			Foreground(theme.ColorEditMeta()).
			Background(theme.ColorEditPanel())
	diffCtxStyle = lipgloss.NewStyle().
			Foreground(theme.ColorMute()).
			Background(theme.ColorEditPanel())
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
	store               *db.Store
	client              *opencode.Client
	workdir             string
	session             *db.Session
	maxSteps            int
	settingsPath        string
	projectSettings     settings.Settings
	settingsPickDefault bool // model/variant picker is setting the project default

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

	model                string // current model; "" = provider default
	variant              string // current reasoning variant; "" = provider default
	models               []string
	modelInfos           []modelscache.Info
	modelsErr            string
	modelsCached         bool // models came from the cache, not a live fetch
	cachePath            string
	activity             string
	tokensUsed           int64
	compacting           bool
	compactHint          string
	pendingCompactReason string
	prevModel            string
	prevWindow           int64
	sessionCost          float64
	tokensPerSec         float64
	tpsEstimated         bool
	cacheHit             int64
	cacheMiss            int64
	turnStarted          time.Time
	turnGenTokens        int64
	turnItemFrom         int
	tpsSamples           []tpsSample
	stepMetrics          bool

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

	usageMode    bool
	usage        opencode.BillingUsage
	usageLoaded  bool
	usageErr     string
	usageLoading bool

	settingsMode      bool
	settingsCursor    int
	settingsEdit      bool
	settingsEditValue string
	stepLimitHit      bool // last turn stopped on agent step limit

	filePickerMode   bool
	filePickerItems  []atPickItem
	filePickerCursor int
	filePickerFilter string
	filePickerAt     int

	// todos is the session checklist from todowrite (shown under the header).
	todos  []db.Todo
	todoVp viewport.Model
	// todosExpanded shows checklist bodies; stored session todos start expanded.
	todosExpanded  bool
	statusSegments []string
	statusMode     bool
	statusCursor   int

	// userNavHover is the Medium-style right-rail mark under the pointer (-1 = none).
	userNavHover int
	// userNavTip is the mark whose label bubble is visible (-1 = hidden).
	userNavTip int
	// userNavTipGen invalidates stale 10s hide timers when the tip is refreshed.
	userNavTipGen uint64

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

	// Sub-agent picker (list) + log viewer for child sessions.
	subagentPickerMode bool
	// subagentDrawerCompact shows a one-line summary instead of the full list
	// (used after todowrite updates so checklist + agents both stay visible).
	subagentDrawerCompact bool
	subagentLogMode       bool
	subagentItems         []subagentRow
	subagentCursor        int
	subagentHover         int
	subagentVp            viewport.Model
	subagentLogVp         viewport.Model
	subagentBuilt         bool
	subagentSelected      subagentRow
	// subagentLogItems is the child transcript rendered with main chat styles
	// (thinking, tools, work rails). Reasoning starts expanded.
	subagentLogItems    []transcriptItem
	subagentLogSelected int

	slashMode       bool
	slashCursor     int
	slashItems      []slashCmd
	slashFromPaste  bool
	selection       textSelection
	promptSel       promptSelection
	copyNotice               string
	promptSelectAll          bool
	tipsIndex                int
	projectInstructionsNotice string // set when workdir AGENTS.md loaded; alert-row only

	dragTarget int // -1 none, 0 transcript, 1 picker
	dragOn     bool

	renderCache *renderCache

	turnSeq    int
	turnCancel context.CancelFunc
	turnCtx    context.Context
	eventCh    chan agent.Event
	errCh      chan error

	// subMgr owns in-process sub-agent jobs for this chat model.
	subMgr *subagent.Manager
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

type tpsSample struct {
	at     time.Time
	tokens int64
}

var slashCommands = []slashCmd{
	{name: "/new", description: "start a new session and clear the transcript"},
	{name: "/resume", description: "open past sessions (ctrl+s, also /session)", aliases: []string{"sessions", "session"}},
	{name: "/model", description: "search and switch the live chat model"},
	{name: "/variant", description: "switch live reasoning effort (low / medium / high / max)"},
	{name: "/agents", description: "open the sub-agent drawer and logs", aliases: []string{"subs", "subagents"}},
	{name: "/refresh", description: "reload the model list into models.json"},
	{name: "/usage", description: "show OpenCode Go plan usage (rolling, weekly, monthly)"},
	{name: "/status", description: "open the status drawer and toggle details"},
	{name: "/settings", description: "project defaults (model, agents, compaction, safety)", aliases: []string{"slot"}},
	{name: "/continue", description: "resume after a step-limit stop (or send continue)"},
	{name: "/compact", description: "summarize older context now (optional notes)"},
	{name: "/help", description: "keyboard shortcuts (?, also /keys)", aliases: []string{"keys"}},
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
	cfg := settings.Default()
	if opts.Settings != nil {
		cfg = *opts.Settings
	} else if opts.MaxSteps > 0 {
		cfg.Slot.MaxSteps = opts.MaxSteps
		cfg.Slot.LimitEnabled = true
	}
	// Effective* helpers normalize clamps and empty model ids.
	eff := cfg.EffectiveMaxSteps()
	m := Model{
		store:               opts.Store,
		client:              opts.Client,
		workdir:             opts.Workdir,
		session:             opts.Session,
		maxSteps:            eff,
		settingsPath:        opts.SettingsPath,
		projectSettings:     cfg,
		err:                 opts.InitialErr,
		width:               defaultWidth,
		height:              defaultHeight,
		confirmCh:           make(chan confirmRequest, confirmQueueSize),
		askCh:               make(chan askRequest, confirmQueueSize),
		doneCh:              make(chan struct{}),
		lastTool:            -1,
		selectedItem:        -1,
		historyCursor:       -1,
		pendingHistoryIndex: -1,
		userNavHover:        -1,
		userNavTip:          -1,
		cachePath:           opts.CachePath,
		transcript:          viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(defaultHeight-chromeLines)),
		todoVp:              viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(maxTodoPanelRows)),
		prompt:              newPromptArea(defaultWidth),
		renderCache:         &renderCache{},
	}
	m.subMgr = subagent.NewManager(subagent.ConfigFromSettings(cfg), subagent.AgentRunner{
		Store:  opts.Store,
		Client: opts.Client,
	})
	m.subMgr.SetStore(opts.Store)
	// Workdir is known at construction; model/confirm are refreshed per turn.
	m.subMgr.SetRuntime(subagent.Runtime{
		Workdir: opts.Workdir,
		Model:   cfg.EffectiveModel(),
		Variant: cfg.EffectiveVariant(),
	})
	// Recover open jobs from a previous process crash/exit.
	if opts.Store != nil {
		_ = m.subMgr.Recover(context.Background())
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
		m = m.loadTodos()
		m.statusSegments = db.NormalizeStatusSegments(m.session.StatusSegments)
	} else {
		// New run: seed live model/variant from project defaults.
		m.model = cfg.EffectiveModel()
		m.variant = cfg.EffectiveVariant()
		m.statusSegments = db.DefaultStatusSegments()
	}
	if _, path, ok := agent.LoadProjectInstructions(opts.Workdir); ok {
		m.projectInstructionsNotice = "project instructions: " + filepath.Base(path)
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
		m = m.finishTurn(msg.err)
		// Continue diamond throb while background sub-agents are still live.
		if m.hasLiveSubagents() {
			return m, pulseTick()
		}
		return m, nil
	case pulseMsg:
		// Keep throbbing for live sub-agents even after the parent turn ends.
		if !m.busy && !m.hasLiveSubagents() {
			m.pulseOn = false
			return m, nil
		}
		m.pulse = (m.pulse + 1) % pulseSteps
		m.pulseOn = true
		// Refresh live status for footer chips even when the drawer is closed;
		// when open, update activity in place without reordering.
		if !m.subagentLogMode && (m.subagentPickerMode || m.hasLiveSubagents() || len(m.subagentItems) > 0) {
			m = m.refreshSubagentDrawerLive()
		}
		return m, pulseTick()
	case tipsTickMsg:
		m.tipsIndex++
		return m, tipsTick()
	case modelsMsg:
		m.models = msg.list
		if len(msg.infos) > 0 {
			m.modelInfos = msg.infos
			m.recomputeSessionCost()
			if m.session != nil {
				m = m.reloadSubagentRows()
			}
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
	case usageMsg:
		m.usage = msg.usage
		m.usageLoaded = msg.err == nil
		m.usageLoading = false
		m.usageErr = ""
		if msg.err != nil {
			m.usageErr = msg.err.Error()
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
	case userNavTipExpireMsg:
		if msg.gen != m.userNavTipGen {
			return m, nil
		}
		// Keep the bubble while the pointer is still on a tick.
		if m.userNavHover < 0 {
			m.userNavTip = -1
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeTodoPanel()
		m = m.focusTodoViewport()
		m.transcript.SetWidth(m.transcriptContentWidth())
		// Keep textarea width identical to the bordered composer content so
		// soft-wrap and mouse hit-testing agree with what is painted.
		m.prompt.SetWidth(m.promptContentWidth())
		m.prompt.SetHeight(m.promptHeight())
		m.transcript.SetHeight(max(1, m.transcriptRenderHeight()))
		m.syncTranscript()
		if m.pickerBuilt {
			m = m.resizePicker()
		}
		if m.sessionBuilt {
			m = m.resizeSessionPicker()
		}
		if m.subagentBuilt {
			if m.subagentLogMode {
				m = m.resizeSubagentLogCard()
			} else if m.subagentPickerMode {
				m = m.resizeSubagentDrawer()
			}
		}
		return m, nil
	case tea.PasteMsg:
		// Full-screen sub-agent log keeps paste blocked; the list drawer does not.
		if m.confirmMode || m.pickerMode || m.subagentLogMode {
			return m, nil
		}
		m.escapePending = false
		m.historyCursor = -1
		m.historyDraft = ""
		m.copyNotice = ""
		m = m.clearTextSelection()
		m = m.rememberPrompt()
		// Ctrl+A then paste should replace the selection, not append.
		if m.promptSelectAll {
			m.promptSelectAll = false
			m.prompt.SetValue(msg.Content)
			m.prompt.SetHeight(m.promptHeight())
			m.slashMode = false
			m.slashCursor = 0
			m.slashFromPaste = strings.HasPrefix(m.prompt.Value(), "/")
			return m, nil
		}
		m.promptSelectAll = false
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
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 'z' && !m.confirmMode && !m.pickerMode && !m.sessionPickerMode && !m.subagentPickerMode {
			return m.undoPrompt(), nil
		}
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 's' && !m.confirmMode && !m.pickerMode && !m.sessionPickerMode && !m.subagentPickerMode && !m.busy {
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
		if m.usageMode {
			return m.updateUsageKey(msg)
		}
		if m.settingsMode {
			return m.updateSettingsKey(msg)
		}
		if m.statusMode {
			return m.updateStatusKey(msg)
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
		if m.subagentPickerMode {
			return m.updateSubagentPickerKey(msg)
		}
		if m.slashMode {
			return m.updateSlashKey(msg)
		}
		return m.updateKey(msg)
	case tea.MouseWheelMsg:
		if m.askMode {
			return m, nil
		}
		if m.sessionPickerMode {
			vp, _ := m.sessionVp.Update(msg)
			m.sessionVp = vp
			return m, nil
		}
		if m.subagentLogMode {
			vp, _ := m.subagentLogVp.Update(msg)
			m.subagentLogVp = vp
			return m, nil
		}
		// Drawer open: wheel over the drawer scrolls the list; wheel over the
		// transcript (or anywhere else) scrolls the chat behind it.
		if m.subagentPickerMode && !m.subagentLogMode && m.pointerInSubagentDrawer(msg.Mouse().Y) {
			vp, _ := m.subagentVp.Update(msg)
			m.subagentVp = vp
			return m, nil
		}
		if m.todoPanelBodyAt(msg.Mouse().Y) {
			vp, _ := m.todoVp.Update(msg)
			m.todoVp = vp
			return m, nil
		}
		if m.pickerMode {
			vp, _ := m.pickerVp.Update(msg)
			m.pickerVp = vp
			return m, nil
		}
		vp, _ := m.transcript.Update(msg)
		m.transcript = vp
		// Drop rail hover so the label bubble follows the active section.
		m.userNavHover = -1
		return m.showActiveUserNavTip()
	case tea.MouseClickMsg:
		return m.mousePress(msg)
	case tea.MouseMotionMsg:
		if m.askMode {
			return m, nil
		}
		if m.sessionPickerMode {
			m.sessionHover = -1
			if idx, ok := m.sessionIndexAtScreenY(msg.Mouse().Y); ok {
				m.sessionHover = idx
			}
			return m.refreshSessionHover(), nil
		}
		// Composer drag-select takes priority over transcript selection.
		if m.promptSel.dragging {
			return m.updatePromptSelection(msg.Mouse()), nil
		}
		// Keep transcript selection / scrollbar drag working while the drawer
		// is open; only update drawer hover when the pointer is on it.
		if m.selection.dragging {
			return m.updateTextSelection(msg), nil
		}
		if m.dragOn {
			return m.mouseDrag(msg), nil
		}
		if m.subagentPickerMode && !m.subagentLogMode {
			prev := m.subagentHover
			m.subagentHover = -1
			if m.pointerInSubagentDrawer(msg.Mouse().Y) {
				if idx, ok := m.subagentIndexAtScreenY(msg.Mouse().Y); ok {
					m.subagentHover = idx
				}
			}
			if m.subagentHover != prev {
				return m.resizeSubagentDrawer(), nil
			}
			return m, nil
		}
		// User-turn rail hover (Medium-style preview + tooltip).
		mu := msg.Mouse()
		prevNav := m.userNavHover
		m.userNavHover = -1
		if idx, ok := m.userNavIndexAtScreen(mu.X, mu.Y); ok {
			m.userNavHover = idx
		}
		if m.userNavHover != prevNav {
			if m.userNavHover >= 0 {
				return m.showUserNavTip(m.userNavHover)
			}
			return m, nil
		}
		if m.userNavHover >= 0 {
			return m, nil
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if m.promptSel.dragging {
			return m.endPromptSelectionDrag()
		}
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
		// On task tool events: open drawer only when a new job appears.
		if ev.Tool.Tool == "task" || strings.HasPrefix(ev.Tool.Tool, "task_") {
			m = m.openSubagentDrawerIfNew()
			m.pulseOn = m.busy || m.hasLiveSubagents()
		}
		if ev.Tool.Tool == "todowrite" {
			m = m.applyTodosFromTool(ev.Tool)
		}
	case agent.EventTokenDelta:
		if ev.TokenDelta > 0 {
			now := time.Now()
			m.tpsSamples = append(m.tpsSamples, tpsSample{at: now, tokens: ev.TokenDelta})
			cutoff := now.Add(-tpsWindowDuration)
			first := 0
			for first < len(m.tpsSamples) && m.tpsSamples[first].at.Before(cutoff) {
				first++
			}
			if first > 0 {
				m.tpsSamples = m.tpsSamples[first:]
			}
		}
	case agent.EventStepMetrics:
		m.stepMetrics = true
		if ev.TokensOutput > 0 && ev.ElapsedMS > 0 {
			m.tokensPerSec = tokensPerSec(ev.TokensOutput, time.Duration(ev.ElapsedMS)*time.Millisecond)
			m.tpsEstimated = false
		}
	case agent.EventError:
		if ev.Err != nil && !isCancelErr(ev.Err) {
			m.err = ev.Err.Error()
			if isStepLimitErr(ev.Err) {
				m.stepLimitHit = true
				m.err = fmt.Sprintf("%s  ·  /continue to keep going", ev.Err.Error())
			}
		}
		m.compacting = false
	case agent.EventCompacting:
		m.compacting = true
		m.activity = "compacting"
	case agent.EventCompacted:
		m.compacting = false
		m.tokensUsed = ev.TokensUsed
		m.cacheHit = 0
		m.cacheMiss = 0
		m.pendingCompactReason = ""
		m.compactHint = ""
		m.applyCompactNotice(ev.Part)
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
	// Load child rows for the footer chip; do not force-open the drawer.
	m = m.loadTodos()
	return m.syncSubagentDrawer()
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
	m.compacting = false
	m.pendingUser = ""
	m.pending = nil
	m.pendingAsk = nil
	m.confirmMode = false
	m.askMode = false
	m.eventCh = nil
	m.errCh = nil
	m.activity = ""
	m.collapseLiveReasoning()
	// Refresh sub-agent rows after the turn; drawer stays as the user left it
	// (only a new spawn re-opens it via openSubagentDrawerIfNew).
	m = m.syncSubagentDrawer()
	if m.hasLiveSubagents() {
		m.pulseOn = true
	} else {
		m.pulseOn = false
	}
	m.syncTranscript()
	if !m.stepMetrics && !m.turnStarted.IsZero() {
		if n := tokensPerSec(m.generatedThisTurn(), time.Since(m.turnStarted)); n > 0 {
			m.tokensPerSec = n
			m.tpsEstimated = true
		}
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
		if m.stepMetrics && m.tokensPerSec > 0 {
			return m.tokensPerSec
		}
		if n := rollingTPS(m.tpsSamples, time.Now()); n > 0 {
			return n
		}
		if n := tokensPerSec(m.generatedThisTurn(), time.Since(m.turnStarted)); n > 0 {
			return n
		}
	}
	return m.tokensPerSec
}

func rollingTPS(samples []tpsSample, now time.Time) float64 {
	if len(samples) == 0 {
		return 0
	}
	cutoff := now.Add(-tpsWindowDuration)
	var total int64
	var first time.Time
	for _, sample := range samples {
		if sample.at.Before(cutoff) {
			continue
		}
		if sample.tokens > 0 {
			total += sample.tokens
		}
		if first.IsZero() || sample.at.Before(first) {
			first = sample.at
		}
	}
	if total <= 0 || first.IsZero() || now.Before(first) {
		return 0
	}
	return tokensPerSec(total, now.Sub(first))
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
			m.activity = "thinking  " + firstLine(*p.Text, activityMaxRunes)
		} else {
			m.activity = "thinking"
		}
	case "text":
		if p.Text != nil && *p.Text != "" {
			m.activity = "writing  " + firstLine(*p.Text, activityMaxRunes)
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
			return name + " done  " + truncateRunes(cmd, doneMaxRunes)
		}
		return name + " done"
	case "error", "denied":
		return name + " " + tc.Status
	default:
		if cmd != "" {
			return name + "  " + truncateRunes(cmd, activityMaxRunes)
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
	if n < countKilo {
		return fmt.Sprintf("%d", n)
	}
	if n < countTenKilo {
		return fmt.Sprintf("%.1fk", float64(n)/countKilo)
	}
	return fmt.Sprintf("%dk", n/countKilo)
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
	if step > pulseSteps/pulseMinSteps {
		step = pulseSteps - step
	}
	if pulseSteps < pulseMinSteps {
		return 0
	}
	return float64(step) / float64(pulseSteps/pulseMinSteps)
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
	// Match bordered composer content width (not full terminal width).
	innerW := max(minPaneWidth, width-composerPad)
	ta.SetWidth(max(minPaneWidth, innerW-composerPad))
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
	// Same hard-wrap as promptBodyPaint / mouse hit-testing.
	n := len(m.promptVisualLines())
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

func (m Model) modelChipLabel() string {
	label := m.modelLabel()
	if label == "" {
		return ""
	}
	return label + " ▾"
}

func (m Model) variantChipLabel() string {
	if m.variant == "" {
		return "default ▾"
	}
	return m.variant + " ▾"
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
	m.askMode = false
	return m
}
