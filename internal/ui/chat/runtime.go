package chat

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

const eventChanBuffer = 64

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
