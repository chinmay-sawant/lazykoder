package chat

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	plasmaBlobWidth      = 14
	plasmaBlobFrameStep  = 0.3
	plasmaBlobColumnStep = 0.4
	plasmaBlobIntensity  = 2.5
)

var plasmaBlobShades = []string{"░", "▒", "▓", "█", "▓", "▒"}

// plasmaBlob renders animation 60 from temp/4_load_animations.go. Its color
// follows the selected light or dark theme accent.
func (m Model) plasmaBlob() string {
	style := lipgloss.NewStyle().Foreground(theme.ColorAccent())
	var line strings.Builder

	for i := 0; i < plasmaBlobWidth; i++ {
		idx := int((math.Sin(
			float64(m.pulse)*plasmaBlobFrameStep+
				float64(i)*plasmaBlobColumnStep,
		)+1.0)*plasmaBlobIntensity) % len(plasmaBlobShades)
		line.WriteString(style.Render(plasmaBlobShades[idx]))
	}

	return line.String()
}
