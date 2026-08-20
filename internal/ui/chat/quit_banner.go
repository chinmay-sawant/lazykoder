package chat

import "strings"

// quitLogo is the post-exit ASCII wordmark printed above the session id.
// Rendered in the pagga block font so LAZYKODER stays readable in a mono
// terminal (verified via screenshots/quit-banner-check.png).
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

// FormatQuitBanner returns the post-alt-screen quit text printed by main.
// sessionID may be empty when the user quits before the first send.
func FormatQuitBanner(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return quitLogo + "\nlk (no session)\nresume older runs with /resume or ctrl+s\n"
	}
	return quitLogo + "\nlk " + id + "\nresume with /resume or ctrl+s\n"
}
