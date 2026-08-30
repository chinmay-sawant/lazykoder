package chat

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestInputAndOverlaySurfacesKeepTheirBackground(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })

	for _, mode := range []theme.Mode{theme.ModeDark, theme.ModeLight} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := settings.Default()
			cfg.Appearance.Theme = string(mode)

			m := New(Options{
				Store:    newTestStore(t),
				Client:   deadClient(),
				Workdir:  t.TempDir(),
				Settings: &cfg,
			})
			m.width, m.height = 80, 24

			assertFilledSurfaceRow(t, "composer", m.promptLine(), ansiBackground(theme.ColorComposer()), "enter send")
			assertFilledSurfaceRow(t, "settings", m.settingsCardView(), ansiBackground(theme.ColorSurface()), "j/k move")
			assertFilledSurfaceRow(t, "session picker", m.sessionPickerView(), ansiBackground(theme.ColorSurface()), "j/k select")
		})
	}
}

func TestComposerFooterUsesPrimaryTextColor(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })

	for _, mode := range []theme.Mode{theme.ModeDark, theme.ModeLight} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := settings.Default()
			cfg.Appearance.Theme = string(mode)

			m := New(Options{
				Store:    newTestStore(t),
				Client:   deadClient(),
				Workdir:  t.TempDir(),
				Settings: &cfg,
			})
			footer := m.composerFooter(80)
			primary := ansiForeground(theme.ColorText())
			mute := ansiForeground(theme.ColorMute())
			if !strings.Contains(footer, primary) {
				t.Fatalf("footer does not use primary text color %s: %q", primary, footer)
			}
			if strings.Contains(footer, mute) {
				t.Fatalf("footer still uses muted text color %s: %q", mute, footer)
			}

			boxed := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(100).Render(" ")
			labeled := embedComposerBorderLabels(boxed, footer, theme.ColorBorder())
			if !strings.Contains(labeled, primary) {
				t.Fatalf("footer border label does not use primary text color %s: %q", primary, labeled)
			}
		})
	}
}

func assertFilledSurfaceRow(t *testing.T, location, rendered, wantBackground, marker string) {
	t.Helper()
	found := false
	for _, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		found = true
		started := false
		for column, background := range ansiCellBackgrounds(line) {
			if background == wantBackground {
				started = true
			}
			if started && background != wantBackground {
				t.Fatalf("%s background ended at column %d in %q", location, column, line)
			}
		}
		if !started {
			t.Fatalf("%s did not paint %q with %s: %q", location, marker, wantBackground, line)
		}
	}
	if !found {
		t.Fatalf("%s is missing %q: %q", location, marker, rendered)
	}
}

func ansiBackground(c color.Color) string {
	red, green, blue, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", red>>ansiColorChannelShift, green>>ansiColorChannelShift, blue>>ansiColorChannelShift)
}

func ansiForeground(c color.Color) string {
	red, green, blue, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", red>>ansiColorChannelShift, green>>ansiColorChannelShift, blue>>ansiColorChannelShift)
}
