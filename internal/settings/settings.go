// Package settings loads and saves project-level lazykoder preferences.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// FileName is the settings file under .lazykoder/.
	FileName = "settings.json"
	// DefaultModelID is the built-in chat model when none is configured.
	DefaultModelID = "deepseek-v4-flash"
	// DefaultMaxSteps matches the agent default when no file exists.
	DefaultMaxSteps = 16
	// MinMaxSteps is the lowest configurable step budget.
	MinMaxSteps = 1
	// MaxMaxSteps caps the step budget so a runaway loop stays bounded.
	MaxMaxSteps = 128
	// unlimitedMaxSteps is the effective agent budget when the limit is off.
	// The agent still needs a finite loop bound for safety.
	unlimitedMaxSteps = 10_000
	// settingsDirMode is used when creating parent dirs for settings.json.
	settingsDirMode = 0o755
	// settingsFileMode is the on-disk mode for settings.json.
	settingsFileMode = 0o600
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

// Settings is the on-disk project config under .lazykoder/settings.json.
type Settings struct {
	Slot  Slot  `json:"slot"`
	Model Model `json:"model"`
}

// Default returns the built-in defaults.
func Default() Settings {
	return Settings{
		Slot: Slot{
			MaxSteps:     DefaultMaxSteps,
			LimitEnabled: true,
		},
		Model: Model{
			Default: DefaultModelID,
			Variant: "",
		},
	}
}

// Path joins workspaceDir with settings.json.
func Path(workspaceDir string) string {
	return filepath.Join(workspaceDir, FileName)
}

// Load reads path. Missing file returns defaults. Invalid values are clamped.
func Load(path string) (Settings, error) {
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
	return s.normalized(), nil
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

func (s Settings) normalized() Settings {
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
	return s
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
	if !jsonHasKey(raw, "model", "default") && s.Model.Default == DefaultModelID {
		// keep default; already set by normalized
	}
	if s.Model.Default == "" {
		s.Model.Default = DefaultModelID
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

// LoadFile is Load with default-true LimitEnabled when the key is omitted.
func LoadFile(path string) (Settings, error) {
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
