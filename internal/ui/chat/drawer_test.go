package chat

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDrawerChromeRendersUnifiedFrame(t *testing.T) {
	const width = 80
	title := "status"
	meta := "3/10 enabled"
	body := "  ▸ model       deepseek-v4  on\n    tokens      1.2k         on"
	hint := "↑/↓ select  •  enter toggle  •  ←/esc close"

	rendered := drawerChrome(title, meta, body, hint, width)
	plain := ansi.Strip(rendered)

	for _, want := range []string{
		"status",
		"3/10 enabled",
		"model",
		"tokens",
		"↑/↓ select",
		"enter toggle",
		"←/esc close",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("drawerChrome missing %q:\n%s", want, plain)
		}
	}

	for _, line := range strings.Split(plain, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("drawer line wider than width (%d > %d): %q", w, width, line)
		}
	}
}

func TestDrawerRowLineSelectedAndUnselected(t *testing.T) {
	const width = 60
	normal := drawerRowLine("label", "value", false, width, 3)
	selected := drawerRowLine("label", "value", true, width, 3)

	if !strings.HasPrefix(ansi.Strip(normal), "  label") {
		t.Errorf("normal row missing 2-space prefix: %q", normal)
	}
	if !strings.HasPrefix(ansi.Strip(selected), "▸ label") {
		t.Errorf("selected row missing cursor marker: %q", selected)
	}
	if !strings.HasSuffix(strings.TrimSpace(ansi.Strip(normal)), "value") {
		t.Errorf("normal row missing right value: %q", normal)
	}
}
