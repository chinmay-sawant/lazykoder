package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
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
