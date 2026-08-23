package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type responsesRequest struct {
	Model           string            `json:"model"`
	Input           []map[string]any  `json:"input"`
	Tools           []map[string]any  `json:"tools,omitempty"`
	Reasoning       map[string]string `json:"reasoning,omitempty"`
	Stream          bool              `json:"stream"`
	MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
}

type responsesEvent struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	ItemID   string          `json:"item_id"`
	Response responsesResult `json:"response"`
	Item     responsesItem   `json:"item"`
	Error    *responsesError `json:"error"`
	Usage    *responsesUsage `json:"usage"`
}

type responsesResult struct {
	Usage *responsesUsage `json:"usage"`
	Error *responsesError `json:"error"`
}

type responsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responsesItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsesUsage struct {
	InputTokens         int64                       `json:"input_tokens"`
	OutputTokens        int64                       `json:"output_tokens"`
	TotalTokens         int64                       `json:"total_tokens"`
	InputTokensDetails  responsesInputTokenDetails  `json:"input_tokens_details"`
	OutputTokensDetails responsesOutputTokenDetails `json:"output_tokens_details"`
}

type responsesInputTokenDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type responsesOutputTokenDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

func isResponsesEndpoint(endpoint string) bool {
	return strings.HasSuffix(strings.TrimSuffix(endpoint, "/"), "/responses")
}

func (c *Client) chatResponses(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return c.chatResponsesStream(ctx, req, nil)
}

func (c *Client) chatResponsesStream(ctx context.Context, req ChatRequest, onDelta func(Delta) error) (*ChatResponse, error) {
	resp, err := c.postResponses(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	br := bufio.NewReaderSize(resp.Body, streamReaderBytes)
	peek, _ := br.Peek(streamPeek)
	if isEventStream(resp.Header.Get("Content-Type"), peek) {
		return parseResponsesEventStream(ctx, br, onDelta)
	}
	raw, err := io.ReadAll(io.LimitReader(br, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("opencode: read responses body: %w", err)
	}
	return completeResponsesJSON(raw, onDelta)
}

func (c *Client) postResponses(ctx context.Context, req ChatRequest) (*http.Response, error) {
	model := c.model
	if req.Model != "" {
		model = req.Model
	}
	payload := responsesRequest{
		Model:           model,
		Input:           responsesInput(normalizeMessages(req.Messages)),
		Reasoning:       responsesReasoning(req.ReasoningEffort),
		Stream:          true,
		MaxOutputTokens: req.MaxTokens,
	}
	for _, tool := range req.Tools {
		parameters := tool.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		payload.Tools = append(payload.Tools, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  parameters,
			"strict":      false,
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("opencode: marshal responses request: %w", err)
	}
	policy := c.RetryPolicy()
	for attempt := 0; ; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(req), bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("opencode: build responses request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		resp, err := c.HTTP().Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("opencode: responses request failed: %w", err)
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return resp, nil
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyLen))
		_ = resp.Body.Close()
		if retryableChatFailure(resp.StatusCode, body) && attempt < policy.MaxRetries {
			if err := waitForRetry(ctx, policy); err != nil {
				return nil, fmt.Errorf("opencode: responses retry wait: %w", err)
			}
			continue
		}
		return nil, statusError("responses request", resp.StatusCode, bytes.NewReader(body))
	}
}

func responsesReasoning(effort string) map[string]string {
	if strings.TrimSpace(effort) == "" {
		return nil
	}
	return map[string]string{"effort": effort}
}

func responsesInput(messages []Message) []map[string]any {
	input := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "assistant" {
			if message.Content != "" {
				input = append(input, map[string]any{
					"role":    "assistant",
					"content": []map[string]any{{"type": "output_text", "text": message.Content}},
				})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      call.Name,
					"arguments": call.Arguments,
				})
			}
			continue
		}
		if message.Role == "tool" {
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  message.Content,
			})
			continue
		}
		input = append(input, map[string]any{
			"role": message.Role,
			"content": []map[string]any{{
				"type": "input_text",
				"text": message.Content,
			}},
		})
	}
	return input
}

type responsesStreamAcc struct {
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	usage     *Usage
	tools     map[string]*ToolCall
	order     []string
}

func newResponsesStreamAcc() *responsesStreamAcc {
	return &responsesStreamAcc{tools: make(map[string]*ToolCall)}
}

func parseResponsesEventStream(ctx context.Context, reader io.Reader, onDelta func(Delta) error) (*ChatResponse, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, streamBufferBytes), maxStreamLine)
	acc := newResponsesStreamAcc()
	eventName, data := "", ""
	flush := func() error {
		if data == "" {
			eventName = ""
			return nil
		}
		if data == "[DONE]" {
			data = ""
			eventName = ""
			return nil
		}
		delta, err := acc.apply(eventName, data)
		if err != nil {
			return err
		}
		data = ""
		eventName = ""
		return emitDelta(onDelta, delta)
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return acc.result(), err
		}
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return acc.result(), err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			part := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" {
				data += "\n"
			}
			data += part
		}
	}
	if err := scanner.Err(); err != nil {
		return acc.result(), fmt.Errorf("opencode: responses stream: %w", err)
	}
	if err := flush(); err != nil {
		return acc.result(), err
	}
	return acc.result(), nil
}

func (a *responsesStreamAcc) apply(eventName, payload string) (Delta, error) {
	var event responsesEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return Delta{}, fmt.Errorf("opencode: decode responses event: %w", err)
	}
	if event.Type == "" {
		event.Type = eventName
	}
	switch event.Type {
	case "response.output_text.delta":
		a.content.WriteString(event.Delta)
		return Delta{Content: event.Delta}, nil
	case "response.reasoning_text.delta", "response.reasoning_summary.delta", "response.reasoning_summary_text.delta":
		a.reasoning.WriteString(event.Delta)
		return Delta{Reasoning: event.Delta}, nil
	case "response.output_item.added", "response.output_item.done":
		if event.Item.Type == "function_call" {
			a.addTool(event.Item)
		}
		return Delta{}, nil
	case "response.function_call_arguments.delta":
		tool := a.tool(event.ItemID)
		if tool == nil {
			return Delta{}, nil
		}
		tool.Arguments += event.Delta
		return Delta{}, nil
	case "response.function_call_arguments.done":
		tool := a.tool(event.ItemID)
		if tool != nil {
			var done struct {
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(payload), &done); err == nil && done.Arguments != "" {
				tool.Arguments = done.Arguments
			}
		}
		return Delta{}, nil
	case "response.completed":
		a.setUsage(event.Response.Usage, event.Usage)
		a.finish = "stop"
		if len(a.order) > 0 {
			a.finish = "tool-calls"
		}
		return Delta{FinishReason: a.finish, Usage: a.usage}, nil
	case "response.incomplete":
		a.setUsage(event.Response.Usage, event.Usage)
		a.finish = "length"
		return Delta{FinishReason: a.finish, Usage: a.usage}, nil
	case "response.failed", "error":
		failure := event.Error
		if failure == nil {
			failure = event.Response.Error
		}
		if failure == nil {
			return Delta{}, fmt.Errorf("opencode: responses request failed")
		}
		if failure.Code != "" {
			return Delta{}, fmt.Errorf("opencode: responses request failed (%s): %s", failure.Code, failure.Message)
		}
		return Delta{}, fmt.Errorf("opencode: responses request failed: %s", failure.Message)
	}
	return Delta{}, nil
}

func (a *responsesStreamAcc) addTool(item responsesItem) {
	key := item.ID
	if key == "" {
		key = item.CallID
	}
	if key == "" {
		key = fmt.Sprintf("tool-%d", len(a.order)+1)
	}
	tool, ok := a.tools[key]
	if !ok {
		tool = &ToolCall{}
		a.tools[key] = tool
		a.order = append(a.order, key)
	}
	if item.CallID != "" {
		tool.ID = item.CallID
	}
	if tool.ID == "" {
		tool.ID = key
	}
	if item.Name != "" {
		tool.Name = item.Name
	}
	if item.Arguments != "" {
		tool.Arguments = item.Arguments
	}
}

func (a *responsesStreamAcc) tool(key string) *ToolCall {
	if tool := a.tools[key]; tool != nil {
		return tool
	}
	if len(a.order) == 1 {
		return a.tools[a.order[0]]
	}
	return nil
}

func (a *responsesStreamAcc) setUsage(values ...*responsesUsage) {
	for _, value := range values {
		if value == nil {
			continue
		}
		a.usage = &Usage{
			TokensTotal:     value.TotalTokens,
			TokensInput:     value.InputTokens,
			TokensOutput:    value.OutputTokens,
			TokensReasoning: value.OutputTokensDetails.ReasoningTokens,
			TokensCacheRead: value.InputTokensDetails.CachedTokens,
		}
		return
	}
}

func (a *responsesStreamAcc) result() *ChatResponse {
	tools := make([]ToolCall, 0, len(a.order))
	for _, key := range a.order {
		tools = append(tools, *a.tools[key])
	}
	return &ChatResponse{
		Content:      a.content.String(),
		Reasoning:    a.reasoning.String(),
		FinishReason: a.finish,
		ToolCalls:    tools,
		Usage:        a.usage,
	}
}

func completeResponsesJSON(raw []byte, onDelta func(Delta) error) (*ChatResponse, error) {
	var body struct {
		Output []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage *responsesUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("opencode: decode responses response: %w", err)
	}
	acc := newResponsesStreamAcc()
	for _, item := range body.Output {
		if item.Type == "function_call" {
			acc.addTool(responsesItem{Type: item.Type, ID: item.ID, CallID: item.CallID, Name: item.Name, Arguments: item.Arguments})
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				acc.content.WriteString(content.Text)
			}
		}
	}
	acc.setUsage(body.Usage)
	acc.finish = "stop"
	if len(acc.order) > 0 {
		acc.finish = "tool-calls"
	}
	response := acc.result()
	if err := emitDelta(onDelta, Delta{
		Content:      response.Content,
		Reasoning:    response.Reasoning,
		FinishReason: response.FinishReason,
		Usage:        response.Usage,
	}); err != nil {
		return nil, err
	}
	return response, nil
}
