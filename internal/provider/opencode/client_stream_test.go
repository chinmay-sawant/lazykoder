package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatStreamAccumulatesChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream        bool `json:"stream"`
			StreamOptions *struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if !req.Stream {
			t.Error("stream = false, want true")
		}
		if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
			t.Error("stream_options.include_usage missing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ch := range []string{"h", "e", "l", "l", "o"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", ch)
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":5,\"total_tokens\":7}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var deltas []Delta
	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(d Delta) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("Content = %q, want hello", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TokensOutput != 5 || resp.Usage.TokensTotal != 7 {
		t.Fatalf("Usage = %+v", resp.Usage)
	}
	var acc strings.Builder
	for _, d := range deltas {
		acc.WriteString(d.Content)
	}
	if acc.String() != "hello" {
		t.Fatalf("delta content = %q, want hello", acc.String())
	}
	if len(deltas) < 5 {
		t.Fatalf("deltas = %d, want at least 5", len(deltas))
	}
}

func TestChatStreamReasoningAndContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"th\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"ink\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Reasoning != "think" {
		t.Fatalf("Reasoning = %q, want think", resp.Reasoning)
	}
	if resp.Content != "ok" {
		t.Fatalf("Content = %q, want ok", resp.Content)
	}
}

func TestChatStreamSkipsGarbageLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		fmt.Fprint(w, "data: not-json\n\n")
		fmt.Fprint(w, ": comment\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Content != "ab" {
		t.Fatalf("Content = %q, want ab", resp.Content)
	}
}

func TestChatStreamAbortReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("ResponseWriter is not a Hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	_, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, nil)
	if err == nil {
		t.Fatal("ChatStream error = nil, want abort error")
	}
}

func TestChatStreamJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi back","reasoning":"thinking..","finish_reason":"stop"}}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)
	}))
	defer srv.Close()

	var deltas []Delta
	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(d Delta) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Content != "hi back" || resp.Reasoning != "thinking.." {
		t.Fatalf("resp = %+v", resp)
	}
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(deltas))
	}
	if deltas[0].Content != "hi back" || deltas[0].Reasoning != "thinking.." {
		t.Fatalf("delta = %+v", deltas[0])
	}
}

func TestChatStreamToolCallFragments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"bash\",\"arguments\":\"\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool-calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "run"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.FinishReason != "tool-calls" {
		t.Fatalf("FinishReason = %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "bash" || tc.Arguments != `{"command":"ls"}` {
		t.Fatalf("ToolCalls[0] = %+v", tc)
	}
}

func TestChatStreamNDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, "{\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n")
		fmt.Fprint(w, "{\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Content != "ok" || resp.FinishReason != "stop" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestChatStreamContextCancel(t *testing.T) {
	started := make(chan struct{})
	serverDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-r.Context().Done()
		close(serverDone)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	c := NewClient("k", WithBaseURL(srv.URL))
	_, err := c.ChatStream(ctx, ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, nil)
	if err == nil {
		t.Fatal("ChatStream error = nil, want cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ChatStream error = %v, want context.Canceled", err)
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server request did not observe cancellation")
	}
}

func TestChatStreamServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"nope"}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	_, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, nil)
	if err == nil {
		t.Fatal("ChatStream error = nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %q, want 502", err)
	}
}
