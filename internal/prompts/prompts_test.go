package prompts

import (
	"strings"
	"testing"
)

func TestMustCompactContainsHeadings(t *testing.T) {
	text := Must("compact.md")
	if text == "" {
		t.Fatal("compact.md is empty")
	}
	for _, heading := range []string{
		"## Primary request and intent",
		"## Key decisions and constraints",
		"## Files and code that matter",
		"## Errors and how they were fixed",
		"## Pending work / TODOs",
		"## Current work",
		"## Next step",
		"## All user messages",
	} {
		if !strings.Contains(text, heading) {
			t.Errorf("compact.md missing heading %q", heading)
		}
	}
	for _, rule := range []string{
		"handoff",
		"Follow the user's language",
		"Do not invent",
		"verbatim",
	} {
		if !strings.Contains(text, rule) {
			t.Errorf("compact.md missing rule %q", rule)
		}
	}
}

func TestMustUnknownPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Must(unknown) should panic")
		}
	}()
	_ = Must("missing-prompt.md")
}
