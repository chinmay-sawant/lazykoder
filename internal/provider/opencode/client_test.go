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
)

func TestAPIKeyFromEnvMissing(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	if _, err := APIKeyFromEnv(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("APIKeyFromEnv() error = %v, want ErrMissingAPIKey", err)
	}
}

func TestAPIKeyFromEnvPriority(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "  first-key  ")
	t.Setenv("OPENCODE_ZEN_API_KEY", "zen-key")
	key, err := APIKeyFromEnv()
	if err != nil {
		t.Fatalf("APIKeyFromEnv() error = %v", err)
	}
	if key != "first-key" {
		t.Fatalf("APIKeyFromEnv() = %q, want %q", key, "first-key")
	}
	t.Setenv("OPENCODE_API_KEY", "   ")
	key, err = APIKeyFromEnv()
	if err != nil {
		t.Fatalf("APIKeyFromEnv() fallback error = %v", err)
	}
	if key != "zen-key" {
		t.Fatalf("APIKeyFromEnv() fallback = %q, want %q", key, "zen-key")
	}
}

func TestChatRequestAndResponse(t *testing.T) {
	var auth, contentType, path string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		path = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi back","reasoning":"thinking..","finish_reason":"stop"}}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"reasoning_tokens":2,"cache_read_tokens":1,"cache_write_tokens":1,"cost":0.001}}`)
	}))
	defer srv.Close()

	c := NewClient("secret-key", WithBaseURL(srv.URL), WithModel("test-model"))
	resp, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if auth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer secret-key")
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
	if path != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", path)
	}
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if req.Model != "test-model" {
		t.Errorf("request model = %q, want %q", req.Model, "test-model")
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi" ||
		req.Messages[1].Role != "assistant" || req.Messages[1].Content != "hello" {
		t.Errorf("request messages = %+v", req.Messages)
	}
	if req.Tools != nil {
		t.Errorf("request tools key present: %s", req.Tools)
	}
	if resp.Content != "hi back" {
		t.Errorf("Content = %q, want %q", resp.Content, "hi back")
	}
	if resp.Reasoning != "thinking.." {
		t.Errorf("Reasoning = %q, want %q", resp.Reasoning, "thinking..")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage == nil {
		t.Fatal("Usage = nil, want non-nil")
	}
	if resp.Usage.TokensTotal != 8 || resp.Usage.TokensInput != 3 || resp.Usage.TokensOutput != 5 ||
		resp.Usage.TokensReasoning != 2 || resp.Usage.TokensCacheRead != 1 || resp.Usage.TokensCacheWrite != 1 ||
		resp.Usage.Cost != 0.001 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestChatSendsTools(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	_, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "list"}},
		Tools: []ToolSpec{{
			Name:        "bash",
			Description: "Run a shell command.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "shell command to run"},
				},
				"required": []string{"command"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var req struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools = %+v, want 1 entry", req.Tools)
	}
	tool := req.Tools[0]
	if tool.Type != "function" || tool.Function.Name != "bash" || tool.Function.Description != "Run a shell command." {
		t.Errorf("tool = %+v", tool)
	}
	if tool.Function.Parameters["type"] != "object" {
		t.Errorf("parameters type = %v", tool.Function.Parameters["type"])
	}
	required, ok := tool.Function.Parameters["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "command" {
		t.Errorf("parameters required = %v", tool.Function.Parameters["required"])
	}
}

func TestChatToolCalls(t *testing.T) {
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}],"finish_reason":"tool-calls"}}],"usage":{"total_tokens":10}}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "run"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1 entry", resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "bash" || tc.Arguments != `{"command":"ls"}` {
		t.Errorf("ToolCalls[0] = %+v", tc)
	}
	if resp.FinishReason != "tool-calls" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "tool-calls")
	}
	if resp.Usage == nil || resp.Usage.TokensTotal != 10 {
		t.Errorf("Usage = %+v", resp.Usage)
	}

	_, err = c.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "run"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"command":"ls"}`}}},
			{Role: "tool", ToolCallID: "call_1", Content: "file.txt"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() round-trip error = %v", err)
	}
	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(lastBody, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(req.Messages))
	}
	asst := req.Messages[1]
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "call_1" || asst.ToolCalls[0].Type != "function" ||
		asst.ToolCalls[0].Function.Name != "bash" || asst.ToolCalls[0].Function.Arguments != `{"command":"ls"}` {
		t.Errorf("assistant message = %+v", asst)
	}
	toolMsg := req.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" || toolMsg.Content != "file.txt" {
		t.Errorf("tool message = %+v", toolMsg)
	}
	if !strings.Contains(string(lastBody), `"content":`) {
		t.Fatalf("request omitted content on an empty assistant turn: %s", lastBody)
	}
}

func TestChatKeepsEmptyContentField(t *testing.T) {
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	_, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "bash", Arguments: `{}`}}},
		},
	})
	if err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	if !strings.Contains(string(lastBody), `"content":""`) {
		t.Fatalf("empty assistant content omitted: %s", lastBody)
	}
}

func TestChatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL), WithRetryPolicy(RetryPolicy{MaxRetries: 0}))
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("Chat() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Chat() error = %q, want it to contain 500 and boom", err)
	}
}

func TestChatServerErrorEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("Chat() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Chat() error = %q, want it to contain 404", err)
	}
}

func TestChatResponseVariants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"x","reasoning_content":"hidden","finish_reason":"stop"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cache_read":7,"cache_write":8,"cost":0.5}}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Reasoning != "hidden" {
		t.Errorf("Reasoning = %q, want %q", resp.Reasoning, "hidden")
	}
	if resp.Usage == nil || resp.Usage.TokensCacheRead != 7 || resp.Usage.TokensCacheWrite != 8 || resp.Usage.Cost != 0.5 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestChatUsageCacheHitAliases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"x","finish_reason":"stop"}}],"usage":{"prompt_tokens":68790,"completion_tokens":10,"prompt_cache_hit_tokens":68000,"prompt_tokens_details":{"cached_tokens":68000}}}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	if resp.Usage == nil || resp.Usage.TokensInput != 68790 || resp.Usage.TokensCacheRead != 68000 {
		t.Fatalf("Usage = %+v, want input 68790 cache hit 68000", resp.Usage)
	}
}

func TestChatResponseWithoutUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"x","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	resp, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Usage != nil {
		t.Errorf("Usage = %+v, want nil", resp.Usage)
	}
}

func TestModels(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash"},{"id":"other"}]}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if auth != "Bearer k" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer k")
	}
	if len(models) != 2 || models[0] != "deepseek-v4-flash" || models[1] != "other" {
		t.Errorf("Models() = %v", models)
	}
}

func TestModelInfosParsesContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash","context_window":131072},{"id":"other","limit":{"context":32000}}]}`)
	}))
	defer srv.Close()
	c := NewClient("k", WithBaseURL(srv.URL))
	infos, err := c.ModelInfos(context.Background())
	if err != nil {
		t.Fatalf("ModelInfos: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("len = %d", len(infos))
	}
	if infos[0].Context != 131072 || infos[1].Context != 32000 {
		t.Errorf("contexts = %+v", infos)
	}
}

func TestModelInfosParsesCachePricesAndVariants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"grok-4.5","pricing":{"input":2,"output":6,"cache_read":0.3},"variants":["low","medium","high"]},{"id":"deepseek-v4-flash-free"}]}`)
	}))
	defer srv.Close()
	c := NewClient("k", WithBaseURL(srv.URL))
	infos, err := c.ModelInfos(context.Background())
	if err != nil {
		t.Fatalf("ModelInfos: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("len = %d", len(infos))
	}
	if infos[0].CacheReadPerM != 0.3 || infos[0].InputPerM != 2 || len(infos[0].Variants) != 3 {
		t.Fatalf("priced model = %+v", infos[0])
	}
	if !infos[1].Free {
		t.Fatalf("free model not marked: %+v", infos[1])
	}
}

func TestFreeModelInfosSkippedOnTestURL(t *testing.T) {
	c := NewClient("k", WithBaseURL("http://127.0.0.1:1"))
	infos, err := c.FreeModelInfos(context.Background())
	if err != nil {
		t.Fatalf("FreeModelInfos: %v", err)
	}
	if infos != nil {
		t.Fatalf("FreeModelInfos = %v, want nil on non-go base", infos)
	}
}

func TestModelInfosStampsGoEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash"}]}`)
	}))
	defer srv.Close()
	c := NewClient("k", WithBaseURL(srv.URL))
	infos, err := c.ModelInfos(context.Background())
	if err != nil {
		t.Fatalf("ModelInfos: %v", err)
	}
	if len(infos) != 1 || infos[0].Endpoint != srv.URL+"/chat/completions" {
		t.Fatalf("infos = %+v", infos)
	}
	if infos[0].Provider != ProviderGo {
		t.Fatalf("go provider = %q", infos[0].Provider)
	}
}

func TestFreeModelInfosStampsZenEndpointAndAuth(t *testing.T) {
	var zenAuth, zenPath, goPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/zen/v1/models"):
			zenPath = r.URL.Path
			zenAuth = r.Header.Get("Authorization")
			fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash-free"},{"id":"deepseek-v4-flash"}]}`)
		case strings.HasSuffix(r.URL.Path, "/zen/go/v1/models"):
			goPath = r.URL.Path
			fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash"}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient("zen-key", WithBaseURL(srv.URL+"/zen/go/v1"))
	goModels, err := c.ModelInfos(context.Background())
	if err != nil {
		t.Fatalf("ModelInfos: %v", err)
	}
	if goPath == "" || len(goModels) != 1 || goModels[0].Endpoint != srv.URL+"/zen/go/v1/chat/completions" {
		t.Fatalf("go models = %+v path=%q", goModels, goPath)
	}

	zenModels, err := c.FreeModelInfos(context.Background())
	if err != nil {
		t.Fatalf("FreeModelInfos: %v", err)
	}
	if zenAuth != "Bearer zen-key" {
		t.Fatalf("zen Authorization = %q", zenAuth)
	}
	if zenPath != "/zen/v1/models" {
		t.Fatalf("zen path = %q", zenPath)
	}
	if len(zenModels) != 1 || zenModels[0].ID != "deepseek-v4-flash-free" {
		t.Fatalf("zen models = %+v, want only the free id", zenModels)
	}
	if zenModels[0].Endpoint != srv.URL+"/zen/v1/chat/completions" {
		t.Fatalf("zen endpoint = %q", zenModels[0].Endpoint)
	}
	if zenModels[0].Provider != ProviderZen {
		t.Fatalf("zen provider = %q", zenModels[0].Provider)
	}
}

func TestChatRequestSendsReasoningEffort(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		got = req.ReasoningEffort
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()
	c := NewClient("k", WithBaseURL(srv.URL))
	if _, err := c.Chat(context.Background(), ChatRequest{
		Model:           "grok-4.5",
		ReasoningEffort: "high",
		Messages:        []Message{{Role: "user", Content: "x"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "high" {
		t.Fatalf("reasoning_effort = %q, want high", got)
	}
}

func TestChatURLHelpers(t *testing.T) {
	goBase := "https://opencode.ai/zen/go/v1"
	if got := ChatURL(goBase); got != goBase+"/chat/completions" {
		t.Fatalf("ChatURL = %q", got)
	}
	zen, ok := ZenChatURL(goBase)
	if !ok || zen != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("ZenChatURL = %q %v", zen, ok)
	}
	if _, ok := ZenChatURL("http://127.0.0.1:1"); ok {
		t.Fatal("ZenChatURL ok on non-go base")
	}
	if got := ChatURLForModel(goBase, "deepseek-v4-flash-free"); got != zen {
		t.Fatalf("free model URL = %q, want %q", got, zen)
	}
	if got := ChatURLForModel(goBase, "deepseek-v4-flash"); got != goBase+"/chat/completions" {
		t.Fatalf("go model URL = %q", got)
	}
}

func TestRouteForModel(t *testing.T) {
	goBase := "https://opencode.ai/zen/go/v1"
	tests := []struct {
		name     string
		id       string
		endpoint string
		provider string
	}{
		{name: "go", id: "deepseek-v4-flash", endpoint: goBase + "/chat/completions", provider: ProviderGo},
		{name: "zen free", id: "deepseek-v4-flash-free", endpoint: "https://opencode.ai/zen/v1/chat/completions", provider: ProviderZen},
		{name: "big pickle", id: "big-pickle", endpoint: "https://opencode.ai/zen/v1/chat/completions", provider: ProviderZen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := RouteForModel(goBase, tt.id)
			if route.Endpoint != tt.endpoint || route.Provider != tt.provider {
				t.Fatalf("RouteForModel(%q) = %+v", tt.id, route)
			}
		})
	}
}

func TestChatRequestUsesEndpoint(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL+"/zen/go/v1"))
	endpoint := srv.URL + "/zen/v1/chat/completions"
	if _, err := c.Chat(context.Background(), ChatRequest{
		Model:    "deepseek-v4-flash-free",
		Endpoint: endpoint,
		Messages: []Message{{Role: "user", Content: "x"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/zen/v1/chat/completions" {
		t.Fatalf("path = %q, want /zen/v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestChatRequestModelOverride(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		got = req.Model
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL), WithModel("default-model"))

	if _, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	if got != "default-model" {
		t.Errorf("model without override = %q, want default-model", got)
	}
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "picked-model", Messages: []Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	if got != "picked-model" {
		t.Errorf("model with override = %q, want picked-model", got)
	}
}

func TestUsageParsesBillingWindows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zen/go/v1/usage" {
			t.Errorf("path = %q, want /zen/go/v1/usage", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer k" {
			t.Errorf("Authorization = %q, want Bearer k", auth)
		}
		fmt.Fprint(w, `{"usage":{"rolling":{"status":"ok","percent":26,"resetsAt":"2026-08-17T13:21:22.000Z"},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-08-24T00:00:00.000Z"},"monthly":{"status":"rate-limited","percent":100,"resetsAt":"2026-09-01T21:29:16.000Z"}}}`)
	}))
	defer srv.Close()
	c := NewClient("k", WithBaseURL(strings.TrimSuffix(srv.URL, "/")+"/zen/go/v1"))
	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage(): %v", err)
	}
	if u.Rolling.Percent != 26 || u.Rolling.Status != "ok" || u.Rolling.RateLimited {
		t.Errorf("rolling = %+v", u.Rolling)
	}
	if u.Rolling.ResetsAt.Year() != 2026 || u.Rolling.ResetsAt.Month() != 8 {
		t.Errorf("rolling resetsAt = %v", u.Rolling.ResetsAt)
	}
	if u.Weekly.Percent != 10 || u.Weekly.Status != "ok" || u.Weekly.RateLimited {
		t.Errorf("weekly = %+v", u.Weekly)
	}
	if !u.Monthly.RateLimited || u.Monthly.Percent != 100 || u.Monthly.Status != "rate-limited" {
		t.Errorf("monthly = %+v", u.Monthly)
	}
}

func TestUsageSkippedOnNonZenBase(t *testing.T) {
	c := NewClient("k", WithBaseURL("http://127.0.0.1:1"))
	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage(): %v", err)
	}
	if u.Rolling.Percent != 0 || u.Weekly.Percent != 0 || u.Monthly.Percent != 0 {
		t.Errorf("expected empty usage on non-zen base, got %+v", u)
	}
}

func TestUsageParsesPartialWindows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"usage":{"weekly":{"status":"ok","percent":42}}}`)
	}))
	defer srv.Close()
	c := NewClient("k", WithBaseURL(strings.TrimSuffix(srv.URL, "/")+"/zen/go/v1"))
	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage(): %v", err)
	}
	if u.Weekly.Percent != 42 || u.Rolling.Percent != 0 || u.Monthly.Percent != 0 {
		t.Errorf("partial usage = %+v", u)
	}
}
