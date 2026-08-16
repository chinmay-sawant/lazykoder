package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	s := Default()
	if s.Slot.MaxSteps != DefaultMaxSteps {
		t.Fatalf("MaxSteps = %d, want %d", s.Slot.MaxSteps, DefaultMaxSteps)
	}
	if !s.Slot.LimitEnabled {
		t.Fatal("LimitEnabled want true")
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
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{Slot: Slot{MaxSteps: 8, LimitEnabled: false}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot.MaxSteps != 8 || got.Slot.LimitEnabled {
		t.Fatalf("got %+v, want %+v", got, want)
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
