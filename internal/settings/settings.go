// Package settings loads and saves project-level lazykoder preferences.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/provider"
)

const (
	// FileName is the settings file under .lazykoder/.
	FileName = "settings.json"
	// DefaultModelID is the built-in chat model when none is configured.
	DefaultModelID = "deepseek-v4-flash"
	// DefaultTheme is the initial application palette.
	DefaultTheme = "dark"
	// DefaultMaxSteps matches the agent default when no file exists.
	DefaultMaxSteps = 16
	// MinMaxSteps is the lowest configurable step budget.
	MinMaxSteps = 1
	// MaxMaxSteps caps the step budget so a runaway loop stays bounded.
	// Raised to 1000 so real multi-tool explore sub-agent jobs (which burn
	// several steps per tool round) do not die with "step limit reached".
	MaxMaxSteps = 1000
	// unlimitedMaxSteps is the effective agent budget when the limit is off.
	// The agent still needs a finite loop bound for safety.
	unlimitedMaxSteps = 10_000
	// DefaultMaxConcurrent is the default number of concurrent sub-agents.
	DefaultMaxConcurrent = 4
	// MaxMaxConcurrent caps concurrent sub-agents.
	MaxMaxConcurrent = 20
	// MinMaxConcurrent is the lowest concurrent sub-agent budget.
	MinMaxConcurrent = 1
	// DefaultChildMaxSteps is the default step budget for child agents.
	// 12 was too tight for multi-tool explore jobs (children burned the
	// whole budget on tools and failed with "step limit reached"). 1000
	// matches MaxMaxSteps so explore jobs can finish tool-heavy work.
	DefaultChildMaxSteps = 1000
	// DefaultAgentsTimeoutSec is the default sub-agent timeout in seconds.
	DefaultAgentsTimeoutSec = 600
	// DefaultMaxQueued is the default sub-agent queue size.
	DefaultMaxQueued = 40
	// DefaultMaxDepth is the default sub-agent nesting depth.
	DefaultMaxDepth = 1
	// MaxMaxDepth caps sub-agent nesting depth. Product depth is 1 until
	// nested Host ships; do not advertise editable depth above 1.
	MaxMaxDepth = 1
	// MaxMaxQueued caps the sub-agent queue size.
	MaxMaxQueued = 100
	// MinCompactPercent is the lowest selectable auto-compact threshold.
	MinCompactPercent = 5
	// MaxCompactPercent is the highest selectable auto-compact threshold.
	MaxCompactPercent = 99
	// DefaultRecapAfterChats schedules a recap after two successful chats.
	DefaultRecapAfterChats = 2
	// MinRecapAfterChats is the lowest recap scheduling threshold.
	MinRecapAfterChats = 1
	// MaxRecapAfterChats caps the recap scheduling threshold.
	MaxRecapAfterChats = 20
	// DefaultRetryMaxRetries is the number of transient API retries after the
	// initial request.
	DefaultRetryMaxRetries = 5
	// MinRetryMaxRetries allows retries to be disabled explicitly.
	MinRetryMaxRetries = 0
	// MaxRetryMaxRetries caps the configured retry count.
	MaxRetryMaxRetries = 20
	// DefaultRetryDelaySeconds is the delay between transient API attempts.
	DefaultRetryDelaySeconds = 10
	// MinRetryDelaySeconds allows immediate retries when explicitly selected.
	MinRetryDelaySeconds = 0
	// MaxRetryDelaySeconds caps the configured retry delay.
	MaxRetryDelaySeconds = 300
	// DefaultSkillMaxAutoMatches limits automatic skill context injection.
	DefaultSkillMaxAutoMatches = 2
	// MinSkillMaxAutoMatches is the smallest automatic skill match count.
	MinSkillMaxAutoMatches = 1
	// MaxSkillMaxAutoMatches caps automatic skill context injection.
	MaxSkillMaxAutoMatches = 12
	// DefaultSkillMaxBodyBytes bounds one descriptor in a model request.
	DefaultSkillMaxBodyBytes = 48 * 1024
	// MaxSkillMaxBodyBytes caps one descriptor body read.
	MaxSkillMaxBodyBytes = 256 * 1024
	// DefaultSkillMaxContextBytes bounds the combined skill context block.
	DefaultSkillMaxContextBytes = 96 * 1024
	// MaxSkillMaxContextBytes caps the combined skill context block.
	MaxSkillMaxContextBytes = 256 * 1024
	// defaultCompactPercent / defaultKeepTokens mirror agent.DefaultCompact*
	// (agent owns the named runtime constants; settings only persists knobs).
	defaultCompactPercent = 80
	defaultKeepTokens     = 15_000
	// settingsDirMode is used when creating parent dirs for settings.json.
	settingsDirMode = 0o755
	// settingsFileMode is the on-disk mode for settings.json.
	settingsFileMode = 0o600
	// percentScale is the denominator for percent calculations (100%).
	percentScale = 100
)

// Slot holds agent loop / step-budget preferences.
type Slot struct {
	// MaxSteps is the tool-calling rounds per user turn when LimitEnabled.
	MaxSteps int `json:"max_steps"`
	// LimitEnabled turns the step budget on or off.
	LimitEnabled bool `json:"limit_enabled"`
}

// Model holds default model and reasoning preferences for new turns.
type Model struct {
	// Default is the model id used for new sessions and when the live
	// chat model has not been chosen yet.
	Default string `json:"default"`
	// Variant is the default reasoning effort (low, medium, high, max).
	// Empty means the provider default.
	Variant string `json:"variant"`
}

// Provider selects the active chat provider for the parent agent.
type Provider struct {
	Active string `json:"active"`
}

// Orchestrator controls hidden decomposition and role model classes.
type Orchestrator struct {
	Enabled      bool   `json:"enabled"`
	Review       bool   `json:"review"`
	Provider     string `json:"provider"`
	ExploreClass string `json:"explore_class"`
	PlanClass    string `json:"plan_class"`
	GeneralClass string `json:"general_class"`
}

// Appearance holds visual preferences for the TUI.
type Appearance struct {
	// Theme is "dark" or "light". Unknown values fall back to dark.
	Theme string `json:"theme"`
}

// Agents holds multi-agent / sub-agent preferences.
type Agents struct {
	Enabled              bool   `json:"enabled"`
	MaxConcurrent        int    `json:"max_concurrent"`
	MaxQueued            int    `json:"max_queued"`
	MaxDepth             int    `json:"max_depth"`
	DefaultTimeoutSec    int    `json:"default_timeout_sec"`
	ChildMaxSteps        int    `json:"child_max_steps"`
	ModelOverride        string `json:"model_override"`
	ModelVariant         string `json:"model_variant"`
	ExploreModel         string `json:"explore_model"`
	BashConfirm          string `json:"bash_confirm"` // "parent" or "deny"
	BashAllowlistEnabled bool   `json:"bash_allowlist_enabled"`
	// BashAllowlist is a comma-separated executable allowlist.
	BashAllowlist        []string `json:"bash_allowlist"`
	AllowParallelWriters bool     `json:"allow_parallel_writers"`
	DefaultRole          string   `json:"default_role"` // explore|plan|general
}

// Compaction holds auto-compact thresholds.
type Compaction struct {
	// Auto runs the preflight size check. Manual /compact and one
	// overflow retry stay available when this is false.
	Auto bool `json:"auto"`
	// Percent is the fill of the live model window that triggers
	// auto-compact (5-99). Default 80 means used > 80% of context.
	Percent int `json:"percent"`
	// KeepTokens is the recent tail kept beside a summary.
	KeepTokens int64 `json:"keep_tokens"`
}

// Recap holds the hidden local-memory recap worker preferences.
type Recap struct {
	// Enabled controls whether completed parent turns may create recaps.
	Enabled bool `json:"enabled"`
	// Model is the model id used for recap generation.
	Model string `json:"model"`
	// AfterChats is the number of successful parent chats before scheduling.
	AfterChats int `json:"after_chats"`
}

// Retry holds transient chat API retry preferences.
type Retry struct {
	// MaxRetries is in addition to the initial request.
	MaxRetries int `json:"max_retries"`
	// DelaySeconds is the wait between retry attempts.
	DelaySeconds int `json:"delay_seconds"`
}

// Skills controls bounded discovery and request-time skill context.
type Skills struct {
	Enabled         bool `json:"enabled"`
	AutoDetect      bool `json:"auto_detect"`
	IncludeLocal    bool `json:"include_local"`
	IncludeGlobal   bool `json:"include_global"`
	Remember        bool `json:"remember"`
	MaxAutoMatches  int  `json:"max_auto_matches"`
	MaxBodyBytes    int  `json:"max_body_bytes"`
	MaxContextBytes int  `json:"max_context_bytes"`
}

// Settings is the on-disk project config under .lazykoder/settings.json.
type Settings struct {
	Appearance   Appearance   `json:"appearance"`
	Slot         Slot         `json:"slot"`
	Model        Model        `json:"model"`
	Provider     Provider     `json:"provider"`
	Orchestrator Orchestrator `json:"orchestrator"`
	Agents       Agents       `json:"agents"`
	Compaction   Compaction   `json:"compaction"`
	Recap        Recap        `json:"recap"`
	Retry        Retry        `json:"retry"`
	Skills       Skills       `json:"skills"`
}

// Default returns the built-in defaults.
func Default() Settings {
	return Settings{
		Appearance: Appearance{Theme: DefaultTheme},
		Slot: Slot{
			MaxSteps:     DefaultMaxSteps,
			LimitEnabled: true,
		},
		Model: Model{
			Default: DefaultModelID,
			Variant: "",
		},
		Provider: Provider{Active: "opencode"},
		Orchestrator: Orchestrator{
			Enabled:      true,
			Review:       true,
			Provider:     provider.IDOpenCode,
			ExploreClass: "flash",
			PlanClass:    "pro",
			GeneralClass: "pro",
		},
		Agents: Agents{
			Enabled:              true,
			MaxConcurrent:        DefaultMaxConcurrent,
			MaxQueued:            DefaultMaxQueued,
			MaxDepth:             DefaultMaxDepth,
			DefaultTimeoutSec:    DefaultAgentsTimeoutSec,
			ChildMaxSteps:        DefaultChildMaxSteps,
			BashConfirm:          "parent",
			BashAllowlistEnabled: false,
			BashAllowlist:        []string{"ls", "pwd", "cat", "echo", "find", "grep", "git", "go", "npm", "python", "make"},
			AllowParallelWriters: false,
			DefaultRole:          "explore",
		},
		Compaction: Compaction{
			Auto:       true,
			Percent:    defaultCompactPercent,
			KeepTokens: defaultKeepTokens,
		},
		Recap: Recap{
			Enabled:    false,
			Model:      DefaultModelID,
			AfterChats: DefaultRecapAfterChats,
		},
		Retry: Retry{
			MaxRetries:   DefaultRetryMaxRetries,
			DelaySeconds: DefaultRetryDelaySeconds,
		},
		Skills: Skills{
			Enabled:         true,
			AutoDetect:      true,
			IncludeLocal:    true,
			IncludeGlobal:   true,
			Remember:        true,
			MaxAutoMatches:  DefaultSkillMaxAutoMatches,
			MaxBodyBytes:    DefaultSkillMaxBodyBytes,
			MaxContextBytes: DefaultSkillMaxContextBytes,
		},
	}
}

// Path joins workspaceDir with settings.json.
func Path(workspaceDir string) string {
	return filepath.Join(workspaceDir, FileName)
}

// Load reads path and restores defaults omitted by older or partial files.
func Load(path string) (Settings, error) {
	return load(path)
}

// Save writes s to path (creates parent dirs as needed).
func Save(path string, s Settings) error {
	s = s.normalized()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), settingsDirMode); err != nil {
		return fmt.Errorf("settings: mkdir: %w", err)
	}
	if err := os.WriteFile(path, data, settingsFileMode); err != nil {
		return fmt.Errorf("settings: write %s: %w", path, err)
	}
	return nil
}

// EffectiveMaxSteps is the value passed to the agent for this config.
// When the limit is disabled, returns a large safety bound.
func (s Settings) EffectiveMaxSteps() int {
	s = s.normalized()
	if !s.Slot.LimitEnabled {
		return unlimitedMaxSteps
	}
	return s.Slot.MaxSteps
}

// EffectiveModel is the default model id. It is empty when the selected
// provider owns a live default model selection.
func (s Settings) EffectiveModel() string {
	return s.normalized().Model.Default
}

// EffectiveProvider returns the canonical active provider name.
func (s Settings) EffectiveProvider() string {
	return provider.Normalize(s.normalized().Provider.Active)
}

// EffectiveOrchestrator returns normalized orchestration settings.
func (s Settings) EffectiveOrchestrator() Orchestrator {
	return s.normalized().Orchestrator
}

// EffectiveVariant is the default reasoning variant (may be empty).
func (s Settings) EffectiveVariant() string {
	return s.normalized().Model.Variant
}

// EffectiveTheme returns the selected supported TUI palette.
func (s Settings) EffectiveTheme() string {
	return s.normalized().Appearance.Theme
}

// EffectiveAgents returns normalized multi-agent preferences.
func (s Settings) EffectiveAgents() Agents {
	return s.normalized().Agents
}

// EffectiveCompaction returns normalized compact settings.
func (s Settings) EffectiveCompaction() Compaction {
	return s.normalized().Compaction
}

// EffectiveRecap returns normalized hidden recap preferences.
func (s Settings) EffectiveRecap() Recap {
	return s.normalized().Recap
}

// EffectiveRetry returns normalized transient chat retry preferences.
func (s Settings) EffectiveRetry() Retry {
	return s.normalized().Retry
}

// EffectiveSkills returns normalized skill discovery preferences.
func (s Settings) EffectiveSkills() Skills {
	return s.normalized().Skills
}

// EffectiveTimeout is the sub-agent timeout duration.
// Zero DefaultTimeoutSec means no timeout from settings.
func (a Agents) EffectiveTimeout() time.Duration {
	if a.DefaultTimeoutSec <= 0 {
		return 0
	}
	return time.Duration(a.DefaultTimeoutSec) * time.Second
}

func (s Settings) normalized() Settings {
	s.Appearance.Theme = NormalizeTheme(s.Appearance.Theme)
	if s.Slot.MaxSteps < MinMaxSteps {
		s.Slot.MaxSteps = DefaultMaxSteps
	}
	if s.Slot.MaxSteps > MaxMaxSteps {
		s.Slot.MaxSteps = MaxMaxSteps
	}
	s.Provider.Active = provider.Normalize(s.Provider.Active)
	s.Model.Default = strings.TrimSpace(s.Model.Default)
	if s.Model.Default == "" {
		s.Model.Default = provider.DefaultModel(s.Provider.Active)
	}
	s.Model.Variant = strings.TrimSpace(s.Model.Variant)
	s.Orchestrator = s.Orchestrator.normalized()
	s.Agents = s.Agents.normalized()
	s.Compaction = s.Compaction.normalized()
	s.Recap = s.Recap.normalized()
	s.Retry = s.Retry.normalized()
	s.Skills = s.Skills.normalized()
	return s
}

func (r Retry) normalized() Retry {
	if r.MaxRetries < MinRetryMaxRetries || r.MaxRetries > MaxRetryMaxRetries {
		r.MaxRetries = DefaultRetryMaxRetries
	}
	if r.DelaySeconds < MinRetryDelaySeconds || r.DelaySeconds > MaxRetryDelaySeconds {
		r.DelaySeconds = DefaultRetryDelaySeconds
	}
	return r
}

func (r Recap) normalized() Recap {
	r.Model = strings.TrimSpace(r.Model)
	if r.Model == "" {
		r.Model = DefaultModelID
	}
	if r.AfterChats < MinRecapAfterChats || r.AfterChats > MaxRecapAfterChats {
		r.AfterChats = DefaultRecapAfterChats
	}
	return r
}

func (s Skills) normalized() Skills {
	if s.MaxAutoMatches < MinSkillMaxAutoMatches || s.MaxAutoMatches > MaxSkillMaxAutoMatches {
		s.MaxAutoMatches = DefaultSkillMaxAutoMatches
	}
	if s.MaxBodyBytes <= 0 || s.MaxBodyBytes > MaxSkillMaxBodyBytes {
		s.MaxBodyBytes = DefaultSkillMaxBodyBytes
	}
	if s.MaxContextBytes <= 0 || s.MaxContextBytes > MaxSkillMaxContextBytes {
		s.MaxContextBytes = DefaultSkillMaxContextBytes
	}
	return s
}

// NormalizeTheme converts an on-disk theme value to a supported mode.
func NormalizeTheme(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "light":
		return "light"
	default:
		return DefaultTheme
	}
}

func (c Compaction) normalized() Compaction {
	if c.Percent <= 0 {
		c.Percent = defaultCompactPercent
	}
	if c.Percent < MinCompactPercent {
		c.Percent = MinCompactPercent
	}
	if c.Percent > MaxCompactPercent {
		c.Percent = MaxCompactPercent
	}
	if c.KeepTokens < 0 {
		c.KeepTokens = defaultKeepTokens
	}
	return c
}

// ThresholdTokens is the token count at which auto-compact fires for window.
func (c Compaction) ThresholdTokens(window int64) int64 {
	c = c.normalized()
	if window <= 0 {
		return 0
	}
	return window * int64(c.Percent) / percentScale
}

func (a Agents) normalized() Agents {
	if a.MaxConcurrent < MinMaxConcurrent {
		a.MaxConcurrent = DefaultMaxConcurrent
	}
	if a.MaxConcurrent > MaxMaxConcurrent {
		a.MaxConcurrent = MaxMaxConcurrent
	}
	if a.MaxQueued < 1 {
		a.MaxQueued = DefaultMaxQueued
	}
	if a.MaxQueued > MaxMaxQueued {
		a.MaxQueued = MaxMaxQueued
	}
	if a.MaxQueued < a.MaxConcurrent {
		a.MaxQueued = a.MaxConcurrent
	}
	if a.MaxDepth < DefaultMaxDepth {
		a.MaxDepth = DefaultMaxDepth
	}
	if a.MaxDepth > MaxMaxDepth {
		a.MaxDepth = MaxMaxDepth
	}
	if a.DefaultTimeoutSec < 0 {
		a.DefaultTimeoutSec = DefaultAgentsTimeoutSec
	}
	if a.ChildMaxSteps < MinMaxSteps {
		a.ChildMaxSteps = DefaultChildMaxSteps
	}
	if a.ChildMaxSteps > MaxMaxSteps {
		a.ChildMaxSteps = MaxMaxSteps
	}
	a.BashConfirm = strings.TrimSpace(a.BashConfirm)
	a.ModelOverride = strings.TrimSpace(a.ModelOverride)
	a.ModelVariant = strings.TrimSpace(a.ModelVariant)
	a.ExploreModel = strings.TrimSpace(a.ExploreModel)
	var cleanAllowlist []string
	if a.BashAllowlist != nil {
		cleanAllowlist = make([]string, 0, len(a.BashAllowlist))
	}
	seenAllowlist := make(map[string]bool)
	for _, item := range a.BashAllowlist {
		item = strings.TrimSpace(item)
		if item == "" || seenAllowlist[item] {
			continue
		}
		seenAllowlist[item] = true
		cleanAllowlist = append(cleanAllowlist, item)
	}
	a.BashAllowlist = cleanAllowlist
	if a.BashConfirm != "parent" && a.BashConfirm != "deny" {
		a.BashConfirm = "parent"
	}
	a.DefaultRole = strings.TrimSpace(a.DefaultRole)
	switch a.DefaultRole {
	case "explore", "plan", "general":
	default:
		a.DefaultRole = "explore"
	}
	return a
}

// NormalizeAfterLoad fixes zero MaxSteps from partial files and defaults
// LimitEnabled to true when the key is omitted.
func NormalizeAfterLoad(s Settings, raw []byte) Settings {
	s = s.normalized()
	if len(raw) == 0 {
		return Default()
	}
	if !jsonHasKey(raw, "slot", "limit_enabled") {
		s.Slot.LimitEnabled = true
	}
	if s.Model.Default == "" && s.Provider.Active != provider.IDCodex {
		s.Model.Default = DefaultModelID
	}
	if !jsonHasKey(raw, "agents") {
		s.Agents = Default().Agents
	} else if !jsonHasKey(raw, "agents", "enabled") {
		s.Agents.Enabled = true
	}
	if !jsonHasKey(raw, "provider") {
		s.Provider = Default().Provider
	}
	if !jsonHasKey(raw, "orchestrator") {
		s.Orchestrator = Default().Orchestrator
	}
	if !jsonHasKey(raw, "compaction") {
		s.Compaction = Default().Compaction
	} else {
		if !jsonHasKey(raw, "compaction", "auto") {
			s.Compaction.Auto = true
		}
		if !jsonHasKey(raw, "compaction", "percent") {
			s.Compaction.Percent = defaultCompactPercent
		}
		if !jsonHasKey(raw, "compaction", "keep_tokens") {
			s.Compaction.KeepTokens = defaultKeepTokens
		}
	}
	if !jsonHasKey(raw, "recap") {
		s.Recap = Default().Recap
	}
	if !jsonHasKey(raw, "retry") {
		s.Retry = Default().Retry
	} else {
		if !jsonHasKey(raw, "retry", "max_retries") {
			s.Retry.MaxRetries = DefaultRetryMaxRetries
		}
		if !jsonHasKey(raw, "retry", "delay_seconds") {
			s.Retry.DelaySeconds = DefaultRetryDelaySeconds
		}
	}
	if !jsonHasKey(raw, "skills") {
		s.Skills = Default().Skills
	}
	return s.normalized()
}

func (o Orchestrator) normalized() Orchestrator {
	o.Provider = provider.Normalize(o.Provider)
	o.ExploreClass = strings.TrimSpace(strings.ToLower(o.ExploreClass))
	o.PlanClass = strings.TrimSpace(strings.ToLower(o.PlanClass))
	o.GeneralClass = strings.TrimSpace(strings.ToLower(o.GeneralClass))
	if o.ExploreClass == "" {
		o.ExploreClass = "flash"
	}
	if o.PlanClass == "" {
		o.PlanClass = "pro"
	}
	if o.GeneralClass == "" {
		o.GeneralClass = "pro"
	}
	return o
}

func jsonHasKey(raw []byte, path ...string) bool {
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return false
	}
	for _, key := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		next, ok := obj[key]
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

// LoadFile is retained for callers that use the older name.
func LoadFile(path string) (Settings, error) {
	return load(path)
}

func load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Default(), fmt.Errorf("settings: read %s: %w", path, err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Default(), fmt.Errorf("settings: parse %s: %w", path, err)
	}
	return NormalizeAfterLoad(s, data), nil
}
