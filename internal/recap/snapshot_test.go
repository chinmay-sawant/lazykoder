package recap

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestBuildSnapshotUsesNewestFiveFromOneHour(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_snapshot"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(2_000_000)
	for i := 6; i >= 1; i-- {
		addRecapMessage(t, store, sess.ID, "user", "message", now.Add(-time.Duration(10*i)*time.Minute), true)
	}

	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snapshot.Messages) != 5 {
		t.Fatalf("messages = %d, want 5", len(snapshot.Messages))
	}
	if snapshot.Messages[0].Seq != 6 || snapshot.Messages[4].Seq != 2 {
		t.Fatalf("seqs = %v, want newest-first 6..2", snapshotSeqs(snapshot))
	}
	if snapshot.SourceStartSeq != 2 || snapshot.SourceEndSeq != 6 || snapshot.SourceEndMessageID == "" {
		t.Fatalf("source range = %+v", snapshot)
	}
}

func TestBuildSnapshotExtendsToTwoHoursForFourEntries(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_two_hours"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(3_000_000)
	for i := 4; i >= 1; i-- {
		addRecapMessage(t, store, sess.ID, "user", "old message", now.Add(-time.Duration(65+i)*time.Minute), true)
	}
	addRecapMessage(t, store, sess.ID, "user", "too old", now.Add(-3*time.Hour), true)

	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snapshot.Messages) != 4 || snapshot.Messages[0].Seq != 4 {
		t.Fatalf("snapshot = %+v, want four two-hour entries newest-first", snapshot)
	}
}

func TestBuildSnapshotRequiresFourAndExcludesCompactionAndIncomplete(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_minimum"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(4_000_000)
	addRecapMessage(t, store, sess.ID, "user", "one", now.Add(-time.Minute), true)
	addRecapMessage(t, store, sess.ID, "assistant", "incomplete", now.Add(-2*time.Minute), false)
	addRecapMessageWithAgent(t, store, sess.ID, "user", "compact", "compaction", now.Add(-3*time.Minute), true)
	addRecapMessage(t, store, sess.ID, "user", "two", now.Add(-4*time.Minute), true)
	addRecapMessage(t, store, sess.ID, "user", "three", now.Add(-5*time.Minute), true)
	addRecapMessage(t, store, sess.ID, "user", "four", now.Add(-6*time.Minute), true)

	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snapshot.Messages) != 4 {
		t.Fatalf("messages = %d, want 4, got %v", len(snapshot.Messages), snapshotSeqs(snapshot))
	}
	for _, message := range snapshot.Messages {
		if message.Text == "incomplete" || message.Text == "compact" {
			t.Fatalf("excluded message leaked into snapshot: %+v", message)
		}
	}

	store2 := openRecapStore(t)
	sess2, err := store2.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_insufficient"})
	if err != nil {
		t.Fatalf("CreateSession insufficient: %v", err)
	}
	for i := 0; i < 3; i++ {
		addRecapMessage(t, store2, sess2.ID, "user", "few", now.Add(-time.Duration(i+1)*time.Minute), true)
	}
	_, err = BuildSnapshot(context.Background(), store2, sess2.ID, SnapshotOptions{Now: now})
	if !errors.Is(err, ErrInsufficientMessages) {
		t.Fatalf("error = %v, want ErrInsufficientMessages", err)
	}
}

func TestBuildSnapshotSupportsSmallerMemoryWindow(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_memory_window"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(4_500_000)
	addRecapMessage(t, store, sess.ID, "user", "Remember this preference.", now.Add(-time.Minute), true)
	addRecapMessage(t, store, sess.ID, "assistant", "I will keep it in project memory.", now, true)

	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{
		Now:                 now,
		MinimumMessageCount: memoryMinimumMessageCount,
		MessageLimit:        defaultMessageLimit,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot memory window: %v", err)
	}
	if len(snapshot.Messages) != 2 || snapshot.SourceEndSeq != 2 {
		t.Fatalf("memory snapshot = %+v, want two newest complete messages", snapshot)
	}
}

func TestBuildSnapshotTieBreaksBySequence(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_ties"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(5_000_000)
	for i := 0; i < 4; i++ {
		addRecapMessage(t, store, sess.ID, "user", "same time", now.Add(-time.Minute), true)
	}
	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if got := snapshotSeqs(snapshot); got[0] != 4 || got[3] != 1 {
		t.Fatalf("seqs = %v, want 4,3,2,1", got)
	}
}

func TestBuildSnapshotCanRebuildAnOlderAnchoredWindow(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_anchor"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(5_500_000)
	var anchorID string
	for i := 6; i >= 1; i-- {
		message := addRecapMessage(t, store, sess.ID, "user", "anchored", now.Add(-time.Duration(i)*time.Minute), true)
		if i == 3 {
			anchorID = message.ID
		}
	}
	addRecapMessage(t, store, sess.ID, "user", "newer", now, true)

	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{AnchorMessageID: anchorID})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snapshot.SourceEndMessageID != anchorID || snapshot.SourceEndSeq == 0 {
		t.Fatalf("source end = %+v, want anchor %q", snapshot, anchorID)
	}
	for _, message := range snapshot.Messages {
		if message.Text == "newer" {
			t.Fatalf("newer message leaked into anchored snapshot: %+v", snapshot)
		}
	}
}

func TestBuildSnapshotIncludesOnlyBoundedTerminalToolFacts(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_tool_facts"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(6_000_000)
	for i := 4; i >= 1; i-- {
		message := addRecapMessage(t, store, sess.ID, "user", "context", now.Add(-time.Duration(i)*time.Minute), true)
		if i != 4 {
			continue
		}
		toolID := "tool_call_1"
		toolName := "bash"
		status := "completed"
		part, err := store.InsertPart(context.Background(), db.Part{
			MessageID:  message.ID,
			Type:       "tool",
			ToolName:   &toolName,
			ToolCallID: &toolID,
			ToolStatus: &status,
		})
		if err != nil {
			t.Fatalf("InsertPart tool: %v", err)
		}
		output := strings.Repeat("x", maxToolFactOutput+100)
		if err := store.InsertToolCall(context.Background(), db.ToolCall{
			PartID: part.ID,
			Tool:   toolName,
			CallID: toolID,
			Status: status,
			Output: &output,
		}); err != nil {
			t.Fatalf("InsertToolCall: %v", err)
		}
	}
	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	var facts []ToolFact
	for _, message := range snapshot.Messages {
		if len(message.ToolFacts) > 0 {
			facts = message.ToolFacts
			break
		}
	}
	if len(facts) != 1 {
		t.Fatalf("tool facts = %+v, want one fact", facts)
	}
	if len([]rune(facts[0].Output)) != maxToolFactOutput {
		t.Fatalf("tool output length = %d, want %d", len([]rune(facts[0].Output)), maxToolFactOutput)
	}
}

func openRecapStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "recap.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func addRecapMessage(t *testing.T, store *db.Store, sessionID, role, text string, created time.Time, complete bool) db.Message {
	return addRecapMessageWithAgent(t, store, sessionID, role, text, "", created, complete)
}

func addRecapMessageWithAgent(t *testing.T, store *db.Store, sessionID, role, text, agent string, created time.Time, complete bool) db.Message {
	t.Helper()
	message, err := store.InsertMessage(context.Background(), db.Message{
		SessionID:   sessionID,
		Role:        role,
		Agent:       agent,
		TimeCreated: created.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	if _, err := store.InsertPart(context.Background(), db.Part{MessageID: message.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("InsertPart text: %v", err)
	}
	if role == "assistant" && complete {
		reason := "stop"
		if _, err := store.InsertPart(context.Background(), db.Part{MessageID: message.ID, Type: "step-finish", FinishReason: &reason}); err != nil {
			t.Fatalf("InsertPart finish: %v", err)
		}
	}
	return message
}

func snapshotSeqs(snapshot Snapshot) []int {
	out := make([]int, 0, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		out = append(out, message.Seq)
	}
	return out
}
