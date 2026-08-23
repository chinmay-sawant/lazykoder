package recap

import (
	"context"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestMemoryWorkerUsesConfiguredModelWithoutTools(t *testing.T) {
	var gotRequest opencode.ChatRequest
	worker := MemoryWorker{
		Client: recapClientFunc(func(_ context.Context, request opencode.ChatRequest) (*opencode.ChatResponse, error) {
			gotRequest = request
			return &opencode.ChatResponse{
				Content:      `{"preferences":[],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	_, err := worker.Generate(context.Background(), workerTestSnapshot(), NewMemoryDocument(), "recap evidence")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotRequest.Model != "deepseek-v4-flash" || gotRequest.Tools != nil {
		t.Fatalf("request = model=%q tools=%v", gotRequest.Model, gotRequest.Tools)
	}
	if gotRequest.MaxTokens != defaultMemoryMaxTokens {
		t.Fatalf("max tokens = %d, want %d", gotRequest.MaxTokens, defaultMemoryMaxTokens)
	}
	if len(gotRequest.Messages) != 2 || !strings.Contains(gotRequest.Messages[0].Content, "exactly one JSON object") || !strings.Contains(gotRequest.Messages[0].Content, "direct user statements") || !strings.Contains(gotRequest.Messages[0].Content, "emit a supersession") || !strings.Contains(gotRequest.Messages[1].Content, "Current memory document") {
		t.Fatalf("messages = %+v", gotRequest.Messages)
	}
}

func TestMemoryWorkerDoesNotExposeHistoricalSourceIDsAsCurrent(t *testing.T) {
	snapshot := workerTestSnapshot()
	previous := NewMemoryDocument()
	previous.Decisions = append(previous.Decisions, MemoryEntry{
		ID:               memoryEntryKey(memorySectionDecisions, "Use the old plan"),
		State:            "active",
		Text:             "Use the old plan",
		Evidence:         "An earlier session recorded this decision.",
		SourceMessageIDs: []string{"msg_old"},
		FirstSeenUTC:     "2026-08-22T10:00:00Z",
		LastSeenUTC:      "2026-08-22T10:00:00Z",
	})
	worker := MemoryWorker{
		Client: recapClientFunc(func(_ context.Context, request opencode.ChatRequest) (*opencode.ChatResponse, error) {
			prompt := request.Messages[1].Content
			if strings.Contains(prompt, "msg_old") {
				t.Fatalf("historical source ID leaked into current prompt: %q", prompt)
			}
			if !strings.Contains(prompt, snapshot.Messages[0].ID) {
				t.Fatalf("current source ID missing from prompt: %q", prompt)
			}
			return &opencode.ChatResponse{
				Content:      `{"preferences":[],"decisions":[{"text":"Use the new plan","evidence":"The current session changed the decision.","source_message_ids":["msg_4"]}],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	if _, err := worker.Generate(context.Background(), snapshot, previous, ""); err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

func TestMemoryWorkerRepairsUncitedRecentContext(t *testing.T) {
	calls := 0
	worker := MemoryWorker{
		Client: recapClientFunc(func(_ context.Context, _ opencode.ChatRequest) (*opencode.ChatResponse, error) {
			calls++
			if calls == 1 {
				return &opencode.ChatResponse{
					Content:      `{"preferences":[],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[{"text":"Temporary note","evidence":"Observed in the latest window.","source_message_ids":[]}],"supersessions":[]}`,
					FinishReason: "stop",
				}, nil
			}
			return &opencode.ChatResponse{
				Content:      `{"preferences":[],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	if _, err := worker.Generate(context.Background(), workerTestSnapshot(), NewMemoryDocument(), ""); err != nil {
		t.Fatalf("Generate after %d provider calls: %v", calls, err)
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want one repair retry", calls)
	}
}

func TestMemoryWorkerDropsUncitedRecentContextAfterRepair(t *testing.T) {
	calls := 0
	worker := MemoryWorker{
		Client: recapClientFunc(func(_ context.Context, _ opencode.ChatRequest) (*opencode.ChatResponse, error) {
			calls++
			return &opencode.ChatResponse{
				Content:      `{"preferences":[],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[{"text":"Temporary note","evidence":"Observed in the latest window.","source_message_ids":[]}],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	envelope, err := worker.Generate(context.Background(), workerTestSnapshot(), NewMemoryDocument(), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(envelope.RecentContext) != 0 || calls != 2 {
		t.Fatalf("recent context = %+v, provider calls = %d, want dropped item after one retry", envelope.RecentContext, calls)
	}
}

func TestMemoryWorkerRestoresExplicitUserInstruction(t *testing.T) {
	snapshot := memoryTestSnapshot()
	snapshot.Messages[0].Text = "I prefer the local memory file by default."
	worker := MemoryWorker{
		Client: recapClientFunc(func(_ context.Context, request opencode.ChatRequest) (*opencode.ChatResponse, error) {
			if !strings.Contains(request.Messages[1].Content, "Explicit user signals") {
				t.Fatalf("memory prompt missing explicit signal: %q", request.Messages[1].Content)
			}
			return &opencode.ChatResponse{
				Content:      `{"preferences":[],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	envelope, err := worker.Generate(context.Background(), snapshot, NewMemoryDocument(), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(envelope.Preferences) != 1 || envelope.Preferences[0].Text != snapshot.Messages[0].Text {
		t.Fatalf("preferences = %+v, want explicit user instruction", envelope.Preferences)
	}
}

func TestMemoryWorkerAcceptsLiteralNewlines(t *testing.T) {
	worker := MemoryWorker{
		Client: recapClientFunc(func(context.Context, opencode.ChatRequest) (*opencode.ChatResponse, error) {
			return &opencode.ChatResponse{
				Content: `{"preferences":[{"text":"Keep plans focused","evidence":"First line
Second line","source_message_ids":["msg_4"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	envelope, err := worker.Generate(context.Background(), workerTestSnapshot(), NewMemoryDocument(), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if envelope.Preferences[0].Evidence != "First line\nSecond line" {
		t.Fatalf("evidence = %q", envelope.Preferences[0].Evidence)
	}
}
