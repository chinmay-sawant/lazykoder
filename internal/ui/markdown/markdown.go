// Package markdown renders the Markdown subset used in assistant messages.
package markdown

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	boldMarker         = "**"
	codeMarker         = "`"
	boldMarkerLength   = 2
	emphasisMarkerSize = 1
	boldTokenLength    = 4
	singleTokenLength  = 2

	tableHeaderOffset   = 2
	tableSideBorders    = 2
	tableMinColumnWidth = 3
	tableCellPadding    = 2

	// quoteIndent is the quote style chrome: the left border glyph plus the
	// padding column, so quoted text wraps one column narrower.
	quoteIndent = 2
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
			Background(theme.ColorBg()).
			Foreground(theme.ColorText())
	codeBlockStyle = lipgloss.NewStyle().
			Background(theme.ColorBg()).
			Foreground(theme.ColorText())
	codeLanguageStyle = lipgloss.NewStyle().
				Foreground(theme.ColorMute()).
				Bold(true)
	quoteStyle = lipgloss.NewStyle().
			Foreground(theme.ColorMute()).
			BorderLeft(true).
			BorderForeground(theme.ColorBorder()).
			PaddingLeft(1)
	tableBorderStyle = lipgloss.NewStyle().
				Foreground(theme.ColorBorder())
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.ColorText())
	tableBodyStyle = lipgloss.NewStyle()
)

// Render formats common assistant Markdown without adding a new renderer
// dependency. Fenced code blocks sit on the same solid black layer as the
// rest of the TUI. GFM pipe tables are aligned into bordered columns. When
// width is greater than zero blocks span at most that many columns.
func Render(input string, width int) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	source := strings.Split(input, "\n")
	output := make([]string, 0, len(source))
	inCode := false
	language := ""
	codeLines := []string{}
	flushCode := func() {
		output = append(output, renderCodeBlock(language, codeLines, width))
		language = ""
		codeLines = []string{}
	}

	for i := 0; i < len(source); i++ {
		trimmed := strings.TrimSpace(source[i])
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
			codeLines = append(codeLines, source[i])
			continue
		}
		if rows, ok := tableRows(source, i); ok {
			output = append(output, renderTable(rows, width))
			i += len(rows)
			continue
		}
		output = append(output, renderLine(source[i], width))
	}
	if inCode {
		flushCode()
	}
	return strings.Join(output, "\n")
}

// tableRows parses a GFM pipe table starting at source[i]: a header row
// followed by a separator row (`| --- | :---: |`) and any body rows. It
// reports the parsed rows and whether a table was found. Cells are trimmed;
// escaped pipes (`\|`) are unescaped.
func tableRows(source []string, i int) ([][]string, bool) {
	header, ok := splitTableRow(source[i])
	if !ok || i+1 >= len(source) {
		return nil, false
	}
	sep, ok := splitTableRow(source[i+1])
	if !ok || !isTableSeparator(sep) {
		return nil, false
	}
	rows := [][]string{header}
	j := i + tableHeaderOffset
	for j < len(source) {
		row, ok := splitTableRow(source[j])
		if !ok {
			break
		}
		rows = append(rows, row)
		j++
	}
	return rows, true
}

func splitTableRow(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.Contains(trimmed, "|") {
		return nil, false
	}
	protected := strings.ReplaceAll(trimmed, `\|`, "\x00")
	trimmed = strings.Trim(protected, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.ReplaceAll(strings.TrimSpace(part), "\x00", "|"))
	}
	if len(cells) == 0 {
		return nil, false
	}
	return cells, true
}

func isTableSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !isSeparatorCell(cell) {
			return false
		}
	}
	return true
}

func isSeparatorCell(cell string) bool {
	cell = strings.TrimSpace(cell)
	if strings.HasPrefix(cell, ":") {
		cell = strings.TrimPrefix(cell, ":")
	}
	if strings.HasSuffix(cell, ":") {
		cell = strings.TrimSuffix(cell, ":")
	}
	return cell != "" && strings.Trim(cell, "-") == ""
}

// renderTable lays the parsed rows out as a bordered, column-aligned grid
// bounded by width (when greater than zero). The header row is bold.
func renderTable(rows [][]string, width int) string {
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return ""
	}
	widths := make([]int, cols)
	for _, row := range rows {
		for c, cell := range row {
			if w := lipgloss.Width(renderInline(cell)); w > widths[c] {
				widths[c] = w
			}
		}
	}
	if width > 0 {
		fitTableWidths(widths, width)
	}
	lines := []string{
		joinTableBorder("╭", "─", "┬", "╮", widths),
		renderTableRow(rows[0], widths, tableHeaderStyle),
	}
	if len(rows) > 1 {
		lines = append(lines, joinTableBorder("├", "─", "┼", "┤", widths))
		for _, row := range rows[1:] {
			lines = append(lines, renderTableRow(row, widths, tableBodyStyle))
		}
	}
	lines = append(lines, joinTableBorder("╰", "─", "┴", "╯", widths))
	return strings.Join(lines, "\n")
}

// fitTableWidths shrinks the widest columns until the bordered table fits in
// the available width. Columns never drop below three cells so truncation
// stays legible.
func fitTableWidths(widths []int, available int) {
	cols := len(widths)
	total := func() int {
		t := tableSideBorders + cols - 1
		for _, w := range widths {
			t += w + tableCellPadding
		}
		return t
	}
	for total() > available {
		widest := 0
		for c := 1; c < cols; c++ {
			if widths[c] > widths[widest] {
				widest = c
			}
		}
		if widths[widest] < tableMinColumnWidth {
			break
		}
		widths[widest]--
	}
}

func renderTableRow(row []string, widths []int, style lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(tableBorderStyle.Render("│"))
	for c, w := range widths {
		cell := ""
		if c < len(row) {
			cell = row[c]
		}
		rendered := renderInline(cell)
		rendered = ansi.Truncate(rendered, w, "…")
		b.WriteString(style.Render(" " + rendered))
		b.WriteString(strings.Repeat(" ", w-lipgloss.Width(rendered)))
		b.WriteString(tableBorderStyle.Render(" │"))
	}
	return b.String()
}

func joinTableBorder(left, fill, mid, right string, widths []int) string {
	var b strings.Builder
	b.WriteString(left)
	for c, w := range widths {
		b.WriteString(strings.Repeat(fill, w+tableCellPadding))
		if c < len(widths)-1 {
			b.WriteString(mid)
		}
	}
	b.WriteString(right)
	return b.String()
}

func renderCodeBlock(language string, lines []string, width int) string {
	body := strings.Join(lines, "\n")
	if language != "" {
		body = codeLanguageStyle.Render(language) + "\n" + body
	}
	style := codeBlockStyle
	if width > 0 {
		style = style.Width(width)
	}
	return style.Render(body)
}

func renderLine(line string, width int) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if level, heading, ok := parseHeading(trimmed); ok {
		style := headingStyles[min(level-1, len(headingStyles)-1)]
		return wrapText(style.Render(heading), width)
	}
	if strings.HasPrefix(trimmed, "> ") {
		inner := renderInline(strings.TrimPrefix(trimmed, "> "))
		return quoteStyle.Render(wrapText(inner, max(1, width-quoteIndent)))
	}
	if item, ok := parseUnorderedItem(trimmed); ok {
		return wrapListItem("• ", item, width)
	}
	if prefix, item, ok := parseOrderedItem(trimmed); ok {
		return wrapListItem(prefix, item, width)
	}
	return wrapText(renderInline(trimmed), width)
}

// wrapText wraps styled or plain text at the given width, preserving ANSI
// escape codes. Lines already inside the width are returned untouched so
// short paragraphs stay cheap to render.
func wrapText(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Wrap(s, width, " ")
}

// wrapListItem wraps a bullet or numbered item with a hanging indent: the
// first line carries the marker and continuation lines align under the text.
func wrapListItem(prefix, item string, width int) string {
	inner := wrapText(renderInline(item), max(1, width-lipgloss.Width(prefix)))
	lines := strings.Split(inner, "\n")
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
		} else {
			lines[i] = strings.Repeat(" ", lipgloss.Width(prefix)) + line
		}
	}
	return strings.Join(lines, "\n")
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
