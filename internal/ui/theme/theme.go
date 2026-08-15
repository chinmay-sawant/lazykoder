// Package theme holds the single dark palette used by the chat TUI.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Hex values for the default dark theme.
const (
	Bg      = "#1c1c1c"
	Surface = "#262626"
	Text    = "#e6e6e6"
	Mute    = "#8a8a8a"
	Accent  = "#7aa2f7"
	Danger  = "#f7768e"
	Border  = "#444444"
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
