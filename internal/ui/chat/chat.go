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
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
	"github.com/chinmay-sawant/lazykoder/internal/roles"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/skills"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/tips"
	toolcatalog "github.com/chinmay-sawant/lazykoder/internal/tools"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/ui/confirm"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// confirmQueueSize buffers concurrent child/parent confirm requests.
const confirmQueueSize = 32

const (
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
	pickerMaxRows = 16

	// centerDiv splits the leftover space for centering the overlay card.
	centerDiv = 2
	// pickerVpMinWidth is the floor for the picker list viewport.
	pickerVpMinWidth = 12
	// pickerVpDefaultW/H seed the picker viewport before the first resize.
	pickerVpDefaultW = 58
	pickerVpDefaultH = 10
	// promptUndoLimit bounds the in-memory prompt edit history.
	promptUndoLimit = 500
	// copyNoticeDuration controls how long the clipboard confirmation stays visible.
	copyNoticeDuration = 2 * time.Second
	// projectInstructionsDuration controls how long the AGENTS.md notice stays
	// on the alert row before the rotating tips take over.
	projectInstructionsDuration = 5 * time.Second
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
	pickerKindSkills   = "skills"
	pickerKindProvider = "provider"
	pickerKindTool     = "tool"
	pickerKindRole     = "role"
	pickerKindTools    = pickerKindTool
	pickerKindRoles    = pickerKindRole
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
	// sessionTitleMaxRunes prevents generated session titles from dominating
	// compact pickers and headers.
	sessionTitleMaxRunes = 60
)

var (
	errStyle            lipgloss.Style
	busyStyle           lipgloss.Style
	hintStyle           lipgloss.Style
	composerFooterStyle lipgloss.Style
	userStyle           lipgloss.Style
	userRoleStyle       lipgloss.Style
	assistantRoleStyle  lipgloss.Style
	roleStyle           lipgloss.Style
	reasoningStyle      lipgloss.Style
	toolCardStyle       lipgloss.Style
	toolOutputStyle     lipgloss.Style
	editCardStyle       lipgloss.Style
	selectionStyle      lipgloss.Style
	diffAddStyle        lipgloss.Style
	diffDelStyle        lipgloss.Style
	diffMetaStyle       lipgloss.Style
	diffCtxStyle        lipgloss.Style
)

func configureThemeStyles() {
	errStyle = lipgloss.NewStyle().Foreground(theme.ColorDanger())
	busyStyle = lipgloss.NewStyle().Foreground(theme.ColorAccent())
	hintStyle = lipgloss.NewStyle().Foreground(theme.ColorMute())
	composerFooterStyle = lipgloss.NewStyle().Foreground(theme.ColorText())
	userStyle = lipgloss.NewStyle().Foreground(theme.ColorAccent())
	userRoleStyle = lipgloss.NewStyle().Foreground(theme.ColorAccent()).Bold(true)
	assistantRoleStyle = lipgloss.NewStyle().Foreground(theme.ColorAssistantBorder()).Bold(true)
	roleStyle = lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true)
	reasoningStyle = lipgloss.NewStyle().Foreground(theme.ColorMute())
	toolCardStyle = lipgloss.NewStyle().Background(theme.ColorBg()).Foreground(theme.ColorText())
	toolOutputStyle = lipgloss.NewStyle().Background(theme.ColorDialog()).Foreground(theme.ColorMute())
	editCardStyle = lipgloss.NewStyle().Background(theme.ColorEditPanel()).Foreground(theme.ColorText())
	selectionStyle = lipgloss.NewStyle().Background(theme.ColorAccent()).Foreground(theme.ColorBg())
	diffAddStyle = lipgloss.NewStyle().Foreground(theme.ColorGood()).Background(theme.ColorEditAddBg())
	diffDelStyle = lipgloss.NewStyle().Foreground(theme.ColorDanger()).Background(theme.ColorEditDelBg())
	diffMetaStyle = lipgloss.NewStyle().Foreground(theme.ColorEditMeta()).Background(theme.ColorEditPanel())
	diffCtxStyle = lipgloss.NewStyle().Foreground(theme.ColorMute()).Background(theme.ColorEditPanel())
	drawerSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Background(theme.ColorBorder())
	drawerNormalStyle = lipgloss.NewStyle().Foreground(theme.ColorMute())
	drawerHeaderTitleStyle = hintStyle
	drawerHeaderMetaStyle = lipgloss.NewStyle().Foreground(theme.ColorText())
}

// Options configures the chat model.
type Options struct {
	Store             *db.Store
	Client            provider.Client
	ChildClient       provider.Client
	NewProviderClient provider.ClientFactory
	ProviderAuth      provider.AuthChecker
	ProviderLogin     provider.LoginCommandFactory
	Workdir           string
	Session           *db.Session
	MaxSteps          int // ignored when Settings is set; kept for tests
	InitialErr        string
	CachePath         string // optional models cache file; empty disables caching
	SettingsPath      string // .lazykoder/settings.json; empty skips persistence
	Settings          *settings.Settings
	WorktreeDirty     worktreeDirtyChecker
}

// Model is the chat screen: title, transcript, prompt, status and confirm flow.
type Model struct {
	store                *db.Store
	client               provider.Client
	childClient          provider.Client
	newProviderClient    provider.ClientFactory
	providerAuth         provider.AuthChecker
	providerLogin        provider.LoginCommandFactory
	providerAuthStatus   map[string]provider.AuthStatus
	providerLoginTarget  string
	workdir              string
	session              *db.Session
	maxSteps             int
	settingsPath         string
	projectSettings      settings.Settings
	settingsPickerTarget settingsPickerTarget

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
	turnHasNewUser      bool
	turnToolErrors      int
	lastTool            int

	model                string // current model; "" = provider default
	variant              string // current reasoning variant; "" = provider default
	models               []string
	modelInfos           []modelscache.Info
	modelDefaults        map[string]modelDefault
	modelsErr            string
	modelsCached         bool // models came from the cache, not a live fetch
	cachePath            string
	activity             string
	recallScanning       bool
	skillsScanning       bool
	skillsCatalog        skills.Catalog
	activeSkills         []skills.Skill
	toolCatalog          toolcatalog.Catalog
	roleCatalog          roles.Catalog
	toolsScanning        bool
	rolesScanning        bool
	pendingSkillRefs     []recap.MemorySkillReference
	memoryScanJobs       int
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

	confirmCh       chan confirmRequest
	askCh           chan askRequest
	doneCh          chan struct{}
	doneClosed      bool
	pending         *confirmRequest
	pendingAsk      *askRequest
	confirm         confirm.Model
	confirmMode     bool
	formMode        bool
	formHost        *formHost
	addProviderData *addProviderData
	askMode         bool
	askQuestion     question.Question
	askCursor       int

	helpMode bool

	usageMode    bool
	usage        opencode.BillingUsage
	usageLoaded  bool
	usageErr     string
	usageLoading bool

	settingsMode      bool
	settingsCursor    int
	settingsHover     int
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

	pickerVp          viewport.Model
	pickerItems       []string
	pickerProviderIDs []string
	pickerCursor      int
	pickerFilter      string
	pickerFiltering   bool
	pickerBuilt       bool
	pickerMode        bool
	pickerKind        string
	pickerFromPrompt  bool
	pickerSkillItems  []skills.Skill

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
	recapDetailMode       bool
	subagentItems         []subagentRow
	recapItems            []db.RecapRecord
	recapSelected         bool
	subagentCursor        int
	subagentHover         int
	subagentVp            viewport.Model
	subagentLogVp         viewport.Model
	recapDetailVp         viewport.Model
	subagentBuilt         bool
	subagentSelected      subagentRow
	recapDetailRecord     db.RecapRecord
	// subagentLogItems is the child transcript rendered with main chat styles
	// (thinking, tools, work rails). Reasoning starts expanded.
	subagentLogItems    []transcriptItem
	subagentLogSelected int

	// memoryHistoryMode reuses the sub-agent drawer shell for current-chat
	// memory entries without mixing them into child-agent rows.
	memoryHistoryMode       bool
	memoryHistoryDetailMode bool
	memoryHistoryItems      []memoryHistoryItem
	memoryHistoryAll        []memoryHistoryItem
	memoryHistoryPage       int
	memoryHistorySelected   memoryHistoryItem
	memoryHistoryError      string
	memoryHistorySelection  textSelection

	// memoryContextMode shows the exact bounded memory block used by the next
	// parent turn. The toggle is intentionally session-local.
	memoryContextMode      bool
	memoryInjectionEnabled bool
	memoryContext          string
	memoryContextVp        viewport.Model

	slashMode                 bool
	slashCursor               int
	slashItems                []slashCmd
	slashFromPaste            bool
	selection                 textSelection
	promptSel                 promptSelection
	copyNotice                string
	promptSelectAll           bool
	tipsIndex                 int
	projectInstructionsNotice string // set when workdir AGENTS.md loaded; alert-row only

	dragTarget int // -1 none, 0 transcript, 1 picker
	dragOn     bool

	renderCache *renderCache

	// layout is the last outer-geometry snapshot (View + mouse share it).
	layout layoutSnap

	turnSeq              int
	successfulRecapChats int
	turnCancel           context.CancelFunc
	turnCtx              context.Context
	eventCh              chan agent.Event
	errCh                chan error

	// subMgr owns in-process sub-agent jobs for this chat model.
	subMgr *subagent.Manager

	worktreeDirty             worktreeDirtyChecker
	pushPromptUntil           time.Time
	pushPromptBusy            bool
	commitDrawerSelected      int
	commitDrawerActionFocused bool
	commitFiles               []WorktreeFile
	commitBranch              string
	commitDiffPreview         map[string]string
	commitDiffDetailMode      bool
	commitDiffDetailPath      string
	commitDiffHunks           []string
	commitDiffHunkSelected    int
	commitDiffHunkContextMode bool
	commitDiffHunkVp          viewport.Model
	commitDiffDetailVp        viewport.Model

	providerDeleteMode   bool
	providerDeleteTarget string
}

// slashCmd is one entry of the slash command menu. aliases are extra
// prefixes that match the same command (e.g. /session finds /resume).
type slashCmd struct {
	name        string
	description string
	aliases     []string
	group       string
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

type projectInstructionsMsg struct{}

type tpsSample struct {
	at     time.Time
	tokens int64
}

var slashCommands = []slashCmd{
	{name: "/new", description: "start a new session and clear the transcript", group: "Session"},
	{name: "/resume", description: "open past sessions (ctrl+s, also /session)", aliases: []string{"sessions", "session"}, group: "Session"},
	{name: "/continue", description: "resume after a step-limit stop (or send continue)", group: "Session"},
	{name: "/compact", description: "summarize older context now (optional notes)", group: "Session"},
	{name: "/provider", description: "select the active chat provider", group: "Model"},
	{name: "/model", description: "open model settings for new chats", group: "Model"},
	{name: "/variant", description: "open reasoning settings for new chats", group: "Model"},
	{name: "/refresh", description: "reload the model list into models.json", group: "Model"},
	{name: "/agents", description: "open the sub-agent drawer and logs", aliases: []string{"subs", "subagents"}, group: "Project"},
	{name: "/history", description: "open memory history for the current chat", group: "Project"},
	{name: "/memory", description: "show next-turn memory context and toggle injection", group: "Project"},
	{name: "/spawn", description: "spawn a new sub-agent via interactive form", aliases: []string{"agent"}, group: "Project"},
	{name: "/settings", description: "project defaults (model, agents, compaction, safety)", aliases: []string{"slot"}, group: "Project"},
	{name: "/skills", description: "discover and activate local and global skills", aliases: []string{"skill"}, group: "Project"},
	{name: "/tools", description: "discover and enable compiled or declarative tools", aliases: []string{"tool"}, group: "Project"},
	{name: "/roles", description: "discover and select sub-agent roles", aliases: []string{"role"}, group: "Project"},
	{name: "/usage", description: "show OpenCode Go plan usage (rolling, weekly, monthly)", group: "Project"},
	{name: "/status", description: "open the status drawer and toggle details", group: "Project"},
	{name: "/diff", description: "open the current worktree diff drawer", group: "Project"},
	{name: "/help", description: "keyboard shortcuts (?, also /keys)", aliases: []string{"keys"}, group: "Help"},
}

type modelsMsg struct {
	list      []string
	infos     []modelscache.Info
	defaults  map[string]modelDefault
	err       error
	fromCache bool
	notice    string
}

type modelDefault struct {
	model   string
	variant string
}

type skillsMsg struct {
	catalog skills.Catalog
	err     error
}

type toolsMsg struct {
	catalog toolcatalog.Catalog
	err     error
}

type rolesMsg struct {
	catalog roles.Catalog
	err     error
}

type eventMsg struct {
	seq    int
	ev     agent.Event
	events <-chan agent.Event
	errs   <-chan error
}

type eventDoneMsg struct {
	seq    int
	err    error
	events <-chan agent.Event
	errs   <-chan error
}

type subagentCancelDoneMsg struct {
	id  string
	err error
}

type pulseMsg struct{}

type recapDoneMsg struct{}

type memoryDoneMsg struct {
	err error
}

type tipsTickMsg struct{}

type providerAuthMsg struct {
	id     string
	status provider.AuthStatus
}

type providerLoginMsg struct {
	id  string
	err error
}

// New returns a chat model for the given options.
func New(opts Options) Model {
	cfg := settings.Default()
	if opts.Settings != nil {
		cfg = *opts.Settings
	} else if opts.MaxSteps > 0 {
		cfg.Slot.MaxSteps = opts.MaxSteps
		cfg.Slot.LimitEnabled = true
	}
	if opts.Client != nil && cfg.EffectiveProvider() == "openai" && cfg.Model.Default == settings.DefaultModelID {
		cfg.Model.Default = opts.Client.Model()
	}
	theme.SetMode(cfg.EffectiveTheme())
	configureThemeStyles()
	if opts.Client != nil && opts.Settings != nil {
		retry := cfg.EffectiveRetry()
		opts.Client.SetRetryPolicy(opencode.RetryPolicy{
			MaxRetries: retry.MaxRetries,
			Delay:      time.Duration(retry.DelaySeconds) * time.Second,
		})
	}
	// Effective* helpers normalize clamps and empty model ids.
	eff := cfg.EffectiveMaxSteps()
	m := Model{
		store:                  opts.Store,
		client:                 opts.Client,
		childClient:            opts.ChildClient,
		newProviderClient:      opts.NewProviderClient,
		providerAuth:           opts.ProviderAuth,
		providerLogin:          opts.ProviderLogin,
		providerAuthStatus:     make(map[string]provider.AuthStatus),
		modelDefaults:          make(map[string]modelDefault),
		workdir:                opts.Workdir,
		session:                opts.Session,
		maxSteps:               eff,
		settingsPath:           opts.SettingsPath,
		projectSettings:        cfg,
		err:                    opts.InitialErr,
		width:                  defaultWidth,
		height:                 defaultHeight,
		confirmCh:              make(chan confirmRequest, confirmQueueSize),
		askCh:                  make(chan askRequest, confirmQueueSize),
		doneCh:                 make(chan struct{}),
		lastTool:               -1,
		selectedItem:           -1,
		historyCursor:          -1,
		pendingHistoryIndex:    -1,
		settingsHover:          -1,
		userNavHover:           -1,
		userNavTip:             -1,
		cachePath:              opts.CachePath,
		worktreeDirty:          opts.WorktreeDirty,
		commitDiffPreview:      make(map[string]string),
		commitDiffHunkVp:       viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(1)),
		commitDiffDetailVp:     viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(1)),
		transcript:             viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(defaultHeight-chromeLines)),
		todoVp:                 viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(maxTodoPanelRows)),
		prompt:                 newPromptArea(defaultWidth),
		memoryInjectionEnabled: true,
		memoryContextVp:        viewport.New(viewport.WithWidth(defaultWidth-1), viewport.WithHeight(1)),
		renderCache:            &renderCache{},
	}
	if m.providerAuth == nil {
		m.providerAuth = provider.CheckAuth
	}
	if m.providerLogin == nil {
		m.providerLogin = provider.LoginCommand
	}
	for _, descriptor := range provider.Descriptors() {
		m.providerAuthStatus[descriptor.ID] = provider.InitialAuthStatus(descriptor.ID)
	}
	// Manager boot + recover; model/confirm refreshed per turn via wireSubMgrRuntime.
	m = m.attachSubMgr(cfg, opts.Store != nil)
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
	cmds := []tea.Cmd{m.confirmWatch(), m.askWatch(), m.fetchModels, tipsTick()}
	for _, descriptor := range provider.Descriptors() {
		if descriptor.AuthMethod != provider.AuthMethodAPIKey {
			cmds = append(cmds, m.checkProviderAuth(descriptor.ID))
		}
	}
	if m.projectSettings.EffectiveRecap().Enabled && m.store != nil && m.client != nil {
		cmds = append(cmds, m.recoverRecaps, m.recoverMemoryUpdates)
	}
	if m.projectInstructionsNotice != "" {
		cmds = append(cmds, clearProjectInstructionsNotice())
	}
	return tea.Batch(cmds...)
}

func (m Model) fetchModels() tea.Msg {
	activeProvider := m.projectSettings.EffectiveProvider()
	if m.cachePath != "" && activeProvider != provider.IDCodex && activeProvider != provider.IDGrok {
		if infos, fresh, err := modelscache.Load(m.cachePath, time.Now(), modelscache.DefaultTTL); err == nil && fresh && len(infos) > 0 && modelscache.HasContext(infos) && cachedModelCatalogsAreCurrent(infos) {
			return modelsMsg{list: modelscache.IDs(infos), infos: infos, fromCache: true}
		}
	}
	return m.refreshModels()
}

func cachedModelCatalogsAreCurrent(infos []modelscache.Info) bool {
	for _, providerID := range modelCatalogProviderIDs() {
		if providerID != provider.IDCodex && providerID != provider.IDGrok {
			continue
		}
		found := false
		for _, info := range infos {
			if providerIDForModelInfo(info) == providerID {
				if providerID == provider.IDGrok && len(info.Variants) == 0 {
					return false
				}
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// refreshModels fetches the model list from the API, rewrites the cache, and
// falls back to a stale cache when the fetch fails.
func (m Model) refreshModels() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
	defer cancel()
	var previous []modelscache.Info
	if m.cachePath != "" {
		previous, _, _ = modelscache.Load(m.cachePath, time.Now(), modelscache.DefaultTTL)
	}
	activeProvider := m.projectSettings.EffectiveProvider()
	var cached []modelscache.Info
	defaults := make(map[string]modelDefault, len(modelCatalogProviderIDs()))
	var unavailable []string
	var activeErr error
	loaded := false
	for _, providerID := range modelCatalogProviderIDs() {
		client, clientErr := m.modelCatalogClient(providerID)
		if clientErr != nil || client == nil {
			cached = modelscache.MergeByID(cached, modelInfosForProvider(previous, providerID))
			unavailable = append(unavailable, modelProviderLabel(providerID))
			if providerID == activeProvider {
				activeErr = clientErr
			}
			continue
		}
		infos, err := client.ModelInfos(ctx)
		if err != nil {
			cached = modelscache.MergeByID(cached, modelInfosForProvider(previous, providerID))
			unavailable = append(unavailable, modelProviderLabel(providerID))
			if providerID == activeProvider {
				activeErr = err
			}
			continue
		}
		loaded = true
		defaults[providerID] = modelDefault{
			model:   client.Model(),
			variant: clientDefaultVariant(client),
		}
		catalog := toCacheInfos(infos)
		catalog = modelscache.PreserveSpecializedEndpoints(catalog, modelInfosForProvider(previous, providerID))
		if providerID == provider.IDOpenCode {
			if catalogClient, ok := client.(provider.FreeModelCatalog); ok {
				extras, xerr := catalogClient.FreeModelInfos(ctx)
				if xerr == nil && len(extras) > 0 {
					catalog = modelscache.MergeByID(catalog, toCacheInfos(extras))
				}
			}
			if live, lerr := fetchLiveCatalog(ctx, client); lerr == nil {
				catalog = modelscache.ApplyLive(catalog, live)
			}
			catalog = modelscache.FilterDeprecated(catalog)
		}
		cached = modelscache.MergeByID(cached, catalog)
	}
	cached = modelscache.FilterDeprecated(cached)
	list := modelscache.IDs(cached)
	if loaded {
		if m.cachePath != "" {
			if serr := modelscache.Save(m.cachePath, cached, time.Now()); serr != nil {
				return modelsMsg{list: list, infos: cached, err: fmt.Errorf("models cache: %w", serr)}
			}
		}
		notice := fmt.Sprintf("models updated (%d)", len(list))
		if len(unavailable) > 0 {
			notice += "; " + strings.Join(unavailable, ", ") + " unavailable"
		}
		return modelsMsg{list: list, infos: cached, defaults: defaults, notice: notice}
	}
	if m.cachePath != "" {
		if len(previous) > 0 {
			return modelsMsg{list: modelscache.IDs(previous), infos: previous, fromCache: true, err: activeErr}
		}
	}
	return modelsMsg{list: list, infos: cached, err: activeErr}
}

// Fetch Codex first so a signed-in subscription row owns a model ID also
// advertised by the public OpenCode catalog. The drawer has its own display
// order below, which keeps OpenCode first for browsing.
func modelCatalogProviderIDs() []string {
	return provider.IDs()
}

func modelPickerProviderIDs() []string {
	return provider.IDs()
}

func (m Model) modelCatalogClient(id string) (provider.Client, error) {
	if id == m.projectSettings.EffectiveProvider() && m.client != nil {
		return m.client, nil
	}
	if m.newProviderClient == nil {
		return nil, nil
	}
	return m.newProviderClient(id)
}

func fetchLiveCatalog(ctx context.Context, client provider.Client) (map[string]modelscache.Info, error) {
	if client == nil || !strings.Contains(client.BaseURL(), "opencode.ai") {
		return nil, nil
	}
	return modelscache.Fetch(ctx, client.HTTP())
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
	case providerAuthMsg:
		m.providerAuthStatus[msg.id] = msg.status
		if m.pickerMode && m.pickerKind == pickerKindProvider {
			m.applyFilter()
		}
		if m.providerLoginTarget == msg.id && msg.status.State == provider.AuthStateReady {
			m.providerLoginTarget = ""
			return m.activateProvider(msg.id)
		}
		return m, nil
	case providerLoginMsg:
		if msg.err != nil {
			m.providerLoginTarget = ""
			m.err = "provider sign-in failed: " + msg.err.Error()
			return m, nil
		}
		m.providerAuthStatus[msg.id] = provider.AuthStatus{State: provider.AuthStateChecking, Label: "checking sign-in"}
		return m, m.checkProviderAuth(msg.id)
	case confirmRequestMsg:
		m.pending = &msg.req
		qualifier := "rm"
		if msg.req.dec.Destructive {
			qualifier = "rm -rf"
		}
		m.confirm = confirm.New(msg.req.subject, qualifier)
		m = m.setFocus(focusConfirm)
		return m, m.confirmWatch()
	case askRequestMsg:
		m.pendingAsk = &msg.req
		m.askQuestion = msg.req.q
		m.askCursor = 0
		m = m.setFocus(focusAsk)
		return m, m.askWatch()
	case eventMsg:
		if msg.seq != m.turnSeq {
			return m, m.watchEvents(msg.seq, msg.events, msg.errs)
		}
		m = m.applyEvent(msg.ev)
		return m, m.watchEvents(msg.seq, msg.events, msg.errs)
	case eventDoneMsg:
		if msg.seq != m.turnSeq {
			return m, nil
		}
		changeEligible := m.parentTurnEligible(msg.err) && m.turnToolErrors == 0
		successfulTurn := m.successfulTurnEligible(msg.err)
		memoryEligible := m.memoryEligible(msg.err)
		recapEligible := m.recapEligible(msg.err)
		if successfulTurn {
			if recapEligible {
				m.successfulRecapChats = 0
			} else {
				m.successfulRecapChats++
			}
		}
		m = m.finishTurn(msg.err)
		memoryCmd := tea.Cmd(nil)
		if memoryEligible {
			memoryCmd = m.scheduleMemoryUpdate()
			if memoryCmd != nil {
				m.memoryScanJobs++
				m.pulseOn = true
			}
		}
		var recapCmd tea.Cmd
		if recapEligible {
			recapCmd = m.scheduleRecap()
		}
		worktreeCmd := tea.Cmd(nil)
		if changeEligible {
			worktreeCmd = m.checkWorktree()
		}
		// Continue baton status motion while background sub-agents are live.
		if m.hasLiveSubagents() || m.hasInFlightTools() || m.memoryScanJobs > 0 {
			return m, tea.Batch(worktreeCmd, memoryCmd, recapCmd, pulseTick())
		}
		return m, tea.Batch(worktreeCmd, memoryCmd, recapCmd)
	case subagentCancelDoneMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m = m.reloadSubagentRows()
		return m.resizeSubagentDrawer(), nil
	case pulseMsg:
		// Keep throbbing for live sub-agents even after the parent turn ends.
		if !m.busy && !m.hasInFlightTools() && !m.hasLiveSubagents() && !m.recallScanning && !m.skillsScanning && m.memoryScanJobs == 0 {
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
	case recapDoneMsg:
		m = m.reloadSubagentRows()
		if m.subagentPickerMode && !m.subagentLogMode {
			return m.resizeSubagentDrawer(), nil
		}
		return m, nil
	case memoryDoneMsg:
		if m.memoryScanJobs > 0 {
			m.memoryScanJobs--
		}
		if msg.err != nil && !errors.Is(msg.err, recap.ErrInsufficientMessages) {
			m.err = appendChatError(m.err, "memory update failed: "+msg.err.Error())
		}
		return m, nil
	case worktreeStatusMsg:
		if msg.err != nil {
			m.err = appendChatError(m.err, msg.err.Error())
			return m, nil
		}
		if msg.info.Dirty || msg.manual {
			m.pushPromptUntil = time.Now().Add(commitActionLifetime)
			m.commitBranch = msg.info.Branch
			m.commitFiles = msg.info.Files
			m.commitDiffPreview = msg.info.Diffs
			m.commitDrawerActionFocused = false
			if len(m.commitFiles) > 0 {
				m.commitDrawerSelected = 0
			}
			m.layout = layoutSnap{}
			return m, m.scheduleCommitPushExpiry()
		}
		m.pushPromptUntil = time.Time{}
		m.commitFiles = nil
		m.commitBranch = msg.info.Branch
		m.commitDiffPreview = nil
		m.commitDrawerActionFocused = false
		m.layout = layoutSnap{}
		return m, nil
	case commitActionExpiredMsg:
		if !m.pushPromptUntil.IsZero() && !time.Now().Before(m.pushPromptUntil) {
			m.pushPromptUntil = time.Time{}
			m.commitDiffDetailMode = false
			m.commitDiffDetailPath = ""
			m.commitDiffHunks = nil
			m.commitDiffHunkSelected = 0
			m.commitDiffHunkContextMode = false
			m.commitDrawerActionFocused = false
			m.layout = layoutSnap{}
		}
		return m, nil
	case tipsTickMsg:
		m.tipsIndex++
		return m, tipsTick()
	case modelsMsg:
		m.models = msg.list
		if msg.defaults != nil {
			m.modelDefaults = msg.defaults
		}
		if len(msg.infos) > 0 {
			m.modelInfos = msg.infos
			if m.projectSettings.EffectiveProvider() == provider.IDCodex && msg.err == nil {
				settingsChanged := false
				if m.model != "" {
					if _, ok := m.selectedModelInfo(m.model); !ok {
						m.model = ""
						settingsChanged = true
					}
				}
				if model := m.projectSettings.Model.Default; model != "" {
					if _, ok := m.selectedModelInfo(model); !ok {
						m.projectSettings.Model.Default = ""
						settingsChanged = true
					}
				}
				if m.model == "" && m.client != nil {
					if model := m.client.Model(); model != "" {
						if _, ok := m.selectedModelInfo(model); ok {
							m.model = model
							m.projectSettings.Model.Default = model
							if m.variant == "" {
								if variant := clientDefaultVariant(m.client); m.modelHasVariant(model, variant) {
									m.variant = variant
									m.projectSettings.Model.Variant = variant
								}
							}
							m.syncSessionModel()
							m.syncSessionVariant()
							settingsChanged = true
						}
					}
				}
				if settingsChanged {
					m = m.persistSettings()
				}
			}
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
	case skillsMsg:
		m.skillsScanning = false
		m.skillsCatalog = msg.catalog
		m.pickerSkillItems = append([]skills.Skill{}, msg.catalog.Skills...)
		if msg.err != nil {
			m.modelsErr = msg.err.Error()
		}
		if m.pickerMode && m.pickerKind == pickerKindSkills {
			m.applyFilter()
		}
		return m, nil
	case toolsMsg:
		m.toolsScanning = false
		m.toolCatalog = msg.catalog
		snapshot := make(map[string]toolplugin.Tool, len(msg.catalog.Tools))
		for _, descriptor := range msg.catalog.Tools {
			snapshot[descriptor.Name] = descriptor.Plugin()
		}
		_ = toolplugin.ReplaceDiscovered(snapshot)
		if msg.err != nil {
			m.modelsErr = msg.err.Error()
		}
		if m.pickerMode && m.pickerKind == pickerKindTools {
			m.applyFilter()
		}
		return m, nil
	case rolesMsg:
		m.rolesScanning = false
		m.roleCatalog = msg.catalog
		if msg.err != nil {
			m.modelsErr = msg.err.Error()
		}
		if m.pickerMode && m.pickerKind == pickerKindRoles {
			m.applyFilter()
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
	case projectInstructionsMsg:
		m.projectInstructionsNotice = ""
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
				if m.memoryHistoryDetailMode {
					m = m.resizeMemoryHistoryDetail()
				} else if m.recapDetailMode {
					m = m.resizeRecapDetail()
				} else {
					m = m.resizeSubagentLogCard()
				}
			} else if m.subagentPickerMode {
				m = m.resizeSubagentDrawer()
			}
		}
		if m.commitDiffDetailMode {
			m = m.resizeCommitDiffDetail()
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
		if msg.Mod.Contains(tea.ModCtrl) && (msg.Code == 'c' || msg.Code == 'C') {
			if m.memoryHistoryDetailMode {
				// The memory detail owns Ctrl+C, including its select-all state.
			} else if m.promptEditing() && (m.prompt.Value() != "" || m.promptSelectAll || m.promptSel.hasRange() || m.selection.hasRange()) {
				// Fall through to updateKey: copy if text is selected, or clear the input box if not selected.
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
		if m.commitDiffDetailMode {
			return m.handleCommitDiffDetailKey(msg)
		}
		if isUndoKey(msg) && !m.confirmMode && !m.pickerMode && !m.sessionPickerMode && !m.subagentPickerMode {
			return m.undoPrompt(), nil
		}
		if msg.Mod.Contains(tea.ModCtrl) && msg.Code == 's' && !m.confirmMode && !m.pickerMode && !m.sessionPickerMode && !m.subagentPickerMode && !m.busy {
			return m.openSessionPicker(), nil
		}
		switch m.currentFocus() {
		case focusProviderDelete:
			return m.updateProviderDeleteKey(msg)
		case focusForm:
			return m.updateFormKey(msg)
		case focusConfirm:
			return m.updateConfirmKey(msg)
		case focusAsk:
			return m.updateAskKey(msg)
		case focusHelp:
			return m.updateHelpKey(msg)
		case focusUsage:
			return m.updateUsageKey(msg)
		case focusSettings:
			return m.updateSettingsKey(msg)
		case focusStatus:
			return m.updateStatusKey(msg)
		case focusFilePicker:
			return m.updateFilePickerKey(msg)
		case focusPicker:
			return m.updatePickerKey(msg)
		case focusSessions:
			return m.updateSessionPickerKey(msg)
		case focusSubagents, focusSubagentLog:
			return m.updateSubagentPickerKey(msg)
		case focusSlash:
			return m.updateSlashKey(msg)
		}
		return m.updateKey(msg)
	case tea.MouseWheelMsg:
		if m.askMode {
			return m, nil
		}
		if m.commitDiffDetailMode {
			if m.commitDiffHunkContextMode {
				vp, viewportCmd := m.commitDiffDetailVp.Update(msg)
				m.commitDiffDetailVp = vp
				m, timerCmd := m.resetCommitDrawerTimer()
				return m, tea.Batch(viewportCmd, timerCmd)
			}
			delta := 1
			if msg.Mouse().Button == tea.MouseWheelUp {
				delta = -1
			}
			return m.navigateCommitDiffHunk(delta)
		}
		if m.sessionPickerMode {
			vp, _ := m.sessionVp.Update(msg)
			m.sessionVp = vp
			return m, nil
		}
		if m.subagentLogMode {
			if m.memoryHistoryDetailMode {
				vp, _ := m.subagentLogVp.Update(msg)
				m.subagentLogVp = vp
				return m, nil
			}
			if m.recapDetailMode {
				vp, _ := m.recapDetailVp.Update(msg)
				m.recapDetailVp = vp
				return m, nil
			}
			vp, _ := m.subagentLogVp.Update(msg)
			m.subagentLogVp = vp
			return m, nil
		}
		if m.commitDrawerVisible() && m.pointerInCommitDrawer(msg.Mouse().Y) {
			if msg.Mouse().Y < m.commitDrawerTop()+1 {
				return m, nil
			}
			delta := 1
			if strings.Contains(msg.String(), "up") || msg.Button == tea.MouseWheelUp {
				delta = -1
			}
			files := m.commitDrawerFiles()
			if len(files) > 0 {
				next := m.commitDrawerSelected + delta
				if next < 0 {
					next = 0
				}
				if next >= len(files) {
					next = len(files) - 1
				}
				m.commitDrawerSelected = next
				nm, cmd := m.resetCommitDrawerTimer()
				m = nm
				return m, cmd
			}
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
		if m.commitDiffDetailMode {
			return m, nil
		}
		if m.settingsMode {
			prev := m.settingsHover
			m.settingsHover = -1
			if row, ok := m.settingsRowAtScreenY(msg.Mouse().Y); ok {
				m.settingsHover = row
			}
			if m.settingsHover != prev {
				m.layout.settingsPaint = ""
				return m, nil
			}
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
		if m.memoryHistoryDetailMode && m.memoryHistorySelection.dragging {
			return m.updateMemoryHistoryDetailSelection(msg.Mouse()), nil
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
		if m.memoryHistorySelection.dragging {
			m.memoryHistorySelection.dragging = false
			text, ok := m.memoryHistorySelectedText()
			if !ok {
				return m, nil
			}
			m.copyNotice = "Text copied"
			return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
		}
		m.selection.dragging = false
		m.dragOn = false
		text, ok := m.selectedText()
		if !ok {
			return m, nil
		}
		m.copyNotice = "Text copied"
		return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
	default:
		if m.formMode && m.formHost != nil {
			return m.updateFormMsg(msg)
		}
	}
	return m, nil
}

type defaultVariantClient interface {
	DefaultVariant() string
}

func clientDefaultVariant(client provider.Client) string {
	if configured, ok := client.(defaultVariantClient); ok {
		return configured.DefaultVariant()
	}
	return ""
}

func (m Model) applyEvent(ev agent.Event) Model {
	switch ev.Kind {
	case agent.EventSessionCreated:
		m = m.adoptSession(ev.SessionID)
	case agent.EventRecallStarted:
		m.recallScanning = true
		m.activity = "scanning memory patterns"
		m.pulseOn = true
	case agent.EventRecallFinished:
		m.recallScanning = false
		if m.busy {
			m.activity = "thinking"
		}
	case agent.EventSkillsStarted:
		m.skillsScanning = true
		m.activity = "scanning skills"
		m.pulseOn = true
	case agent.EventSkillsFinished:
		m.skillsScanning = false
		m.pendingSkillRefs = m.pendingSkillRefs[:0]
		for _, skill := range ev.Skills {
			m.pendingSkillRefs = append(m.pendingSkillRefs, recap.MemorySkillReference{
				ID:          recap.SkillReferenceID(skill.Name, string(skill.Scope), skill.Path),
				Name:        skill.Name,
				Scope:       string(skill.Scope),
				Path:        skill.Path,
				ContentHash: skill.ContentHash,
				UseCount:    1,
			})
		}
		if m.busy {
			m.activity = "thinking"
		}
	case agent.EventMessage:
		if ev.Role == "user" && m.pendingHistoryIndex >= 0 && m.pendingHistoryIndex < len(m.inputHistory) {
			m.inputHistory[m.pendingHistoryIndex].messageID = ev.MessageID
			m.pendingHistoryIndex = -1
		}
	case agent.EventPart:
		part := dbPartFromDelta(ev.Part)
		m.applyPart(part)
		m = m.noteActivityFromPart(ev.Part)
	case agent.EventTool:
		m.applyTool(ev)
		if ev.Tool.Status == "error" || ev.Tool.Status == "denied" {
			m.turnToolErrors++
		}
		m.activity = toolActivity(ev.Tool)
		// On task tool events: open drawer only when a new job appears.
		if ev.Tool.Name == "task" || strings.HasPrefix(ev.Tool.Name, "task_") {
			m = m.openSubagentDrawerIfNew()
			m.pulseOn = m.busy || m.hasInFlightTools() || m.hasLiveSubagents()
		}
		if ev.Tool.Name == "todowrite" {
			m = m.applyTodosFromTool(dbToolFromDelta(ev.Tool))
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
		m.applyCompactNotice(dbPartFromDelta(ev.Part))
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

func (m Model) ensureSession(title string) Model {
	if m.session != nil || m.store == nil {
		return m
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Sub-agent run"
	}
	if len([]rune(title)) > sessionTitleMaxRunes {
		title = string([]rune(title)[:sessionTitleMaxRunes])
	}
	var variantPtr *string
	if m.variant != "" {
		variantPtr = &m.variant
	}
	sess, err := m.store.CreateSession(context.Background(), db.Session{
		Title:     title,
		Directory: m.workdir,
		Provider:  m.projectSettings.EffectiveProvider(),
		Model:     m.model,
		Variant:   variantPtr,
	})
	if err == nil {
		m.session = &sess
		m.wireSubMgrRuntime()
	}
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
	m.compacting = false
	m.pendingUser = ""
	m.turnHasNewUser = false
	m.turnToolErrors = 0
	m.pushPromptBusy = false
	m.pending = nil
	m.pendingAsk = nil
	m.activeSkills = nil
	m = m.clearFocus(focusConfirm)
	m = m.clearFocus(focusAsk)
	m.eventCh = nil
	m.errCh = nil
	m.activity = ""
	m.recallScanning = false
	m.skillsScanning = false
	m.collapseLiveReasoning()
	// Refresh sub-agent rows after the turn; drawer stays as the user left it
	// (only a new spawn re-opens it via openSubagentDrawerIfNew).
	m = m.syncSubagentDrawer()
	if m.hasLiveSubagents() || m.hasInFlightTools() {
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
	return errors.Is(err, agent.ErrStepLimit)
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

func (m Model) noteActivityFromPart(p agent.PartDelta) Model {
	switch p.Kind {
	case agent.PartDeltaReasoning:
		if p.Text != "" {
			m.activity = "thinking  " + firstLine(p.Text, activityMaxRunes)
		} else {
			m.activity = "thinking"
		}
	case agent.PartDeltaText:
		if p.Text != "" {
			m.activity = "writing  " + firstLine(p.Text, activityMaxRunes)
		}
	case agent.PartDeltaStepStart:
		if m.activity == "" {
			m.activity = "thinking"
		}
	}
	return m
}

func toolActivity(tc agent.ToolDelta) string {
	name := tc.Name
	if name == "" {
		name = "tool"
	}
	cmd := toolCommand(dbToolFromDelta(tc))
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

func clearProjectInstructionsNotice() tea.Cmd {
	return tea.Tick(projectInstructionsDuration, func(time.Time) tea.Msg { return projectInstructionsMsg{} })
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
	ta.SetStyles(promptStyles())
	ta.Focus()
	return ta
}

func promptStyles() textarea.Styles {
	plain := lipgloss.NewStyle().Background(theme.ColorComposer()).Foreground(theme.ColorText())
	mute := lipgloss.NewStyle().Background(theme.ColorComposer()).Foreground(theme.ColorMute())
	return textarea.Styles{
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
	}
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

// modelEndpoint is the provider endpoint for the selected model. Cached
// OpenCode rows are re-derived so a stale chat-completions cache entry cannot
// bypass a model's current protocol route.
func (m Model) modelEndpoint() string {
	id := m.model
	if id == "" && m.client != nil {
		id = m.client.Model()
	}
	if info, ok := m.selectedModelInfo(id); ok {
		if endpoint := canonicalModelEndpoint(m.client, info); endpoint != "" {
			return endpoint
		}
	}
	if m.client == nil {
		return ""
	}
	return fallbackModelEndpoint(m.client.BaseURL(), id)
}

func (m Model) modelEndpointFor(id string) string {
	if info, ok := m.selectedModelInfo(id); ok {
		if endpoint := canonicalModelEndpoint(m.client, info); endpoint != "" {
			return endpoint
		}
	}
	if m.client == nil {
		return ""
	}
	if id == "" {
		id = m.client.Model()
	}
	if id != "" {
		return fallbackModelEndpoint(m.client.BaseURL(), id)
	}
	return ""
}

func canonicalModelEndpoint(client provider.Client, info modelscache.Info) string {
	if info.Endpoint != "" {
		return info.Endpoint
	}
	if client == nil {
		return ""
	}
	return fallbackModelEndpoint(client.BaseURL(), info.ID)
}

func fallbackModelEndpoint(base, id string) string {
	if modelscache.IsFree(modelscache.Info{ID: id}) {
		return opencode.RouteForModel(base, id).Endpoint
	}
	return opencode.ChatURL(base)
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
	// Include the hard-coded custom option as an extra selectable row
	total := len(opts) + 1
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q':
		return m.resolveAskIndex(-1), nil
	case tea.KeyEnter:
		if m.askCursor == len(opts) {
			return m.openAskCustomForm()
		}
		return m.resolveAskIndex(m.askCursor), nil
	case 'j', tea.KeyDown:
		if m.askCursor < total-1 {
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
		if idx == len(opts) {
			return m.openAskCustomForm()
		}
	}
	return m, nil
}

func (m Model) openAskCustomForm() (Model, tea.Cmd) {
	var text string
	input := huh.NewInput().
		Title("Type your own answer here").
		Description("This will be sent to the LLM instead of the canned options").
		Placeholder("Your custom answer...").
		Value(&text).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("answer cannot be empty")
			}
			return nil
		})
	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(formTheme()).
		WithWidth(min(formOverlayMaxWidth, max(minPaneWidth, m.width-cardBorder)))
	host := &formHost{
		form:  form,
		title: "Custom answer",
		kind:  "ask-custom",
		width: min(formOverlayMaxWidth, max(minPaneWidth, m.width-cardBorder)),
		onDone: func(mod Model) (Model, tea.Cmd) {
			trimmed := strings.TrimSpace(text)
			// Append custom answer to both the UI question and the pending ask
			// so the tool's index validation passes and the LLM receives the
			// free-form text instead of a canned option.
			if mod.pendingAsk != nil {
				mod.pendingAsk.q.Options = append(mod.pendingAsk.q.Options, trimmed)
				mod.askQuestion.Options = append(mod.askQuestion.Options, trimmed)
				idx := len(mod.pendingAsk.q.Options) - 1
				mod = mod.clearFocus(focusForm)
				return mod.resolveAskIndex(idx), nil
			}
			// Fallback: treat as custom index
			mod.askQuestion.Options = append(mod.askQuestion.Options, trimmed)
			idx := len(mod.askQuestion.Options) - 1
			mod = mod.clearFocus(focusForm)
			return mod.resolveAskIndex(idx), nil
		},
		onCancel: func(mod Model) (Model, tea.Cmd) {
			mod = mod.clearFocus(focusForm)
			// Restore the ask overlay
			mod = mod.setFocus(focusAsk)
			return mod, nil
		},
	}
	// Keep the pending ask alive but hide the ask overlay while the custom
	// input is shown. The overlayOn dim stays via form overlay.
	m = m.setFocus(focusForm)
	m.formHost = host
	// Preserve askCursor at custom index so cancel returns to same spot
	m.askCursor = len(m.askQuestion.Options)
	cmd := form.Init()
	host.form = form
	return m, cmd
}

func (m Model) updateHelpKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', '?':
		m = m.clearFocus(focusHelp)
	}
	return m, nil
}
