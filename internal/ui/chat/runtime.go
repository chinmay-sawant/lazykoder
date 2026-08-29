package chat

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/orchestrator"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/skills"
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
	maxMemoryRecall  = 4_000
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
	childClient := m.childClient
	if childClient == nil {
		childClient = m.client
	}
	runner := subagent.AgentRunner{
		Store:    m.store,
		Client:   childClient,
		Provider: cfg.EffectiveOrchestrator().Provider,
	}
	m.subMgr = subagent.NewManager(subagent.ConfigFromSettings(cfg), runner)
	rt := subagent.Runtime{
		Workdir:  m.workdir,
		Model:    childModel(cfg),
		Endpoint: m.childModelEndpoint(cfg),
		Variant:  m.childModelVariant(cfg),
		Profiles: m.subagentModelProfiles(),
		Confirm:  m.confirmHook,
		Ask:      m.runtimeAsk,
		Skills:   m.explicitSkillContexts(context.Background()),
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
		Provider:             m.projectSettings.EffectiveProvider(),
		Model:                m.model,
		Endpoint:             m.modelEndpoint(),
		Variant:              m.effectiveVariantFor(m.modelLabel(), m.projectSettings.EffectiveProvider(), m.variant),
		ToolNames:            enabledToolNames(m.projectSettings),
		Confirm:              m.confirmHook,
		Ask:                  m.askHook,
		BashAllowlist:        m.projectSettings.EffectiveAgents().BashAllowlist,
		BashAllowlistEnabled: m.projectSettings.EffectiveAgents().BashAllowlistEnabled,
		ContextWindow:        int64(m.modelContext(m.modelLabel())),
		TokensUsed:           m.tokensUsed,
		OutgoingModel:        m.prevModel,
		OutgoingWindow:       m.prevWindow,
		OutgoingEndpoint:     m.modelEndpointFor(m.prevModel),
		CompactAuto:          cfg.Auto,
		CompactPercent:       cfg.Percent,
		KeepTokens:           cfg.KeepTokens,
		CompactReason:        m.pendingCompactReason,
		Orchestrator: orchestrator.Config{
			Enabled:          m.projectSettings.EffectiveAgents().Enabled && m.projectSettings.EffectiveOrchestrator().Enabled,
			Review:           m.projectSettings.EffectiveOrchestrator().Review,
			Model:            m.model,
			Endpoint:         m.modelEndpoint(),
			MaxSubtasks:      orchestrator.MaxSubtasks,
			ModelClassByRole: m.projectSettings.EffectiveOrchestrator().ModelClassByRole,
			ExploreClass:     m.projectSettings.EffectiveOrchestrator().ExploreClass,
			PlanClass:        m.projectSettings.EffectiveOrchestrator().PlanClass,
			GeneralClass:     m.projectSettings.EffectiveOrchestrator().GeneralClass,
		},
	}
	opts.ToolProvider = func(_ context.Context, _, _ string) ([]string, error) {
		return enabledToolNames(m.projectSettings), nil
	}
	opts.RoleProvider = func(_ context.Context, _, _ string) (string, error) {
		return m.projectSettings.EffectiveAgents().DefaultRole, nil
	}
	if m.projectSettings.EffectiveRecap().Enabled {
		opts.Recall = m.recall
	}
	if m.memoryInjectionEnabled {
		opts.Memory = m.memoryProvider
	}
	if m.projectSettings.EffectiveSkills().Enabled {
		opts.Skills = m.skillProvider
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

func enabledToolNames(s settings.Settings) []string {
	enabled := s.EffectiveTools().Enabled
	names := make([]string, 0, len(enabled))
	for name := range enabled {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// skillProvider discovers approved roots and reads only the selected,
// bounded descriptors needed for the current request. It never writes to the
// transcript or executes descriptor content.
func (m Model) skillProvider(ctx context.Context, _ string, userText string) ([]skills.Context, error) {
	cfg := m.projectSettings.EffectiveSkills()
	if !cfg.Enabled {
		return nil, nil
	}
	opts := skills.DefaultOptions(m.workdir)
	opts.IncludeLocal = cfg.IncludeLocal
	opts.IncludeGlobal = cfg.IncludeGlobal
	opts.MaxAutoMatches = cfg.MaxAutoMatches
	opts.MaxBody = cfg.MaxBodyBytes
	catalog, err := skills.Discover(ctx, opts)
	if err != nil {
		return nil, err
	}
	selected := make([]skills.Match, 0, cfg.MaxAutoMatches)
	seen := make(map[string]struct{})
	for _, skill := range m.activeSkills {
		for _, candidate := range catalog.Skills {
			if candidate.DescriptorPath != skill.DescriptorPath {
				continue
			}
			selected = append(selected, skills.Match{Skill: candidate, Reasons: []string{"explicit"}})
			seen[strings.ToLower(candidate.Name)] = struct{}{}
			break
		}
	}
	if cfg.AutoDetect {
		for _, match := range catalog.AutoMatches(userText, cfg.MaxAutoMatches) {
			key := strings.ToLower(match.Skill.Name)
			if _, ok := seen[key]; ok {
				continue
			}
			selected = append(selected, match)
			seen[key] = struct{}{}
			if len(selected) >= cfg.MaxAutoMatches {
				break
			}
		}
	}
	var out []skills.Context
	used := 0
	for _, match := range selected {
		body, readErr := skills.ReadBody(ctx, match.Skill, cfg.MaxBodyBytes)
		if readErr != nil {
			continue
		}
		remaining := cfg.MaxContextBytes - used
		if remaining <= 0 {
			break
		}
		body = truncateRecall(body, remaining)
		used += len([]rune(body))
		out = append(out, skills.Context{
			Name:        match.Skill.Name,
			Description: match.Skill.Description,
			Scope:       match.Skill.Scope,
			Path:        match.Skill.DisplayPath,
			ContentHash: match.Skill.ContentHash,
			Reasons:     append([]string{}, match.Reasons...),
			Body:        body,
		})
	}
	return out, nil
}

// recall searches local memory sources before the first ordinary parent
// request when the prompt asks for historical context. The pattern is built
// from quoted words so user text never becomes executable regular expression
// syntax.
func (m Model) recall(ctx context.Context, _ string, userText string) (string, error) {
	if m.workdir == "" || strings.TrimSpace(userText) == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !recallRequested(userText) {
		return "", nil
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
	pattern := strings.Join(quoted, "|")
	var parts []string
	sources := []struct {
		label string
		path  string
		cap   int
		valid bool
	}{
		{label: "MEMORY", path: "knowledge-base/memories.md", cap: maxMemoryRecall, valid: true},
		{label: "RECAP", path: "knowledge-base/recaps", cap: maxRecallOutput},
	}
	for _, source := range sources {
		if source.valid {
			if _, err := recap.ReadMemoryDocument(m.workdir); err != nil {
				continue
			}
		}
		result, err := grep.Run(searchCtx, m.workdir, grep.Options{
			Pattern:         pattern,
			Path:            source.path,
			Glob:            "*.md",
			CaseInsensitive: true,
			MaxMatches:      maxRecallMatches,
		}, nil)
		if err != nil || strings.TrimSpace(result.Output) == "" || strings.TrimSpace(result.Output) == "no matches" {
			continue
		}
		parts = append(parts, source.label+"\n"+truncateRecall(result.Output, source.cap))
		if source.label == "MEMORY" {
			return truncateRecall(strings.Join(parts, "\n\n"), maxRecallOutput), nil
		}
	}
	if len(parts) == 0 {
		result, err := grep.Run(searchCtx, m.workdir, grep.Options{
			Pattern:         pattern,
			Path:            "knowledge-base",
			Glob:            "*.md",
			CaseInsensitive: true,
			MaxMatches:      maxRecallMatches,
		}, nil)
		if err == nil && strings.TrimSpace(result.Output) != "" && strings.TrimSpace(result.Output) != "no matches" {
			parts = append(parts, "KNOWLEDGE-BASE\n"+truncateRecall(result.Output, maxRecallOutput))
		}
	}
	if len(parts) == 0 && m.client != nil {
		selected, selectErr := recap.SelectRecapContext(
			searchCtx,
			m.client,
			m.projectSettings.EffectiveRecap().Model,
			m.recapModelInfo(m.projectSettings.EffectiveRecap().Model),
			userText,
			m.workdir,
		)
		if selectErr == nil && strings.TrimSpace(selected) != "" {
			parts = append(parts, "RECAP\n"+selected)
		}
	}
	return truncateRecall(strings.Join(parts, "\n\n"), maxRecallOutput), nil
}

func recallRequested(text string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, phrase := range []string{"last session", "what did we decide"} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	words := strings.FieldsFunc(normalized, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	triggers := map[string]struct{}{
		"memory": {}, "remember": {}, "recall": {}, "recap": {}, "recent": {},
		"previous": {}, "earlier": {}, "preference": {}, "decision": {},
		"avoid": {}, "mistake": {}, "history": {}, "context": {},
	}
	for _, word := range words {
		if _, ok := triggers[word]; ok {
			return true
		}
	}
	return false
}

func truncateRecall(value string, limit ...int) string {
	max := maxRecallOutput
	if len(limit) > 0 && limit[0] > 0 {
		max = limit[0]
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
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
	childClient := m.childClient
	if childClient == nil {
		childClient = m.client
	}
	m.subMgr.SetRunner(subagent.AgentRunner{
		Store:    m.store,
		Client:   childClient,
		Provider: m.projectSettings.EffectiveOrchestrator().Provider,
	})
	m.subMgr.SetStore(m.store)
	m.subMgr.SetRuntime(subagent.Runtime{
		Workdir:  m.workdir,
		Model:    childModel(m.projectSettings),
		Endpoint: m.childModelEndpoint(m.projectSettings),
		Variant:  m.childModelVariant(m.projectSettings),
		Profiles: m.subagentModelProfiles(),
		Confirm:  m.confirmHook,
		Ask:      m.runtimeAsk,
		Skills:   m.explicitSkillContexts(context.Background()),
	})
}

func (m Model) explicitSkillContexts(ctx context.Context) []skills.Context {
	if !m.projectSettings.EffectiveSkills().Enabled || len(m.activeSkills) == 0 {
		return nil
	}
	cfg := m.projectSettings.EffectiveSkills()
	opts := skills.DefaultOptions(m.workdir)
	opts.IncludeLocal = cfg.IncludeLocal
	opts.IncludeGlobal = cfg.IncludeGlobal
	catalog, err := skills.Discover(ctx, opts)
	if err != nil {
		return nil
	}
	var out []skills.Context
	for _, active := range m.activeSkills {
		for _, candidate := range catalog.Skills {
			if candidate.DescriptorPath != active.DescriptorPath {
				continue
			}
			body, readErr := skills.ReadBody(ctx, candidate, cfg.MaxBodyBytes)
			if readErr != nil {
				continue
			}
			out = append(out, skills.Context{
				Name: candidate.Name, Description: candidate.Description, Scope: candidate.Scope,
				Path: candidate.DisplayPath, ContentHash: candidate.ContentHash, Body: body,
			})
		}
	}
	return out
}

func (m Model) subagentModelProfiles() []subagent.ModelProfile {
	childProvider := m.projectSettings.EffectiveOrchestrator().Provider
	infos := modelInfosForProvider(m.modelInfos, childProvider)
	if len(infos) == 0 {
		for _, info := range m.modelInfos {
			if providerIDForModelInfo(info) == "" {
				infos = append(infos, info)
			}
		}
	}
	profiles := make([]subagent.ModelProfile, 0, len(infos))
	for _, info := range infos {
		profiles = append(profiles, subagent.ModelProfile{
			ID:            info.ID,
			Endpoint:      info.Endpoint,
			ContextWindow: int64(info.Context),
			Variants:      append([]string{}, info.Variants...),
		})
	}
	return profiles
}

func childModel(cfg settings.Settings) string {
	if model := strings.TrimSpace(cfg.EffectiveAgents().ModelOverride); model != "" {
		return model
	}
	return provider.DefaultModel(cfg.EffectiveOrchestrator().Provider)
}

func (m Model) childModelVariant(cfg settings.Settings) string {
	a := cfg.EffectiveAgents()
	providerID := cfg.EffectiveOrchestrator().Provider
	return m.effectiveVariantFor(childModel(cfg), providerID, a.ModelVariant)
}

func (m Model) childModelEndpoint(cfg settings.Settings) string {
	model := childModel(cfg)
	if info, ok := m.modelInfoForProvider(model, cfg.EffectiveOrchestrator().Provider); ok {
		if endpoint := canonicalModelEndpoint(m.childClient, info); endpoint != "" {
			return endpoint
		}
	}
	if model == m.model && m.client != nil && (m.childClient == nil || m.childClient.BaseURL() == m.client.BaseURL()) {
		return m.modelEndpoint()
	}
	if m.childClient == nil {
		return ""
	}
	return fallbackModelEndpoint(m.childClient.BaseURL(), model)
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
	m.pendingSkillRefs = nil
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
	return m.parentTurnEligible(err) && m.projectSettings.EffectiveRecap().Enabled
}

func (m Model) parentTurnEligible(err error) bool {
	if err != nil || !m.turnHasNewUser {
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

func (m Model) memoryEligible(err error) bool {
	return m.parentTurnEligible(err) && (m.projectSettings.EffectiveRecap().Enabled || m.projectSettings.EffectiveSkills().Remember)
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
		worker := recap.NewWorker(client, model, info, m.effectiveVariantFor(model, m.projectSettings.EffectiveProvider(), ""))
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

// scheduleMemoryUpdate returns a hidden background command for every
// successful parent turn. It owns a fresh context because finishTurn cancels
// the interactive turn context.
func (m Model) scheduleMemoryUpdate() tea.Cmd {
	recapCfg := m.projectSettings.EffectiveRecap()
	skillCfg := m.projectSettings.EffectiveSkills()
	if (!recapCfg.Enabled && !skillCfg.Remember) || m.store == nil || m.client == nil || m.session == nil {
		return nil
	}
	sessionID := m.session.ID
	model := m.memoryWorkerModel()
	if model == "" {
		return nil
	}
	workdir, err := filepath.Abs(m.workdir)
	if err != nil {
		return nil
	}
	settingsPath := m.settingsPath
	store := m.store
	client := m.client
	info := m.recapModelInfo(model)
	snapshotStarted := time.Now()
	snapshot, snapshotErr := recap.BuildSnapshot(context.Background(), store, sessionID, recap.SnapshotOptions{
		MinimumMessageCount: recap.MemoryMinimumMessageCount,
	})
	var anchor recap.Snapshot
	var anchorErr error
	if snapshotErr != nil && errors.Is(snapshotErr, recap.ErrInsufficientMessages) {
		anchor, anchorErr = recap.BuildAnchorSnapshot(context.Background(), store, sessionID)
		if anchorErr == nil {
			snapshot = anchor
			snapshotErr = nil
		}
	}
	snapshotDuration := time.Since(snapshotStarted)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), recapTimeout)
		defer cancel()
		if !memoryEnabledAtPath(settingsPath) {
			return memoryDoneMsg{}
		}
		if snapshotErr != nil || anchorErr != nil {
			if snapshotErr != nil {
				return memoryDoneMsg{err: fmt.Errorf("build snapshot: %w", snapshotErr)}
			}
			return memoryDoneMsg{err: fmt.Errorf("build anchor snapshot: %w", anchorErr)}
		}
		record, created, err := store.ReserveMemoryUpdate(ctx, db.MemoryUpdate{
			Workdir:            workdir,
			SourceSessionID:    snapshot.SessionID,
			SourceEndSeq:       snapshot.SourceEndSeq,
			SourceEndMessageID: snapshot.SourceEndMessageID,
			Model:              model,
		})
		if err != nil {
			return memoryDoneMsg{err: fmt.Errorf("reserve memory update: %w", err)}
		}
		if !created {
			switch record.Status {
			case db.MemoryUpdateStatusCompleted, db.MemoryUpdateStatusQueued, db.MemoryUpdateStatusRunning:
				return memoryDoneMsg{}
			case db.MemoryUpdateStatusFailed:
				if err := store.RequeueMemoryUpdate(ctx, record.ID); err != nil {
					return memoryDoneMsg{err: fmt.Errorf("requeue memory update: %w", err)}
				}
			default:
				return memoryDoneMsg{}
			}
		}
		if !memoryEnabledAtPath(settingsPath) {
			return memoryDoneMsg{}
		}
		workerModel := record.Model
		workerInfo := info
		if workerModel != model {
			workerInfo = m.recapModelInfo(workerModel)
		}
		worker := recap.NewMemoryWorker(client, workerModel, workerInfo, m.effectiveVariantFor(workerModel, m.projectSettings.EffectiveProvider(), ""))
		runErr := recap.RunMemoryUpdate(ctx, recap.MemoryRunInput{
			Store:            store,
			Record:           record,
			Snapshot:         snapshot,
			Workdir:          workdir,
			Worker:           worker,
			SkillReferences:  append([]recap.MemorySkillReference{}, m.pendingSkillRefs...),
			Enabled:          func() bool { return memoryEnabledAtPath(settingsPath) },
			SnapshotDuration: snapshotDuration,
		})
		return memoryDoneMsg{err: runErr}
	}
}

func (m Model) memoryWorkerModel() string {
	if recap := m.projectSettings.EffectiveRecap(); recap.Enabled {
		if model := strings.TrimSpace(recap.Model); model != "" {
			return model
		}
	}
	candidates := []string{m.model}
	if m.session != nil {
		candidates = append(candidates, m.session.Model)
	}
	if m.client != nil {
		candidates = append(candidates, m.client.Model())
	}
	candidates = append(candidates, m.projectSettings.EffectiveModel())
	for _, candidate := range candidates {
		if model := strings.TrimSpace(candidate); model != "" {
			return model
		}
	}
	return ""
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

func memoryEnabledAtPath(settingsPath string) bool {
	if strings.TrimSpace(settingsPath) == "" {
		return true
	}
	cfg, err := settings.LoadFile(settingsPath)
	if err != nil {
		return true
	}
	return cfg.EffectiveRecap().Enabled || cfg.EffectiveSkills().Remember
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
		worker := recap.NewWorker(m.client, record.Model, m.recapModelInfo(record.Model), m.effectiveVariantFor(record.Model, m.projectSettings.EffectiveProvider(), ""))
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

// recoverMemoryUpdates resumes queued or interrupted updates for the current
// workdir. The source message anchor makes recovery independent of newer chat
// messages added after the process stopped.
func (m Model) recoverMemoryUpdates() tea.Msg {
	if (!m.projectSettings.EffectiveRecap().Enabled && !m.projectSettings.EffectiveSkills().Remember) || m.store == nil || m.client == nil || (m.session != nil && (m.session.Kind == db.SessionKindSubagent || m.session.ParentSessionID != nil)) {
		return nil
	}
	workdir, err := filepath.Abs(m.workdir)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), recapTimeout)
	defer cancel()
	updates, err := m.store.ListMemoryUpdatesForRecovery(ctx, workdir)
	if err != nil {
		return nil
	}
	for _, update := range updates {
		if update.Status == db.MemoryUpdateStatusRunning || update.Status == db.MemoryUpdateStatusFailed {
			if err := m.store.RequeueMemoryUpdate(ctx, update.ID); err != nil {
				continue
			}
			update.Status = db.MemoryUpdateStatusQueued
		}
		snapshotStarted := time.Now()
		snapshot, err := recap.BuildSnapshot(ctx, m.store, update.SourceSessionID, recap.SnapshotOptions{
			AnchorMessageID:     update.SourceEndMessageID,
			MinimumMessageCount: recap.MemoryMinimumMessageCount,
		})
		snapshotDuration := time.Since(snapshotStarted)
		if err != nil {
			_ = m.store.FailMemoryUpdate(ctx, update.ID, "source snapshot unavailable: "+err.Error())
			continue
		}
		worker := recap.NewMemoryWorker(m.client, update.Model, m.recapModelInfo(update.Model), m.effectiveVariantFor(update.Model, m.projectSettings.EffectiveProvider(), ""))
		_ = recap.RunMemoryUpdate(ctx, recap.MemoryRunInput{
			Store:            m.store,
			Record:           update,
			Snapshot:         snapshot,
			Workdir:          workdir,
			Worker:           worker,
			Enabled:          func() bool { return memoryEnabledAtPath(m.settingsPath) },
			SnapshotDuration: snapshotDuration,
		})
	}
	return nil
}

func (m Model) recapModelInfo(model string) modelscache.Info {
	if info, ok := m.selectedModelInfo(model); ok {
		return info
	}
	info := modelscache.Info{ID: model}
	if m.client != nil {
		info.Endpoint = fallbackModelEndpoint(m.client.BaseURL(), model)
	}
	return info
}
