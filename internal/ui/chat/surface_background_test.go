package chat

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestInputAndOverlaySurfacesKeepTheirBackground(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })

	for _, mode := range []theme.Mode{theme.ModeDark, theme.ModeLight} {
		t.Run(string(mode), func(t *testing.T) {
			theme.SetMode(string(mode))
			configureThemeStyles()

			m := New(Options{
				Store:   newTestStore(t),
				Client:  deadClient(),
				Workdir: t.TempDir(),
			})
			m.width, m.height = 80, 24

			assertFilledSurfaceRow(t, "composer", m.promptLine(), ansiBackground(theme.ColorComposer()), "enter send")
			assertFilledSurfaceRow(t, "settings", m.settingsCardView(), ansiBackground(theme.ColorSurface()), "j/k move")
			assertFilledSurfaceRow(t, "session picker", m.sessionPickerView(), ansiBackground(theme.ColorSurface()), "j/k select")
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
