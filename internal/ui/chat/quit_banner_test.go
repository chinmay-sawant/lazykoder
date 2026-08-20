package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestFormatQuitBanner(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		id    string
		title string
		want  string
	}{
		{
			name:  "with session and title",
			id:    "ses_abcdef0123456789",
			title: "fix the resume picker",
			want:  quitLogo + "\nlk ses_abcdef0123456789\nsession name: fix the resume picker\nresume with /resume or ctrl+s\n",
		},
		{
			name:  "empty title becomes untitled",
			id:    "ses_abcdef0123456789",
			title: "  \t  ",
			want:  quitLogo + "\nlk ses_abcdef0123456789\nsession name: untitled\nresume with /resume or ctrl+s\n",
		},
		{
			name:  "empty",
			id:    "",
			title: "ignored",
			want:  quitLogo + "\nlk (no session)\nresume older runs with /resume or ctrl+s\n",
		},
		{
			name:  "whitespace only id",
			id:    "  \t  ",
			title: "x",
			want:  quitLogo + "\nlk (no session)\nresume older runs with /resume or ctrl+s\n",
		},
		{
			name:  "trimmed id and title",
			id:    "  ses_abc  ",
			title: "  hello   world  ",
			want:  quitLogo + "\nlk ses_abc\nsession name: hello world\nresume with /resume or ctrl+s\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FormatQuitBanner(tc.id, tc.title)
			if got != tc.want {
				t.Fatalf("FormatQuitBanner(%q, %q) = %q, want %q", tc.id, tc.title, got, tc.want)
			}
			if !strings.HasPrefix(got, quitLogo) {
				t.Fatalf("quit logo missing from banner: %q", got)
			}
		})
	}
}

func TestSessionIDAndTitle(t *testing.T) {
	t.Parallel()
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	if got := m.SessionID(); got != "" {
		t.Fatalf("SessionID on fresh model = %q, want empty", got)
	}
	if got := m.SessionTitle(); got != "" {
		t.Fatalf("SessionTitle on fresh model = %q, want empty", got)
	}

	tmp := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "quit banner title", Directory: tmp})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	m = New(Options{Store: st, Client: deadClient(), Workdir: tmp, Session: &sess})
	if got := m.SessionID(); got != sess.ID {
		t.Fatalf("SessionID = %q, want %q", got, sess.ID)
	}
	if got := m.SessionTitle(); got != "quit banner title" {
		t.Fatalf("SessionTitle = %q, want quit banner title", got)
	}
}

func TestFreshLaunchDoesNotReplayExistingSession(t *testing.T) {
	tmp := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{
		Title:     "prior run",
		Directory: tmp,
		Model:     "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{
		SessionID: sess.ID,
		Role:      "user",
		Agent:     "user",
	})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	prior := "unique prior transcript text xyzzy"
	if _, err := st.InsertPart(context.Background(), db.Part{
		MessageID: um.ID,
		Type:      "text",
		Text:      &prior,
	}); err != nil {
		t.Fatalf("InsertPart: %v", err)
	}

	// Launch path: Session is nil even though the store has a main session.
	m := New(Options{Store: st, Client: deadClient(), Workdir: tmp})
	if m.session != nil {
		t.Fatal("fresh launch attached a session")
	}
	if got := m.SessionID(); got != "" {
		t.Fatalf("SessionID = %q, want empty", got)
	}
	v := stripANSI(viewText(m))
	if strings.Contains(v, prior) {
		t.Fatalf("fresh launch replayed prior transcript: %q", v)
	}
	if !strings.Contains(v, "new session") {
		t.Fatalf("fresh launch missing new-session chrome: %q", v)
	}

	// Explicit resume still works when a session is passed.
	resumed := New(Options{Store: st, Client: deadClient(), Workdir: tmp, Session: &sess})
	if !strings.Contains(stripANSI(viewText(resumed)), prior) {
		t.Fatal("explicit Session resume did not replay prior text")
	}
}

func TestNewSurfacesProjectInstructionsNotice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir})
	if m.projectInstructionsNotice != "project instructions: AGENTS.md" {
		t.Fatalf("notice = %q", m.projectInstructionsNotice)
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "project instructions: AGENTS.md") {
		t.Fatalf("view missing notice: %q", v)
	}
	m2 := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	if m2.projectInstructionsNotice != "" {
		t.Fatalf("unexpected notice without AGENTS.md: %q", m2.projectInstructionsNotice)
	}
}
