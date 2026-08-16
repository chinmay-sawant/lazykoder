// Package settings loads and saves project-level lazykoder preferences.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// FileName is the settings file under .lazykoder/.
	FileName = "settings.json"
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

// Settings is the on-disk project config under .lazykoder/settings.json.
type Settings struct {
	Slot Slot `json:"slot"`
}

// Default returns the built-in defaults (step limit on, 16 steps).
func Default() Settings {
	return Settings{
		Slot: Slot{
			MaxSteps:     DefaultMaxSteps,
			LimitEnabled: true,
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

func (s Settings) normalized() Settings {
	if s.Slot.MaxSteps < MinMaxSteps {
		s.Slot.MaxSteps = DefaultMaxSteps
	}
	if s.Slot.MaxSteps > MaxMaxSteps {
		s.Slot.MaxSteps = MaxMaxSteps
	}
	// json zero-value for bool is false; treat missing file via Default().
	// After load, keep LimitEnabled as unmarshaled (explicit false is ok).
	// If MaxSteps was present but LimitEnabled omitted and max_steps set,
	// callers using Default() first then overlay are fine. For a file that
	// only has {"slot":{"max_steps":8}}, LimitEnabled is false - wrong.
	// Prefer: if the file exists with max_steps but never set limit_enabled,
	// we cannot distinguish. Document that limit_enabled defaults to true
	// only when the whole file is missing. When present, use the bool.
	return s
}

// NormalizeAfterLoad fixes zero MaxSteps from partial files and defaults
// LimitEnabled to true when MaxSteps is set but the file had no slot block.
// Call this after Load when you want "missing limit_enabled" => true.
func NormalizeAfterLoad(s Settings, raw []byte) Settings {
	s = s.normalized()
	if len(raw) == 0 {
		return Default()
	}
	// If limit_enabled key is absent, default it to true so partial files
	// that only set max_steps still keep the limit on.
	if !jsonHasKey(raw, "limit_enabled") {
		s.Slot.LimitEnabled = true
	}
	return s.normalized()
}

func jsonHasKey(raw []byte, key string) bool {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return false
	}
	slotRaw, ok := top["slot"]
	if !ok {
		return false
	}
	var slot map[string]json.RawMessage
	if err := json.Unmarshal(slotRaw, &slot); err != nil {
		return false
	}
	_, ok = slot[key]
	return ok
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
