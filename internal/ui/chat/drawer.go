package chat

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

var (
	drawerSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.ColorText()).
				Background(theme.ColorBorder())
	drawerNormalStyle = lipgloss.NewStyle().
				Foreground(theme.ColorMute())
	drawerHeaderTitleStyle = hintStyle
	drawerHeaderMetaStyle  = lipgloss.NewStyle().Foreground(theme.ColorText())
)

// drawerChrome renders the shared frame for all drawers (status, models,
// commands, sub-agents) to keep titles, meta, body lines, and hint footers unified.
func drawerChrome(title, meta, body, hint string, width int) string {
	var parts []string

	if title != "" || meta != "" {
		head := ""
		if title != "" {
			head = drawerHeaderTitleStyle.Render(title)
		}
		if title != "" && meta != "" {
			head += hintStyle.Render("  ·  ")
		}
		if meta != "" {
			head += drawerHeaderMetaStyle.Render(meta)
		}
		if lipgloss.Width(head) > width {
			head = truncateRunes(head, width)
		}
		parts = append(parts, head)
	}

	if body != "" {
		parts = append(parts, body)
	}

	if hint != "" {
		foot := hintStyle.Width(width).Render(truncateRunes(hint, width))
		parts = append(parts, foot)
	}

	content := strings.Join(parts, "\n")
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(content)
}

// drawerRowLine formats a standard drawer row with left label and right value.
func drawerRowLine(left, right string, selected bool, width, leftPad int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	leftWidth := max(1, width-lipgloss.Width(right)-leftPad)
	leftTrunc := truncateRunes(prefix+left, leftWidth)
	gap := max(1, width-lipgloss.Width(leftTrunc)-lipgloss.Width(right))
	line := leftTrunc + strings.Repeat(" ", gap) + right
	if selected {
		return drawerSelectedStyle.Width(width).MaxWidth(width).Render(line)
	}
	return drawerNormalStyle.Width(width).MaxWidth(width).Render(line)
}
