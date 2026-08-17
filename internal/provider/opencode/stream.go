package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	// maxStreamLine caps a single SSE/NDJSON line.
	maxStreamLine = 1 << 20
	// streamPeek is enough to tell event-stream data from a JSON object.
	streamPeek = 64
	// streamReaderBytes sizes the SSE/NDJSON reader buffer.
	streamReaderBytes = 32 << 10
	// streamBufferBytes sizes the scanner buffer for event-stream lines.
	streamBufferBytes = 64 << 10
)

// Delta is one streamed chat chunk. Content and Reasoning are this chunk
// only; Usage and FinishReason are copied from the chunk when present.
type Delta struct {
	Content      string
	Reasoning    string
	FinishReason string
	Usage        *Usage
}

// ChatStream POSTs the same chat-completions body as Chat with stream=true.
// onDelta is invoked for each useful chunk. A non-SSE JSON body is treated as
// one complete response so tests and non-streaming servers still work.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onDelta func(Delta) error) (*ChatResponse, error) {
	resp, err := c.postChat(ctx, req, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	br := bufio.NewReaderSize(resp.Body, streamReaderBytes)
	peek, _ := br.Peek(streamPeek)
	if isEventStream(resp.Header.Get("Content-Type"), peek) {
		return parseEventStream(ctx, br, onDelta)
	}
	raw, err := io.ReadAll(io.LimitReader(br, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("opencode: read response: %w", err)
	}
	return completeJSONAsStream(raw, onDelta)
}

func isEventStream(contentType string, peek []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "event-stream") || strings.Contains(ct, "ndjson") || strings.Contains(ct, "jsonl") {
		return true
	}
	trim := bytes.TrimSpace(peek)
	return bytes.HasPrefix(trim, []byte("data:")) || bytes.HasPrefix(trim, []byte("event:"))
}

func completeJSONAsStream(raw []byte, onDelta func(Delta) error) (*ChatResponse, error) {
	var wire wireResponse
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("opencode: decode response: %w", err)
	}
	out := chatResponseFromWire(wire)
	d := Delta{
		Content:      out.Content,
		Reasoning:    out.Reasoning,
		FinishReason: out.FinishReason,
		Usage:        out.Usage,
	}
	if err := emitDelta(onDelta, d); err != nil {
		return nil, err
	}
	return out, nil
}

func parseEventStream(ctx context.Context, r io.Reader, onDelta func(Delta) error) (*ChatResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, streamBufferBytes), maxStreamLine)
	acc := newStreamAcc()
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return acc.result(), err
		}
		payload, ok := streamPayload(sc.Text())
		if !ok {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		d, err := acc.applyJSON(payload)
		if err != nil {
			continue
		}
		if err := emitDelta(onDelta, d); err != nil {
			return acc.result(), err
		}
	}
	if err := sc.Err(); err != nil {
		return acc.result(), fmt.Errorf("opencode: stream: %w", err)
	}
	return acc.result(), nil
}

func streamPayload(line string) (string, bool) {
	line = strings.TrimRight(line, "\r")
	if line == "" || strings.HasPrefix(line, ":") {
		return "", false
	}
	if rest, ok := strings.CutPrefix(line, "data:"); ok {
		return strings.TrimSpace(rest), true
	}
	if strings.HasPrefix(line, "{") {
		return line, true
	}
	return "", false
}

func emitDelta(onDelta func(Delta) error, d Delta) error {
	if onDelta == nil {
		return nil
	}
	if d.Content == "" && d.Reasoning == "" && d.FinishReason == "" && d.Usage == nil {
		return nil
	}
	return onDelta(d)
}

type streamAcc struct {
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	usage     *Usage
	tools     map[int]*ToolCall
	order     []int
}

func newStreamAcc() *streamAcc {
	return &streamAcc{tools: map[int]*ToolCall{}}
}

func (a *streamAcc) applyJSON(payload string) (Delta, error) {
	var wire wireResponse
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return Delta{}, err
	}
	var d Delta
	if wire.Usage != nil {
		a.usage = usageFromWire(wire.Usage)
		d.Usage = a.usage
	}
	if len(wire.Choices) == 0 {
		return d, nil
	}
	ch := wire.Choices[0]
	if fr := firstNonEmpty(ch.FinishReason, ch.Message.FinishReason); fr != "" {
		a.finish = fr
		d.FinishReason = fr
	}
	reason := firstNonEmpty(ch.Delta.Reasoning, ch.Delta.ReasoningContent)
	if reason != "" {
		a.reasoning.WriteString(reason)
		d.Reasoning = reason
	}
	if ch.Delta.Content != "" {
		a.content.WriteString(ch.Delta.Content)
		d.Content = ch.Delta.Content
	}
	for _, tc := range ch.Delta.ToolCalls {
		a.addTool(tc)
	}
	if ch.Message.Content != "" && a.content.Len() == 0 {
		a.content.WriteString(ch.Message.Content)
		d.Content = ch.Message.Content
	}
	msgReason := firstNonEmpty(ch.Message.Reasoning, ch.Message.ReasoningContent)
	if msgReason != "" && a.reasoning.Len() == 0 {
		a.reasoning.WriteString(msgReason)
		d.Reasoning = msgReason
	}
	for _, tc := range ch.Message.ToolCalls {
		a.addCompleteTool(tc)
	}
	return d, nil
}

func (a *streamAcc) addTool(tc wireStreamToolCall) {
	slot, ok := a.tools[tc.Index]
	if !ok {
		slot = &ToolCall{}
		a.tools[tc.Index] = slot
		a.order = append(a.order, tc.Index)
	}
	if tc.ID != "" {
		slot.ID = tc.ID
	}
	if tc.Function.Name != "" {
		slot.Name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		slot.Arguments += tc.Function.Arguments
	}
}

func (a *streamAcc) addCompleteTool(tc ToolCall) {
	if tc.ID == "" && tc.Name == "" {
		return
	}
	for _, idx := range a.order {
		if a.tools[idx].ID == tc.ID {
			return
		}
	}
	idx := len(a.order)
	cp := tc
	a.tools[idx] = &cp
	a.order = append(a.order, idx)
}

func (a *streamAcc) result() *ChatResponse {
	tools := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		tools = append(tools, *a.tools[idx])
	}
	return &ChatResponse{
		Content:      a.content.String(),
		Reasoning:    a.reasoning.String(),
		FinishReason: a.finish,
		ToolCalls:    tools,
		Usage:        a.usage,
	}
}
