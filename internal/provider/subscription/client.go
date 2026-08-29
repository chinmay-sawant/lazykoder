// Package subscription adapts authenticated Codex and Grok CLIs to the
// provider client contract without exposing their credentials to lazykoder.
package subscription

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

const (
	providerCodex = "codex"
	providerGrok  = "grok"
)

// Runner starts one provider-owned CLI turn. It is a seam for tests and
// deliberately deals in prompt text rather than any provider credential.
type Runner func(context.Context, Request) (string, error)

// ModelCatalog is the set of models currently available to a subscription
// account. Default is the model the provider selected as its live default.
type ModelCatalog struct {
	Models         []opencode.ModelInfo
	Default        string
	DefaultVariant string
}

// CatalogLoader reads the currently available models without starting a model
// turn. It is a test seam for the provider-owned catalog protocol.
type CatalogLoader func(context.Context) (ModelCatalog, error)

// Request is one restricted structured-output CLI invocation.
type Request struct {
	Provider        string
	Model           string
	ReasoningEffort string
	Prompt          string
	Schema          []byte
	Workdir         string
}

// Client runs authenticated subscription providers through their official
// CLIs. It asks the model for lazykoder tool calls but never grants the CLI
// permission to execute those tools itself.
type Client struct {
	mu          sync.RWMutex
	provider    string
	model       string
	variant     string
	autoModel   bool
	runner      Runner
	catalogLoad CatalogLoader
	policy      opencode.RetryPolicy
}

// NewCodex returns a ChatGPT-subscription client backed by the Codex CLI.
func NewCodex(model string, opts ...Option) *Client {
	return newClient(providerCodex, model, codexModelCatalog, opts...)
}

// NewGrok returns a Grok-subscription client backed by the Grok CLI.
func NewGrok(model string, opts ...Option) *Client {
	return newClient(providerGrok, model, grokModelCatalog, opts...)
}

func newClient(providerName, model string, catalogLoad CatalogLoader, opts ...Option) *Client {
	client := &Client{
		provider:    providerName,
		model:       strings.TrimSpace(model),
		autoModel:   providerName == providerCodex && strings.TrimSpace(model) == "",
		runner:      runCLI,
		catalogLoad: catalogLoad,
		policy:      opencode.DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// Option configures a subscription client.
type Option func(*Client)

// WithRunner replaces the CLI process runner. It is intended for tests.
func WithRunner(runner Runner) Option {
	return func(client *Client) {
		if runner != nil {
			client.runner = runner
		}
	}
}

// WithCatalogLoader replaces the subscription model catalog loader. It is
// intended for tests and for provider-owned catalog integrations.
func WithCatalogLoader(loader CatalogLoader) Option {
	return func(client *Client) {
		if loader != nil {
			client.catalogLoad = loader
		}
	}
}

// Chat requests a complete structured answer.
func (client *Client) Chat(ctx context.Context, request opencode.ChatRequest) (*opencode.ChatResponse, error) {
	return client.ChatStream(ctx, request, func(opencode.Delta) error { return nil })
}

// ChatStream emits the final structured answer as one delta. The provider CLI
// owns token streaming, but the constrained final object is what preserves
// lazykoder's tool confirmation and transcript contract.
func (client *Client) ChatStream(
	ctx context.Context,
	request opencode.ChatRequest,
	onDelta func(opencode.Delta) error,
) (*opencode.ChatResponse, error) {
	schema, err := responseSchema(request.Tools)
	if err != nil {
		return nil, err
	}
	requestedModel := strings.TrimSpace(request.Model)
	model := requestedModel
	if model == "" {
		model = client.Model()
	}
	reasoningEffort := strings.TrimSpace(request.ReasoningEffort)
	if reasoningEffort == "" && requestedModel == "" {
		reasoningEffort = client.DefaultVariant()
	}
	raw, err := client.runner(ctx, Request{
		Provider:        client.provider,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		Prompt:          promptFor(request),
		Schema:          schema,
	})
	if err != nil {
		return nil, fmt.Errorf("%s subscription: %w", client.provider, err)
	}
	response, err := responseFromJSON(raw, request.Tools)
	if err != nil {
		return nil, fmt.Errorf("%s subscription: %w", client.provider, err)
	}
	if err := onDelta(opencode.Delta{
		Content:      response.Content,
		FinishReason: response.FinishReason,
		Usage:        response.Usage,
	}); err != nil {
		return nil, err
	}
	return response, nil
}

// Model returns the provider's configured default model.
func (client *Client) Model() string {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.model
}

// DefaultVariant returns the live catalog's recommended reasoning effort for
// the provider default model. It is empty when the provider did not supply one.
func (client *Client) DefaultVariant() string {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.variant
}

// BaseURL identifies a non-HTTP provider route for model and runtime checks.
func (client *Client) BaseURL() string { return "cli://" + client.provider }

// HTTP returns the default client to satisfy the shared provider contract.
func (client *Client) HTTP() *http.Client { return http.DefaultClient }

// RetryPolicy returns the stored policy. CLI authentication and retries remain
// provider-owned, so this value does not cause duplicate CLI turns.
func (client *Client) RetryPolicy() opencode.RetryPolicy { return client.policy }

// SetRetryPolicy records the application policy for a uniform provider API.
func (client *Client) SetRetryPolicy(policy opencode.RetryPolicy) { client.policy = policy }

// ModelInfos returns the provider's current available model catalog. Codex
// reads the signed-in account catalog from its official local app server.
func (client *Client) ModelInfos(ctx context.Context) ([]opencode.ModelInfo, error) {
	if client.catalogLoad != nil {
		catalog, err := client.catalogLoad(ctx)
		if err != nil {
			return nil, err
		}
		if client.autoModel && catalog.Default != "" {
			client.mu.Lock()
			client.model = catalog.Default
			client.variant = catalog.DefaultVariant
			client.mu.Unlock()
		}
		return catalog.Models, nil
	}
	model := client.Model()
	if model == "" {
		return nil, nil
	}
	return []opencode.ModelInfo{{
		ID:       model,
		Provider: client.provider,
		Endpoint: opencode.ChatURL(client.BaseURL()),
	}}, nil
}

type responseBody struct {
	Content   string         `json:"content"`
	ToolCalls []toolCallBody `json:"tool_calls"`
}

type toolCallBody struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func responseSchema(tools []opencode.ToolSpec) ([]byte, error) {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if name := strings.TrimSpace(tool.Name); name != "" {
			names = append(names, name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"content", "tool_calls"},
		"properties": map[string]any{
			"content": map[string]any{"type": "string"},
			"tool_calls": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"name", "arguments"},
					"properties": map[string]any{
						"name":      map[string]any{"type": "string", "enum": names},
						"arguments": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	if len(names) == 0 {
		properties := schema["properties"].(map[string]any)
		delete(properties, "tool_calls")
		schema["required"] = []string{"content"}
	}
	return json.Marshal(schema)
}

func promptFor(request opencode.ChatRequest) string {
	type transcript struct {
		Messages []opencode.Message  `json:"messages"`
		Tools    []opencode.ToolSpec `json:"tools"`
	}
	body, err := json.Marshal(transcript{Messages: request.Messages, Tools: request.Tools})
	if err != nil {
		return "Unable to encode the lazykoder transcript. Return a short explanation in content and no tool calls."
	}
	return "You are the language-model backend for lazykoder. Do not use your own tools, shell, filesystem, browser, network, subagents, memory, or plan. Read the transcript JSON below and produce only the final object requested by the output schema. Use tool_calls only for tools declared in the transcript. lazykoder, not you, executes every tool call after explicit policy checks. Tool-call arguments must be JSON-encoded object strings.\n\nTranscript:\n" + string(body)
}

func responseFromJSON(raw string, tools []opencode.ToolSpec) (*opencode.ChatResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &fields); err != nil {
		return nil, fmt.Errorf("decode structured response: %w", err)
	}
	if _, ok := fields["content"]; !ok {
		return nil, errors.New("structured response is missing content")
	}
	if len(tools) > 0 {
		if _, ok := fields["tool_calls"]; !ok {
			return nil, errors.New("structured response is missing tool_calls")
		}
	}
	if len(tools) == 0 {
		delete(fields, "tool_calls")
	}
	var body responseBody
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return nil, fmt.Errorf("decode structured response: %w", err)
	}
	allowed := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		allowed[tool.Name] = struct{}{}
	}
	response := &opencode.ChatResponse{Content: body.Content, FinishReason: "stop"}
	for index, call := range body.ToolCalls {
		if _, ok := allowed[call.Name]; !ok {
			return nil, fmt.Errorf("response requested undeclared tool %q", call.Name)
		}
		argumentsRaw, err := decodeArguments(call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("response arguments for %q must be an object: %w", call.Name, err)
		}
		if len(argumentsRaw) == 0 || !json.Valid(argumentsRaw) {
			return nil, fmt.Errorf("response gave invalid arguments for %q", call.Name)
		}
		var arguments map[string]any
		if err := json.Unmarshal(argumentsRaw, &arguments); err != nil {
			return nil, fmt.Errorf("response arguments for %q must be an object", call.Name)
		}
		encoded, err := json.Marshal(arguments)
		if err != nil {
			return nil, fmt.Errorf("encode arguments for %q: %w", call.Name, err)
		}
		response.ToolCalls = append(response.ToolCalls, opencode.ToolCall{
			ID:        fmt.Sprintf("%s-tool-%d", "subscription", index+1),
			Name:      call.Name,
			Arguments: string(encoded),
		})
	}
	if len(response.ToolCalls) > 0 {
		response.FinishReason = "tool-calls"
	}
	return response, nil
}

func decodeArguments(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, errors.New("arguments are empty")
	}
	if raw[0] != '"' {
		return raw, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("decode encoded arguments: %w", err)
	}
	return json.RawMessage(encoded), nil
}

func runCLI(ctx context.Context, request Request) (string, error) {
	switch request.Provider {
	case providerCodex:
		return runCodex(ctx, request)
	case providerGrok:
		return runGrok(ctx, request)
	default:
		return "", errors.New("unknown subscription provider")
	}
}

func runCodex(ctx context.Context, request Request) (string, error) {
	schemaPath, err := writeTemp("lazykoder-codex-schema-*.json", request.Schema)
	if err != nil {
		return "", err
	}
	defer os.Remove(schemaPath)
	outputPath, err := writeTemp("lazykoder-codex-output-*.json", nil)
	if err != nil {
		return "", err
	}
	defer os.Remove(outputPath)
	args := []string{
		"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules",
		"--sandbox", "read-only", "--output-schema", schemaPath,
		"--output-last-message", outputPath, "-",
	}
	if request.Model != "" {
		args = append(args[:1], append([]string{"--model", request.Model}, args[1:]...)...)
	}
	if request.ReasoningEffort != "" {
		args = append(args[:1], append([]string{"--config", `model_reasoning_effort="` + request.ReasoningEffort + `"`}, args[1:]...)...)
	}
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Env = withoutEnv(os.Environ(), "OPENAI_API_KEY")
	cmd.Stdin = strings.NewReader(request.Prompt)
	if request.Workdir != "" {
		cmd.Dir = request.Workdir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", commandError(err, stderr.String())
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read Codex response: %w", err)
	}
	return string(output), nil
}

func runGrok(ctx context.Context, request Request) (string, error) {
	promptPath, err := writeTemp("lazykoder-grok-prompt-*.txt", []byte(request.Prompt))
	if err != nil {
		return "", err
	}
	defer os.Remove(promptPath)
	args := []string{
		"--no-auto-update", "--no-subagents", "--no-plan", "--no-memory",
		"--disable-web-search", "--tools", "", "--max-turns", "1",
		"--permission-mode", "dontAsk", "--json-schema", string(request.Schema),
		"--output-format", "json", "--prompt-file", promptPath,
	}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort", request.ReasoningEffort)
	}
	if request.Workdir != "" {
		args = append(args, "--cwd", request.Workdir)
	}
	cmd := exec.CommandContext(ctx, "grok", args...)
	cmd.Env = withoutEnv(os.Environ(), "XAI_API_KEY")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", commandError(err, stderr.String())
	}
	return string(output), nil
}

func writeTemp(pattern string, body []byte) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err := file.Write(body); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return filepath.Clean(name), nil
}

func commandError(err error, stderr string) error {
	message := strings.TrimSpace(stderr)
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func withoutEnv(env []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
