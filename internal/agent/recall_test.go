package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendInjectsRecallAfterProjectInstructions(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	workdir := t.TempDir()
	if err := writeTestFile(workdir, "AGENTS.md", "follow the project rules\n"); err != nil {
		t.Fatal(err)
	}
	var calls int
	a := New(st, newClient(t, fake.srv), workdir, Options{
		DisableStreaming: true,
		Recall: func(ctx context.Context, sessionID, userText string) (string, error) {
			calls++
			if sessionID == "" || userText != "fix the parser" {
				t.Fatalf("recall inputs = %q, %q", sessionID, userText)
			}
			messages, err := st.ListMessages(ctx, sessionID)
			if err != nil {
				return "", err
			}
			if len(messages) != 1 || messages[0].Role != "user" {
				t.Fatalf("messages during recall = %+v, want persisted user turn", messages)
			}
			return "knowledge-base/recaps/things-to-avoid/parser.md:12: do not repeat this fix", nil
		},
	})

	events, err := sendAndCollect(t, a, "fix the parser")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	start, finish := -1, -1
	for index, event := range events {
		switch event.Kind {
		case EventRecallStarted:
			start = index
		case EventRecallFinished:
			finish = index
		}
	}
	if start < 0 || finish <= start {
		t.Fatalf("recall events = start %d finish %d, want ordered scan lifecycle", start, finish)
	}
	if calls != 1 {
		t.Fatalf("recall calls = %d, want 1", calls)
	}
	msgs := fake.requestMessages(0)
	if len(msgs) != 3 {
		t.Fatalf("request messages = %+v, want project instructions, recall, history", msgs)
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "follow the project rules") {
		t.Fatalf("project instructions = %+v", msgs[0])
	}
	if msgs[1].Role != "system" || !strings.Contains(msgs[1].Content, "untrusted historical hints") ||
		!strings.Contains(msgs[1].Content, "do not repeat this fix") {
		t.Fatalf("recall message = %+v", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "fix the parser" {
		t.Fatalf("user message = %+v", msgs[2])
	}

	persisted, err := st.ListMessages(context.Background(), a.sessionID())
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range persisted {
		if message.Role == "system" {
			t.Fatalf("system message persisted: %+v", message)
		}
	}
}

func TestRecallRunsOnceBeforeToolFollowUp(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{{ID: "unknown", Name: "not-a-tool", Args: `{}`}}, nil),
		respBody("done", "", "stop", nil, nil),
	)
	st, _ := newTestEnv(t)
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		Recall: func(context.Context, string, string) (string, error) {
			calls++
			return "old hint", nil
		},
	})

	if _, err := sendAndCollect(t, a, "continue"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if calls != 1 {
		t.Fatalf("recall calls = %d, want 1", calls)
	}
	if !hasRecallMessage(fake.requestMessages(0)) {
		t.Fatal("first request missing recall")
	}
	if hasRecallMessage(fake.requestMessages(1)) {
		t.Fatal("tool follow-up repeated recall")
	}
}

func TestContinueDoesNotPrepareOrInjectRecall(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{{ID: "loop", Name: "not-a-tool", Args: `{}`}}, nil),
		respBody("continued", "", "stop", nil, nil),
	)
	st, _ := newTestEnv(t)
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		MaxSteps:         1,
		Recall: func(context.Context, string, string) (string, error) {
			calls++
			return "old hint", nil
		},
	})

	if _, err := sendAndCollect(t, a, "work"); !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Send error = %v, want step limit", err)
	}
	if _, err := continueAndCollect(t, a); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if calls != 1 {
		t.Fatalf("recall calls = %d, want 1 from Send only", calls)
	}
	if hasRecallMessage(fake.requestMessages(1)) {
		t.Fatal("Continue injected recall")
	}
}

func TestOverflowRetryReusesRecallWithoutAnotherLookup(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("recovered", "", "stop", nil, nil),
	)
	fake.overflowLeft = 1
	st, _ := newTestEnv(t)
	sess := seedLongSession(t, st, "m")
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Session:          &sess,
		DisableStreaming: true,
		Recall: func(context.Context, string, string) (string, error) {
			calls++
			return "old hint", nil
		},
		ContextWindow: 1_000_000,
		KeepTokens:    50,
	})

	if _, err := sendAndCollect(t, a, "work"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if calls != 1 {
		t.Fatalf("recall calls = %d, want 1", calls)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("requests = %d, want overflow, compact, retry", fake.requestCount())
	}
	if hasRecallMessage(fake.requestMessages(1)) {
		t.Fatal("compact request included recall")
	}
	if !hasRecallMessage(fake.requestMessages(2)) {
		t.Fatal("overflow retry lost cached recall")
	}
}

func TestRecallFailureIsSilent(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		Recall: func(context.Context, string, string) (string, error) {
			return "", errors.New("lookup unavailable")
		},
	})

	if _, err := sendAndCollect(t, a, "work"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if hasRecallMessage(fake.requestMessages(0)) {
		t.Fatal("failed recall should not reach provider")
	}
}

func TestChildAgentDoesNotPrepareRecall(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		AgentName:        "worker",
		Recall: func(context.Context, string, string) (string, error) {
			calls++
			return "old hint", nil
		},
	})

	if _, err := sendAndCollect(t, a, "work"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if calls != 0 {
		t.Fatalf("recall calls = %d, want 0 for child agent", calls)
	}
}

func hasRecallMessage(messages []wireMessage) bool {
	for _, message := range messages {
		if message.Role == "system" && strings.Contains(message.Content, "untrusted historical hints") {
			return true
		}
	}
	return false
}

func writeTestFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}
