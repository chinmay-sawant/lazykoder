package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestMemoryHistoryFiltersSortsAndPaginatesCurrentSession(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	current, err := store.CreateSession(context.Background(), db.Session{Directory: workdir})
	if err != nil {
		t.Fatalf("create current session: %v", err)
	}
	other, err := store.CreateSession(context.Background(), db.Session{Directory: workdir})
	if err != nil {
		t.Fatalf("create other session: %v", err)
	}
	message, err := store.InsertMessage(context.Background(), db.Message{SessionID: current.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert current message: %v", err)
	}
	otherMessage, err := store.InsertMessage(context.Background(), db.Message{SessionID: other.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert other message: %v", err)
	}

	base := time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC)
	document := recap.NewMemoryDocument()
	for i := 0; i < 21; i++ {
		seen := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		document.Decisions = append(document.Decisions, recap.MemoryEntry{
			ID:               fmt.Sprintf("memory-%02d", i),
			State:            "active",
			Text:             fmt.Sprintf("memory-%02d", i),
			Evidence:         "current chat evidence",
			SourceMessageIDs: []string{message.ID},
			FirstSeenUTC:     seen,
			LastSeenUTC:      seen,
		})
	}
	document.Decisions = append(document.Decisions, recap.MemoryEntry{
		ID:               "other-chat",
		State:            "active",
		Text:             "other-chat",
		Evidence:         "other chat evidence",
		SourceMessageIDs: []string{otherMessage.ID},
		FirstSeenUTC:     base.Add(24 * time.Hour).Format(time.RFC3339),
		LastSeenUTC:      base.Add(24 * time.Hour).Format(time.RFC3339),
	})
	if _, err := recap.WriteMemoryDocument(context.Background(), workdir, document); err != nil {
		t.Fatalf("write memory document: %v", err)
	}

	m := New(Options{Store: store, Workdir: workdir, Session: &current})
	m.width, m.height = 100, 40
	m = m.openMemoryHistory()
	if !m.memoryHistoryMode {
		t.Fatal("/history did not open the memory drawer")
	}
	if len(m.memoryHistoryAll) != 21 {
		t.Fatalf("history entries = %d, want 21 current-chat entries", len(m.memoryHistoryAll))
	}
	if got := m.memoryHistoryAll[0].entry.Text; got != "memory-20" {
		t.Fatalf("newest entry = %q, want memory-20", got)
	}
	if len(m.memoryHistoryItems) != memoryHistoryPageSize {
		t.Fatalf("first page entries = %d, want %d", len(m.memoryHistoryItems), memoryHistoryPageSize)
	}
	if strings.Contains(m.memoryHistoryDrawerView(), "other-chat") {
		t.Fatal("history included a memory from another chat")
	}

	m = m.setMemoryHistoryPage(1)
	if len(m.memoryHistoryItems) != 1 || m.memoryHistoryItems[0].entry.Text != "memory-00" {
		t.Fatalf("second page = %+v, want only oldest current-chat entry", m.memoryHistoryItems)
	}
}

func TestMemoryHistoryMouseOpensAndClosesDetail(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	message, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	document := recap.NewMemoryDocument()
	document.Preferences = append(document.Preferences, recap.MemoryEntry{
		ID:               "memory-detail",
		State:            "active",
		Text:             "keep the history drawer useful",
		Evidence:         "the current chat requested a history view",
		SourceMessageIDs: []string{message.ID},
		FirstSeenUTC:     "2026-08-23T10:00:00Z",
		LastSeenUTC:      "2026-08-23T10:05:00Z",
	})
	if _, err := recap.WriteMemoryDocument(context.Background(), workdir, document); err != nil {
		t.Fatalf("write memory document: %v", err)
	}

	m := New(Options{Store: store, Workdir: workdir, Session: &session})
	m.width, m.height = 100, 30
	m = m.openMemoryHistory()
	top := m.subagentDrawerTop()
	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: top + 1, Button: tea.MouseLeft}))
	m = next.(Model)
	if !m.memoryHistoryDetailMode || !m.subagentLogMode {
		t.Fatalf("memory detail mode = %v, log mode = %v", m.memoryHistoryDetailMode, m.subagentLogMode)
	}
	if !strings.Contains(stripANSI(m.memoryHistoryDetailScreen()), "keep the history drawer useful") {
		t.Fatal("memory detail screen is missing the selected memory")
	}
	ansiDetail := m.memoryHistoryDetailScreen()
	for _, background := range []string{
		ansiBackground(theme.ColorEditPanel()),
		ansiBackground(theme.ColorAssistantPanel()),
	} {
		if !strings.Contains(ansiDetail, background) {
			t.Fatalf("memory detail missing panel background %q: %q", background, ansiDetail)
		}
	}

	next, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: m.width - 2, Y: 0, Button: tea.MouseLeft}))
	m = next.(Model)
	if m.memoryHistoryDetailMode {
		t.Fatal("close-button click did not leave memory detail")
	}

	m, _ = m.updateMemoryHistoryPickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.memoryHistoryMode {
		t.Fatal("escape did not close memory history")
	}
}

func TestMemoryHistoryDetailCtrlACopiesEvidence(t *testing.T) {
	m := newMemoryHistoryDetailFixture(t)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m = next.(Model)
	if cmd != nil {
		t.Fatalf("ctrl+a returned command %v", cmd)
	}

	next, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = next.(Model)
	if m.quitConfirm {
		t.Fatal("ctrl+c in memory history detail triggered quit confirmation")
	}
	if !copyCmdIncludes(cmd, "keep the history drawer useful") {
		t.Fatal("ctrl+c after ctrl+a did not copy memory history detail")
	}
}

func TestMemoryHistoryDetailDragCopiesEvidence(t *testing.T) {
	m := newMemoryHistoryDetailFixture(t)
	bodyTop := subagentLogHeaderRows
	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: bodyTop + 1, Button: tea.MouseLeft}))
	m = next.(Model)
	next, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: 40, Y: bodyTop + 14, Button: tea.MouseLeft}))
	m = next.(Model)
	next, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 40, Y: bodyTop + 14, Button: tea.MouseLeft}))
	m = next.(Model)
	if !copyCmdIncludes(cmd, "keep the history drawer useful") {
		t.Fatal("drag selection did not copy memory history detail")
	}
}

func TestMemoryHistoryShowsDocumentErrorInHistoryView(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workdir, "knowledge-base"), 0o755); err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	malformed := `---
format_version: 2
updated_at_utc: "2026-08-23T10:00:00Z"
last_session_id: ""
last_message_id: ""
last_message_seq: 0
---
# Project memory
## User preferences

- id: "bad-memory"
  state: "active"
  text: "invalid source anchor"
  evidence: "test"
  source_message_ids: []
  first_seen_utc: "2026-08-23T10:00:00Z"
  last_seen_utc: "2026-08-23T10:00:00Z"
`
	if err := os.WriteFile(filepath.Join(workdir, "knowledge-base", "memories.md"), []byte(malformed), 0o644); err != nil {
		t.Fatalf("write malformed memory document: %v", err)
	}

	m := New(Options{Store: store, Workdir: workdir, Session: &session})
	m.width, m.height = 100, 30
	m = m.openMemoryHistory()
	if !strings.Contains(stripANSI(m.memoryHistoryDrawerView()), "needs one to 8 source message IDs") {
		t.Fatalf("memory history view does not show the document error: %q", stripANSI(m.memoryHistoryDrawerView()))
	}
}

func newMemoryHistoryDetailFixture(t *testing.T) Model {
	t.Helper()
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	message, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	document := recap.NewMemoryDocument()
	document.Preferences = append(document.Preferences, recap.MemoryEntry{
		ID:               "memory-detail-copy",
		State:            "active",
		Text:             "keep the history drawer useful",
		Evidence:         "the current chat requested a history view",
		SourceMessageIDs: []string{message.ID},
		FirstSeenUTC:     "2026-08-23T10:00:00Z",
		LastSeenUTC:      "2026-08-23T10:05:00Z",
	})
	if _, err := recap.WriteMemoryDocument(context.Background(), workdir, document); err != nil {
		t.Fatalf("write memory document: %v", err)
	}

	m := New(Options{Store: store, Workdir: workdir, Session: &session})
	m.width, m.height = 100, 30
	m = m.openMemoryHistory()
	next, _ := m.openSelectedMemoryHistoryDetail()
	return next
}

func copyCmdIncludes(cmd tea.Cmd, want string) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if strings.Contains(fmt.Sprint(sub()), want) {
				return true
			}
		}
		return false
	}
	return strings.Contains(fmt.Sprint(msg), want)
}

func TestMemoryHistoryShowsMemoryUpdateOutcomes(t *testing.T) {
	store := newTestStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	first, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert first message: %v", err)
	}
	second, err := store.InsertMessage(context.Background(), db.Message{SessionID: session.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert second message: %v", err)
	}
	document := recap.NewMemoryDocument()
	document.Decisions = append(document.Decisions, recap.MemoryEntry{
		ID:               "completed-memory",
		State:            "active",
		Text:             "the completed memory",
		Evidence:         "evidence",
		SourceMessageIDs: []string{first.ID},
		FirstSeenUTC:     "2026-08-23T10:00:00Z",
		LastSeenUTC:      "2026-08-23T10:00:00Z",
	})
	if _, err := recap.WriteMemoryDocument(context.Background(), workdir, document); err != nil {
		t.Fatalf("write memory document: %v", err)
	}
	completed, _, err := store.ReserveMemoryUpdate(context.Background(), db.MemoryUpdate{
		Workdir:            workdir,
		SourceSessionID:    session.ID,
		SourceEndSeq:       first.Seq,
		SourceEndMessageID: first.ID,
		Model:              "memory-model",
	})
	if err != nil {
		t.Fatalf("reserve completed update: %v", err)
	}
	if err := store.ClaimMemoryUpdate(context.Background(), completed.ID); err != nil {
		t.Fatalf("claim completed update: %v", err)
	}
	if err := store.CompleteMemoryUpdate(context.Background(), completed.ID, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("complete memory update: %v", err)
	}
	failed, _, err := store.ReserveMemoryUpdate(context.Background(), db.MemoryUpdate{
		Workdir:            workdir,
		SourceSessionID:    session.ID,
		SourceEndSeq:       second.Seq,
		SourceEndMessageID: second.ID,
		Model:              "memory-model",
	})
	if err != nil {
		t.Fatalf("reserve failed update: %v", err)
	}
	if err := store.ClaimMemoryUpdate(context.Background(), failed.ID); err != nil {
		t.Fatalf("claim failed update: %v", err)
	}
	if err := store.FailMemoryUpdate(context.Background(), failed.ID, "provider call: 503 service unavailable"); err != nil {
		t.Fatalf("fail memory update: %v", err)
	}

	m := New(Options{Store: store, Workdir: workdir, Session: &session})
	m.width, m.height = 100, 30
	m = m.openMemoryHistory()
	if len(m.memoryHistoryAll) != 2 {
		t.Fatalf("history entries = %d, want completed memory plus failed update", len(m.memoryHistoryAll))
	}
	statuses := make(map[string]bool, len(m.memoryHistoryAll))
	for _, item := range m.memoryHistoryAll {
		statuses[item.update.Status] = true
	}
	if !statuses[db.MemoryUpdateStatusFailed] || !statuses[db.MemoryUpdateStatusCompleted] {
		t.Fatalf("history statuses = %+v, want completed and failed", statuses)
	}
	plain := stripANSI(m.memoryHistoryDrawerContent(80))
	for _, want := range []string{"failed", "ok"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("history missing %q: %q", want, plain)
		}
	}
	failedTextVisible := false
	for _, item := range m.memoryHistoryAll {
		if strings.Contains(memoryHistoryText(item), "provider call: 503") {
			failedTextVisible = true
			break
		}
	}
	if !failedTextVisible {
		t.Fatal("failed history item is missing its recorded error")
	}
	for _, want := range []string{
		lipgloss.NewStyle().Foreground(theme.ColorDanger()).Render("●"),
		lipgloss.NewStyle().Foreground(theme.ColorGood()).Render("●"),
	} {
		if !strings.Contains(m.memoryHistoryDrawerContent(80), want) {
			t.Fatalf("history missing colored status mark %q", want)
		}
	}
}

func TestMemoryUpdateFailureKeepsCauseVisible(t *testing.T) {
	m := New(Options{Workdir: t.TempDir()})
	m.memoryScanJobs = 1
	next, cmd := m.Update(memoryDoneMsg{err: errors.New("provider call: 503 service unavailable")})
	m = next.(Model)
	if cmd != nil {
		t.Fatalf("memory failure returned command %v, want none", cmd)
	}
	if m.memoryScanJobs != 0 {
		t.Fatalf("memory jobs = %d, want 0", m.memoryScanJobs)
	}
	if m.copyNotice != "" {
		t.Fatalf("memory failure used transient copy notice %q", m.copyNotice)
	}
	if !strings.Contains(m.err, "memory update failed: provider call: 503 service unavailable") {
		t.Fatalf("memory error = %q, want original cause", m.err)
	}
	if !strings.Contains(stripANSI(viewText(m)), "503 service unavailable") {
		t.Fatal("memory failure cause is not rendered")
	}
}
