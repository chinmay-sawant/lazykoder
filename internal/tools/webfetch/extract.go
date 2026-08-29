package webfetch

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const (
	maxExtractedLinks    = 100
	maxLinkTextRunes     = 200
	maxLinkURLRunes      = 2048
	maxExtractedEmails   = 100
	maxEmailFieldRunes   = 512
	maxTitleRunes        = 512
	maxReadableTextBytes = 5 * 1024 * 1024
	maxMetadataBytes     = 64 * 1024
)

var visibleEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

type extractedLink struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type extractedEmailLink struct {
	URL     string `json:"url"`
	Address string `json:"address"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
}

func extractHTML(body []byte, baseURL, format string) (Result, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return Result{}, err
	}
	root := readableRoot(doc)
	if root == nil {
		return Result{}, nil
	}

	links := make([]extractedLink, 0)
	emailLinks := make([]extractedEmailLink, 0)
	collectLinks(root, baseURL, &links, &emailLinks)
	content := renderReadableText(root, strings.EqualFold(format, "markdown"))
	content, contentTruncated := truncateUTF8(content, maxReadableTextBytes)
	emails := collectVisibleEmails(content)
	title := strings.TrimSpace(textContent(findFirst(doc, "title")))
	if title == "" {
		title = strings.TrimSpace(textContent(findFirst(root, "h1")))
	}
	title = truncateRunes(title, maxTitleRunes)

	needsBrowser := strings.TrimSpace(content) == "" ||
		(len([]rune(content)) < 240 && bytesContainFold(body, "<script") &&
			findFirst(doc, "article") == nil && findFirst(doc, "main") == nil)
	metadata := map[string]any{
		"title":             title,
		"final_url":         truncateRunes(baseURL, maxLinkURLRunes),
		"links":             links,
		"email_links":       emailLinks,
		"emails":            emails,
		"needs_browser":     needsBrowser,
		"readable":          strings.TrimSpace(content) != "",
		"content_truncated": contentTruncated,
	}
	return Result{
		Output:   content,
		Metadata: boundMetadata(metadata),
	}, nil
}

func boundMetadata(metadata map[string]any) map[string]any {
	if metadataSize(metadata) <= maxMetadataBytes {
		return metadata
	}
	metadata["metadata_truncated"] = true
	for metadataSize(metadata) > maxMetadataBytes {
		if trimMetadataSlice(metadata, "links") ||
			trimMetadataSlice(metadata, "email_links") ||
			trimMetadataSlice(metadata, "emails") {
			continue
		}
		break
	}
	return metadata
}

func metadataSize(metadata map[string]any) int {
	data, err := json.Marshal(metadata)
	if err != nil {
		return maxMetadataBytes + 1
	}
	return len(data)
}

func trimMetadataSlice(metadata map[string]any, key string) bool {
	switch values := metadata[key].(type) {
	case []extractedLink:
		if len(values) == 0 {
			return false
		}
		metadata[key] = values[:len(values)-1]
		return true
	case []extractedEmailLink:
		if len(values) == 0 {
			return false
		}
		metadata[key] = values[:len(values)-1]
		return true
	case []string:
		if len(values) == 0 {
			return false
		}
		metadata[key] = values[:len(values)-1]
		return true
	default:
		return false
	}
}

func readableRoot(doc *html.Node) *html.Node {
	if article := findFirst(doc, "article"); article != nil {
		return article
	}
	if main := findFirst(doc, "main"); main != nil {
		return main
	}
	return findFirst(doc, "body")
}

func findFirst(node *html.Node, tag string) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func renderReadableText(root *html.Node, markdown bool) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || isHidden(node) {
			return
		}
		if node.Type == html.TextNode {
			appendText(&builder, node.Data)
			return
		}
		if node.Type != html.ElementNode {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			return
		}
		if skippedContentTag(node.Data) {
			return
		}

		level := headingLevel(node.Data)
		if markdown {
			if level > 0 {
				ensureLineBreak(&builder)
				builder.WriteString(strings.Repeat("#", level))
				builder.WriteByte(' ')
			}
			if node.Data == "li" {
				ensureLineBreak(&builder)
				builder.WriteString("- ")
			}
		}
		block := isBlockTag(node.Data)
		if block && level == 0 {
			ensureLineBreak(&builder)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if block {
			ensureLineBreak(&builder)
		}
	}
	walk(root)
	return normalizeLines(builder.String())
}

func collectLinks(root *html.Node, baseURL string, links *[]extractedLink, emailLinks *[]extractedEmailLink) {
	if root == nil || (len(*links) >= maxExtractedLinks && len(*emailLinks) >= maxExtractedEmails) {
		return
	}
	if root.Type == html.ElementNode && root.Data == "a" {
		href := attribute(root, "href")
		if href != "" {
			if emailLink, ok := parseEmailLink(href); ok {
				*emailLinks = appendBoundedEmailLink(*emailLinks, emailLink)
			} else if link, ok := parseHTTPLink(href, baseURL); ok {
				*links = appendBoundedLink(*links, extractedLink{
					Text: truncateRunes(strings.TrimSpace(textContent(root)), maxLinkTextRunes),
					URL:  link,
				})
			}
		}
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		collectLinks(child, baseURL, links, emailLinks)
		if len(*links) >= maxExtractedLinks && len(*emailLinks) >= maxExtractedEmails {
			return
		}
	}
}

func parseHTTPLink(raw, baseURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}
	if len([]rune(resolved.String())) > maxLinkURLRunes {
		return "", false
	}
	return resolved.String(), true
}

func parseEmailLink(raw string) (extractedEmailLink, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "mailto") {
		return extractedEmailLink{}, false
	}
	address := parsed.Opaque
	if address == "" {
		address = parsed.Path
	}
	address, err = url.PathUnescape(address)
	if err != nil || address == "" {
		return extractedEmailLink{}, false
	}
	query := parsed.Query()
	if len([]rune(parsed.String())) > maxLinkURLRunes {
		return extractedEmailLink{}, false
	}
	return extractedEmailLink{
		URL:     parsed.String(),
		Address: truncateRunes(address, maxEmailFieldRunes),
		Subject: truncateRunes(query.Get("subject"), maxEmailFieldRunes),
		Body:    truncateRunes(query.Get("body"), maxEmailFieldRunes),
	}, true
}

func appendBoundedLink(links []extractedLink, link extractedLink) []extractedLink {
	for _, existing := range links {
		if existing.URL == link.URL {
			return links
		}
	}
	if len(links) >= maxExtractedLinks {
		return links
	}
	return append(links, link)
}

func appendBoundedEmailLink(links []extractedEmailLink, link extractedEmailLink) []extractedEmailLink {
	for _, existing := range links {
		if existing.URL == link.URL {
			return links
		}
	}
	if len(links) >= maxExtractedEmails {
		return links
	}
	return append(links, link)
}

func collectVisibleEmails(content string) []string {
	emails := make([]string, 0)
	for _, candidate := range visibleEmailPattern.FindAllString(content, -1) {
		seen := false
		for _, existing := range emails {
			if strings.EqualFold(existing, candidate) {
				seen = true
				break
			}
		}
		if !seen && len(emails) < maxExtractedEmails {
			emails = append(emails, truncateRunes(candidate, maxEmailFieldRunes))
		}
	}
	return emails
}

func textContent(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.TextNode {
		return node.Data
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(textContent(child))
	}
	return builder.String()
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func isHidden(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	if hasAttribute(node, "hidden") {
		return true
	}
	style := strings.ToLower(strings.ReplaceAll(attribute(node, "style"), " ", ""))
	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden")
}

func hasAttribute(node *html.Node, name string) bool {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return true
		}
	}
	return false
}

func skippedContentTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "script", "style", "noscript", "template", "head", "nav", "footer", "aside", "form":
		return true
	default:
		return false
	}
}

func isBlockTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "address", "article", "blockquote", "br", "div", "dl", "dt", "dd", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tr", "ul":
		return true
	default:
		return false
	}
}

func headingLevel(tag string) int {
	if len(tag) != 2 || tag[0] != 'h' || tag[1] < '1' || tag[1] > '6' {
		return 0
	}
	return int(tag[1] - '0')
}

func appendText(builder *strings.Builder, value string) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return
	}
	if builder.Len() > 0 && !strings.HasSuffix(builder.String(), " ") && !strings.HasSuffix(builder.String(), "\n") {
		builder.WriteByte(' ')
	}
	builder.WriteString(value)
}

func ensureLineBreak(builder *strings.Builder) {
	for builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
		builder.WriteByte('\n')
	}
}

func normalizeLines(value string) string {
	lines := strings.Split(value, "\n")
	output := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(output) > 0 && output[len(output)-1] != "" {
				output = append(output, "")
			}
			continue
		}
		output = append(output, line)
	}
	return strings.TrimSpace(strings.Join(output, "\n"))
}

func bytesContainFold(body []byte, needle string) bool {
	return strings.Contains(strings.ToLower(string(body)), strings.ToLower(needle))
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	if maxBytes <= 0 {
		return "", true
	}
	end := maxBytes
	for end > 0 && end < len(value) && value[end]&0xc0 == 0x80 {
		end--
	}
	return value[:end], true
}
