package db

import (
	"context"
	"reflect"
	"testing"
)

func TestLoadSessionGraphEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.LoadSessionGraph(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadSessionGraph: %v", err)
	}
	want := assembleSessionGraph(t, s, ctx, sess.ID)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty graph mismatch:\n got %+v\nwant %+v", got, want)
	}
	if got.Entries == nil {
		t.Fatal("Entries should be non-nil empty slice")
	}
	if got.ToolCallsByPart == nil {
		t.Fatal("ToolCallsByPart should be non-nil empty map")
	}
}

func TestLoadSessionGraphMatchesListHelpers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sess, err := s.CreateSession(ctx, Session{Directory: "/work"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	userText := "hello"
	user, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("InsertMessage user: %v", err)
	}
	if _, err := s.InsertPart(ctx, Part{MessageID: user.ID, Type: "text", Text: &userText}); err != nil {
		t.Fatalf("InsertPart user: %v", err)
	}

	asst, err := s.InsertMessage(ctx, Message{
		SessionID:  sess.ID,
		Role:       "assistant",
		ProviderID: "opencode-go",
		ModelID:    "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("InsertMessage assistant: %v", err)
	}
	reason := "thinking"
	if _, err := s.InsertPart(ctx, Part{MessageID: asst.ID, Type: "reasoning", Text: &reason}); err != nil {
		t.Fatalf("InsertPart reasoning: %v", err)
	}
	reply := "world"
	if _, err := s.InsertPart(ctx, Part{MessageID: asst.ID, Type: "text", Text: &reply}); err != nil {
		t.Fatalf("InsertPart text: %v", err)
	}
	status := "completed"
	toolPart, err := s.InsertPart(ctx, Part{
		MessageID:  asst.ID,
		Type:       "tool",
		ToolName:   strPtr("bash"),
		ToolCallID: strPtr("call_1"),
		ToolStatus: &status,
	})
	if err != nil {
		t.Fatalf("InsertPart tool: %v", err)
	}
	out := "ok"
	code := 0
	start := int64(10)
	end := int64(20)
	if err := s.InsertToolCall(ctx, ToolCall{
		PartID:    toolPart.ID,
		Tool:      "bash",
		CallID:    "call_1",
		Status:    "completed",
		InputJSON: `{"command":"echo hi"}`,
		Output:    &out,
		ExitCode:  &code,
		TimeStart: &start,
		TimeEnd:   &end,
	}); err != nil {
		t.Fatalf("InsertToolCall: %v", err)
	}

	// Message with no parts stays in the graph with a nil Parts slice
	// (same as ListParts on an empty message).
	if _, err := s.InsertMessage(ctx, Message{SessionID: sess.ID, Role: "user"}); err != nil {
		t.Fatalf("InsertMessage empty: %v", err)
	}

	got, err := s.LoadSessionGraph(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadSessionGraph: %v", err)
	}
	want := assembleSessionGraph(t, s, ctx, sess.ID)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graph mismatch:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(got.Entries))
	}
	if len(got.Entries[1].Parts) != 3 {
		t.Fatalf("assistant parts = %d, want 3", len(got.Entries[1].Parts))
	}
	if len(got.ToolCallsByPart) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(got.ToolCallsByPart))
	}
	tc, ok := got.ToolCallsByPart[toolPart.ID]
	if !ok || tc.CallID != "call_1" || tc.InputJSON != `{"command":"echo hi"}` {
		t.Fatalf("ToolCallsByPart[%s] = %+v", toolPart.ID, tc)
	}
}

func TestLoadSessionGraphUnknownSession(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	got, err := s.LoadSessionGraph(ctx, "ses_missing")
	if err != nil {
		t.Fatalf("LoadSessionGraph missing: %v", err)
	}
	want := assembleSessionGraph(t, s, ctx, "ses_missing")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing session graph mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// assembleSessionGraph mirrors agent.sessionEntries using List* helpers.
func assembleSessionGraph(t *testing.T, s *Store, ctx context.Context, sessionID string) SessionGraph {
	t.Helper()
	msgs, err := s.ListMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	tcs, err := s.ListToolCalls(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	byPart := make(map[string]ToolCall, len(tcs))
	for _, tc := range tcs {
		byPart[tc.PartID] = tc
	}
	entries := make([]SessionEntry, 0, len(msgs))
	for _, msg := range msgs {
		parts, err := s.ListParts(ctx, msg.ID)
		if err != nil {
			t.Fatalf("ListParts(%s): %v", msg.ID, err)
		}
		entries = append(entries, SessionEntry{Message: msg, Parts: parts})
	}
	return SessionGraph{Entries: entries, ToolCallsByPart: byPart}
}
