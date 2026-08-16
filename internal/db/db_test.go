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
WHERE type = 'table' AND name IN ('sessions', 'messages', 'parts', 'tool_calls', 'schema_migrations')`).Scan(&n); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if n != 5 {
		t.Fatalf("got %d tables, want 5", n)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if n != 4 {
		t.Fatalf("got %d schema_migrations rows, want 4", n)
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
	if sess.Provider != "opencode-go" || sess.Model != "deepseek-v4-flash" || sess.Status != "active" {
		t.Fatalf("defaults not applied: %+v", sess)
	}
	if sess.TimeCreated == 0 || sess.TimeUpdated == 0 {
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
		t.Fatalf("not ordered by time_updated DESC: %+v", sessions)
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
