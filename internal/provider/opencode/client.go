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
	"sort"
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

// BaseURL returns the API base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// HTTP returns the HTTP client used for API calls.
func (c *Client) HTTP() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
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
	Role string `json:"role"` // "user" | "assistant" | "tool"
	// Content is always sent. The Go API rejects messages that omit the
	// field, including assistant tool-call turns with an empty body.
	Content    string     `json:"content"`
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
	Model string
	// ReasoningEffort is the selected model variant (low, medium, high).
	// Empty omits the field so the provider default applies.
	ReasoningEffort string
	Messages        []Message
	Tools           []ToolSpec // nil/empty = no tools advertised; the tools key is omitted
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
	Model           string         `json:"model"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Messages        []Message      `json:"messages"`
	Tools           []wireToolSpec `json:"tools,omitempty"`
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
	TotalTokens           int64 `json:"total_tokens"`
	PromptTokens          int64 `json:"prompt_tokens"`
	InputTokens           int64 `json:"input_tokens"`
	CompletionTokens      int64 `json:"completion_tokens"`
	ReasoningTokens       int64 `json:"reasoning_tokens"`
	CacheReadTokens       int64 `json:"cache_read_tokens"`
	CacheRead             int64 `json:"cache_read"`
	CacheHitTokens        int64 `json:"cache_hit_tokens"`
	CacheHit              int64 `json:"cache_hit"`
	PromptCacheHitTokens  int64 `json:"prompt_cache_hit_tokens"`
	CacheWriteTokens      int64 `json:"cache_write_tokens"`
	CacheWrite            int64 `json:"cache_write"`
	CacheMissTokens       int64 `json:"cache_miss_tokens"`
	CacheMiss             int64 `json:"cache_miss"`
	PromptCacheMissTokens int64 `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	Cost float64 `json:"cost"`
}

func firstPositive(vals ...int64) int64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// Chat POSTs <base>/chat/completions with Authorization: Bearer <key>.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := c.model
	if req.Model != "" {
		model = req.Model
	}
	payload := wireRequest{Model: model, ReasoningEffort: req.ReasoningEffort, Messages: req.Messages}
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
		cachedDetails := int64(0)
		if wire.Usage.PromptTokensDetails != nil {
			cachedDetails = wire.Usage.PromptTokensDetails.CachedTokens
		}
		cacheRead := firstPositive(
			wire.Usage.CacheReadTokens,
			wire.Usage.CacheRead,
			wire.Usage.CacheHitTokens,
			wire.Usage.CacheHit,
			wire.Usage.PromptCacheHitTokens,
			cachedDetails,
		)
		cacheWrite := firstPositive(wire.Usage.CacheWriteTokens, wire.Usage.CacheWrite)
		input := firstPositive(wire.Usage.PromptTokens, wire.Usage.InputTokens)
		out.Usage = &Usage{
			TokensTotal:      wire.Usage.TotalTokens,
			TokensInput:      input,
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
	ID             string
	Context        int
	InputPerM      float64
	OutputPerM     float64
	CacheReadPerM  float64
	CacheWritePerM float64
	Variants       []string
	Free           bool
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
		out = append(out, m.info())
	}
	return out, nil
}

// FreeModelInfos GETs the Zen models list (sibling of the Go endpoint) and
// returns only the free models. A non-Go base URL returns nil without error.
func (c *Client) FreeModelInfos(ctx context.Context) ([]ModelInfo, error) {
	url, ok := zenModelsURL(c.baseURL)
	if !ok {
		return nil, nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("opencode: build free models request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("opencode: free models request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, statusError("free models request", resp.StatusCode, resp.Body)
	}
	var wire struct {
		Data []wireModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("opencode: decode free models response: %w", err)
	}
	out := make([]ModelInfo, 0)
	for _, m := range wire.Data {
		if m.ID == "" || !m.isFree() {
			continue
		}
		out = append(out, m.info())
	}
	return out, nil
}

func zenModelsURL(base string) (string, bool) {
	if !strings.Contains(base, "/zen/go/") {
		return "", false
	}
	return strings.Replace(base, "/zen/go/", "/zen/", 1) + "/models", true
}

type wireModel struct {
	ID             string          `json:"id"`
	ContextWindow  int             `json:"context_window"`
	ContextLength  int             `json:"context_length"`
	MaxContext     int             `json:"max_context"`
	MaxInputTokens int             `json:"max_input_tokens"`
	Context        int             `json:"context"`
	Variants       json.RawMessage `json:"variants"`
	Limit          struct {
		Context int     `json:"context"`
		Input   float64 `json:"input"`
		Output  float64 `json:"output"`
	} `json:"limit"`
	Info struct {
		Context int `json:"context"`
	} `json:"info"`
	Pricing *struct {
		Input       float64 `json:"input"`
		Prompt      float64 `json:"prompt"`
		Output      float64 `json:"output"`
		Completion  float64 `json:"completion"`
		CacheRead   float64 `json:"cache_read"`
		CachedRead  float64 `json:"cached_read"`
		CacheWrite  float64 `json:"cache_write"`
		CachedWrite float64 `json:"cached_write"`
	} `json:"pricing"`
	Cost *struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
}

func (m wireModel) info() ModelInfo {
	return ModelInfo{
		ID:             m.ID,
		Context:        m.contextWindow(),
		InputPerM:      m.inputPrice(),
		OutputPerM:     m.outputPrice(),
		CacheReadPerM:  m.cacheReadPrice(),
		CacheWritePerM: m.cacheWritePrice(),
		Variants:       m.variants(),
		Free:           m.isFree(),
	}
}

func (m wireModel) isFree() bool {
	return strings.HasSuffix(m.ID, "-free") || m.ID == "big-pickle"
}

func (m wireModel) contextWindow() int {
	for _, n := range []int{m.ContextWindow, m.ContextLength, m.MaxContext, m.MaxInputTokens, m.Context, m.Limit.Context, m.Info.Context} {
		if n > 0 {
			return n
		}
	}
	return 0
}

func (m wireModel) inputPrice() float64 {
	if m.Pricing != nil {
		if m.Pricing.Input > 0 {
			return m.Pricing.Input
		}
		if m.Pricing.Prompt > 0 {
			return m.Pricing.Prompt
		}
	}
	if m.Cost != nil && m.Cost.Input > 0 {
		return m.Cost.Input
	}
	if m.Limit.Input > 0 {
		return m.Limit.Input
	}
	return 0
}

func (m wireModel) outputPrice() float64 {
	if m.Pricing != nil {
		if m.Pricing.Output > 0 {
			return m.Pricing.Output
		}
		if m.Pricing.Completion > 0 {
			return m.Pricing.Completion
		}
	}
	if m.Cost != nil && m.Cost.Output > 0 {
		return m.Cost.Output
	}
	if m.Limit.Output > 0 {
		return m.Limit.Output
	}
	return 0
}

func (m wireModel) cacheReadPrice() float64 {
	if m.Pricing != nil {
		if m.Pricing.CacheRead > 0 {
			return m.Pricing.CacheRead
		}
		if m.Pricing.CachedRead > 0 {
			return m.Pricing.CachedRead
		}
	}
	if m.Cost != nil && m.Cost.CacheRead > 0 {
		return m.Cost.CacheRead
	}
	return 0
}

func (m wireModel) cacheWritePrice() float64 {
	if m.Pricing != nil {
		if m.Pricing.CacheWrite > 0 {
			return m.Pricing.CacheWrite
		}
		if m.Pricing.CachedWrite > 0 {
			return m.Pricing.CachedWrite
		}
	}
	if m.Cost != nil && m.Cost.CacheWrite > 0 {
		return m.Cost.CacheWrite
	}
	return 0
}

func (m wireModel) variants() []string {
	if len(m.Variants) == 0 {
		return nil
	}
	var names []string
	if err := json.Unmarshal(m.Variants, &names); err == nil && len(names) > 0 {
		return names
	}
	var obj map[string]any
	if err := json.Unmarshal(m.Variants, &obj); err == nil && len(obj) > 0 {
		for k := range obj {
			names = append(names, k)
		}
		sort.Strings(names)
		return names
	}
	var items []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(m.Variants, &items); err == nil {
		for _, it := range items {
			if it.ID != "" {
				names = append(names, it.ID)
			} else if it.Name != "" {
				names = append(names, it.Name)
			}
		}
	}
	return names
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
