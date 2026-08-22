package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestRecallSearchesOnlyLocalRecapFilesWithQuotedTerms(t *testing.T) {
	workdir := t.TempDir()
	recapDir := filepath.Join(workdir, "knowledge-base", "recaps", "things-to-avoid")
	if err := os.MkdirAll(recapDir, 0o755); err != nil {
		t.Fatalf("mkdir recaps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recapDir, "avoid.md"), []byte("do not repeat parser.v2 migration\n"), 0o600); err != nil {
		t.Fatalf("write recap: %v", err)
	}
	m := New(Options{Workdir: workdir})
	got, err := m.recall(context.Background(), "session", "parser.v2 migration")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(got, "avoid.md") || !strings.Contains(got, "parser.v2") {
		t.Fatalf("recall = %q, want matching recap hit", got)
	}
}

func TestRecallIgnoresEmptyOrUnhelpfulPrompts(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	for _, prompt := range []string{"", "the with user"} {
		got, err := m.recall(context.Background(), "session", prompt)
		if err != nil {
			t.Fatalf("recall %q: %v", prompt, err)
		}
		if got != "" {
			t.Fatalf("recall %q = %q, want empty", prompt, got)
		}
	}
}

func TestRecapEligibilitySurvivesPersistedUserEcho(t *testing.T) {
	store := newTestStore(t)
	session, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cfg := settings.Default()
	cfg.Recap.Enabled = true
	cfg.Recap.AfterChats = 1
	m := New(Options{Store: store, Client: deadClient(), Workdir: t.TempDir(), Session: &session, Settings: &cfg})
	m.pendingUser = "hello"
	m.turnHasNewUser = true
	m.applyPart(db.Part{Type: "text", Text: recapStringPtr("hello")})
	if m.pendingUser != "" {
		t.Fatalf("pending user = %q, want echoed user cleared", m.pendingUser)
	}
	if !m.recapEligible(nil) {
		t.Fatal("recap became ineligible after the persisted user echo")
	}
	if got := m.finishTurn(nil).turnHasNewUser; got {
		t.Fatal("finished turn kept recap eligibility armed")
	}
}

func TestRecapSchedulesAfterConfiguredSuccessfulChats(t *testing.T) {
	store := newTestStore(t)
	session, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cfg := settings.Default()
	cfg.Recap.Enabled = true
	cfg.Recap.AfterChats = 2
	m := New(Options{Store: store, Client: deadClient(), Workdir: t.TempDir(), Session: &session, Settings: &cfg})
	m.turnSeq = 1
	m.busy = true
	m.turnHasNewUser = true
	first, firstCmd := m.Update(eventDoneMsg{seq: 1})
	m = first.(Model)
	if firstCmd != nil || m.successfulRecapChats != 1 {
		t.Fatalf("after first chat: cmd=%v count=%d, want no command and count 1", firstCmd != nil, m.successfulRecapChats)
	}
	m.busy = true
	m.turnHasNewUser = true
	second, secondCmd := m.Update(eventDoneMsg{seq: 1})
	m = second.(Model)
	if secondCmd == nil || m.successfulRecapChats != 0 {
		t.Fatalf("after second chat: cmd=%v count=%d, want recap command and reset", secondCmd != nil, m.successfulRecapChats)
	}
}

func TestAgentsDrawerShowsLatestRecapRecord(t *testing.T) {
	store := newTestStore(t)
	session, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	message, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	record, _, err := store.ReserveRecap(context.Background(), db.RecapRecord{
		SessionID: session.ID, SourceStartSeq: message.Seq, SourceEndSeq: message.Seq,
		SourceEndMessageID: message.ID, Model: "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("reserve recap: %v", err)
	}
	if err := store.ClaimRecap(context.Background(), record.ID); err != nil {
		t.Fatalf("claim recap: %v", err)
	}
	if err := store.CompleteRecap(context.Background(), record.ID, db.RecapArtifacts{
		Sessions:      db.RecapArtifact{Path: "sessions/000001-msg.md", SHA256: strings.Repeat("a", 64)},
		Questions:     db.RecapArtifact{Path: "questions/000001-msg.md", SHA256: strings.Repeat("b", 64)},
		ThingsToAvoid: db.RecapArtifact{Path: "things-to-avoid/000001-msg.md", SHA256: strings.Repeat("c", 64)},
	}); err != nil {
		t.Fatalf("complete recap: %v", err)
	}
	cfg := settings.Default()
	cfg.Recap.Enabled = true
	workdir := t.TempDir()
	writeRecapTestArtifact(t, workdir, "sessions/000001-msg.md", "---\nmodel: deepseek-v4-flash\n---\n\n## Decision\n\nThe **local** recap is kept.\n\n```sh\nrg recap knowledge-base/recaps\n```")
	writeRecapTestArtifact(t, workdir, "questions/000001-msg.md", "---\nmodel: deepseek-v4-flash\n---\n\n# Questions\n\n- Should the worker retry?\n")
	writeRecapTestArtifact(t, workdir, "things-to-avoid/000001-msg.md", "---\nmodel: deepseek-v4-flash\n---\n\n# Things to avoid\n\n- Do not hide failed recaps.\n")
	m := New(Options{Store: store, Client: deadClient(), Workdir: workdir, Session: &session, Settings: &cfg})
	for _, path := range []string{
		"sessions/000001-msg.md",
		"knowledge-base/recaps/sessions/000001-msg.md",
	} {
		if body, err := m.readRecapArtifact(path); err != nil || !strings.Contains(body, "local") {
			t.Fatalf("read recap artifact %q: body=%q err=%v", path, body, err)
		}
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = updated.(Model)
	m = m.openSubagentPicker()
	view := stripANSI(m.subagentDrawerView())
	for _, want := range []string{"recaps", "completed", "│", "enter/→ context"} {
		if !strings.Contains(view, want) {
			t.Fatalf("drawer missing %q: %q", want, view)
		}
	}
	if !m.recapSelected {
		t.Fatalf("recap row should be selected when no child rows: recaps=%d children=%d", len(m.recapItems), len(m.subagentItems))
	}
	if strings.TrimSpace(m.prompt.Value()) != "" {
		t.Fatalf("prompt unexpectedly populated: %q", m.prompt.Value())
	}
	next, _ := m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next
	if !m.recapDetailMode || !m.subagentLogMode {
		t.Fatalf("recap enter did not open detail: recap=%v log=%v", m.recapDetailMode, m.subagentLogMode)
	}
	detail := stripANSI(m.recapDetailScreen())
	for _, want := range []string{"SUMMARY", "SUMMARY  ·  sessions/000001-msg.md", "Decision", "local", "rg recap knowledge-base/recaps", "QUESTIONS", "Should the worker retry?", "THINGS TO AVOID", "Do not hide failed recaps."} {
		if !strings.Contains(detail, want) {
			t.Fatalf("recap detail missing %q: %q", want, detail)
		}
	}
	if strings.Contains(detail, "```") || strings.Contains(detail, "\n---") {
		t.Fatalf("recap detail kept raw Markdown or front matter: %q", detail)
	}
	ansiDetail := m.recapDetailScreen()
	for _, background := range []string{
		ansiBackground(theme.ColorEditPanel()),
		ansiBackground(theme.ColorAssistantPanel()),
		ansiBackground(theme.ColorEditDelBg()),
	} {
		if !strings.Contains(ansiDetail, background) {
			t.Fatalf("recap detail missing panel background %q: %q", background, ansiDetail)
		}
	}
	next, _ = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = next
	if m.recapDetailMode || !m.subagentPickerMode || !m.recapSelected {
		t.Fatalf("left did not return to selected recap row: detail=%v picker=%v selected=%v", m.recapDetailMode, m.subagentPickerMode, m.recapSelected)
	}
	rowY := -1
	for y := range m.paintedLines() {
		if m.recapRowAt(y) {
			rowY = y
			break
		}
	}
	if rowY < 0 {
		t.Fatalf("recap row was not painted for click hit testing: %q", stripANSI(strings.Join(m.paintedLines(), "\\n")))
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 4, Y: rowY, Button: tea.MouseLeft})
	m = updated.(Model)
	if !m.recapDetailMode || !m.subagentLogMode {
		t.Fatalf("recap click did not open detail: recap=%v log=%v", m.recapDetailMode, m.subagentLogMode)
	}
	next, _ = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = next
	m.subagentItems = []subagentRow{{ID: "child-1", Name: "worker", Status: "completed"}}
	m.recapSelected = false
	m.subagentCursor = 0
	next, _ = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyUp})
	m = next
	if !m.recapSelected {
		t.Fatal("up from the first child row did not select the recap row")
	}
	next, _ = m.updateSubagentPickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next
	if m.recapSelected || m.subagentCursor != 0 {
		t.Fatalf("down from recap did not select the first child row: recap=%v cursor=%d", m.recapSelected, m.subagentCursor)
	}
}

func TestAgentsDrawerShowsRecapFailureReason(t *testing.T) {
	store := newTestStore(t)
	session, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	message, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	record, _, err := store.ReserveRecap(context.Background(), db.RecapRecord{
		SessionID:          session.ID,
		SourceStartSeq:     message.Seq,
		SourceEndSeq:       message.Seq,
		SourceEndMessageID: message.ID,
		Model:              "ox-alpha-free",
	})
	if err != nil {
		t.Fatalf("reserve recap: %v", err)
	}
	if err := store.ClaimRecap(context.Background(), record.ID); err != nil {
		t.Fatalf("claim recap: %v", err)
	}
	failure := `recap: decode envelope: unexpected end of JSON input`
	if err := store.FailRecap(context.Background(), record.ID, failure); err != nil {
		t.Fatalf("fail recap: %v", err)
	}

	cfg := settings.Default()
	cfg.Recap.Enabled = true
	m := New(Options{Store: store, Client: deadClient(), Workdir: t.TempDir(), Session: &session, Settings: &cfg})
	m = m.openSubagentPicker()
	view := stripANSI(m.subagentDrawerView())
	for _, want := range []string{"recaps", "failed", "error=recap: decode envelope"} {
		if !strings.Contains(view, want) {
			t.Fatalf("drawer missing %q: %q", want, view)
		}
	}
}

func recapStringPtr(value string) *string {
	return &value
}

func writeRecapTestArtifact(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, "knowledge-base", "recaps", filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir recap artifact: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write recap artifact: %v", err)
	}
}
