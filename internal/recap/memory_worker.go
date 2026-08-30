package recap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/prompts"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

const defaultMemoryMaxTokens = 8_000

var (
	memoryRepairInstruction = prompts.Must("recap/memory-repair.md")
)

// MemoryWorker performs one no-tools update of the project memory aggregate.
// It does not create a session, persist provider rows, or emit agent events.
type MemoryWorker struct {
	Client    Client
	Model     string
	Endpoint  string
	Variant   string
	MaxTokens int
	Workdir   string
}

// NewMemoryWorker resolves the configured model endpoint from the cached
// catalog, falling back to the provider default when it is empty.
func NewMemoryWorker(client Client, model string, info modelscache.Info, variant string) MemoryWorker {
	return MemoryWorker{
		Client:   client,
		Model:    model,
		Endpoint: info.Endpoint,
		Variant:  variant,
	}
}

// Generate requests one strict memory envelope from bounded local evidence.
func (w MemoryWorker) Generate(ctx context.Context, snapshot Snapshot, document MemoryDocument, relatedRecaps string) (MemoryEnvelope, error) {
	envelope, err := w.generate(ctx, snapshot, document, relatedRecaps, nil)
	return envelope, err
}

// GenerateWithTimings requests one memory envelope and returns provider-stage
// durations in microseconds for the durable memory update ledger.
func (w MemoryWorker) GenerateWithTimings(ctx context.Context, snapshot Snapshot, document MemoryDocument, relatedRecaps string) (MemoryEnvelope, map[string]int64, error) {
	timings := make(map[string]int64)
	envelope, err := w.generate(ctx, snapshot, document, relatedRecaps, timings)
	return envelope, timings, err
}

func (w MemoryWorker) generate(ctx context.Context, snapshot Snapshot, document MemoryDocument, relatedRecaps string, timings map[string]int64) (MemoryEnvelope, error) {
	if err := requireContext(ctx); err != nil {
		return MemoryEnvelope{}, err
	}
	if w.Client == nil {
		return MemoryEnvelope{}, errors.New("memory: worker client is required")
	}
	if strings.TrimSpace(w.Model) == "" {
		return MemoryEnvelope{}, errors.New("memory: worker model is required")
	}
	if len(snapshot.Messages) < memoryMinimumMessageCount {
		return MemoryEnvelope{}, ErrInsufficientMessages
	}
	prompt, err := BuildMemoryPromptFor(w.Workdir, snapshot, document, relatedRecaps)
	if err != nil {
		return MemoryEnvelope{}, err
	}
	maxTokens := w.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMemoryMaxTokens
	}
	chat := func(requestPrompt string) (*opencode.ChatResponse, error) {
		return w.Client.Chat(ctx, opencode.ChatRequest{
			Model:           w.Model,
			Endpoint:        w.Endpoint,
			ReasoningEffort: w.Variant,
			Messages: []opencode.Message{
				{Role: "system", Content: prompts.New(w.Workdir).Must("recap/memory-system.md")},
				{Role: "user", Content: requestPrompt},
			},
			MaxTokens: maxTokens,
			PromptDir: prompts.New(w.Workdir).Dir(),
		})
	}
	parseResponse := func(response *opencode.ChatResponse, recoverRecentContext bool) (MemoryEnvelope, error) {
		if response == nil {
			return MemoryEnvelope{}, errors.New("memory: provider returned no response")
		}
		if len(response.ToolCalls) != 0 || response.FinishReason == "tool-calls" {
			return MemoryEnvelope{}, errors.New("memory: provider requested tools")
		}
		if response.FinishReason != "" && response.FinishReason != "stop" && response.FinishReason != "length" {
			return MemoryEnvelope{}, fmt.Errorf("memory: provider finish reason %q", response.FinishReason)
		}
		if strings.TrimSpace(response.Content) == "" {
			return MemoryEnvelope{}, errors.New("memory: provider returned empty content")
		}
		// If the provider truncated due to max_tokens, try to salvage the JSON
		// by closing open brackets so a partial envelope can still be recovered.
		content := response.Content
		if response.FinishReason == "length" {
			if fixed := fixTruncatedJSON(content); fixed != content {
				content = fixed
			}
		}
		if recoverRecentContext {
			return parseMemoryEnvelopeWithRecentContextRecovery([]byte(content), snapshot)
		}
		return ParseMemoryEnvelope([]byte(content), snapshot)
	}
	providerStarted := time.Now()
	response, err := chat(prompt)
	recordMemoryStageDuration(timings, "provider_call", providerStarted)
	if err != nil {
		return MemoryEnvelope{}, fmt.Errorf("memory: provider call: %w", err)
	}
	envelope, err := parseResponse(response, false)
	// Handle length truncation: retry with an explicit instruction to produce
	// a more compact envelope with fewer entries.
	if response != nil && response.FinishReason == "length" && err != nil {
		repairStarted := time.Now()
		retryResponse, retryErr := chat(prompt + prompts.New(w.Workdir).Must("recap/memory-length-repair.md"))
		recordMemoryStageDuration(timings, "length_repair_call", repairStarted)
		if retryErr == nil {
			if retryEnvelope, retryParseErr := parseResponse(retryResponse, false); retryParseErr == nil {
				return ApplyExplicitMemorySignals(retryEnvelope, ExtractMemorySignals(snapshot), snapshot)
			} else if isRepairableRecentContextError(retryParseErr) {
				if retryEnvelope, retryParseErr2 := parseResponse(retryResponse, true); retryParseErr2 == nil {
					return ApplyExplicitMemorySignals(retryEnvelope, ExtractMemorySignals(snapshot), snapshot)
				}
			}
			// If retry still fails, fall through to try fixing original truncated JSON
			if salvage, salvageErr := trySalvageTruncatedMemory(response.Content, snapshot); salvageErr == nil {
				return ApplyExplicitMemorySignals(salvage, ExtractMemorySignals(snapshot), snapshot)
			}
		}
	}
	if isRepairableRecentContextError(err) {
		repairStarted := time.Now()
		response, retryErr := chat(prompt + prompts.New(w.Workdir).Must("recap/memory-repair.md"))
		recordMemoryStageDuration(timings, "repair_call", repairStarted)
		if retryErr != nil {
			return MemoryEnvelope{}, fmt.Errorf("memory: provider repair call: %w", retryErr)
		}
		envelope, err = parseResponse(response, false)
		if isRepairableRecentContextError(err) {
			envelope, err = parseResponse(response, true)
		}
	}
	if err != nil {
		// Final attempt: try to salvage truncated JSON even if not flagged as length
		if response != nil && response.FinishReason == "length" {
			if salvage, salvageErr := trySalvageTruncatedMemory(response.Content, snapshot); salvageErr == nil {
				return ApplyExplicitMemorySignals(salvage, ExtractMemorySignals(snapshot), snapshot)
			}
		}
		return MemoryEnvelope{}, err
	}
	return ApplyExplicitMemorySignals(envelope, ExtractMemorySignals(snapshot), snapshot)
}

func fixTruncatedJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}
	// Count open vs close braces/brackets outside strings
	openBraces := 0
	openBrackets := 0
	inString := false
	escaped := false
	for _, r := range trimmed {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch r {
		case '{':
			openBraces++
		case '}':
			openBraces--
		case '[':
			openBrackets++
		case ']':
			openBrackets--
		}
	}
	if openBraces < 0 {
		openBraces = 0
	}
	if openBrackets < 0 {
		openBrackets = 0
	}
	if openBraces == 0 && openBrackets == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(trimmed)
	// Close any open strings first (unlikely but defensive)
	if inString {
		b.WriteString("\"")
	}
	for i := 0; i < openBrackets; i++ {
		b.WriteString("]")
	}
	for i := 0; i < openBraces; i++ {
		b.WriteString("}")
	}
	return b.String()
}

func trySalvageTruncatedMemory(content string, snapshot Snapshot) (MemoryEnvelope, error) {
	fixed := fixTruncatedJSON(content)
	if fixed == content {
		return MemoryEnvelope{}, errors.New("memory: truncated JSON could not be salvaged")
	}
	// Try strict then with recovery
	if env, err := ParseMemoryEnvelope([]byte(fixed), snapshot); err == nil {
		return env, nil
	}
	return parseMemoryEnvelopeWithRecentContextRecovery([]byte(fixed), snapshot)
}

func recordMemoryStageDuration(timings map[string]int64, name string, started time.Time) {
	if timings == nil {
		return
	}
	setMemoryStageDuration(timings, name, time.Since(started))
}

func setMemoryStageDuration(timings map[string]int64, name string, duration time.Duration) {
	if timings == nil {
		return
	}
	microseconds := duration.Microseconds()
	if microseconds < 1 {
		microseconds = 1
	}
	timings[name] = microseconds
}

func isRepairableRecentContextError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "recent_context ") &&
		(strings.Contains(message, "needs one to") || strings.Contains(message, "cites unknown message"))
}
