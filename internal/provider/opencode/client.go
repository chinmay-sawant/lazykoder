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
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the OpenCode Go API base used for model-route fallback.
	DefaultBaseURL = "https://opencode.ai/zen/go/v1"
	DefaultModelID = "deepseek-v4-flash"

	ProviderGo  = "opencode go"
	ProviderZen = "opencode zen"

	// maxResponseBytes caps how much of a chat response body is read.
	maxResponseBytes = 64 << 20
	// maxErrorBodyLen caps how much of an error body is echoed in errors.
	maxErrorBodyLen = 300
	// DefaultMaxRetries is the number of retries after the initial request.
	DefaultMaxRetries = 5
	// DefaultRetryDelay is the wait between retry attempts.
	DefaultRetryDelay = 10 * time.Second
)

// ChatURL returns the OpenAI-compatible chat-completions URL for an API base.
func ChatURL(base string) string {
	return strings.TrimSuffix(base, "/") + "/chat/completions"
}

// ResponsesURL returns the OpenAI Responses URL for an API base.
func ResponsesURL(base string) string {
	return strings.TrimSuffix(base, "/") + "/responses"
}

// Route is the endpoint and provider identity selected for a model.
type Route struct {
	Endpoint string
	Provider string
}

// ZenBaseURL maps an OpenCode Go base (.../zen/go/v1) to the Zen sibling
// (.../zen/v1). Non-Go bases return false so tests and other providers skip it.
func ZenBaseURL(goBase string) (string, bool) {
	if !strings.Contains(goBase, "/zen/go/") {
		return "", false
	}
	return strings.Replace(goBase, "/zen/go/", "/zen/", 1), true
}

// ZenChatURL is the Zen chat-completions URL derived from a Go API base.
func ZenChatURL(goBase string) (string, bool) {
	zen, ok := ZenBaseURL(goBase)
	if !ok {
		return "", false
	}
	return ChatURL(zen), true
}

// ChatURLForModel picks the chat URL for a model id when models.json has no
// stored endpoint. Free Zen models go to the Zen sibling; others use base.
func ChatURLForModel(base, id string) string {
	return RouteForModel(base, id).Endpoint
}

// RouteForModel chooses a chat route for an OpenCode model id. Free models use
// the Zen sibling when the base is an OpenCode Go route.
func RouteForModel(base, id string) Route {
	if isFreeModelID(id) {
		if endpoint, ok := ZenChatURL(base); ok {
			return Route{Endpoint: endpoint, Provider: ProviderZen}
		}
	}
	if isResponsesModel(id) {
		return Route{Endpoint: ResponsesURL(base), Provider: ProviderGo}
	}
	return Route{Endpoint: ChatURL(base), Provider: ProviderGo}
}

// RouteForCatalogProvider turns a models.dev provider key into the matching
// OpenCode route. Unknown keys have no OpenCode route.
func RouteForCatalogProvider(base, provider string) (Route, bool) {
	return RouteForCatalogModel(base, provider, "")
}

// RouteForCatalogModel turns a models.dev provider and model id into the
// matching OpenCode route. The model id is used only for protocol capabilities
// that differ within the same provider.
func RouteForCatalogModel(base, provider, id string) (Route, bool) {
	switch provider {
	case "opencode-go":
		return RouteForModel(base, id), true
	case "opencode":
		endpoint, ok := ZenChatURL(base)
		if !ok {
			return Route{}, false
		}
		return Route{Endpoint: endpoint, Provider: ProviderZen}, true
	default:
		return Route{}, false
	}
}

func isFreeModelID(id string) bool {
	return strings.HasSuffix(id, "-free") || id == "big-pickle"
}

// OpenCode's Go gateway exposes Responses for this model family while the
// remaining Go catalog continues to use chat completions. Keep this protocol
// capability in one route table so callers do not grow model-specific checks.
var responsesModels = map[string]struct{}{
	"gpt-5.6-luna": {},
}

func isResponsesModel(id string) bool {
	_, ok := responsesModels[strings.TrimSpace(id)]
	return ok
}

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

// RetryPolicy controls retries for transient chat API failures. MaxRetries is
// in addition to the initial request, so the default permits up to six total
// attempts. Wait is optional and is primarily useful for deterministic tests.
type RetryPolicy struct {
	MaxRetries int
	Delay      time.Duration
	Wait       func(context.Context, time.Duration) error
}

// DefaultRetryPolicy returns the built-in transient failure policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxRetries: DefaultMaxRetries, Delay: DefaultRetryDelay}
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxRetries < 0 {
		p.MaxRetries = 0
	}
	if p.Delay < 0 {
		p.Delay = 0
	}
	return p
}

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

// WithRetryPolicy sets the transient chat failure retry policy.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *Client) { c.retryPolicy = policy.normalized() }
}

// Client talks to the OpenCode Go API.
type Client struct {
	apiKey      string
	baseURL     string
	model       string
	httpClient  *http.Client
	retryMu     sync.RWMutex
	retryPolicy RetryPolicy
}

// NewClient returns a Client with the given API key and options.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:      apiKey,
		baseURL:     DefaultBaseURL,
		model:       DefaultModelID,
		httpClient:  http.DefaultClient,
		retryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// RetryPolicy returns a snapshot of the active transient failure policy.
func (c *Client) RetryPolicy() RetryPolicy {
	c.retryMu.RLock()
	defer c.retryMu.RUnlock()
	return c.retryPolicy
}

// SetRetryPolicy changes the transient failure policy for future chat calls.
func (c *Client) SetRetryPolicy(policy RetryPolicy) {
	c.retryMu.Lock()
	c.retryPolicy = policy.normalized()
	c.retryMu.Unlock()
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
	Role string `json:"role"` // "system" | "user" | "assistant" | "tool"
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
	// Endpoint is the full chat-completions URL. Empty uses the client base.
	// Stored per model in models.json so Go, Zen, and later OpenAI-style
	// providers can share this client.
	Endpoint string
	// ReasoningEffort is the selected model variant (low, medium, high).
	// Empty omits the field so the provider default applies.
	ReasoningEffort string
	Messages        []Message
	Tools           []ToolSpec // nil/empty = no tools advertised; the tools key is omitted
	// MaxTokens caps completion tokens when the provider honors it.
	MaxTokens int
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
	Model           string             `json:"model"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	Messages        []Message          `json:"messages"`
	Tools           []wireToolSpec     `json:"tools,omitempty"`
	Stream          bool               `json:"stream,omitempty"`
	StreamOptions   *wireStreamOptions `json:"stream_options,omitempty"`
	MaxTokens       int                `json:"max_tokens,omitempty"`
}

type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
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
	Message      wireResponseMessage `json:"message"`
	Delta        wireStreamDelta     `json:"delta"`
	FinishReason string              `json:"finish_reason"`
}

type wireStreamDelta struct {
	Content          string               `json:"content"`
	Reasoning        string               `json:"reasoning"`
	ReasoningContent string               `json:"reasoning_content"`
	ToolCalls        []wireStreamToolCall `json:"tool_calls"`
}

type wireStreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func usageFromWire(u *wireUsage) *Usage {
	if u == nil {
		return nil
	}
	cachedDetails := int64(0)
	if u.PromptTokensDetails != nil {
		cachedDetails = u.PromptTokensDetails.CachedTokens
	}
	return &Usage{
		TokensTotal:     u.TotalTokens,
		TokensInput:     firstPositive(u.PromptTokens, u.InputTokens),
		TokensOutput:    u.CompletionTokens,
		TokensReasoning: u.ReasoningTokens,
		TokensCacheRead: firstPositive(
			u.CacheReadTokens,
			u.CacheRead,
			u.CacheHitTokens,
			u.CacheHit,
			u.PromptCacheHitTokens,
			cachedDetails,
		),
		TokensCacheWrite: firstPositive(u.CacheWriteTokens, u.CacheWrite),
		Cost:             u.Cost,
	}
}

func chatResponseFromWire(wire wireResponse) *ChatResponse {
	out := &ChatResponse{}
	if len(wire.Choices) > 0 {
		ch := wire.Choices[0]
		m := ch.Message
		out.Content = m.Content
		out.Reasoning = firstNonEmpty(m.Reasoning, m.ReasoningContent)
		out.FinishReason = firstNonEmpty(ch.FinishReason, m.FinishReason)
		out.ToolCalls = m.ToolCalls
	}
	out.Usage = usageFromWire(wire.Usage)
	return out
}

func normalizeMessages(messages []Message) []Message {
	out := append([]Message(nil), messages...)
	pending := make([]string, 0)
	seq := 0
	for i := range out {
		if out[i].Role == "assistant" {
			for j := range out[i].ToolCalls {
				if strings.TrimSpace(out[i].ToolCalls[j].ID) == "" {
					seq++
					out[i].ToolCalls[j].ID = fmt.Sprintf("call_lazykoder_%d", seq)
				}
				pending = append(pending, out[i].ToolCalls[j].ID)
			}
		}
		if out[i].Role == "tool" && strings.TrimSpace(out[i].ToolCallID) == "" && len(pending) > 0 {
			out[i].ToolCallID = pending[0]
			pending = pending[1:]
		}
	}
	return out
}

func (c *Client) postChat(ctx context.Context, req ChatRequest, stream bool) (*http.Response, error) {
	model := c.model
	if req.Model != "" {
		model = req.Model
	}
	payload := wireRequest{
		Model:           model,
		ReasoningEffort: req.ReasoningEffort,
		Messages:        normalizeMessages(req.Messages),
		MaxTokens:       req.MaxTokens,
	}
	if stream {
		payload.Stream = true
		payload.StreamOptions = &wireStreamOptions{IncludeUsage: true}
	}
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
	policy := c.RetryPolicy()
	for attempt := 0; ; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(req), bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("opencode: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		resp, err := c.HTTP().Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("opencode: chat request failed: %w", err)
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return resp, nil
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyLen))
		_ = resp.Body.Close()
		if retryableChatFailure(resp.StatusCode, body) && attempt < policy.MaxRetries {
			if err := waitForRetry(ctx, policy); err != nil {
				return nil, fmt.Errorf("opencode: chat retry wait: %w", err)
			}
			continue
		}
		return nil, statusError("chat request", resp.StatusCode, bytes.NewReader(body))
	}
}

func retryableChatFailure(status int, body []byte) bool {
	if status != http.StatusInternalServerError && status != http.StatusServiceUnavailable {
		return false
	}
	return !authenticationFailureBody(body)
}

func authenticationFailureBody(body []byte) bool {
	text := strings.ToLower(string(body))
	for _, marker := range []string{
		"authentication",
		"unauthorized",
		"invalid api key",
		"invalid_api_key",
		"api key",
		"access token",
		"invalid token",
		"forbidden",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func waitForRetry(ctx context.Context, policy RetryPolicy) error {
	if policy.Wait != nil {
		return policy.Wait(ctx, policy.Delay)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if policy.Delay <= 0 {
		return nil
	}
	timer := time.NewTimer(policy.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Chat POSTs <base>/chat/completions with Authorization: Bearer <key>.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if isResponsesEndpoint(c.chatURL(req)) {
		return c.chatResponses(ctx, req)
	}
	resp, err := c.postChat(ctx, req, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var wire wireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&wire); err != nil {
		return nil, fmt.Errorf("opencode: decode response: %w", err)
	}
	return chatResponseFromWire(wire), nil
}

func (c *Client) chatURL(req ChatRequest) string {
	if req.Endpoint != "" {
		return strings.TrimSuffix(req.Endpoint, "/")
	}
	return ChatURL(c.baseURL)
}

// ModelInfo is one entry from GET /models.
type ModelInfo struct {
	ID             string
	Provider       string
	Endpoint       string // full chat-completions URL for this model
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
		info := m.info()
		route := RouteForModel(c.baseURL, m.ID)
		info.Endpoint = route.Endpoint
		info.Provider = route.Provider
		out = append(out, info)
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
	endpoint, _ := ZenChatURL(c.baseURL)
	for _, m := range wire.Data {
		if m.ID == "" || !m.isFree() {
			continue
		}
		info := m.info()
		info.Endpoint = endpoint
		info.Provider = ProviderZen
		out = append(out, info)
	}
	return out, nil
}

func zenModelsURL(base string) (string, bool) {
	zen, ok := ZenBaseURL(base)
	if !ok {
		return "", false
	}
	return zen + "/models", true
}

// BillingWindow is one billing window's consumption reported by the OpenCode Go
// API. Percent is a 0-100 floor of usage against the plan limit; ResetsAt is
// the RFC3339 time the window rolls over. RateLimited is true at 100%.
type BillingWindow struct {
	Status      string // "ok" | "rate-limited"
	Percent     int    // 0-100
	ResetsAt    time.Time
	RateLimited bool
}

// BillingUsage is the parsed GET <base>/usage response. The OpenCode Go API
// tracks three windows: a rolling window (the "hourly-like" rollingUsage bucket),
// weekly, and monthly. Any window missing from the payload stays zero.
type BillingUsage struct {
	Rolling BillingWindow
	Weekly  BillingWindow
	Monthly BillingWindow
}

type wireBillingWindow struct {
	Status   string `json:"status"`
	Percent  int    `json:"percent"`
	ResetsAt string `json:"resetsAt"`
}

type wireBillingUsage struct {
	Usage struct {
		Rolling *wireBillingWindow `json:"rolling"`
		Weekly  *wireBillingWindow `json:"weekly"`
		Monthly *wireBillingWindow `json:"monthly"`
	} `json:"usage"`
}

// Usage GETs <base>/usage and returns the rolling, weekly, and monthly
// billing windows. A non-Go base URL returns an empty Usage without error so
// tests and future providers can gate on the endpoint existing.
func (c *Client) Usage(ctx context.Context) (BillingUsage, error) {
	if !strings.Contains(c.baseURL, "/zen/go/") && !strings.Contains(c.baseURL, "/zen/") {
		return BillingUsage{}, nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/usage", nil)
	if err != nil {
		return BillingUsage{}, fmt.Errorf("opencode: build usage request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return BillingUsage{}, fmt.Errorf("opencode: usage request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return BillingUsage{}, statusError("usage request", resp.StatusCode, resp.Body)
	}
	var wire wireBillingUsage
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return BillingUsage{}, fmt.Errorf("opencode: decode usage response: %w", err)
	}
	out := BillingUsage{}
	if wire.Usage.Rolling != nil {
		out.Rolling = billingWindowFromWire(wire.Usage.Rolling)
	}
	if wire.Usage.Weekly != nil {
		out.Weekly = billingWindowFromWire(wire.Usage.Weekly)
	}
	if wire.Usage.Monthly != nil {
		out.Monthly = billingWindowFromWire(wire.Usage.Monthly)
	}
	return out, nil
}

func billingWindowFromWire(w *wireBillingWindow) BillingWindow {
	out := BillingWindow{Status: w.Status, Percent: w.Percent}
	out.RateLimited = w.Status == "rate-limited"
	if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
		out.ResetsAt = t
	}
	return out
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
	return isFreeModelID(m.ID)
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
