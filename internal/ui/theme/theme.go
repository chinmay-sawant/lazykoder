// Package theme holds the single dark palette used by the chat TUI.
package theme

import (
	"fmt"
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
)

// Hex values for the default dark theme (neutral night + magenta accent).
const (
	Bg      = "#121212"
	Surface = "#1a1a1a"
	Text    = "#eceae6"
	Mute    = "#8a8680"
	Accent  = "#d4a0c7"
	Danger  = "#e06c75"
	Border  = "#3a3836"
	Good    = "#9ece6a"
)

func ColorBg() color.Color      { return lipgloss.Color(Bg) }
func ColorSurface() color.Color { return lipgloss.Color(Surface) }
func ColorText() color.Color    { return lipgloss.Color(Text) }
func ColorMute() color.Color    { return lipgloss.Color(Mute) }
func ColorAccent() color.Color  { return lipgloss.Color(Accent) }
func ColorDanger() color.Color  { return lipgloss.Color(Danger) }
func ColorBorder() color.Color  { return lipgloss.Color(Border) }
func ColorGood() color.Color    { return lipgloss.Color(Good) }

// PulseAccent returns a color between Border and Accent for thinking glow.
// t is 0..1; values outside are clamped.
func PulseAccent(t float64) color.Color {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	br, bg, bb := hexRGB(Border)
	ar, ag, ab := hexRGB(Accent)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		br+int(float64(ar-br)*t),
		bg+int(float64(ag-bg)*t),
		bb+int(float64(ab-bb)*t),
	))
}

func hexRGB(hex string) (r, g, b int) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, 0, 0
	}
	n, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(n >> 16), int((n >> 8) & 0xff), int(n & 0xff)
}
