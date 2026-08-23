// Package theme provides the color tokens shared by the chat TUI.
package theme

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
)

// Mode identifies one of the application color palettes.
type Mode string

const (
	// ModeDark is the default high-contrast palette for coding work.
	ModeDark Mode = "dark"
	// ModeLight is the bright palette for daytime use.
	ModeLight Mode = "light"
)

// Palette contains semantic color tokens. Callers should use the Color*
// helpers so a settings change updates the full interface together.
type Palette struct {
	Bg              string
	Surface         string
	Dialog          string
	Composer        string
	UserPanel       string
	AssistantPanel  string
	PlanPanel       string
	UserBorder      string
	AssistantBorder string
	PlanBorder      string
	Text            string
	Mute            string
	Accent          string
	Danger          string
	Border          string
	Good            string
	EditPanel       string
	EditAddBg       string
	EditDelBg       string
	EditMeta        string
}

// The exported constants retain the default palette for integrations that
// need a stable color literal. The Color* helpers follow the selected mode.
const (
	Bg              = "#050505"
	Surface         = "#0d0d0d"
	Dialog          = "#141414"
	Composer        = "#101010"
	UserPanel       = "#121412"
	AssistantPanel  = "#102832"
	PlanPanel       = "#2b2416"
	UserBorder      = "#758576"
	AssistantBorder = "#7aaec2"
	PlanBorder      = "#c79b4b"
	Text            = "#e9e7e2"
	Mute            = "#a6a29a"
	Accent          = "#a3b18a"
	Danger          = "#d17a7a"
	Border          = "#30302e"
	Good            = "#8fbf8f"
	EditPanel       = "#151a15"
	EditAddBg       = "#203021"
	EditDelBg       = "#311e1e"
	EditMeta        = "#b2baa9"
)

var darkPalette = Palette{
	Bg:              Bg,
	Surface:         Surface,
	Dialog:          Dialog,
	Composer:        Composer,
	UserPanel:       UserPanel,
	AssistantPanel:  AssistantPanel,
	PlanPanel:       PlanPanel,
	UserBorder:      UserBorder,
	AssistantBorder: AssistantBorder,
	PlanBorder:      PlanBorder,
	Text:            Text,
	Mute:            Mute,
	Accent:          Accent,
	Danger:          Danger,
	Border:          Border,
	Good:            Good,
	EditPanel:       EditPanel,
	EditAddBg:       EditAddBg,
	EditDelBg:       EditDelBg,
	EditMeta:        EditMeta,
}

var lightPalette = Palette{
	Bg:              "#f7f8fc",
	Surface:         "#ffffff",
	Dialog:          "#edf2ff",
	Composer:        "#ffffff",
	UserPanel:       "#f5e8ff",
	AssistantPanel:  "#e4f7fb",
	PlanPanel:       "#fff4d6",
	UserBorder:      "#a855f7",
	AssistantBorder: "#0891b2",
	PlanBorder:      "#a66a00",
	Text:            "#152033",
	Mute:            "#52637a",
	Accent:          "#7c3aed",
	Danger:          "#dc2626",
	Border:          "#b8c4d8",
	Good:            "#15803d",
	EditPanel:       "#edf8f0",
	EditAddBg:       "#d9f5e4",
	EditDelBg:       "#ffe5e9",
	EditMeta:        "#54725a",
}

var selected = struct {
	sync.RWMutex
	mode Mode
}{mode: ModeDark}

// NormalizeMode returns a supported mode. Unknown and empty values use dark.
func NormalizeMode(value string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case ModeLight:
		return ModeLight
	default:
		return ModeDark
	}
}

// SetMode changes the process-wide palette and returns the applied mode.
func SetMode(value string) Mode {
	mode := NormalizeMode(value)
	selected.Lock()
	selected.mode = mode
	selected.Unlock()
	return mode
}

// CurrentMode returns the selected palette mode.
func CurrentMode() Mode {
	selected.RLock()
	defer selected.RUnlock()
	return selected.mode
}

// Current returns a copy of the selected palette.
func Current() Palette {
	if CurrentMode() == ModeLight {
		return lightPalette
	}
	return darkPalette
}

func ColorBg() color.Color              { return lipgloss.Color(Current().Bg) }
func ColorSurface() color.Color         { return lipgloss.Color(Current().Surface) }
func ColorDialog() color.Color          { return lipgloss.Color(Current().Dialog) }
func ColorComposer() color.Color        { return lipgloss.Color(Current().Composer) }
func ColorUserPanel() color.Color       { return lipgloss.Color(Current().UserPanel) }
func ColorAssistantPanel() color.Color  { return lipgloss.Color(Current().AssistantPanel) }
func ColorPlanPanel() color.Color       { return lipgloss.Color(Current().PlanPanel) }
func ColorUserBorder() color.Color      { return lipgloss.Color(Current().UserBorder) }
func ColorAssistantBorder() color.Color { return lipgloss.Color(Current().AssistantBorder) }
func ColorPlanBorder() color.Color      { return lipgloss.Color(Current().PlanBorder) }
func ColorText() color.Color            { return lipgloss.Color(Current().Text) }
func ColorMute() color.Color            { return lipgloss.Color(Current().Mute) }
func ColorAccent() color.Color          { return lipgloss.Color(Current().Accent) }
func ColorDanger() color.Color          { return lipgloss.Color(Current().Danger) }
func ColorBorder() color.Color          { return lipgloss.Color(Current().Border) }
func ColorGood() color.Color            { return lipgloss.Color(Current().Good) }
func ColorEditPanel() color.Color       { return lipgloss.Color(Current().EditPanel) }
func ColorEditAddBg() color.Color       { return lipgloss.Color(Current().EditAddBg) }
func ColorEditDelBg() color.Color       { return lipgloss.Color(Current().EditDelBg) }
func ColorEditMeta() color.Color        { return lipgloss.Color(Current().EditMeta) }

func TextHex() string   { return Current().Text }
func BgHex() string     { return Current().Bg }
func AccentHex() string { return Current().Accent }
func MuteHex() string   { return Current().Mute }

// StatusBatonFrames are the baton frames used for live status marks.
const StatusBatonFrames = "|/—\\"

// StatusBatonFrame returns one baton frame, clamped to the first
// frame for negative values.
func StatusBatonFrame(frame int) string {
	if frame < 0 {
		frame = 0
	}
	frames := []rune(StatusBatonFrames)
	return string(frames[frame%len(frames)])
}

// StatusColor is the status color for a tool-run status. Pending and
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
	return interpolate(Current().Border, Current().Accent, t)
}

// PulseAssistant returns a color between Border and AssistantBorder for the
// assistant work rail. t is clamped to 0..1.
func PulseAssistant(t float64) color.Color {
	return interpolate(Current().Border, Current().AssistantBorder, t)
}

func interpolate(from, to string, t float64) color.Color {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	fr, fg, fb := hexRGB(from)
	tr, tg, tb := hexRGB(to)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		fr+int(float64(tr-fr)*t),
		fg+int(float64(tg-fg)*t),
		fb+int(float64(tb-fb)*t),
	))
}

func hexRGB(hex string) (r, g, b int) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, 0, 0
	}
	channel := func(s string) int {
		value, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return 0
		}
		return int(value)
	}
	return channel(hex[1:3]), channel(hex[3:5]), channel(hex[5:7])
}
