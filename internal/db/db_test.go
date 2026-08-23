package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
WHERE type = 'table' AND name IN ('sessions', 'messages', 'parts', 'tool_calls', 'subagent_jobs', 'todos', 'recap_records', 'memory_updates', 'schema_migrations')`).Scan(&n); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if n != 9 {
		t.Fatalf("got %d tables, want 9", n)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if n != schemaVersion {
		t.Fatalf("got %d schema_migrations rows, want %d", n, schemaVersion)
	}
}

func TestSessionActivityMigrationBackfillsZeroValues(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/work", TimeCreated: 10, TimeUpdated: 20})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET time_active = 0 WHERE id = ?`, sess.ID); err != nil {
		t.Fatalf("zero time_active: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, migrationSessionActive); err != nil {
		t.Fatalf("rewind migration: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.TimeActive != got.TimeUpdated {
		t.Fatalf("time_active = %d, want backfilled time_updated %d", got.TimeActive, got.TimeUpdated)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestRecapRecordLifecycleAndOrdering(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, Session{Directory: "/work", Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user", TimeCreated: 100})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant", TimeCreated: 200})
	if err != nil {
		t.Fatal(err)
	}
	want := RecapRecord{
		SessionID:          sess.ID,
		SourceStartSeq:     first.Seq,
		SourceEndSeq:       second.Seq,
		SourceStartTime:    first.TimeCreated,
		SourceEndTime:      second.TimeCreated,
		SourceEndMessageID: second.ID,
		Model:              "deepseek-v4-flash",
		Artifacts:          RecapArtifacts{},
	}
	reserved, created, err := s.ReserveRecap(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if !created || reserved.ID == "" || reserved.Status != RecapStatusQueued || reserved.Attempts != 0 {
		t.Fatalf("reserved = %+v created=%t", reserved, created)
	}
	duplicate, created, err := s.ReserveRecap(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if created || duplicate.ID != reserved.ID {
		t.Fatalf("duplicate = %+v created=%t", duplicate, created)
	}
	open, err := s.ListOpenRecaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != reserved.ID {
		t.Fatalf("open recaps = %+v", open)
	}
	after, err := s.ListRecapsAfter(ctx, sess.ID, first.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].SourceEndSeq != second.Seq {
		t.Fatalf("recaps after = %+v", after)
	}
	newest, err := s.ListRecaps(ctx, sess.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(newest) != 1 || newest[0].ID != reserved.ID {
		t.Fatalf("newest recaps = %+v", newest)
	}
	if err := s.ClaimRecap(ctx, reserved.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteRecap(ctx, reserved.ID, RecapArtifacts{
		Sessions: RecapArtifact{Path: "sessions/000002-msg.md", SHA256: strings.Repeat("a", 64)},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRecap(ctx, reserved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RecapStatusCompleted || got.TimeStarted == nil || got.TimeFinished == nil {
		t.Fatalf("completed = %+v", got)
	}
	if got.Artifacts.Sessions.Path != "sessions/000002-msg.md" {
		t.Fatalf("artifacts = %+v", got.Artifacts)
	}
}

func TestRecapRecordStatusTransitionsAndValidation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	base := RecapRecord{SessionID: sess.ID, SourceStartSeq: msg.Seq, SourceEndSeq: msg.Seq,
		SourceStartTime: msg.TimeCreated, SourceEndTime: msg.TimeCreated, SourceEndMessageID: msg.ID, Model: "m"}
	queued, _, err := s.ReserveRecap(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailRecap(ctx, queued.ID, "worker failed"); err != nil {
		t.Fatal(err)
	}
	failed, err := s.GetRecap(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != RecapStatusFailed || failed.Error != "worker failed" || failed.Attempts != 1 {
		t.Fatalf("failed = %+v", failed)
	}
	if err := s.RequeueRecap(ctx, queued.ID); err != nil {
		t.Fatalf("RequeueRecap: %v", err)
	}
	requeued, err := s.GetRecap(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.Status != RecapStatusQueued || requeued.Error != "" || requeued.TimeStarted != nil || requeued.TimeFinished != nil {
		t.Fatalf("requeued = %+v", requeued)
	}
	cancelQueued, _, err := s.ReserveRecap(ctx, RecapRecord{
		SessionID:          sess.ID,
		SourceEndMessageID: "cancel-me",
		Model:              "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelRecap(ctx, cancelQueued.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.GetRecap(ctx, cancelQueued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != RecapStatusCancelled {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	if _, _, err := s.ReserveRecap(ctx, RecapRecord{SessionID: sess.ID, SourceEndMessageID: "", Model: "m"}); err == nil {
		t.Fatal("empty source message id should fail")
	}
	if _, _, err := s.ReserveRecap(ctx, RecapRecord{SessionID: sess.ID, SourceEndMessageID: "new", Model: "m", Artifacts: RecapArtifacts{Sessions: RecapArtifact{Path: "../../bad"}}}); err == nil {
		t.Fatal("invalid artifact path should fail")
	}
}

func TestPersistAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("reopen Migrate: %v", err)
	}
	defer func() { _ = s.Close() }()

	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !strings.HasPrefix(sess.ID, "ses_") {
		t.Fatalf("session id %q does not start with ses_", sess.ID)
	}
	if sess.Provider != "opencode-go" || sess.Model != "" || sess.Status != "active" {
		t.Fatalf("defaults not applied: %+v", sess)
	}
	if sess.TimeCreated == 0 || sess.TimeUpdated == 0 || sess.TimeActive == 0 {
		t.Fatalf("timestamps not filled: %+v", sess)
	}

	text := "hello"
	user, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage user: %v", err)
	}
	if !strings.HasPrefix(user.ID, "msg_") || user.TimeCreated == 0 || user.Seq != 1 {
		t.Fatalf("user message not filled: %+v", user)
	}
	if _, err := s.InsertPart(ctx, Part{MessageID: user.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("InsertPart user: %v", err)
	}
	assistant, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("InsertMessage assistant: %v", err)
	}
	if _, err := s.InsertPart(ctx, Part{MessageID: assistant.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("InsertPart assistant: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s.Close() }()

	messages, err := s.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("wrong roles: %+v", messages)
	}
	if !messages[0].Visible || !messages[1].Visible {
		t.Fatalf("messages should be visible by default: %+v", messages)
	}
	total := 0
	for _, m := range messages {
		parts, err := s.ListParts(ctx, m.ID)
		if err != nil {
			t.Fatalf("ListParts: %v", err)
		}
		total += len(parts)
		if len(parts) != 1 || parts[0].Type != "text" {
			t.Fatalf("unexpected parts: %+v", parts)
		}
	}
	if total != 2 {
		t.Fatalf("got %d parts, want 2", total)
	}

	sessions, err := s.ListSessionsByDir(ctx, "/work")
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != sess.ID {
		t.Fatalf("session not listed: %+v", sessions)
	}
}

func TestMessageVisibility(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	if err := s.SetMessageVisibility(ctx, msg.ID, false); err != nil {
		t.Fatalf("hide message: %v", err)
	}
	messages, err := s.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Visible {
		t.Fatalf("message visibility after hide = %+v", messages)
	}
	if err := s.SetMessageVisibility(ctx, msg.ID, true); err != nil {
		t.Fatalf("restore message: %v", err)
	}
	messages, err = s.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages after restore: %v", err)
	}
	if len(messages) != 1 || !messages[0].Visible {
		t.Fatalf("message visibility after restore = %+v", messages)
	}
}

func TestDeleteSessionCascades(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	text := "hi"
	if _, err := s.InsertPart(ctx, Part{MessageID: msg.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("InsertPart: %v", err)
	}

	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	messages, err := s.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages after delete: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("got %d messages after delete, want 0", len(messages))
	}
	parts, err := s.ListParts(ctx, msg.ID)
	if err != nil {
		t.Fatalf("ListParts after delete: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("got %d parts after delete, want 0", len(parts))
	}
}

func TestSeqIncrements(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	first, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage 1: %v", err)
	}
	second, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("InsertMessage 2: %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("got seq %d, %d; want 1, 2", first.Seq, second.Seq)
	}

	other, err := s.CreateSession(ctx, Session{Directory: "/elsewhere"})
	if err != nil {
		t.Fatalf("CreateSession other: %v", err)
	}
	firstOther, err := s.InsertMessage(ctx, Message{SessionID: other.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage other: %v", err)
	}
	if firstOther.Seq != 1 {
		t.Fatalf("other session seq %d, want 1", firstOther.Seq)
	}

	p1, err := s.InsertPart(ctx, Part{MessageID: first.ID, Type: "step-start"})
	if err != nil {
		t.Fatalf("InsertPart 1: %v", err)
	}
	p2, err := s.InsertPart(ctx, Part{MessageID: first.ID, Type: "text"})
	if err != nil {
		t.Fatalf("InsertPart 2: %v", err)
	}
	if p1.Seq != 1 || p2.Seq != 2 {
		t.Fatalf("got part seq %d, %d; want 1, 2", p1.Seq, p2.Seq)
	}
}

func TestUpdatePartText(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	start := "th"
	part, err := s.InsertPart(ctx, Part{MessageID: msg.ID, Type: "reasoning", Text: &start})
	if err != nil {
		t.Fatalf("InsertPart: %v", err)
	}
	if err := s.UpdatePartText(ctx, part.ID, "think"); err != nil {
		t.Fatalf("UpdatePartText: %v", err)
	}
	parts, err := s.ListParts(ctx, msg.ID)
	if err != nil {
		t.Fatalf("ListParts: %v", err)
	}
	if len(parts) != 1 || parts[0].Text == nil || *parts[0].Text != "think" {
		t.Fatalf("parts = %+v, want reasoning text think", parts)
	}
}

func TestUpdateToolCall(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	part, err := s.InsertPart(ctx, Part{MessageID: msg.ID, Type: "tool", ToolName: strPtr("bash"), ToolStatus: strPtr("pending")})
	if err != nil {
		t.Fatalf("InsertPart: %v", err)
	}

	tc := ToolCall{
		PartID:    part.ID,
		Tool:      "bash",
		CallID:    "call_1",
		Status:    "pending",
		InputJSON: `{"command":"ls"}`,
	}
	if err := s.UpdateToolCall(ctx, tc); err != nil {
		t.Fatalf("UpdateToolCall insert: %v", err)
	}

	parts, err := s.ListParts(ctx, msg.ID)
	if err != nil {
		t.Fatalf("ListParts: %v", err)
	}
	if parts[0].ToolStatus == nil || *parts[0].ToolStatus != "pending" {
		t.Fatalf("part tool_status after insert: %+v", parts[0].ToolStatus)
	}

	exitCode := 2
	output := "denied"
	tc.Status = "denied"
	tc.Output = &output
	tc.ExitCode = &exitCode
	if err := s.UpdateToolCall(ctx, tc); err != nil {
		t.Fatalf("UpdateToolCall update: %v", err)
	}

	parts, err = s.ListParts(ctx, msg.ID)
	if err != nil {
		t.Fatalf("ListParts: %v", err)
	}
	if parts[0].ToolStatus == nil || *parts[0].ToolStatus != "denied" {
		t.Fatalf("part tool_status not updated: %+v", parts[0].ToolStatus)
	}

	var gotStatus, gotCallID, gotOutput string
	var gotExit sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT status, call_id, output, exit_code FROM tool_calls WHERE part_id = ?`, part.ID).
		Scan(&gotStatus, &gotCallID, &gotOutput, &gotExit); err != nil {
		t.Fatalf("query tool_calls: %v", err)
	}
	if gotStatus != "denied" || gotCallID != "call_1" || gotOutput != "denied" || !gotExit.Valid || gotExit.Int64 != 2 {
		t.Fatalf("tool_calls row wrong: status=%q call_id=%q output=%q exit=%v", gotStatus, gotCallID, gotOutput, gotExit)
	}
}

func TestListSessionsByDirOrder(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	base := int64(1_000)
	for i := 0; i < 3; i++ {
		_, err := s.CreateSession(ctx, Session{Directory: "/b", TimeCreated: base + int64(i), TimeUpdated: base + int64(i)})
		if err != nil {
			t.Fatalf("CreateSession %d: %v", i, err)
		}
	}
	// Sessions in /a with explicit timestamps so ordering is deterministic,
	// independent of the wall clock.
	first, err := s.CreateSession(ctx, Session{Directory: "/a", TimeCreated: 5, TimeUpdated: 5})
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	second, err := s.CreateSession(ctx, Session{Directory: "/a", TimeCreated: 9, TimeUpdated: 9})
	if err != nil {
		t.Fatalf("CreateSession second: %v", err)
	}

	sessions, err := s.ListSessionsByDir(ctx, "/a")
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].ID != second.ID || sessions[1].ID != first.ID {
		t.Fatalf("not ordered by time_active DESC: %+v", sessions)
	}
}

func TestListToolCalls(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	sess, err := s.CreateSession(ctx, Session{Directory: "/a"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	status := "pending"
	part, err := s.InsertPart(ctx, Part{MessageID: msg.ID, Type: "tool", ToolName: strPtr("bash"), ToolCallID: strPtr("call_1"), ToolStatus: &status})
	if err != nil {
		t.Fatalf("InsertPart: %v", err)
	}
	tc := ToolCall{PartID: part.ID, Tool: "bash", CallID: "call_1", Status: "pending", InputJSON: `{"command":"ls"}`}
	if err := s.InsertToolCall(ctx, tc); err != nil {
		t.Fatalf("InsertToolCall: %v", err)
	}
	out := "hi"
	code := 0
	now := int64(42)
	tc.Status = "completed"
	tc.Output = &out
	tc.ExitCode = &code
	tc.TimeStart = &now
	tc.TimeEnd = &now
	if err := s.UpdateToolCall(ctx, tc); err != nil {
		t.Fatalf("UpdateToolCall: %v", err)
	}

	calls, err := s.ListToolCalls(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	got := calls[0]
	if got.PartID != part.ID || got.Status != "completed" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("unexpected tool call: %+v", got)
	}
	if got.Output == nil || *got.Output != "hi" {
		t.Fatalf("output not round-tripped: %+v", got)
	}
	if got.InputJSON != `{"command":"ls"}` {
		t.Fatalf("input_json not preserved: %q", got.InputJSON)
	}
}

func TestNewID(t *testing.T) {
	id := NewID("prt_")
	if len(id) != 4+16 {
		t.Fatalf("got length %d, want 20", len(id))
	}
	if !strings.HasPrefix(id, "prt_") {
		t.Fatalf("missing prefix: %q", id)
	}
	for _, r := range id[4:] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("non-hex char %q in %q", r, id)
		}
	}
	a, b := NewID("a"), NewID("a")
	if a == b {
		t.Fatal("NewID returned duplicate values")
	}
}

func strPtr(s string) *string { return &s }

func TestUpdateSessionModel(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/a"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// time_updated is Unix ms; sleep so the bump is strictly greater.
	time.Sleep(2 * time.Millisecond)
	if err := s.UpdateSessionModel(ctx, sess.ID, "gpt-9"); err != nil {
		t.Fatalf("UpdateSessionModel: %v", err)
	}
	sessions, err := s.ListSessionsByDir(ctx, "/a")
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Model != "gpt-9" {
		t.Fatalf("model not updated: %+v", sessions)
	}
	if sessions[0].TimeUpdated <= sess.TimeUpdated {
		t.Errorf("time_updated not bumped: %d <= %d", sessions[0].TimeUpdated, sess.TimeUpdated)
	}
}

func TestInsertMessageBumpsSessionTimeUpdated(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	// Fixed created/updated so a later message cannot match by chance.
	sess, err := s.CreateSession(ctx, Session{
		Directory:   "/a",
		TimeCreated: 1_000,
		TimeUpdated: 1_000,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.TimeCreated != 1_000 || sess.TimeUpdated != 1_000 {
		t.Fatalf("session timestamps not preserved: %+v", sess)
	}
	time.Sleep(2 * time.Millisecond)
	msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	if msg.TimeCreated == 0 {
		t.Fatal("message time_created not filled")
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.TimeCreated != 1_000 {
		t.Errorf("time_created changed: %d", got.TimeCreated)
	}
	if got.TimeUpdated < msg.TimeCreated {
		t.Errorf("time_updated = %d, want >= message time %d", got.TimeUpdated, msg.TimeCreated)
	}
	if got.TimeUpdated <= sess.TimeUpdated {
		t.Errorf("time_updated not bumped: %d <= %d", got.TimeUpdated, sess.TimeUpdated)
	}
	if got.TimeActive < msg.TimeCreated {
		t.Errorf("time_active = %d, want >= message time %d", got.TimeActive, msg.TimeCreated)
	}
}

func TestTouchSessionBumpsTimeUpdated(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/a", TimeCreated: 50, TimeUpdated: 50})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.TouchSession(ctx, sess.ID); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.TimeCreated != 50 {
		t.Errorf("time_created changed: %d", got.TimeCreated)
	}
	if got.TimeUpdated <= 50 {
		t.Errorf("time_updated not bumped: %d", got.TimeUpdated)
	}
	if got.TimeActive != 50 {
		t.Errorf("time_active changed during background touch: %d", got.TimeActive)
	}
}

func TestListSessionsByDirIgnoresBackgroundActivityForOrdering(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	active, err := s.CreateSession(ctx, Session{
		Title: "active-chat", Directory: "/a", TimeCreated: 1_000, TimeUpdated: 1_000,
	})
	if err != nil {
		t.Fatalf("CreateSession active: %v", err)
	}
	background, err := s.CreateSession(ctx, Session{
		Title: "background-chat", Directory: "/a", TimeCreated: 2_000, TimeUpdated: 2_000,
	})
	if err != nil {
		t.Fatalf("CreateSession background: %v", err)
	}
	if _, err := s.InsertMessage(ctx, Message{
		SessionID: active.ID, Role: "user", TimeCreated: 3_000,
	}); err != nil {
		t.Fatalf("InsertMessage active: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.TouchSession(ctx, background.ID); err != nil {
		t.Fatalf("TouchSession background: %v", err)
	}

	sessions, err := s.ListSessionsByDir(ctx, "/a")
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].ID != active.ID {
		t.Fatalf("background activity reordered history: first=%q, want active %q", sessions[0].ID, active.ID)
	}
}

func TestUpdateSessionVariant(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/a"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.UpdateSessionVariant(ctx, sess.ID, "high"); err != nil {
		t.Fatalf("UpdateSessionVariant: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Variant == nil || *got.Variant != "high" {
		t.Fatalf("variant = %v, want high", got.Variant)
	}
	if err := s.UpdateSessionVariant(ctx, sess.ID, ""); err != nil {
		t.Fatalf("clear variant: %v", err)
	}
	got, err = s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession after clear: %v", err)
	}
	if got.Variant != nil {
		t.Fatalf("variant = %v, want nil", got.Variant)
	}
}

func TestUpdateSessionSegmentsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/a"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.UpdateSessionSegments(ctx, sess.ID, []string{"tps", "model", "tps", "unknown"}); err != nil {
		t.Fatalf("UpdateSessionSegments: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	want := []string{"tps", "model"}
	if strings.Join(got.StatusSegments, ",") != strings.Join(want, ",") {
		t.Fatalf("segments = %v, want %v", got.StatusSegments, want)
	}
}

func TestLegacyStatusSegmentsExpandOnMigration(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/legacy-status"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	legacy := `["model","tps","tokens","cost","scroll","models","prompt"]`
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET status_segments = ? WHERE id = ?`, legacy, sess.ID); err != nil {
		t.Fatalf("seed legacy segments: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, migrationStatusV2); err != nil {
		t.Fatalf("rewind migration: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, migrationRecaps); err != nil {
		t.Fatalf("rewind recap migration: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, migrationMemories); err != nil {
		t.Fatalf("rewind memory migration: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, migrationMemoryTimings); err != nil {
		t.Fatalf("rewind memory timing migration: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, migrationSessionActive); err != nil {
		t.Fatalf("rewind session activity migration: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	for _, name := range []string{"model", "variant", "tokens", "cache", "cost", "tps", "subs", "scroll", "models", "prompt"} {
		if !containsString(got.StatusSegments, name) {
			t.Fatalf("migrated segments %v missing %q", got.StatusSegments, name)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCreateSessionListedByProjectRoot(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	project := "/home/user/proj"
	sess, err := s.CreateSession(ctx, Session{Title: "root", Directory: project})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.ListSessionsByDir(ctx, project)
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("listed %+v, want session %s", got, sess.ID)
	}
	hidden, err := s.ListSessionsByDir(ctx, project+"/.lazykoder")
	if err != nil {
		t.Fatalf("ListSessionsByDir lazykoder: %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("project/.lazykoder listed %d sessions, want 0", len(hidden))
	}
	loaded, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if loaded.Directory != project {
		t.Errorf("GetSession directory = %q, want %q", loaded.Directory, project)
	}
}

func TestRepairSessionDirectories(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	project := "/home/user/proj"
	old := project + "/.lazykoder"
	if _, err := s.CreateSession(ctx, Session{Title: "old", Directory: old}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	before, err := s.ListSessionsByDir(ctx, project)
	if err != nil {
		t.Fatalf("ListSessionsByDir before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("project listed %d sessions before repair, want 0", len(before))
	}
	if err := s.RepairSessionDirectories(ctx); err != nil {
		t.Fatalf("RepairSessionDirectories: %v", err)
	}
	after, err := s.ListSessionsByDir(ctx, project)
	if err != nil {
		t.Fatalf("ListSessionsByDir after: %v", err)
	}
	if len(after) != 1 || after[0].Title != "old" || after[0].Directory != project {
		t.Fatalf("repaired sessions = %+v, want title old dir %s", after, project)
	}
	if err := s.RepairSessionDirectories(ctx); err != nil {
		t.Fatalf("second RepairSessionDirectories: %v", err)
	}
	again, err := s.ListSessionsByDir(ctx, project)
	if err != nil {
		t.Fatalf("ListSessionsByDir after second repair: %v", err)
	}
	if len(again) != 1 || again[0].Directory != project {
		t.Fatalf("second repair changed rows: %+v", again)
	}
}

func TestConcurrentWritersNoBusy(t *testing.T) {
	// Simulates parent + several sub-agent sessions writing parts at once.
	// Pre-fix this failed with SQLITE_BUSY under a multi-conn pool.
	ctx := context.Background()
	s := openTestStore(t)

	const agents = 12
	const partsPer = 20
	type pair struct {
		sess Session
		msg  Message
	}
	pairs := make([]pair, agents)
	for i := 0; i < agents; i++ {
		sess, err := s.CreateSession(ctx, Session{Directory: "/work", Title: "child"})
		if err != nil {
			t.Fatalf("CreateSession %d: %v", i, err)
		}
		msg, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "assistant", Agent: "sub"})
		if err != nil {
			t.Fatalf("InsertMessage %d: %v", i, err)
		}
		pairs[i] = pair{sess: sess, msg: msg}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, agents*partsPer)
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msgID := pairs[idx].msg.ID
			for j := 0; j < partsPer; j++ {
				text := fmt.Sprintf("agent-%d-part-%d", idx, j)
				if _, err := s.InsertPart(ctx, Part{MessageID: msgID, Type: "text", Text: &text}); err != nil {
					errCh <- err
					return
				}
				if j%3 == 0 {
					if _, err := s.InsertMessage(ctx, Message{SessionID: pairs[idx].sess.ID, Role: "user"}); err != nil {
						errCh <- err
						return
					}
				}
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}
}

func TestListChildSessionsHiddenFromMain(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	parent, err := s.CreateSession(ctx, Session{Directory: "/work", Title: "parent"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := s.CreateSession(ctx, Session{
		Directory:       "/work",
		Title:           "agent_alpha",
		ParentSessionID: &pid,
		Kind:            SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	main, err := s.ListSessionsByDir(ctx, "/work")
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	if len(main) != 1 || main[0].ID != parent.ID {
		t.Fatalf("main list = %+v", main)
	}
	kids, err := s.ListChildSessions(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListChildSessions: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("kids = %+v", kids)
	}
}

func TestListChildSessionsKeepsActivityOrdering(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	parent, err := s.CreateSession(ctx, Session{Directory: "/work", Title: "parent"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	parentID := parent.ID
	first, err := s.CreateSession(ctx, Session{
		Directory: "/work", Title: "first", ParentSessionID: &parentID,
		Kind: SessionKindSubagent, TimeCreated: 1_000, TimeUpdated: 1_000,
	})
	if err != nil {
		t.Fatalf("first child: %v", err)
	}
	second, err := s.CreateSession(ctx, Session{
		Directory: "/work", Title: "second", ParentSessionID: &parentID,
		Kind: SessionKindSubagent, TimeCreated: 2_000, TimeUpdated: 2_000,
	})
	if err != nil {
		t.Fatalf("second child: %v", err)
	}
	if _, err := s.InsertMessage(ctx, Message{SessionID: first.ID, Role: "assistant", TimeCreated: 3_000}); err != nil {
		t.Fatalf("first child message: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.TouchSession(ctx, second.ID); err != nil {
		t.Fatalf("second child activity: %v", err)
	}

	kids, err := s.ListChildSessions(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListChildSessions: %v", err)
	}
	if len(kids) != 2 || kids[0].ID != second.ID || kids[1].ID != first.ID {
		t.Fatalf("children not ordered by activity: %+v", kids)
	}
}

func TestSubagentJobRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	parent, err := s.CreateSession(ctx, Session{Directory: "/work", Title: "parent"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Optional FKs require real parent part / child session when set.
	am, err := s.InsertMessage(ctx, Message{SessionID: parent.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	part, err := s.InsertPart(ctx, Part{MessageID: am.ID, Type: "tool", ToolName: strPtr("task"), ToolStatus: strPtr("completed")})
	if err != nil {
		t.Fatalf("InsertPart: %v", err)
	}
	pid := parent.ID
	child, err := s.CreateSession(ctx, Session{
		Directory: "/work", Title: "child", ParentSessionID: &pid, Kind: SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child session: %v", err)
	}
	job := SubagentJob{
		ID:              "sub_testjob01aabbcc",
		ParentSessionID: parent.ID,
		ParentPartID:    part.ID,
		Name:            "layout-audit",
		Role:            "explore",
		Status:          "queued",
		Prompt:          "audit layout",
		Description:     "layout",
		Model:           "configured-child-model",
		Variant:         "high",
		MaxSteps:        32,
		TimeoutMS:       60000,
	}
	if err := s.UpsertSubagentJob(ctx, job); err != nil {
		t.Fatalf("UpsertSubagentJob: %v", err)
	}
	got, err := s.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if got.Name != "layout-audit" || got.Status != "queued" || got.Prompt != "audit layout" ||
		got.Model != "configured-child-model" || got.Variant != "high" {
		t.Fatalf("got %+v", got)
	}
	got.Status = "completed"
	got.Summary = "all good"
	got.ChildSessionID = child.ID
	if err := s.UpsertSubagentJob(ctx, got); err != nil {
		t.Fatalf("Upsert completed: %v", err)
	}
	list, err := s.ListSubagentJobs(ctx, parent.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSubagentJobs: %v %#v", err, list)
	}
	if list[0].Summary != "all good" || list[0].Status != "completed" {
		t.Fatalf("list row: %+v", list[0])
	}
	open, err := s.ListOpenSubagentJobs(ctx)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("open should be empty after completed, got %#v", open)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	s := openTestStore(t)
	var on int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if on != 1 {
		t.Fatalf("foreign_keys = %d, want 1", on)
	}
	if err := s.foreignKeyCheck(context.Background()); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
}

func TestParentSessionFKRejectsOrphan(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	missing := "ses_does_not_exist"
	_, err := s.CreateSession(ctx, Session{
		Directory:       "/work",
		ParentSessionID: &missing,
		Kind:            SessionKindSubagent,
	})
	if err == nil {
		t.Fatal("expected FK failure for missing parent_session_id")
	}
}

func TestChildSessionsCascadeOnParentDelete(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	parent, err := s.CreateSession(ctx, Session{Directory: "/work", Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := s.CreateSession(ctx, Session{
		Directory:       "/work",
		Title:           "child",
		ParentSessionID: &pid,
		Kind:            SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	text := "hi"
	um, err := s.InsertMessage(ctx, Message{SessionID: child.ID, Role: "user"})
	if err != nil {
		t.Fatalf("msg: %v", err)
	}
	if _, err := s.InsertPart(ctx, Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("part: %v", err)
	}
	if err := s.DeleteSession(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSession(ctx, child.ID); err == nil {
		t.Fatal("child session should be cascade-deleted")
	}
	msgs, err := s.ListMessages(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("child messages should cascade, got %d", len(msgs))
	}
}

func TestSubagentJobChildFKSetNull(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	parent, err := s.CreateSession(ctx, Session{Directory: "/work", Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := s.CreateSession(ctx, Session{
		Directory:       "/work",
		Title:           "agent",
		ParentSessionID: &pid,
		Kind:            SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	job := SubagentJob{
		ID:              "sub_fksetnull01aabb",
		ParentSessionID: parent.ID,
		ChildSessionID:  child.ID,
		Name:            "job",
		Role:            "explore",
		Status:          "completed",
		Prompt:          "p",
		Summary:         "report body",
	}
	if err := s.UpsertSubagentJob(ctx, job); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Delete only the child session row (not the parent).
	if err := s.DeleteSession(ctx, child.ID); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	got, err := s.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if got.ChildSessionID != "" {
		t.Fatalf("child_session_id = %q, want empty after SET NULL", got.ChildSessionID)
	}
	if got.Summary != "report body" {
		t.Fatalf("summary lost: %q", got.Summary)
	}
}

func TestUniqueMessageSeq(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	m1, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	// Force a duplicate seq via raw insert.
	_, err = s.db.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, time_created, seq, visible)
VALUES ('msg_dupseq00000001', ?, 'user', 1, ?, 1)`, sess.ID, m1.Seq)
	if err == nil {
		t.Fatal("expected unique seq violation")
	}
}

func TestSchemaHasIntegrityIndexes(t *testing.T) {
	s := openTestStore(t)
	want := []string{
		"idx_messages_session_seq",
		"idx_parts_message_seq",
		"idx_sessions_dir_kind_updated",
		"idx_sessions_parent_kind_updated",
		"idx_subagent_jobs_parent_started",
		"idx_subagent_jobs_open",
	}
	for _, name := range want {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n); err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		if n != 1 {
			t.Errorf("index %s missing", name)
		}
	}
	// Unique seq indexes.
	var sql string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_messages_session_seq'`).Scan(&sql); err != nil {
		t.Fatalf("index sql: %v", err)
	}
	if !strings.Contains(strings.ToUpper(sql), "UNIQUE") {
		t.Fatalf("idx_messages_session_seq not UNIQUE: %s", sql)
	}
}

func TestParentDeleteRemovesSubagentJobs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	parent, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	job := SubagentJob{
		ID: "sub_parentdel01aabb", ParentSessionID: parent.ID,
		Name: "j", Role: "explore", Status: "completed", Prompt: "p",
	}
	if err := s.UpsertSubagentJob(ctx, job); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.DeleteSession(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSubagentJob(ctx, job.ID); err == nil {
		t.Fatal("job should cascade-delete with parent")
	}
}

func TestReplaceAndListTodos(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: t.TempDir(), Title: "t"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.ReplaceTodos(ctx, sess.ID, []Todo{
		{Content: "a", Status: TodoPending},
		{Content: "b", Status: TodoInProgress},
		{Content: "c", Status: TodoCompleted},
	}); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}
	got, err := s.ListTodos(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Content != "a" || got[0].Seq != 0 || got[1].Status != TodoInProgress {
		t.Fatalf("got = %+v", got)
	}
	// Replace-all shrinks the list.
	if err := s.ReplaceTodos(ctx, sess.ID, []Todo{{Content: "only", Status: TodoCompleted}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListTodos(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "only" || got[0].Status != TodoCompleted {
		t.Fatalf("after replace = %+v", got)
	}
}
