// Package markdown renders the Markdown subset used in assistant messages.
package markdown

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	boldMarker         = "**"
	codeMarker         = "`"
	boldMarkerLength   = 2
	emphasisMarkerSize = 1
	boldTokenLength    = 4
	singleTokenLength  = 2
)

var (
	headingStyles = []lipgloss.Style{
		lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()),
		lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent()),
		lipgloss.NewStyle().Bold(true).Foreground(theme.ColorMute()),
	}
	boldStyle       = lipgloss.NewStyle().Bold(true)
	italicStyle     = lipgloss.NewStyle().Italic(true)
	inlineCodeStyle = lipgloss.NewStyle().
			Background(theme.ColorSurface()).
			Foreground(theme.ColorText())
	codeBlockStyle = lipgloss.NewStyle().
			Background(theme.ColorBg()).
			Foreground(theme.ColorText()).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorBorder()).
			Padding(0, 1)
	codeLanguageStyle = lipgloss.NewStyle().
				Foreground(theme.ColorMute()).
				Bold(true)
	quoteStyle = lipgloss.NewStyle().
			Foreground(theme.ColorMute()).
			BorderLeft(true).
			BorderForeground(theme.ColorBorder()).
			PaddingLeft(1)
)

// Render formats common assistant Markdown without adding a new renderer
// dependency. Fenced code blocks receive their own dark bordered card.
func Render(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	source := strings.Split(input, "\n")
	output := make([]string, 0, len(source))
	inCode := false
	language := ""
	codeLines := []string{}
	flushCode := func() {
		output = append(output, renderCodeBlock(language, codeLines))
		language = ""
		codeLines = []string{}
	}

	for _, line := range source {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				flushCode()
				inCode = false
				continue
			}
			inCode = true
			language = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			continue
		}
		if inCode {
			codeLines = append(codeLines, line)
			continue
		}
		output = append(output, renderLine(line))
	}
	if inCode {
		flushCode()
	}
	return strings.Join(output, "\n")
}

func renderCodeBlock(language string, lines []string) string {
	body := strings.Join(lines, "\n")
	if language != "" {
		body = codeLanguageStyle.Render(language) + "\n" + body
	}
	return codeBlockStyle.Render(body)
}

func renderLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if level, heading, ok := parseHeading(trimmed); ok {
		style := headingStyles[min(level-1, len(headingStyles)-1)]
		return style.Render(heading)
	}
	if strings.HasPrefix(trimmed, "> ") {
		return quoteStyle.Render(renderInline(strings.TrimPrefix(trimmed, "> ")))
	}
	if item, ok := parseUnorderedItem(trimmed); ok {
		return "• " + renderInline(item)
	}
	if prefix, item, ok := parseOrderedItem(trimmed); ok {
		return prefix + renderInline(item)
	}
	return renderInline(trimmed)
}

func parseHeading(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level == len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func parseUnorderedItem(line string) (string, bool) {
	if len(line) < 3 || (line[0] != '-' && line[0] != '*' && line[0] != '+') || line[1] != ' ' {
		return "", false
	}
	return strings.TrimSpace(line[2:]), true
}

func parseOrderedItem(line string) (string, string, bool) {
	idx := 0
	for idx < len(line) && line[idx] >= '0' && line[idx] <= '9' {
		idx++
	}
	if idx == 0 || idx+1 >= len(line) || line[idx] != '.' || line[idx+1] != ' ' {
		return "", "", false
	}
	return line[:idx+2], strings.TrimSpace(line[idx+2:]), true
}

func renderInline(input string) string {
	var output strings.Builder
	for i := 0; i < len(input); {
		if strings.HasPrefix(input[i:], boldMarker) {
			if end := strings.Index(input[i+boldMarkerLength:], boldMarker); end >= 0 {
				output.WriteString(boldStyle.Render(input[i+boldMarkerLength : i+boldMarkerLength+end]))
				i += end + boldTokenLength
				continue
			}
		}
		if input[i] == '`' {
			if end := strings.IndexByte(input[i+emphasisMarkerSize:], codeMarker[0]); end >= 0 {
				output.WriteString(inlineCodeStyle.Render(input[i+emphasisMarkerSize : i+emphasisMarkerSize+end]))
				i += end + singleTokenLength
				continue
			}
		}
		if input[i] == '*' || input[i] == '_' {
			delimiter := input[i : i+emphasisMarkerSize]
			if end := strings.Index(input[i+emphasisMarkerSize:], delimiter); end > 0 {
				output.WriteString(italicStyle.Render(input[i+emphasisMarkerSize : i+emphasisMarkerSize+end]))
				i += end + singleTokenLength
				continue
			}
		}
		output.WriteByte(input[i])
		i++
	}
	return output.String()
}
