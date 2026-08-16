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
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{
		Slot:  Slot{MaxSteps: 8, LimitEnabled: false},
		Model: Model{Default: "claude-4", Variant: "high"},
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
			MaxDepth:             2,
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
