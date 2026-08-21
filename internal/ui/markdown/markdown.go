// Package markdown renders the Markdown subset used in assistant messages.
package markdown

import (
	"os"
	"strings"
)

// Render formats common assistant Markdown. It routes to the glamour renderer
// configured with the dark palette in internal/ui/theme. If glamour fails or
// if LAZYKODER_MARKDOWN=fallback is set, it falls back to the hand-rolled renderer.
func Render(input string, width int) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	flag := strings.ToLower(os.Getenv("LAZYKODER_MARKDOWN"))
	if flag == "fallback" || flag == "legacy" || flag == "custom" {
		return renderFallback(input, width)
	}

	out, err := renderGlow(input, width)
	if err != nil {
		return renderFallback(input, width)
	}
	return out
}
