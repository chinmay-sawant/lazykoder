package agent

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxProjectInstructionsBytes caps AGENTS.md on the wire so a huge file
	// cannot dominate the request alone.
	maxProjectInstructionsBytes = 200_000
	projectInstructionsHeader   = "Project instructions from AGENTS.md (follow these conventions for this workdir):"
)

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
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return projectInstructionsHeader + "\n\n" + content
}
