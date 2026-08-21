// Package roles owns sub-agent role names and their effective tool policies.
package roles

import "strings"

const (
	Explore = "explore"
	Plan    = "plan"
	General = "general"
)

// Normalize returns a known role or the normalized fallback.
func Normalize(role, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case Explore:
		return Explore
	case Plan:
		return Plan
	case General:
		return General
	}
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case Plan:
		return Plan
	case General:
		return General
	default:
		return Explore
	}
}

// Tools returns a new tool allowlist for role.
func Tools(role string) []string {
	switch Normalize(role, Explore) {
	case General:
		return []string{"bash", "read", "grep", "write", "edit", "webfetch"}
	default:
		return []string{"bash", "read", "grep", "webfetch"}
	}
}
