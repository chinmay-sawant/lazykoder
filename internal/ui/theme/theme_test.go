package theme

import (
	"testing"
)

func TestThemeColors(t *testing.T) {
	t.Cleanup(func() { SetMode(string(ModeDark)) })
	SetMode(string(ModeDark))
	if Bg == "" || Surface == "" || Dialog == "" || Composer == "" {
		t.Fatal("base theme background colors are empty")
	}
	if Bg != "#050505" {
		t.Fatalf("dark canvas = %q, want near-black graphite", Bg)
	}
	if Surface == Dialog || Dialog == Composer {
		t.Fatalf("dark surfaces must remain visually distinct: %+v", darkPalette)
	}
	if AssistantBorder != "#7aaec2" {
		t.Fatalf("expected AssistantBorder to be #7aaec2, got %q", AssistantBorder)
	}
	if ColorAssistantBorder() == nil {
		t.Fatal("ColorAssistantBorder returned nil")
	}
}

func TestModeSelection(t *testing.T) {
	t.Cleanup(func() { SetMode(string(ModeDark)) })
	if got := SetMode(string(ModeLight)); got != ModeLight {
		t.Fatalf("SetMode(light) = %q", got)
	}
	if Current().Bg == Bg || Current().Text == Text || Current().Composer == Composer {
		t.Fatalf("light palette did not change: %+v", Current())
	}
	if got := SetMode("unknown"); got != ModeDark {
		t.Fatalf("SetMode(unknown) = %q, want dark", got)
	}
}

func TestPulseAccent(t *testing.T) {
	SetMode(string(ModeDark))
	c0 := PulseAccent(0.0)
	c1 := PulseAccent(1.0)
	cmid := PulseAccent(0.5)

	if c0 == nil || c1 == nil || cmid == nil {
		t.Fatal("PulseAccent returned nil color")
	}
}

func TestPulseAssistant(t *testing.T) {
	SetMode(string(ModeDark))
	c0 := PulseAssistant(0.0)
	c1 := PulseAssistant(1.0)
	cmid := PulseAssistant(0.5)

	if c0 == nil || c1 == nil || cmid == nil {
		t.Fatal("PulseAssistant returned nil color")
	}

	// Clamp behavior tests
	under := PulseAssistant(-0.5)
	over := PulseAssistant(1.5)
	if under != c0 {
		t.Fatalf("expected under-clamped color to match c0, got %v vs %v", under, c0)
	}
	if over != c1 {
		t.Fatalf("expected over-clamped color to match c1, got %v vs %v", over, c1)
	}
}

func TestStatusBatonFrames(t *testing.T) {
	if got := StatusBatonFrame(0); got != "|" {
		t.Fatalf("first baton frame = %q, want |", got)
	}
	if got := StatusBatonFrame(len([]rune(StatusBatonFrames))); got != "|" {
		t.Fatalf("baton spinner should wrap, got %q", got)
	}
	if got := StatusBatonFrame(-1); got != "|" {
		t.Fatalf("negative baton frame = %q, want |", got)
	}
}

func TestHexRGB(t *testing.T) {
	r, g, b := hexRGB("#4d6b78")
	if r != 0x4d || g != 0x6b || b != 0x78 {
		t.Fatalf("unexpected hexRGB output: r=%x, g=%x, b=%x", r, g, b)
	}

	badR, badB, badG := hexRGB("invalid")
	if badR != 0 || badB != 0 || badG != 0 {
		t.Fatalf("expected 0,0,0 for invalid hex, got %d,%d,%d", badR, badG, badB)
	}
}
