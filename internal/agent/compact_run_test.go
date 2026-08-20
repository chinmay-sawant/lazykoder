package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestSendUnderBudgetSkipsCompact(t *testing.T) {
	fake := newFakeProvider(t, respBody("hello", "", "stop", nil, testUsage))
	st, _ := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		CompactAuto:      true,
		ContextWindow:    1_000_000,
		TokensUsed:       100,
	})
	if _, err := sendAndCollect(t, a, "hi"); err != nil {
		t.Fatal(err)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1", fake.requestCount())
	}
	if !fake.requestHadTools(0) {
		t.Fatal("normal turn should advertise tools")
	}
}

func TestSendOverBudgetCompactsThenChats(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("CHECKPOINT", "", "stop", nil, nil),
		respBody("continued", "", "stop", nil, testUsage),
	)
	st, _ := newTestEnv(t)
	sess := seedLongSession(t, st, "old-model")
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		CompactAuto:      true,
		ContextWindow:    1_000,
		CompactPercent:   80,
		TokensUsed:       8_000,
		Model:            "small-model",
		KeepTokens:       50,
	})
	events, err := sendAndCollect(t, a, "please continue")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEventKind(events, EventCompacting) || !hasEventKind(events, EventCompacted) {
		t.Fatalf("missing compact events: %+v", eventKinds(events))
	}
	if fake.requestCount() != 2 {
		t.Fatalf("requests = %d, want 2 (compact + chat)", fake.requestCount())
	}
	if fake.requestHadTools(0) {
		t.Fatal("compact call must not advertise tools")
	}
	if !fake.requestHadTools(1) {
		t.Fatal("follow-up chat should advertise tools")
	}
	compactMsgs := fake.requestMessages(0)
	if len(compactMsgs) != 1 || compactMsgs[0].Role != "user" {
		t.Fatalf("compact payload = %+v", compactMsgs)
	}
	if !strings.Contains(compactMsgs[0].Content, "Primary request") {
		t.Fatalf("compact prompt missing headings: %q", compactMsgs[0].Content)
	}
	chat := fake.requestMessages(1)
	if len(chat) < 2 {
		t.Fatalf("chat history too short: %+v", chat)
	}
	if !strings.Contains(chat[0].Content, "checkpoint") {
		t.Fatalf("chat should start at checkpoint: %q", chat[0].Content)
	}
	if strings.Contains(chat[0].Content, "old-prefix-should-drop") && len(chat) > 6 {
		t.Fatal("prefix should not be fully replayed after compact")
	}
}

func TestCompactShrinkUsesOutgoingModel(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("SHRINK-SUMMARY", "", "stop", nil, nil),
		respBody("ok on small", "", "stop", nil, testUsage),
	)
	st, _ := newTestEnv(t)
	sess := seedLongSession(t, st, "deepseek-large")
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		CompactAuto:      true,
		CompactReason:    CompactReasonShrink,
		Model:            "small-256k",
		ContextWindow:    256_000,
		OutgoingModel:    "deepseek-large",
		OutgoingWindow:   1_000_000,
		TokensUsed:       400_000,
		KeepTokens:       50,
	})
	if _, err := sendAndCollect(t, a, "switch and go"); err != nil {
		t.Fatal(err)
	}
	if fake.requestModels(0) != "deepseek-large" {
		t.Fatalf("compact model = %q, want outgoing deepseek-large", fake.requestModels(0))
	}
	if fake.requestModels(1) != "small-256k" {
		t.Fatalf("follow-up model = %q, want incoming small-256k", fake.requestModels(1))
	}
}

func TestOverflowRetriesOnce(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("AFTER-OVERFLOW", "", "stop", nil, nil),
		respBody("recovered", "", "stop", nil, testUsage),
	)
	fake.overflowLeft = 1
	st, _ := newTestEnv(t)
	sess := seedLongSession(t, st, "m")
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		CompactAuto:      false,
		ContextWindow:    1_000_000,
		Model:            "m",
		KeepTokens:       50,
	})
	if _, err := sendAndCollect(t, a, "go"); err != nil {
		t.Fatal(err)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("requests = %d, want 3 (overflow + compact + retry)", fake.requestCount())
	}
	if fake.requestHadTools(1) {
		t.Fatal("compact after overflow must be tools-off")
	}
}

func TestBuildHistoryPrunesOldToolBodies(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, testUsage))
	st, _ := newTestEnv(t)
	sess := seedToolHeavySession(t, st)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		KeepTokens:       80,
	})
	if _, err := sendAndCollect(t, a, "next"); err != nil {
		t.Fatal(err)
	}
	msgs := fake.requestMessages(0)
	var sawPlaceholder, sawRecent bool
	for _, msg := range msgs {
		if msg.Role != "tool" {
			continue
		}
		if msg.Content == PrunedToolPlaceholder {
			sawPlaceholder = true
		}
		if strings.Contains(msg.Content, "recent-output") {
			sawRecent = true
		}
		if strings.Contains(msg.Content, "OLD-TOOL-BODY") {
			t.Fatalf("old tool body leaked into request: %q", prunePreview(msg.Content))
		}
	}
	if !sawPlaceholder {
		t.Fatal("expected pruned tool placeholder")
	}
	if !sawRecent {
		t.Fatal("expected recent tool body to remain")
	}
}

func TestBuildHistoryStartsAtCheckpoint(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, testUsage))
	st, _ := newTestEnv(t)
	ctx := context.Background()
	sess, err := st.CreateSession(ctx, db.Session{Title: "mixed", Directory: t.TempDir(), Model: "new"})
	if err != nil {
		t.Fatal(err)
	}
	mustUser(t, st, sess.ID, "old-prefix-should-drop")
	mustAssistant(t, st, sess.ID, "old-model", "old reply")
	tail := mustUser(t, st, sess.ID, "keep this tail")
	env := EncodeCompactText(CompactEnvelope{
		Summary:            "earlier work is done",
		TailStartMessageID: tail.ID,
		FromModel:          "old-model",
		ToModel:            "new-model",
		Reason:             CompactReasonAuto,
	})
	cmsg, err := st.InsertMessage(ctx, db.Message{SessionID: sess.ID, Role: "assistant", Agent: CompactAgentName, ModelID: "old-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertPart(ctx, db.Part{MessageID: cmsg.ID, Type: CompactPartType, Text: &env}); err != nil {
		t.Fatal(err)
	}
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		Model:            "new-model",
	})
	if _, err := sendAndCollect(t, a, "and now"); err != nil {
		t.Fatal(err)
	}
	chat := fake.requestMessages(0)
	joined := ""
	for _, msg := range chat {
		joined += msg.Content
	}
	if strings.Contains(joined, "old-prefix-should-drop") {
		t.Fatalf("prefix leaked after checkpoint: %q", joined)
	}
	if !strings.Contains(chat[0].Content, "earlier work is done") {
		t.Fatalf("missing checkpoint summary: %+v", chat)
	}
	if !strings.Contains(joined, "keep this tail") {
		t.Fatalf("tail missing: %q", joined)
	}
}

func TestManualCompactStopsAfterCheckpoint(t *testing.T) {
	fake := newFakeProvider(t, respBody("MANUAL", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	sess := seedLongSession(t, st, "m")
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		Model:            "m",
		ContextWindow:    1_000_000,
		KeepTokens:       50,
	})
	events := make(chan Event, 64)
	if err := a.Compact(context.Background(), events, CompactReasonManual, "focus on tests"); err != nil {
		t.Fatal(err)
	}
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	if !hasEventKind(got, EventCompacted) {
		t.Fatal("manual compact missing EventCompacted")
	}
	if fake.requestCount() != 1 {
		t.Fatalf("manual compact requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(fake.requestMessages(0)[0].Content, "focus on tests") {
		t.Fatal("missing compact instructions")
	}
	var after int64
	for _, ev := range got {
		if ev.Kind == EventCompacted {
			after = ev.TokensUsed
			if ev.Part.Text == "" {
				t.Fatal("compact part missing text")
			}
			env := ParseCompactText(ev.Part.Text)
			if env.TokensAfter <= 0 || env.TokensAfter != ev.TokensUsed {
				t.Fatalf("tokens_after = %d event = %d", env.TokensAfter, ev.TokensUsed)
			}
		}
	}
	if after <= 0 {
		t.Fatal("EventCompacted TokensUsed unset")
	}
}

func TestIsContextOverflow(t *testing.T) {
	if !IsContextOverflow(errString("opencode: chat request failed: status 400: context_length_exceeded")) {
		t.Fatal("should detect context_length_exceeded")
	}
	if IsContextOverflow(errString("agent: provider: timeout")) {
		t.Fatal("timeout is not overflow")
	}
}

func seedLongSession(t *testing.T, st *db.Store, model string) db.Session {
	t.Helper()
	ctx := context.Background()
	sess, err := st.CreateSession(ctx, db.Session{Title: "long", Directory: t.TempDir(), Model: model})
	if err != nil {
		t.Fatal(err)
	}
	mustUser(t, st, sess.ID, "first task old-prefix-should-drop")
	mustAssistant(t, st, sess.ID, model, "working on first")
	mustUser(t, st, sess.ID, "second task")
	mustAssistant(t, st, sess.ID, model, "working on second")
	return sess
}

func seedToolHeavySession(t *testing.T, st *db.Store) db.Session {
	t.Helper()
	ctx := context.Background()
	sess, err := st.CreateSession(ctx, db.Session{Title: "tools", Directory: t.TempDir(), Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	mustUser(t, st, sess.ID, "read old")
	asst := mustAssistant(t, st, sess.ID, "m", "reading")
	old := strings.Repeat("OLD-TOOL-BODY", 200)
	mustTool(t, st, asst.ID, "c_old", "bash", old)
	mustUser(t, st, sess.ID, "again")
	asst2 := mustAssistant(t, st, sess.ID, "m", "reading more")
	mustTool(t, st, asst2.ID, "c_old2", "bash", old)
	mustUser(t, st, sess.ID, "recent")
	asst3 := mustAssistant(t, st, sess.ID, "m", "tail")
	mustTool(t, st, asst3.ID, "c_new", "bash", "recent-output")
	return sess
}

func mustUser(t *testing.T, st *db.Store, sessionID, text string) db.Message {
	t.Helper()
	ctx := context.Background()
	msg, err := st.InsertMessage(ctx, db.Message{SessionID: sessionID, Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertPart(ctx, db.Part{MessageID: msg.ID, Type: "text", Text: &text}); err != nil {
		t.Fatal(err)
	}
	return msg
}

func mustAssistant(t *testing.T, st *db.Store, sessionID, model, text string) db.Message {
	t.Helper()
	ctx := context.Background()
	msg, err := st.InsertMessage(ctx, db.Message{SessionID: sessionID, Role: "assistant", ModelID: model})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertPart(ctx, db.Part{MessageID: msg.ID, Type: "text", Text: &text}); err != nil {
		t.Fatal(err)
	}
	return msg
}

func mustTool(t *testing.T, st *db.Store, msgID, callID, name, output string) {
	t.Helper()
	ctx := context.Background()
	part, err := st.InsertPart(ctx, db.Part{
		MessageID:  msgID,
		Type:       "tool",
		ToolName:   &name,
		ToolCallID: &callID,
		ToolStatus: strPtr("completed"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToolCall(ctx, db.ToolCall{
		PartID:   part.ID,
		Tool:     name,
		CallID:   callID,
		Status:   "completed",
		Output:   &output,
		ExitCode: intPtr(0),
	}); err != nil {
		t.Fatal(err)
	}
}

func eventKinds(events []Event) []EventKind {
	out := make([]EventKind, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}

type errString string

func (e errString) Error() string { return string(e) }

func intPtr(n int) *int { return &n }
