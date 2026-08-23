package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesChatStreamUsesResponsesWireFormat(t *testing.T) {
	var body map[string]any
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "event: response.output_text.delta")
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","item_id":"msg_1","delta":"OK"}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: response.completed")
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := NewClient("key", WithBaseURL(server.URL), WithRetryPolicy(RetryPolicy{MaxRetries: 0}))
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Model:           "gpt-5.6-luna",
		Endpoint:        server.URL + "/responses",
		ReasoningEffort: "low",
		Messages:        []Message{{Role: "user", Content: "hello"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/responses" || response.Content != "OK" || response.FinishReason != "stop" {
		t.Fatalf("path=%q response=%+v", path, response)
	}
	if response.Usage == nil || response.Usage.TokensTotal != 5 {
		t.Fatalf("usage=%+v", response.Usage)
	}
	if _, ok := body["input"]; !ok {
		t.Fatalf("responses body missing input: %v", body)
	}
	if _, ok := body["messages"]; ok {
		t.Fatalf("responses body still contains chat messages: %v", body)
	}
	if got := body["reasoning"].(map[string]any)["effort"]; got != "low" {
		t.Fatalf("reasoning effort=%v", got)
	}
}

func TestResponsesChatStreamCollectsFunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "event: response.output_item.added")
		fmt.Fprintln(w, `data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"bash","arguments":""}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: response.function_call_arguments.delta")
		fmt.Fprintln(w, `data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"command\":\"pwd\"}"}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: response.completed")
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := NewClient("key", WithBaseURL(server.URL), WithRetryPolicy(RetryPolicy{MaxRetries: 0}))
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Model:    "gpt-5.6-luna",
		Endpoint: server.URL + "/responses",
		Messages: []Message{{Role: "user", Content: "run pwd"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls=%+v", response.ToolCalls)
	}
	call := response.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "bash" || !strings.Contains(call.Arguments, `"command":"pwd"`) {
		t.Fatalf("tool call=%+v", call)
	}
	if response.FinishReason != "tool-calls" {
		t.Fatalf("finish reason=%q", response.FinishReason)
	}
}
