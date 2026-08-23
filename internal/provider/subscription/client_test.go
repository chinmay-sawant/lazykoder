package subscription

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestCodexClientReturnsLazyKoderToolCalls(t *testing.T) {
	var got Request
	client := NewCodex("gpt-test-codex", WithRunner(func(_ context.Context, request Request) (string, error) {
		got = request
		return `{"content":"I will inspect it.","tool_calls":[{"name":"bash","arguments":{"command":"pwd"}}]}`, nil
	}))
	var deltas []opencode.Delta
	response, err := client.ChatStream(context.Background(), opencode.ChatRequest{
		Messages: []opencode.Message{{Role: "user", Content: "Where am I?"}},
		Tools:    []opencode.ToolSpec{{Name: "bash", Description: "Run a command"}},
	}, func(delta opencode.Delta) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != providerCodex || got.Model != "gpt-test-codex" {
		t.Fatalf("runner request = %+v", got)
	}
	if got.ReasoningEffort != "" {
		t.Fatalf("runner reasoning effort = %q, want empty", got.ReasoningEffort)
	}
	if !strings.Contains(got.Prompt, "Do not use your own tools") || !strings.Contains(got.Prompt, `"Name":"bash"`) {
		t.Fatalf("prompt does not preserve lazykoder tool boundary: %q", got.Prompt)
	}
	if response.FinishReason != "tool-calls" || len(response.ToolCalls) != 1 {
		t.Fatalf("response = %+v", response)
	}
	if response.ToolCalls[0].Name != "bash" || response.ToolCalls[0].Arguments != `{"command":"pwd"}` {
		t.Fatalf("tool call = %+v", response.ToolCalls[0])
	}
	if len(deltas) != 1 || deltas[0].Content != "I will inspect it." {
		t.Fatalf("deltas = %+v", deltas)
	}
}

func TestGrokClientRejectsUndeclaredTool(t *testing.T) {
	client := NewGrok("grok-4.6", WithRunner(func(context.Context, Request) (string, error) {
		return `{"content":"","tool_calls":[{"name":"shell","arguments":{}}]}`, nil
	}))
	_, err := client.Chat(context.Background(), opencode.ChatRequest{
		Messages: []opencode.Message{{Role: "user", Content: "Run this"}},
		Tools:    []opencode.ToolSpec{{Name: "bash"}},
	})
	if err == nil || !strings.Contains(err.Error(), "undeclared tool") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubscriptionClientRejectsToolCallsWithoutAdvertisedTools(t *testing.T) {
	client := NewGrok("grok-4.6", WithRunner(func(context.Context, Request) (string, error) {
		return `{"content":"","tool_calls":[{"name":"bash","arguments":{}}]}`, nil
	}))
	_, err := client.Chat(context.Background(), opencode.ChatRequest{
		Messages: []opencode.Message{{Role: "user", Content: "Run this"}},
	})
	if err == nil || !strings.Contains(err.Error(), "undeclared tool") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubscriptionClientRejectsIncompleteStructuredResponse(t *testing.T) {
	client := NewCodex("gpt-test-codex", WithRunner(func(context.Context, Request) (string, error) {
		return `{"content":"missing the tool calls field"}`, nil
	}))
	_, err := client.Chat(context.Background(), opencode.ChatRequest{
		Messages: []opencode.Message{{Role: "user", Content: "Hello"}},
		Tools:    []opencode.ToolSpec{{Name: "bash"}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing tool_calls") {
		t.Fatalf("error = %v", err)
	}
}

func TestCodexClientRefreshesSignedInCatalog(t *testing.T) {
	var got Request
	client := NewCodex("", WithCatalogLoader(func(context.Context) (ModelCatalog, error) {
		return ModelCatalog{
			Default:        "gpt-account-default",
			DefaultVariant: "low",
			Models: []opencode.ModelInfo{{
				ID:       "gpt-account-default",
				Provider: providerCodex,
				Variants: []string{"low", "high"},
			}},
		}, nil
	}), WithRunner(func(_ context.Context, request Request) (string, error) {
		got = request
		return `{"content":"done","tool_calls":[]}`, nil
	}))

	infos, err := client.ModelInfos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.Model() != "gpt-account-default" || client.DefaultVariant() != "low" || len(infos) != 1 || infos[0].ID != "gpt-account-default" {
		t.Fatalf("catalog = model:%q infos:%+v", client.Model(), infos)
	}
	if _, err := client.Chat(context.Background(), opencode.ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-account-default" || got.ReasoningEffort != "low" {
		t.Fatalf("request = %+v, want account default with low effort", got)
	}
}

func TestCodexClientForwardsSelectedReasoningEffort(t *testing.T) {
	var got Request
	client := NewCodex("gpt-5.6-luna", WithRunner(func(_ context.Context, request Request) (string, error) {
		got = request
		return `{"content":"done","tool_calls":[]}`, nil
	}))
	if _, err := client.Chat(context.Background(), opencode.ChatRequest{ReasoningEffort: "low"}); err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-5.6-luna" || got.ReasoningEffort != "low" {
		t.Fatalf("runner request = %+v", got)
	}
}

func TestResponseSchemaUsesCodexCompatibleToolArguments(t *testing.T) {
	raw, err := responseSchema([]opencode.ToolSpec{{Name: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	toolCalls := properties["tool_calls"].(map[string]any)
	item := toolCalls["items"].(map[string]any)
	itemProperties := item["properties"].(map[string]any)
	arguments := itemProperties["arguments"].(map[string]any)
	if arguments["type"] != "string" {
		t.Fatalf("arguments schema=%v, want JSON-encoded string", arguments)
	}
}

func TestResponseSchemaForNoToolsOmitsToolCalls(t *testing.T) {
	raw, err := responseSchema(nil)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema["properties"].(map[string]any)["tool_calls"]; ok {
		t.Fatalf("no-tools schema unexpectedly advertises tool_calls: %v", schema)
	}
}

func TestResponseFromJSONAcceptsEncodedToolArguments(t *testing.T) {
	response, err := responseFromJSON(`{"content":"run","tool_calls":[{"name":"bash","arguments":"{\"command\":\"pwd\"}"}]}`, []opencode.ToolSpec{{Name: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := response.ToolCalls[0].Arguments; got != `{"command":"pwd"}` {
		t.Fatalf("arguments=%q", got)
	}
}

func TestResponseFromJSONAllowsNoToolCallsWhenNoToolsAreAdvertised(t *testing.T) {
	response, err := responseFromJSON(`{"content":"OK"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "OK" || len(response.ToolCalls) != 0 {
		t.Fatalf("response=%+v", response)
	}
}

func TestParseCodexModelCatalogUsesModelIDsAndVariants(t *testing.T) {
	catalog, err := parseCodexModelCatalog(json.RawMessage(`{
		"data": [
			{
				"id": "catalog-default",
				"model": "gpt-account-default",
				"hidden": false,
				"isDefault": true,
				"supportedReasoningEfforts": [
					{"reasoningEffort": "low"},
					{"reasoningEffort": "high"},
					{"reasoningEffort": "high"}
				]
			},
			{
				"id": "hidden-model",
				"model": "hidden-model",
				"hidden": true,
				"isDefault": false,
				"supportedReasoningEfforts": []
			}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Default != "gpt-account-default" || len(catalog.Models) != 1 {
		t.Fatalf("catalog = %+v", catalog)
	}
	model := catalog.Models[0]
	if model.ID != "gpt-account-default" || model.Provider != providerCodex {
		t.Fatalf("model = %+v", model)
	}
	if strings.Join(model.Variants, ",") != "low,high" {
		t.Fatalf("variants = %v", model.Variants)
	}
}

func TestParseCodexModelCatalogPrefersLunaLow(t *testing.T) {
	catalog, err := parseCodexModelCatalog(json.RawMessage(`{
		"data": [
			{
				"id": "account-default",
				"model": "gpt-5.6-sol",
				"hidden": false,
				"isDefault": true,
				"supportedReasoningEfforts": [{"reasoningEffort": "medium"}]
			},
			{
				"id": "luna",
				"model": "gpt-5.6-luna",
				"hidden": false,
				"isDefault": false,
				"supportedReasoningEfforts": [{"reasoningEffort": "low"}, {"reasoningEffort": "high"}]
			}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Default != codexPreferredModel || catalog.DefaultVariant != codexPreferredVariant {
		t.Fatalf("catalog defaults = model:%q variant:%q", catalog.Default, catalog.DefaultVariant)
	}
}
