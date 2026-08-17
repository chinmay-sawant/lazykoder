package db

import (
	"crypto/rand"
	"encoding/hex"
)

// Session kind values.
const (
	SessionKindMain     = "main"
	SessionKindSubagent = "subagent"
)

// Session is one conversation stored per working directory.
type Session struct {
	ID              string
	Title           string
	Directory       string
	Provider        string
	Model           string
	Variant         *string
	TimeCreated     int64
	TimeUpdated     int64
	Status          string
	ParentSessionID *string // set for kind=subagent
	Kind            string  // main | subagent; empty treated as main
	StatusSegments  []string
}

// StatusSegmentNames are the persisted footer segment identifiers.
var StatusSegmentNames = []string{"model", "tps", "tokens", "cost", "scroll", "models", "prompt"}

// DefaultStatusSegments returns a fresh copy of the default footer layout.
func DefaultStatusSegments() []string {
	return append([]string(nil), StatusSegmentNames...)
}

// NormalizeStatusSegments removes unknown and duplicate identifiers. An empty
// result means the caller should use the default layout.
func NormalizeStatusSegments(values []string) []string {
	allowed := make(map[string]struct{}, len(StatusSegmentNames))
	for _, name := range StatusSegmentNames {
		allowed[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, name := range values {
		if _, ok := allowed[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return DefaultStatusSegments()
	}
	return out
}

// Message is one provider round-trip within a session.
type Message struct {
	ID          string
	SessionID   string
	Role        string
	Agent       string
	ProviderID  string
	ModelID     string
	Variant     *string
	TimeCreated int64
	Seq         int
	Visible     bool
}

// Part is one content block within a message.
type Part struct {
	ID               string
	MessageID        string
	Type             string
	TimeCreated      int64
	Seq              int
	Text             *string
	TimeStart        *int64
	TimeEnd          *int64
	FinishReason     *string
	TokensTotal      *int64
	TokensInput      *int64
	TokensOutput     *int64
	TokensReasoning  *int64
	TokensCacheRead  *int64
	TokensCacheWrite *int64
	Cost             *float64
	ToolName         *string
	ToolCallID       *string
	ToolStatus       *string
}

// ToolCall is one tool invocation owned by a tool part.
type ToolCall struct {
	PartID       string
	Tool         string
	CallID       string
	Status       string
	Title        *string
	TimeStart    *int64
	TimeEnd      *int64
	ExitCode     *int
	InputJSON    string
	Output       *string
	MetadataJSON *string
}

// Todo status values (model-driven checklist items).
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
	TodoCancelled  = "cancelled"
)

// Todo is one checklist item for a session (todowrite replace-all).
type Todo struct {
	SessionID   string
	Seq         int
	Content     string
	Status      string
	TimeUpdated int64
}

// SubagentJob is a durable parent-side task handle (sub_...).
// Survives process restarts so task_list / task_wait / resume can reattach.
type SubagentJob struct {
	ID              string
	ParentSessionID string
	ParentPartID    string
	ChildSessionID  string
	Name            string
	Role            string
	Status          string
	Prompt          string
	Description     string
	Model           string
	Variant         string
	MaxSteps        int
	TimeoutMS       int64
	Summary         string
	Error           string
	TimeCreated     int64
	TimeUpdated     int64
	TimeStarted     int64
	TimeFinished    int64
}

// NewID returns prefix plus 16 lowercase hex characters from crypto/rand.
func NewID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
