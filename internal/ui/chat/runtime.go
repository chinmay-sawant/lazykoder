package chat

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/tools/grep"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

const (
	eventChanBuffer  = 64
	recallTimeout    = 750 * time.Millisecond
	recapTimeout     = 5 * time.Minute
	maxRecallTerms   = 12
	maxRecallOutput  = 12_000
	maxRecallMatches = 20
)

// turnRun is the agent call for one start-turn path (send / compact / continue).
type turnRun func(ctx context.Context, ag *agent.Agent, eventCh chan agent.Event) error

// turnStart is shared prep for submit / runCompact / resumeAfterLimit.
type turnStart struct {
	activity   string
	compacting bool
	run        turnRun
}

// runtimeAsk adapts the agent question hook to subagent.Runtime.Ask.
func (m Model) runtimeAsk(prompt string, options []string) (int, error) {
	return m.askHook(question.Question{Question: prompt, Options: options})
}

// attachSubMgr boots Manager with store/runner/runtime and optionally recovers jobs.
func (m Model) attachSubMgr(cfg settings.Settings, recoverJobs bool) Model {
	runner := subagent.AgentRunner{Store: m.store, Client: m.client}
	m.subMgr = subagent.NewManager(subagent.ConfigFromSettings(cfg), runner)
	rt := subagent.Runtime{
		Workdir:  m.workdir,
		Model:    cfg.EffectiveModel(),
		Variant:  cfg.EffectiveVariant(),
		Profiles: m.subagentModelProfiles(),
		Confirm:  m.confirmHook,
		Ask:      m.runtimeAsk,
	}
	if recoverJobs && m.store != nil {
		if err := m.subMgr.Boot(context.Background(), m.store, rt, runner); err != nil {
			m.err = "subagent recovery failed: " + err.Error()
		}
		return m
	}
	m.subMgr.SetStore(m.store)
	m.subMgr.SetRuntime(rt)
	return m
}

func (m Model) rebuildSubMgr() Model {
	if m.store == nil || m.client == nil {
		return m
	}
	if m.subMgr != nil {
		m.subMgr.Shutdown()
	}
	return m.attachSubMgr(m.projectSettings, true)
}

// agentOptions builds Options for the parent agent, including the subagent Host.
func (m Model) agentOptions() agent.Options {
	cfg := m.projectSettings.EffectiveCompaction()
	opts := agent.Options{
		Session:              m.session,
		MaxSteps:             m.maxSteps,
		Model:                m.model,
		Endpoint:             m.modelEndpoint(),
		Variant:              m.variant,
		Confirm:              m.confirmHook,
		Ask:                  m.askHook,
		BashAllowlist:        m.projectSettings.EffectiveAgents().BashAllowlist,
		BashAllowlistEnabled: m.projectSettings.EffectiveAgents().BashAllowlistEnabled,
		ContextWindow:        int64(modelscache.ContextOf(m.modelInfos, m.modelLabel())),
		TokensUsed:           m.tokensUsed,
		OutgoingModel:        m.prevModel,
		OutgoingWindow:       m.prevWindow,
		OutgoingEndpoint:     modelscache.EndpointOf(m.modelInfos, m.prevModel),
		CompactAuto:          cfg.Auto,
		CompactPercent:       cfg.Percent,
		KeepTokens:           cfg.KeepTokens,
		CompactReason:        m.pendingCompactReason,
	}
	if m.projectSettings.EffectiveRecap().Enabled {
		opts.Recall = m.recall
	}
	if m.subMgr == nil || !m.projectSettings.EffectiveAgents().Enabled {
		return opts
	}
	m.wireSubMgrRuntime()
	host := subagent.NewHost(m.subMgr)
	if m.session != nil {
		host.ParentSessionID = m.session.ID
	}
	opts.Host = host
	return opts
}

// recall searches the local recap tree before the first ordinary parent
// request. The pattern is built from quoted words so user text never becomes
// executable regular expression syntax.
func (m Model) recall(ctx context.Context, _ string, userText string) (string, error) {
	if m.workdir == "" || strings.TrimSpace(userText) == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	terms := recallTerms(userText)
	if len(terms) == 0 {
		return "", nil
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, regexp.QuoteMeta(term))
	}
	searchCtx, cancel := context.WithTimeout(ctx, recallTimeout)
	defer cancel()
	result, err := grep.Run(searchCtx, m.workdir, grep.Options{
		Pattern:         strings.Join(quoted, "|"),
		Path:            "knowledge-base/recaps",
		Glob:            "*.md",
		CaseInsensitive: true,
		MaxMatches:      maxRecallMatches,
	}, nil)
	if err != nil || strings.TrimSpace(result.Output) == "" || strings.TrimSpace(result.Output) == "no matches" {
		return "", nil
	}
	return truncateRecall(result.Output), nil
}

func truncateRecall(value string) string {
	runes := []rune(value)
	if len(runes) <= maxRecallOutput {
		return value
	}
	return string(runes[:maxRecallOutput])
}

func recallTerms(text string) []string {
	terms := make(map[string]struct{})
	for _, raw := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-'
	}) {
		term := strings.ToLower(strings.TrimSpace(raw))
		if len([]rune(term)) < 4 || recallStopWord(term) {
			continue
		}
		terms[term] = struct{}{}
	}
	ordered := make([]string, 0, len(terms))
	for term := range terms {
		ordered = append(ordered, term)
	}
	sort.Strings(ordered)
	if len(ordered) > maxRecallTerms {
		ordered = ordered[:maxRecallTerms]
	}
	return ordered
}

func recallStopWord(term string) bool {
	switch term {
	case "that", "this", "with", "from", "have", "will", "should", "would", "there", "about", "into", "only", "then", "than", "were", "been", "they", "your", "user", "assistant":
		return true
	default:
		return false
	}
}

// wireSubMgrRuntime refreshes runner/store/runtime before a parent turn.
func (m Model) wireSubMgrRuntime() {
	if m.subMgr == nil {
		return
	}
	m.subMgr.SetRunner(subagent.AgentRunner{Store: m.store, Client: m.client})
	m.subMgr.SetStore(m.store)
	m.subMgr.SetRuntime(subagent.Runtime{
		Workdir:  m.workdir,
		Model:    m.model,
		Endpoint: m.modelEndpoint(),
		Variant:  m.variant,
		Profiles: m.subagentModelProfiles(),
		Confirm:  m.confirmHook,
		Ask:      m.runtimeAsk,
	})
}

func (m Model) subagentModelProfiles() []subagent.ModelProfile {
	profiles := make([]subagent.ModelProfile, 0, len(m.modelInfos))
	for _, info := range m.modelInfos {
		profiles = append(profiles, subagent.ModelProfile{
			ID:            info.ID,
			Endpoint:      info.Endpoint,
			ContextWindow: int64(info.Context),
			Variants:      append([]string{}, info.Variants...),
		})
	}
	return profiles
}

// startTurn arms channels, builds the Agent, and returns watch/pulse cmds.
func (m Model) startTurn(start turnStart) (Model, tea.Cmd) {
	m.busy = true
	m.compacting = start.compacting
	m.err = ""
	m.stepLimitHit = false
	m.copyNotice = ""
	m.projectInstructionsNotice = ""
	m.promptSelectAll = false
	m.promptUndo = nil
	m.slashFromPaste = false
	m.turnGenTokens = 0
	m.tpsSamples = nil
	m.stepMetrics = false
	m.syncTranscript()
	m.turnSeq++
	seq := m.turnSeq
	ctx, cancel := context.WithCancel(context.Background())
	m.turnCancel = cancel
	m.turnCtx = ctx
	m.eventCh = make(chan agent.Event, eventChanBuffer)
	m.errCh = make(chan error, 1)
	ag := agent.New(m.store, m.client, m.workdir, m.agentOptions())
	eventCh, errCh := m.eventCh, m.errCh
	run := start.run
	sendCmd := func() tea.Msg {
		go func() { errCh <- run(ctx, ag, eventCh) }()
		return nil
	}
	m.pulse = 0
	m.pulseOn = true
	m.activity = start.activity
	m.turnStarted = time.Now()
	return m, tea.Batch(sendCmd, m.watchEvents(seq), pulseTick())
}

func (m Model) successfulTurnEligible(err error) bool {
	if err != nil {
		return false
	}
	if !m.projectSettings.EffectiveRecap().Enabled || !m.turnHasNewUser {
		return false
	}
	if m.store == nil || m.client == nil || m.session == nil {
		return false
	}
	return m.session.Kind != db.SessionKindSubagent && m.session.ParentSessionID == nil
}

func (m Model) recapEligible(err error) bool {
	if !m.successfulTurnEligible(err) {
		return false
	}
	return m.successfulRecapChats+1 >= m.projectSettings.EffectiveRecap().AfterChats
}

// scheduleRecap returns a hidden background command. It deliberately creates
// its own context because finishTurn cancels the interactive turn context.
func (m Model) scheduleRecap() tea.Cmd {
	cfg := m.projectSettings.EffectiveRecap()
	if !cfg.Enabled || m.store == nil || m.client == nil || m.session == nil {
		return nil
	}
	sessionID := m.session.ID
	model := cfg.Model
	workdir := m.workdir
	settingsPath := m.settingsPath
	store := m.store
	client := m.client
	info := m.recapModelInfo(model)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), recapTimeout)
		defer cancel()
		if !recapEnabledAtPath(settingsPath) {
			return recapDoneMsg{}
		}
		snapshot, err := recap.BuildSnapshot(ctx, store, sessionID, recap.SnapshotOptions{})
		if err != nil {
			return recapDoneMsg{}
		}
		record, created, err := store.ReserveRecap(ctx, db.RecapRecord{
			SessionID:          snapshot.SessionID,
			SourceStartSeq:     snapshot.SourceStartSeq,
			SourceEndSeq:       snapshot.SourceEndSeq,
			SourceStartTime:    snapshot.SourceStartTime,
			SourceEndTime:      snapshot.SourceEndTime,
			SourceEndMessageID: snapshot.SourceEndMessageID,
			Model:              model,
		})
		if err != nil {
			return recapDoneMsg{}
		}
		if !created {
			switch record.Status {
			case db.RecapStatusCompleted, db.RecapStatusCancelled, db.RecapStatusQueued, db.RecapStatusRunning:
				return recapDoneMsg{}
			case db.RecapStatusFailed:
				if err := store.RequeueRecap(ctx, record.ID); err != nil {
					return recapDoneMsg{}
				}
				record.Status = db.RecapStatusQueued
			default:
				return recapDoneMsg{}
			}
		}
		if !recapEnabledAtPath(settingsPath) {
			_ = store.CancelRecap(ctx, record.ID)
			return recapDoneMsg{}
		}
		worker := recap.NewWorker(client, model, info, "")
		_, _ = recap.Run(ctx, recap.RunInput{
			Store:    store,
			Record:   record,
			Snapshot: snapshot,
			Workdir:  workdir,
			Worker:   worker,
			Enabled:  func() bool { return recapEnabledAtPath(settingsPath) },
		})
		return recapDoneMsg{}
	}
}

func recapEnabledAtPath(settingsPath string) bool {
	if strings.TrimSpace(settingsPath) == "" {
		return true
	}
	cfg, err := settings.LoadFile(settingsPath)
	if err != nil {
		return true
	}
	return cfg.EffectiveRecap().Enabled
}

// recoverRecaps resumes queued or interrupted records for the current main
// session. The source message ID anchors each rebuilt window, so later turns
// cannot change the work that was reserved before a restart.
func (m Model) recoverRecaps() tea.Msg {
	if !m.projectSettings.EffectiveRecap().Enabled || m.store == nil || m.client == nil || m.session == nil || m.session.Kind == db.SessionKindSubagent || m.session.ParentSessionID != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), recapTimeout)
	defer cancel()
	records, err := m.store.ListOpenRecaps(ctx)
	if err != nil {
		return nil
	}
	for _, record := range records {
		if record.SessionID != m.session.ID {
			continue
		}
		if record.Status == db.RecapStatusRunning {
			if err := m.store.RequeueRecap(ctx, record.ID); err != nil {
				continue
			}
			record.Status = db.RecapStatusQueued
		}
		snapshot, err := recap.BuildSnapshot(ctx, m.store, record.SessionID, recap.SnapshotOptions{
			AnchorMessageID: record.SourceEndMessageID,
		})
		if err != nil {
			_ = m.store.FailRecap(ctx, record.ID, "source snapshot unavailable: "+err.Error())
			continue
		}
		worker := recap.NewWorker(m.client, record.Model, m.recapModelInfo(record.Model), "")
		_, _ = recap.Run(ctx, recap.RunInput{
			Store:    m.store,
			Record:   record,
			Snapshot: snapshot,
			Workdir:  m.workdir,
			Worker:   worker,
			Enabled:  func() bool { return recapEnabledAtPath(m.settingsPath) },
		})
	}
	return nil
}

func (m Model) recapModelInfo(model string) modelscache.Info {
	for _, info := range m.modelInfos {
		if info.ID == model {
			return info
		}
	}
	info := modelscache.Info{ID: model}
	if m.client != nil {
		info.Endpoint = opencode.RouteForModel(m.client.BaseURL(), model).Endpoint
	}
	return info
}
