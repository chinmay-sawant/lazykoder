// Package settings loads and saves project-level lazykoder preferences.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/roles"
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
	// maxMaxQueued caps the sub-agent queue size.
	maxMaxQueued = 100
	// MinCompactPercent is the lowest selectable auto-compact threshold.
	MinCompactPercent = 5
	// MaxCompactPercent is the highest selectable auto-compact threshold.
	MaxCompactPercent = 99
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

// Settings is the on-disk project config under .lazykoder/settings.json.
type Settings struct {
	Appearance Appearance `json:"appearance"`
	Slot       Slot       `json:"slot"`
	Model      Model      `json:"model"`
	Agents     Agents     `json:"agents"`
	Compaction Compaction `json:"compaction"`
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

// EffectiveModel is the default model id (never empty after normalize).
func (s Settings) EffectiveModel() string {
	return s.normalized().Model.Default
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

// EffectiveTimeout is the sub-agent timeout duration.
// Zero DefaultTimeoutSec means no timeout from settings.
func (a Agents) EffectiveTimeout() time.Duration {
	if a.DefaultTimeoutSec <= 0 {
		return 0
	}
	return time.Duration(a.DefaultTimeoutSec) * time.Second
}

// ToolsForRole returns the tool allow-list for a sub-agent role.
func (a Agents) ToolsForRole(role string) []string {
	return roles.Tools(role)
}

func (s Settings) normalized() Settings {
	s.Appearance.Theme = NormalizeTheme(s.Appearance.Theme)
	if s.Slot.MaxSteps < MinMaxSteps {
		s.Slot.MaxSteps = DefaultMaxSteps
	}
	if s.Slot.MaxSteps > MaxMaxSteps {
		s.Slot.MaxSteps = MaxMaxSteps
	}
	s.Model.Default = strings.TrimSpace(s.Model.Default)
	if s.Model.Default == "" {
		s.Model.Default = DefaultModelID
	}
	s.Model.Variant = strings.TrimSpace(s.Model.Variant)
	s.Agents = s.Agents.normalized()
	s.Compaction = s.Compaction.normalized()
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
	if a.MaxQueued > maxMaxQueued {
		a.MaxQueued = maxMaxQueued
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
	if s.Model.Default == "" {
		s.Model.Default = DefaultModelID
	}
	if !jsonHasKey(raw, "agents") {
		s.Agents = Default().Agents
	} else if !jsonHasKey(raw, "agents", "enabled") {
		s.Agents.Enabled = true
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
	return s.normalized()
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
