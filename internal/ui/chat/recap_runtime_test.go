package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
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
	got, err := m.recall(context.Background(), "session", "recall parser.v2 migration")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(got, "avoid.md") || !strings.Contains(got, "parser.v2") {
		t.Fatalf("recall = %q, want matching recap hit", got)
	}
}

func TestRecallSearchesMemoryBeforeRecaps(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "knowledge-base", "recaps"), 0o755); err != nil {
		t.Fatalf("mkdir recaps: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workdir, "knowledge-base"), 0o755); err != nil {
		t.Fatalf("mkdir knowledge base: %v", err)
	}
	document := recap.NewMemoryDocument()
	document.Preferences = append(document.Preferences, recap.MemoryEntry{
		ID:               "mem_test",
		State:            "active",
		Text:             "Keep memory local.",
		Evidence:         "The test records a project preference.",
		SourceMessageIDs: []string{"msg_1"},
		FirstSeenUTC:     "2026-08-22T12:00:00Z",
		LastSeenUTC:      "2026-08-22T12:00:00Z",
	})
	if _, err := recap.WriteMemoryDocument(context.Background(), workdir, document); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "knowledge-base", "recaps", "recent.md"), []byte("keep memory recent\n"), 0o600); err != nil {
		t.Fatalf("write recap: %v", err)
	}
	m := New(Options{Workdir: workdir})
	got, err := m.recall(context.Background(), "session", "memory recent")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(got, "MEMORY") || strings.Contains(got, "RECAP") {
		t.Fatalf("recall = %q, want memory hit to stop broader search", got)
	}
}

func TestRecallSkipsMalformedMemoryAndUsesRecaps(t *testing.T) {
	workdir := t.TempDir()
	recapDir := filepath.Join(workdir, "knowledge-base", "recaps")
	if err := os.MkdirAll(recapDir, 0o755); err != nil {
		t.Fatalf("mkdir recaps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "knowledge-base", "memories.md"), []byte("not an application-owned memory document\n"), 0o600); err != nil {
		t.Fatalf("write malformed memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recapDir, "recent.md"), []byte("keep memory recent\n"), 0o600); err != nil {
		t.Fatalf("write recap: %v", err)
	}
	got, err := (New(Options{Workdir: workdir})).recall(context.Background(), "session", "memory recent")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if strings.Contains(got, "MEMORY") || !strings.Contains(got, "RECAP") {
		t.Fatalf("recall = %q, want recap-only result", got)
	}
}

func TestRecallFallsBackToKnowledgeBaseAfterNoMemoryMatch(t *testing.T) {
	workdir := t.TempDir()
	path := filepath.Join(workdir, "knowledge-base", "03-concepts", "memory.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir knowledge base: %v", err)
	}
	if err := os.WriteFile(path, []byte("parser.v2 is documented in the project memory contract\n"), 0o600); err != nil {
		t.Fatalf("write knowledge-base page: %v", err)
	}
	got, err := (New(Options{Workdir: workdir})).recall(context.Background(), "session", "check recent parser.v2")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(got, "KNOWLEDGE-BASE") || !strings.Contains(got, "parser.v2") {
		t.Fatalf("recall = %q, want knowledge-base fallback", got)
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

func TestMemoryScanUsesBrailleLoader(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.busy = true
	m.recallScanning = true
	m.activity = "scanning memory patterns"
	first := stripANSI(m.memoryScanMark())
	m.pulse = 1
	second := stripANSI(m.memoryScanMark())
	if first != "⠋" || second != "⠙" || first == second {
		t.Fatalf("Braille loader did not advance: first=%q second=%q", first, second)
	}
	text := stripANSI(viewText(m))
	if !strings.Contains(text, "scanning memory patterns") {
		t.Fatalf("scan activity missing: %q", text)
	}
	if !strings.Contains(text, string([]rune(memoryScanFrames)[1])) {
		t.Fatalf("Braille scan marker missing: %q", text)
	}
	for _, marker := range []string{"⌕", "∘", "⊙"} {
		if strings.Contains(text, marker) {
			t.Fatalf("legacy scan marker %q still rendered: %q", marker, text)
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

func TestMemoryScheduleCreatesFileFromOneCompletedTurn(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{
		Directory: workdir,
		Model:     "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	userText := "Remember that plans should stay focused."
	user, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: user.ID, Type: "text", Text: &userText}); err != nil {
		t.Fatalf("insert user text: %v", err)
	}
	assistantText := "I will keep the plan focused."
	assistant, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: assistant.ID, Type: "text", Text: &assistantText}); err != nil {
		t.Fatalf("insert assistant text: %v", err)
	}
	finish := "stop"
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: assistant.ID, Type: "step-finish", FinishReason: &finish}); err != nil {
		t.Fatalf("insert assistant finish: %v", err)
	}
	content := `{"preferences":[{"text":"Keep plans focused","evidence":"The user explicitly requested focused plans.","source_message_ids":["` + user.ID + `"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`
	fake := newFakeProvider(t, 0, respBody(content, "stop", nil))
	cfg := settings.Default()
	cfg.Recap.Enabled = true
	cfg.Recap.Model = "deepseek-v4-flash"
	m := New(Options{
		Store:    store,
		Client:   opencode.NewClient("test-key", opencode.WithBaseURL(fake.srv.URL)),
		Workdir:  workdir,
		Session:  &session,
		Settings: &cfg,
	})
	cmd := m.scheduleMemoryUpdate()
	if cmd == nil {
		t.Fatal("scheduleMemoryUpdate returned nil")
	}
	_ = cmd()
	body, err := os.ReadFile(filepath.Join(workdir, "knowledge-base", "memories.md"))
	if err != nil {
		t.Fatalf("memories.md was not created: %v", err)
	}
	if !strings.Contains(string(body), "Keep plans focused") || !strings.Contains(string(body), user.ID) {
		t.Fatalf("memories.md = %q", body)
	}
}

func TestMemoryScheduleUsesCurrentModelWhenProviderDefaultIsEmpty(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{
		Directory: workdir,
		Provider:  "codex",
		Model:     "gpt-account-default",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	userText := "Remember that the memory worker must use the active model."
	user, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: user.ID, Type: "text", Text: &userText}); err != nil {
		t.Fatalf("insert user text: %v", err)
	}
	assistantText := "The memory worker will use the active model."
	assistant, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: assistant.ID, Type: "text", Text: &assistantText}); err != nil {
		t.Fatalf("insert assistant text: %v", err)
	}
	finish := "stop"
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: assistant.ID, Type: "step-finish", FinishReason: &finish}); err != nil {
		t.Fatalf("insert assistant finish: %v", err)
	}
	content := `{"preferences":[{"text":"Use the active model","evidence":"The test requires the current session model.","source_message_ids":["` + user.ID + `"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`
	fake := newFakeProvider(t, 0, respBody(content, "stop", nil))
	cfg := settings.Default()
	cfg.Provider.Active = "codex"
	cfg.Model.Default = ""
	cfg.Recap.Enabled = false
	cfg.Skills.Remember = true
	m := New(Options{
		Store:    store,
		Client:   opencode.NewClient("test-key", opencode.WithBaseURL(fake.srv.URL)),
		Workdir:  workdir,
		Session:  &session,
		Settings: &cfg,
	})
	cmd := m.scheduleMemoryUpdate()
	if cmd == nil {
		t.Fatal("scheduleMemoryUpdate returned nil")
	}
	if msg := cmd(); msg != nil {
		if done, ok := msg.(memoryDoneMsg); ok && done.err != nil {
			t.Fatalf("memory update: %v", done.err)
		}
	}
	body, err := os.ReadFile(filepath.Join(workdir, "knowledge-base", "memories.md"))
	if err != nil {
		t.Fatalf("memories.md was not created: %v", err)
	}
	if !strings.Contains(string(body), "Use the active model") {
		t.Fatalf("memories.md = %q", body)
	}
	updates, err := store.ListMemoryUpdatesForSession(context.Background(), workdir, session.ID)
	if err != nil {
		t.Fatalf("list memory updates: %v", err)
	}
	if len(updates) != 1 || updates[0].Model != session.Model {
		t.Fatalf("memory updates = %+v, want model %q", updates, session.Model)
	}
}

func TestMemoryRecoveryRetriesInsufficientContextFailure(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir, Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	userText := "Remember to keep the implementation focused."
	user, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: user.ID, Type: "text", Text: &userText}); err != nil {
		t.Fatalf("insert user text: %v", err)
	}
	assistantText := "The implementation will stay focused."
	assistant, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: assistant.ID, Type: "text", Text: &assistantText}); err != nil {
		t.Fatalf("insert assistant text: %v", err)
	}
	finish := "stop"
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: assistant.ID, Type: "step-finish", FinishReason: &finish}); err != nil {
		t.Fatalf("insert assistant finish: %v", err)
	}
	record, _, err := store.ReserveMemoryUpdate(context.Background(), db.MemoryUpdate{
		Workdir:            workdir,
		SourceSessionID:    session.ID,
		SourceEndSeq:       assistant.Seq,
		SourceEndMessageID: assistant.ID,
		Model:              "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("reserve memory update: %v", err)
	}
	if err := store.ClaimMemoryUpdate(context.Background(), record.ID); err != nil {
		t.Fatalf("claim memory update: %v", err)
	}
	if err := store.FailMemoryUpdate(context.Background(), record.ID, "recap: fewer than four complete messages"); err != nil {
		t.Fatalf("fail memory update: %v", err)
	}
	content := `{"preferences":[{"text":"Keep implementation focused","evidence":"The user explicitly requested focused implementation.","source_message_ids":["` + user.ID + `"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`
	fake := newFakeProvider(t, 0, respBody(content, "stop", nil))
	cfg := settings.Default()
	cfg.Recap.Enabled = true
	cfg.Recap.Model = "deepseek-v4-flash"
	m := New(Options{Store: store, Client: opencode.NewClient("test-key", opencode.WithBaseURL(fake.srv.URL)), Workdir: workdir, Session: &session, Settings: &cfg})
	_ = m.recoverMemoryUpdates()
	body, err := os.ReadFile(filepath.Join(workdir, "knowledge-base", "memories.md"))
	if err != nil {
		t.Fatalf("recovered memories.md: %v", err)
	}
	if !strings.Contains(string(body), "Keep implementation focused") {
		t.Fatalf("recovered memories.md = %q", body)
	}
	got, err := store.GetMemoryUpdate(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("get recovered update: %v", err)
	}
	if got.Status != db.MemoryUpdateStatusCompleted {
		t.Fatalf("recovered update = %+v", got)
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
	if firstCmd == nil || m.successfulRecapChats != 1 || m.memoryScanJobs != 1 {
		t.Fatalf("after first chat: cmd=%v count=%d memory_jobs=%d, want memory update and count 1", firstCmd != nil, m.successfulRecapChats, m.memoryScanJobs)
	}
	m.busy = true
	m.turnHasNewUser = true
	second, secondCmd := m.Update(eventDoneMsg{seq: 1})
	m = second.(Model)
	if secondCmd == nil || m.successfulRecapChats != 0 || m.memoryScanJobs != 2 {
		t.Fatalf("after second chat: cmd=%v count=%d memory_jobs=%d, want independent memory update and recap reset", secondCmd != nil, m.successfulRecapChats, m.memoryScanJobs)
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
