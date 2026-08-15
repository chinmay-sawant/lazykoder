// Package opencode implements the OpenCode Go chat-completions client.
package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	defaultBaseURL = "https://opencode.ai/zen/go/v1"
	defaultModel   = "deepseek-v4-flash"

	// maxResponseBytes caps how much of a chat response body is read.
	maxResponseBytes = 64 << 20
	// maxErrorBodyLen caps how much of an error body is echoed in errors.
	maxErrorBodyLen = 300
)

// ErrMissingAPIKey is returned when no API key is available from the environment.
var ErrMissingAPIKey = errors.New("opencode: OPENCODE_API_KEY (or OPENCODE_ZEN_API_KEY) is not set")

// APIKeyFromEnv reads OPENCODE_API_KEY, then OPENCODE_ZEN_API_KEY. Returns ErrMissingAPIKey if both empty.
func APIKeyFromEnv() (string, error) {
	for _, name := range []string{"OPENCODE_API_KEY", "OPENCODE_ZEN_API_KEY"} {
		if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", ErrMissingAPIKey
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the default API base URL.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(u, "/") }
}

// WithModel overrides the default model id.
func WithModel(m string) Option {
	return func(c *Client) { c.model = m }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// Client talks to the OpenCode Go API.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewClient returns a Client with the given API key and options.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		model:      defaultModel,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Model returns the model id in effect (the override if set, else the default).
func (c *Client) Model() string {
	return c.model
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID        string // call id, echoed back by tool messages
	Name      string // tool name
	Arguments string // raw JSON object string, e.g. `{"command":"ls"}`
}

// MarshalJSON emits the OpenAI wire shape with the function wrapper.
func (t ToolCall) MarshalJSON() ([]byte, error) {
	wire := struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}{
		ID:   t.ID,
		Type: "function",
	}
	wire.Function.Name = t.Name
	wire.Function.Arguments = t.Arguments
	return json.Marshal(wire)
}

// UnmarshalJSON parses the OpenAI wire shape for a tool call.
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	wire := struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	t.ID = wire.ID
	t.Name = wire.Function.Name
	t.Arguments = wire.Function.Arguments
	return nil
}

// Message is a single chat message in OpenAI-compatible wire format.
type Message struct {
	Role       string     `json:"role"` // "user" | "assistant" | "tool"
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set for role "tool"
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // set for role "assistant" when the model asks for tools
}

// ToolSpec advertises a callable tool to the model.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON schema; may be nil
}

// ChatRequest is a single chat-completions request.
type ChatRequest struct {
	// Model overrides the client default model id when non-empty.
	Model    string
	Messages []Message
	Tools    []ToolSpec // nil/empty = no tools advertised; the tools key is omitted
}

// Usage reports token counts and cost from the API.
type Usage struct {
	TokensTotal      int64
	TokensInput      int64
	TokensOutput     int64
	TokensReasoning  int64
	TokensCacheRead  int64
	TokensCacheWrite int64
	Cost             float64
}

// ChatResponse is the parsed chat-completions result.
type ChatResponse struct {
	Content      string
	Reasoning    string // empty when the API did not return reasoning
	FinishReason string // "stop" | "tool-calls" | ...
	ToolCalls    []ToolCall
	Usage        *Usage // nil when the API did not return usage
}

type wireRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Tools    []wireToolSpec `json:"tools,omitempty"`
}

type wireToolSpec struct {
	Type     string         `json:"type"`
	Function wireToolSpecFn `json:"function"`
}

type wireToolSpecFn struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type wireResponse struct {
	Choices []wireChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage"`
}

type wireChoice struct {
	Message wireResponseMessage `json:"message"`
}

type wireResponseMessage struct {
	Content          string     `json:"content"`
	Reasoning        string     `json:"reasoning"`
	ReasoningContent string     `json:"reasoning_content"`
	FinishReason     string     `json:"finish_reason"`
	ToolCalls        []ToolCall `json:"tool_calls"`
}

type wireUsage struct {
	TotalTokens      int64   `json:"total_tokens"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheRead        int64   `json:"cache_read"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CacheWrite       int64   `json:"cache_write"`
	Cost             float64 `json:"cost"`
}

// Chat POSTs <base>/chat/completions with Authorization: Bearer <key>.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := c.model
	if req.Model != "" {
		model = req.Model
	}
	payload := wireRequest{Model: model, Messages: req.Messages}
	for _, t := range req.Tools {
		payload.Tools = append(payload.Tools, wireToolSpec{
			Type:     "function",
			Function: wireToolSpecFn(t),
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("opencode: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("opencode: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("opencode: chat request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, statusError("chat request", resp.StatusCode, resp.Body)
	}
	var wire wireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&wire); err != nil {
		return nil, fmt.Errorf("opencode: decode response: %w", err)
	}
	out := &ChatResponse{}
	if len(wire.Choices) > 0 {
		m := wire.Choices[0].Message
		out.Content = m.Content
		out.Reasoning = m.Reasoning
		if out.Reasoning == "" {
			out.Reasoning = m.ReasoningContent
		}
		out.FinishReason = m.FinishReason
		out.ToolCalls = m.ToolCalls
	}
	if wire.Usage != nil {
		cacheRead := wire.Usage.CacheReadTokens
		if cacheRead == 0 {
			cacheRead = wire.Usage.CacheRead
		}
		cacheWrite := wire.Usage.CacheWriteTokens
		if cacheWrite == 0 {
			cacheWrite = wire.Usage.CacheWrite
		}
		out.Usage = &Usage{
			TokensTotal:      wire.Usage.TotalTokens,
			TokensInput:      wire.Usage.PromptTokens,
			TokensOutput:     wire.Usage.CompletionTokens,
			TokensReasoning:  wire.Usage.ReasoningTokens,
			TokensCacheRead:  cacheRead,
			TokensCacheWrite: cacheWrite,
			Cost:             wire.Usage.Cost,
		}
	}
	return out, nil
}

// ModelInfo is one entry from GET /models.
type ModelInfo struct {
	ID      string
	Context int
}

// Models GETs <base>/models and returns the model ids.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	infos, err := c.ModelInfos(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(infos))
	for _, m := range infos {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// ModelInfos GETs <base>/models and returns ids plus any context window the
// payload advertises (context_window, context_length, limit.context, ...).
func (c *Client) ModelInfos(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("opencode: build models request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("opencode: models request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, statusError("models request", resp.StatusCode, resp.Body)
	}
	var wire struct {
		Data []wireModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("opencode: decode models response: %w", err)
	}
	out := make([]ModelInfo, 0, len(wire.Data))
	for _, m := range wire.Data {
		if m.ID == "" {
			continue
		}
		out = append(out, ModelInfo{ID: m.ID, Context: m.contextWindow()})
	}
	return out, nil
}

type wireModel struct {
	ID             string `json:"id"`
	ContextWindow  int    `json:"context_window"`
	ContextLength  int    `json:"context_length"`
	MaxContext     int    `json:"max_context"`
	MaxInputTokens int    `json:"max_input_tokens"`
	Context        int    `json:"context"`
	Limit          struct {
		Context int `json:"context"`
	} `json:"limit"`
	Info struct {
		Context int `json:"context"`
	} `json:"info"`
}

func (m wireModel) contextWindow() int {
	for _, n := range []int{m.ContextWindow, m.ContextLength, m.MaxContext, m.MaxInputTokens, m.Context, m.Limit.Context, m.Info.Context} {
		if n > 0 {
			return n
		}
	}
	return 0
}

func statusError(kind string, status int, body io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBodyLen))
	if err != nil {
		return fmt.Errorf("opencode: %s failed: status %d", kind, status)
	}
	if snippet := strings.TrimSpace(string(raw)); snippet != "" {
		return fmt.Errorf("opencode: %s failed: status %d: %s", kind, status, snippet)
	}
	return fmt.Errorf("opencode: %s failed: status %d", kind, status)
}
