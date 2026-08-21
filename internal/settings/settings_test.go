package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	s := Default()
	if s.Slot.MaxSteps != DefaultMaxSteps {
		t.Fatalf("MaxSteps = %d, want %d", s.Slot.MaxSteps, DefaultMaxSteps)
	}
	if !s.Slot.LimitEnabled {
		t.Fatal("LimitEnabled want true")
	}
	if s.Model.Default != DefaultModelID {
		t.Fatalf("Default model = %q, want %q", s.Model.Default, DefaultModelID)
	}
	if s.Appearance.Theme != DefaultTheme {
		t.Fatalf("Theme = %q, want %q", s.Appearance.Theme, DefaultTheme)
	}
	a := s.Agents
	if !a.Enabled {
		t.Fatal("Agents.Enabled want true")
	}
	if a.MaxConcurrent != DefaultMaxConcurrent {
		t.Fatalf("MaxConcurrent = %d, want %d", a.MaxConcurrent, DefaultMaxConcurrent)
	}
	if a.MaxQueued != DefaultMaxQueued {
		t.Fatalf("MaxQueued = %d, want %d", a.MaxQueued, DefaultMaxQueued)
	}
	if a.MaxDepth != DefaultMaxDepth {
		t.Fatalf("MaxDepth = %d, want %d", a.MaxDepth, DefaultMaxDepth)
	}
	if a.DefaultTimeoutSec != DefaultAgentsTimeoutSec {
		t.Fatalf("DefaultTimeoutSec = %d, want %d", a.DefaultTimeoutSec, DefaultAgentsTimeoutSec)
	}
	if a.ChildMaxSteps != DefaultChildMaxSteps {
		t.Fatalf("ChildMaxSteps = %d, want %d", a.ChildMaxSteps, DefaultChildMaxSteps)
	}
	if a.BashConfirm != "parent" {
		t.Fatalf("BashConfirm = %q, want parent", a.BashConfirm)
	}
	if a.DefaultRole != "explore" {
		t.Fatalf("DefaultRole = %q, want explore", a.DefaultRole)
	}
	if a.AllowParallelWriters {
		t.Fatal("AllowParallelWriters want false")
	}
	if !s.Compaction.Auto || s.Compaction.Percent != 80 || s.Compaction.KeepTokens != 15_000 {
		t.Fatalf("compaction defaults %+v", s.Compaction)
	}
	if s.Recap.Enabled || s.Recap.Model != DefaultModelID {
		t.Fatalf("recap defaults %+v", s.Recap)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Slot.MaxSteps != DefaultMaxSteps || !s.Slot.LimitEnabled {
		t.Fatalf("got %+v", s)
	}
	if s.Model.Default != DefaultModelID {
		t.Fatalf("model = %q", s.Model.Default)
	}
	if !s.Agents.Enabled || s.Agents.MaxConcurrent != DefaultMaxConcurrent {
		t.Fatalf("agents %+v", s.Agents)
	}
	if !s.Compaction.Auto || s.Compaction.Percent != 80 {
		t.Fatalf("compaction %+v", s.Compaction)
	}
	if s.Recap.Enabled || s.Recap.Model != DefaultModelID {
		t.Fatalf("recap %+v", s.Recap)
	}
}

func TestLegacySettingsNormalizeRecapDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":{"default":"claude-4"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Recap.Enabled || s.Recap.Model != DefaultModelID {
		t.Fatalf("legacy recap = %+v", s.Recap)
	}
}

func TestRecapModelNormalizesWhitespaceAndEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"recap":{"enabled":true,"model":"  "}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Recap.Enabled || s.EffectiveRecap().Model != DefaultModelID {
		t.Fatalf("recap = %+v", s.Recap)
	}
}

func TestRecapSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{Recap: Recap{Enabled: true, Model: "claude-4"}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Recap, want.Recap) {
		t.Fatalf("recap got %+v want %+v", got.Recap, want.Recap)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{
		Appearance: Appearance{Theme: "light"},
		Slot:       Slot{MaxSteps: 8, LimitEnabled: false},
		Model:      Model{Default: "claude-4", Variant: "high"},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot.MaxSteps != 8 || got.Slot.LimitEnabled {
		t.Fatalf("slot %+v", got.Slot)
	}
	if got.Model.Default != "claude-4" || got.Model.Variant != "high" {
		t.Fatalf("model %+v", got.Model)
	}
	if got.Appearance.Theme != "light" {
		t.Fatalf("appearance %+v", got.Appearance)
	}
}

func TestThemeNormalizesOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"appearance":{"theme":"noon"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.EffectiveTheme() != DefaultTheme {
		t.Fatalf("invalid theme = %q, want %q", got.EffectiveTheme(), DefaultTheme)
	}
}

func TestSaveLoadRoundTripWithAgents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{
		Slot:  Slot{MaxSteps: 8, LimitEnabled: false},
		Model: Model{Default: "claude-4", Variant: "high"},
		Agents: Agents{
			Enabled:              true,
			MaxConcurrent:        6,
			MaxQueued:            50,
			MaxDepth:             1,
			DefaultTimeoutSec:    120,
			ChildMaxSteps:        20,
			ModelOverride:        "fast-model",
			ExploreModel:         "explore-model",
			BashConfirm:          "deny",
			AllowParallelWriters: true,
			DefaultRole:          "plan",
		},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot.MaxSteps != 8 || got.Slot.LimitEnabled {
		t.Fatalf("slot %+v", got.Slot)
	}
	if got.Model.Default != "claude-4" || got.Model.Variant != "high" {
		t.Fatalf("model %+v", got.Model)
	}
	if !reflect.DeepEqual(got.Agents, want.Agents) {
		t.Fatalf("agents got %+v want %+v", got.Agents, want.Agents)
	}
}

func TestCompactionLoadSaveAndMissingBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{
		Slot:       Slot{MaxSteps: 8, LimitEnabled: true},
		Model:      Model{Default: DefaultModelID},
		Compaction: Compaction{Auto: false, Percent: 50, KeepTokens: 5_000},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compaction.Auto || got.Compaction.Percent != 50 || got.Compaction.KeepTokens != 5_000 {
		t.Fatalf("saved compaction %+v", got.Compaction)
	}
	if err := os.WriteFile(path, []byte(`{"slot":{"max_steps":4}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Compaction.Auto || got.Compaction.Percent != 80 || got.Compaction.KeepTokens != 15_000 {
		t.Fatalf("missing block should default: %+v", got.Compaction)
	}
	if got.Compaction.ThresholdTokens(1_000_000) != 800_000 {
		t.Fatalf("80%% of 1M = %d", got.Compaction.ThresholdTokens(1_000_000))
	}
}

func TestLoadPartialMaxStepsKeepsLimitOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"slot":{"max_steps":4}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot.MaxSteps != 4 {
		t.Fatalf("MaxSteps = %d, want 4", got.Slot.MaxSteps)
	}
	if !got.Slot.LimitEnabled {
		t.Fatal("LimitEnabled should default true when omitted")
	}
	if got.Model.Default != DefaultModelID {
		t.Fatalf("model default = %q", got.Model.Default)
	}
	if !got.Agents.Enabled || got.Agents.MaxConcurrent != DefaultMaxConcurrent {
		t.Fatalf("missing agents should use defaults: %+v", got.Agents)
	}
}

func TestLoadAndLoadFileRestoreTheSameDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"slot":{"max_steps":4}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loadedFile, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, loadedFile) {
		t.Fatalf("Load() = %+v, LoadFile() = %+v", loaded, loadedFile)
	}
	if !loaded.Slot.LimitEnabled || !loaded.Agents.Enabled || !loaded.Compaction.Auto {
		t.Fatalf("partial defaults = %+v", loaded)
	}
}

func TestClampMaxSteps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"slot":{"max_steps":9999,"limit_enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot.MaxSteps != MaxMaxSteps {
		t.Fatalf("MaxSteps = %d, want %d", got.Slot.MaxSteps, MaxMaxSteps)
	}
}

func TestClampMaxConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"agents":{"enabled":true,"max_concurrent":99}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents.MaxConcurrent != MaxMaxConcurrent {
		t.Fatalf("MaxConcurrent = %d, want %d", got.Agents.MaxConcurrent, MaxMaxConcurrent)
	}
	if got.Agents.MaxQueued < got.Agents.MaxConcurrent {
		t.Fatalf("MaxQueued = %d, want >= %d", got.Agents.MaxQueued, got.Agents.MaxConcurrent)
	}
}

func TestClampMaxDepthToProductLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"agents":{"enabled":true,"max_depth":3}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents.MaxDepth != MaxMaxDepth || MaxMaxDepth != 1 {
		t.Fatalf("MaxDepth = %d MaxMaxDepth = %d, want both 1", got.Agents.MaxDepth, MaxMaxDepth)
	}
}

func TestEffectiveMaxSteps(t *testing.T) {
	on := Settings{Slot: Slot{MaxSteps: 5, LimitEnabled: true}}
	if on.EffectiveMaxSteps() != 5 {
		t.Fatalf("enabled = %d", on.EffectiveMaxSteps())
	}
	off := Settings{Slot: Slot{MaxSteps: 5, LimitEnabled: false}}
	if off.EffectiveMaxSteps() != unlimitedMaxSteps {
		t.Fatalf("disabled = %d, want %d", off.EffectiveMaxSteps(), unlimitedMaxSteps)
	}
}

func TestEmptyModelNormalizes(t *testing.T) {
	s := Settings{Model: Model{Default: "  "}}.normalized()
	if s.Model.Default != DefaultModelID {
		t.Fatalf("got %q", s.Model.Default)
	}
}

func TestToolsForRole(t *testing.T) {
	a := Default().Agents
	explore := a.ToolsForRole("explore")
	if !reflect.DeepEqual(explore, []string{"bash", "read", "grep", "webfetch"}) {
		t.Fatalf("explore = %v", explore)
	}
	plan := a.ToolsForRole("plan")
	if !reflect.DeepEqual(plan, []string{"bash", "read", "grep", "webfetch"}) {
		t.Fatalf("plan = %v", plan)
	}
	general := a.ToolsForRole("general")
	if !reflect.DeepEqual(general, []string{"bash", "read", "grep", "write", "edit", "webfetch"}) {
		t.Fatalf("general = %v", general)
	}
	unknown := a.ToolsForRole("other")
	if !reflect.DeepEqual(unknown, []string{"bash", "read", "grep", "webfetch"}) {
		t.Fatalf("unknown = %v", unknown)
	}
}

func TestEffectiveTimeout(t *testing.T) {
	a := Agents{DefaultTimeoutSec: 0}
	if a.EffectiveTimeout() != 0 {
		t.Fatalf("zero timeout = %v, want 0", a.EffectiveTimeout())
	}
	a.DefaultTimeoutSec = 90
	if a.EffectiveTimeout() != 90*time.Second {
		t.Fatalf("timeout = %v", a.EffectiveTimeout())
	}
}
