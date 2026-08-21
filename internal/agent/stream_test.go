package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestFormatTokensPerSecond(t *testing.T) {
	if got := FormatTokensPerSecond(60, 2*time.Second); got != "30.0" {
		t.Fatalf("rate = %q, want 30.0", got)
	}
	if got := FormatTokensPerSecond(60, 0); got != "-" {
		t.Fatalf("zero elapsed rate = %q, want -", got)
	}
}

func TestSendStreamingReasoningAndText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Stream bool `json:"stream"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if !req.Stream {
			t.Error("request missing stream=true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"th\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"ink\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":6,\"total_tokens\":10,\"reasoning_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	st, _ := newTestEnv(t)
	a := New(st, newClient(t, srv), t.TempDir(), Options{})
	events, err := sendAndCollect(t, a, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sid := sessionIDFromEvents(t, events)
	msgs, err := st.ListMessages(context.Background(), sid)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	parts, err := st.ListParts(context.Background(), msgs[1].ID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	wantTypes := []string{"step-start", "reasoning", "text", "step-finish"}
	if len(parts) != len(wantTypes) {
		t.Fatalf("assistant parts = %d (%v), want %v", len(parts), partTypes(parts), wantTypes)
	}
	for i, want := range wantTypes {
		if parts[i].Type != want {
			t.Errorf("part %d type = %q, want %q", i, parts[i].Type, want)
		}
	}
	if parts[1].Text == nil || *parts[1].Text != "think" {
		t.Errorf("reasoning = %v, want think", parts[1].Text)
	}
	if parts[2].Text == nil || *parts[2].Text != "ok" {
		t.Errorf("text = %v, want ok", parts[2].Text)
	}
	var reasonEvents int
	for _, ev := range events {
		if ev.Kind == EventPart && ev.Part.Kind == PartDeltaReasoning {
			reasonEvents++
		}
	}
	if reasonEvents < 2 {
		t.Fatalf("reasoning events = %d, want at least 2 streamed updates", reasonEvents)
	}
}

func partTypes(parts []db.Part) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Type)
	}
	return out
}
