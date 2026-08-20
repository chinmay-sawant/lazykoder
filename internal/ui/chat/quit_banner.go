package chat

import "strings"

// quitLogo is the post-exit ASCII wordmark printed above the session id.
const quitLogo = "" +
	"  █    █▀▀█ ▀▀▀█ █  █ █ ▄▀ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█\n" +
	"  █    █▄▄█  ▄▀   ▀▀█ █▀▄  █  █ █  █ █▀▀▀ █▄▄▀\n" +
	"  ▀▀▀▀ ▀  ▀ ▀▀▀▀  ▀▀▀ ▀  ▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀  ▀"

// SessionID returns the live session id, or "" when no session exists yet.
func (m Model) SessionID() string {
	if m.session == nil {
		return ""
	}
	return strings.TrimSpace(m.session.ID)
}

// SessionTitle returns the live session title, or "" when none exists yet.
func (m Model) SessionTitle() string {
	if m.session == nil {
		return ""
	}
	return strings.Join(strings.Fields(m.session.Title), " ")
}

// FormatQuitBanner returns the post-alt-screen quit text printed by main.
// sessionID may be empty when the user quits before the first send.
// title is the session name; empty becomes "untitled" when an id is present.
func FormatQuitBanner(sessionID, title string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return quitLogo + "\nlk (no session)\nresume older runs with /resume or ctrl+s\n"
	}
	name := strings.Join(strings.Fields(title), " ")
	if name == "" {
		name = "untitled"
	}
	return quitLogo + "\nlk " + id + "\nsession name: " + name + "\nresume with /resume or ctrl+s\n"
}
