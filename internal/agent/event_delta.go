package agent

import "github.com/chinmay-sawant/lazykoder/internal/db"

// PartDeltaKind is the UI-facing part class on the Event seam.
// Persistence still uses db.Part.Type strings inside agent only.
type PartDeltaKind string

const (
	PartDeltaText       PartDeltaKind = "text"
	PartDeltaReasoning  PartDeltaKind = "reasoning"
	PartDeltaStepStart  PartDeltaKind = "step-start"
	PartDeltaStepFinish PartDeltaKind = "step-finish"
	PartDeltaTool       PartDeltaKind = "tool"
	PartDeltaCompaction PartDeltaKind = CompactPartType
	PartDeltaOther      PartDeltaKind = "other"
)

// PartDelta is one transcript part update for the TUI. It does not embed db.Part.
type PartDelta struct {
	Kind        PartDeltaKind
	ID          string
	MessageID   string
	Text        string
	TimeCreated int64
	TimeStart   int64
	TimeEnd     int64
	FinishReason string

	TokensTotal      int64
	TokensInput      int64
	TokensOutput     int64
	TokensReasoning  int64
	TokensCacheRead  int64
	TokensCacheWrite int64
	Cost             float64
	HasCost          bool

	ToolName   string
	ToolCallID string
	ToolStatus string
}

// ToolDelta is one tool_calls update for the TUI. It does not embed db.ToolCall.
type ToolDelta struct {
	PartID       string
	Name         string
	CallID       string
	Status       string
	Title        string
	InputJSON    string
	Output       string
	MetadataJSON string
	ExitCode     *int
	TimeStart    *int64
	TimeEnd      *int64
}

func partDeltaFromDB(p db.Part) PartDelta {
	d := PartDelta{
		ID:          p.ID,
		MessageID:   p.MessageID,
		TimeCreated: p.TimeCreated,
		Kind:        partDeltaKind(p.Type),
	}
	if p.Text != nil {
		d.Text = *p.Text
	}
	if p.TimeStart != nil {
		d.TimeStart = *p.TimeStart
	}
	if p.TimeEnd != nil {
		d.TimeEnd = *p.TimeEnd
	}
	if p.FinishReason != nil {
		d.FinishReason = *p.FinishReason
	}
	if p.TokensTotal != nil {
		d.TokensTotal = *p.TokensTotal
	}
	if p.TokensInput != nil {
		d.TokensInput = *p.TokensInput
	}
	if p.TokensOutput != nil {
		d.TokensOutput = *p.TokensOutput
	}
	if p.TokensReasoning != nil {
		d.TokensReasoning = *p.TokensReasoning
	}
	if p.TokensCacheRead != nil {
		d.TokensCacheRead = *p.TokensCacheRead
	}
	if p.TokensCacheWrite != nil {
		d.TokensCacheWrite = *p.TokensCacheWrite
	}
	if p.Cost != nil {
		d.Cost = *p.Cost
		d.HasCost = true
	}
	if p.ToolName != nil {
		d.ToolName = *p.ToolName
	}
	if p.ToolCallID != nil {
		d.ToolCallID = *p.ToolCallID
	}
	if p.ToolStatus != nil {
		d.ToolStatus = *p.ToolStatus
	}
	return d
}

func toolDeltaFromDB(tc db.ToolCall) ToolDelta {
	d := ToolDelta{
		PartID:    tc.PartID,
		Name:      tc.Tool,
		CallID:    tc.CallID,
		Status:    tc.Status,
		InputJSON: tc.InputJSON,
		ExitCode:  tc.ExitCode,
		TimeStart: tc.TimeStart,
		TimeEnd:   tc.TimeEnd,
	}
	if tc.Title != nil {
		d.Title = *tc.Title
	}
	if tc.Output != nil {
		d.Output = *tc.Output
	}
	if tc.MetadataJSON != nil {
		d.MetadataJSON = *tc.MetadataJSON
	}
	return d
}

func partDeltaKind(typ string) PartDeltaKind {
	switch typ {
	case "text":
		return PartDeltaText
	case "reasoning":
		return PartDeltaReasoning
	case "step-start":
		return PartDeltaStepStart
	case "step-finish":
		return PartDeltaStepFinish
	case "tool":
		return PartDeltaTool
	case CompactPartType:
		return PartDeltaCompaction
	default:
		return PartDeltaOther
	}
}
