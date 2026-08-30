package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/prompts"
)

const (
	// maxProjectInstructionsBytes caps AGENTS.md on the wire so a huge file
	// cannot dominate the request alone.
	maxProjectInstructionsBytes = 200_000
)

var projectInstructionsHeader = prompts.Must("agent/project-instructions-header.md")

// LoadProjectInstructions reads AGENTS.md (then agents.md) from workdir.
// ok is false when missing, empty, or unreadable.
func LoadProjectInstructions(workdir string) (content, path string, ok bool) {
	if strings.TrimSpace(workdir) == "" {
		return "", "", false
	}
	for _, name := range []string{"AGENTS.md", "agents.md"} {
		p := filepath.Join(workdir, name)
		raw, err := os.ReadFile(p)
		if err != nil || len(raw) == 0 {
			continue
		}
		text := string(raw)
		if len(raw) > maxProjectInstructionsBytes {
			text = string(raw[:maxProjectInstructionsBytes]) +
				"\n\n[truncated: AGENTS.md exceeded 200000 bytes; remaining content omitted]"
		}
		return text, p, true
	}
	return "", "", false
}

// FormatProjectInstructionsMessage builds the system-message body.
func FormatProjectInstructionsMessage(content string) string {
	return FormatProjectInstructionsMessageFor("", content)
}

// FormatProjectInstructionsMessageFor builds the system-message body using
// the prompt customization for workdir.
func FormatProjectInstructionsMessageFor(workdir, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return prompts.New(workdir).Must("agent/project-instructions-header.md") + "\n\n" + content
}
