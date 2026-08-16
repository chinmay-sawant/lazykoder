// Package theme holds the single dark palette used by the chat TUI.
package theme

import (
	"fmt"
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
)

// Hex values for the default theme. Bg is solid black so the terminal
// profile color cannot show through empty cells.
const (
	Bg      = "#000000"
	Surface = "#000000"
	Text    = "#eceae6"
	Mute    = "#8a8680"
	Accent  = "#d4a0c7"
	Danger  = "#e06c75"
	Border  = "#2a2a2a"
	Good = "#9ece6a"
	// Edit diff row tints: solid soft washes on black (terminals rarely blend
	// true alpha). Tuned like Grok Build / GitHub dark: very light greenish
	// and reddish so the row is tinted, not painted neon.
	EditPanel = "#0c120e"
	EditAddBg = "#102418"
	EditDelBg = "#241014"
	EditMeta  = "#7a8574"
)

func ColorBg() color.Color      { return lipgloss.Color(Bg) }
func ColorSurface() color.Color { return lipgloss.Color(Surface) }
func ColorText() color.Color    { return lipgloss.Color(Text) }
func ColorMute() color.Color    { return lipgloss.Color(Mute) }
func ColorAccent() color.Color  { return lipgloss.Color(Accent) }
func ColorDanger() color.Color  { return lipgloss.Color(Danger) }
func ColorBorder() color.Color  { return lipgloss.Color(Border) }
func ColorGood() color.Color    { return lipgloss.Color(Good) }

// ColorEditPanel is the soft greenish chrome behind the edit card header /
// context lines (barely lighter than pure black).
func ColorEditPanel() color.Color { return lipgloss.Color(EditPanel) }

// ColorEditAddBg is a very light greenish full-row wash for + lines.
func ColorEditAddBg() color.Color { return lipgloss.Color(EditAddBg) }

// ColorEditDelBg is a very light reddish full-row wash for - lines.
func ColorEditDelBg() color.Color { return lipgloss.Color(EditDelBg) }

func ColorEditMeta() color.Color { return lipgloss.Color(EditMeta) }

// StatusDiamond is the persistent status mark on every tool run card.
const StatusDiamond = "◆"

// StatusColor is the diamond color for a tool-run status. Pending and
// running stay on the text color; success is green; failures are red.
func StatusColor(status string) color.Color {
	switch status {
	case "completed", "success":
		return ColorGood()
	case "error", "denied", "failed", "cancelled":
		return ColorDanger()
	default:
		return ColorText()
	}
}

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
