package db

import (
	"crypto/rand"
	"encoding/hex"
)

// Session is one conversation stored per working directory.
type Session struct {
	ID          string
	Title       string
	Directory   string
	Provider    string
	Model       string
	Variant     *string
	TimeCreated int64
	TimeUpdated int64
	Status      string
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

// NewID returns prefix plus 16 lowercase hex characters from crypto/rand.
func NewID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
