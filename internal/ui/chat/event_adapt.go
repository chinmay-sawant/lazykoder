package chat

import (
	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
)

// dbPartFromDelta maps an Event PartDelta into the db.Part shape still used
// on transcriptItem (replay and live share that storage). The Event seam
// itself does not carry db.Part.
func dbPartFromDelta(d agent.PartDelta) db.Part {
	p := db.Part{
		ID:          d.ID,
		MessageID:   d.MessageID,
		Type:        string(d.Kind),
		TimeCreated: d.TimeCreated,
	}
	if d.Text != "" {
		t := d.Text
		p.Text = &t
	}
	if d.TimeStart != 0 {
		v := d.TimeStart
		p.TimeStart = &v
	}
	if d.TimeEnd != 0 {
		v := d.TimeEnd
		p.TimeEnd = &v
	}
	if d.FinishReason != "" {
		v := d.FinishReason
		p.FinishReason = &v
	}
	if d.TokensTotal != 0 {
		v := d.TokensTotal
		p.TokensTotal = &v
	}
	if d.TokensInput != 0 {
		v := d.TokensInput
		p.TokensInput = &v
	}
	if d.TokensOutput != 0 {
		v := d.TokensOutput
		p.TokensOutput = &v
	}
	if d.TokensReasoning != 0 {
		v := d.TokensReasoning
		p.TokensReasoning = &v
	}
	if d.TokensCacheRead != 0 {
		v := d.TokensCacheRead
		p.TokensCacheRead = &v
	}
	if d.TokensCacheWrite != 0 {
		v := d.TokensCacheWrite
		p.TokensCacheWrite = &v
	}
	if d.HasCost {
		v := d.Cost
		p.Cost = &v
	}
	if d.ToolName != "" {
		v := d.ToolName
		p.ToolName = &v
	}
	if d.ToolCallID != "" {
		v := d.ToolCallID
		p.ToolCallID = &v
	}
	if d.ToolStatus != "" {
		v := d.ToolStatus
		p.ToolStatus = &v
	}
	return p
}

func dbToolFromDelta(d agent.ToolDelta) db.ToolCall {
	tc := db.ToolCall{
		PartID:    d.PartID,
		Tool:      d.Name,
		CallID:    d.CallID,
		Status:    d.Status,
		InputJSON: d.InputJSON,
		ExitCode:  d.ExitCode,
		TimeStart: d.TimeStart,
		TimeEnd:   d.TimeEnd,
	}
	if d.Title != "" {
		v := d.Title
		tc.Title = &v
	}
	if d.Output != "" {
		v := d.Output
		tc.Output = &v
	}
	if d.MetadataJSON != "" {
		v := d.MetadataJSON
		tc.MetadataJSON = &v
	}
	return tc
}
