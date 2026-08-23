// Package recap builds and processes local conversation memory artifacts.
package recap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

const (
	defaultRecentWindow       = time.Hour
	defaultMaximumWindow      = 2 * time.Hour
	defaultMessageLimit       = 5
	minimumMessageCount       = 4
	memoryMinimumMessageCount = 2
	maxToolFactsPerMessage    = 8
	maxToolFactOutput         = 800
	maxToolFactTitle          = 200
)

// MemoryMinimumMessageCount is the smallest complete context window used by
// the project memory updater. Recap artifacts continue to require four.
const MemoryMinimumMessageCount = memoryMinimumMessageCount

var (
	// ErrInsufficientMessages means a session has no four-message recap window.
	ErrInsufficientMessages = errors.New("recap: fewer than four complete messages")
	ErrSubagentSession      = errors.New("recap: sub-agent sessions cannot be recapped")
	ErrAnchorNotFound       = errors.New("recap: anchor message is not eligible")
)

// SnapshotOptions controls the clock and bounded time windows used by
// BuildSnapshot. Zero values use the one-hour, two-hour, five-message defaults.
type SnapshotOptions struct {
	Now           time.Time
	RecentWindow  time.Duration
	MaximumWindow time.Duration
	MessageLimit  int
	// MinimumMessageCount overrides the default four-message recap threshold
	// for bounded consumers such as the project memory updater.
	MinimumMessageCount int
	// AnchorMessageID rebuilds the window ending at a durable source message.
	// It lets restart recovery reproduce the reservation even after newer
	// messages have been added to the session.
	AnchorMessageID string
}

// Snapshot is a stable, newest-first view of one main session.
type Snapshot struct {
	SessionID          string
	SourceStartSeq     int
	SourceEndSeq       int
	SourceStartTime    int64
	SourceEndTime      int64
	SourceEndMessageID string
	Messages           []SnapshotMessage
}

// SnapshotMessage is one durable text-bearing message and bounded tool facts.
type SnapshotMessage struct {
	ID          string
	SessionID   string
	Role        string
	Seq         int
	TimeCreated int64
	Text        string
	ToolFacts   []ToolFact
}

// ToolFact records only a bounded terminal outcome from a tool call.
type ToolFact struct {
	Tool     string `json:"tool"`
	Status   string `json:"status"`
	Title    string `json:"title,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Output   string `json:"output,omitempty"`
}

// BuildSnapshot reads the main session's durable graph. Message sequence is
// the identity tie-breaker; timestamps only determine the one-to-two-hour
// windows and are preserved as source metadata.
func BuildSnapshot(ctx context.Context, store *db.Store, sessionID string, opts SnapshotOptions) (Snapshot, error) {
	if store == nil {
		return Snapshot{}, errors.New("recap: store is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return Snapshot{}, errors.New("recap: session id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("recap: get session: %w", err)
	}
	if sess.Kind == db.SessionKindSubagent || sess.ParentSessionID != nil {
		return Snapshot{}, ErrSubagentSession
	}
	graph, err := store.LoadSessionGraph(ctx, sessionID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("recap: load session graph: %w", err)
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	recentWindow := opts.RecentWindow
	if recentWindow <= 0 {
		recentWindow = defaultRecentWindow
	}
	maximumWindow := opts.MaximumWindow
	if maximumWindow <= 0 {
		maximumWindow = defaultMaximumWindow
	}
	if maximumWindow < recentWindow {
		maximumWindow = recentWindow
	}
	limit := opts.MessageLimit
	if limit <= 0 {
		limit = defaultMessageLimit
	}
	minimum := minimumMessageCount
	if opts.MinimumMessageCount > 0 {
		minimum = opts.MinimumMessageCount
	}
	candidates := snapshotCandidates(graph)
	if len(candidates) == 0 {
		return Snapshot{}, ErrInsufficientMessages
	}
	if opts.AnchorMessageID != "" {
		anchor, ok := findAnchor(candidates, opts.AnchorMessageID)
		if !ok {
			return Snapshot{}, ErrAnchorNotFound
		}
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if candidate.message.TimeCreated < anchor.message.TimeCreated ||
				(candidate.message.TimeCreated == anchor.message.TimeCreated && candidate.message.Seq <= anchor.message.Seq) {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
		now = time.UnixMilli(anchor.message.TimeCreated)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].message.TimeCreated != candidates[j].message.TimeCreated {
			return candidates[i].message.TimeCreated > candidates[j].message.TimeCreated
		}
		return candidates[i].message.Seq > candidates[j].message.Seq
	})
	selected := selectWindow(candidates, now.Add(-recentWindow).UnixMilli(), limit)
	if len(selected) < minimum {
		selected = selectWindow(candidates, now.Add(-maximumWindow).UnixMilli(), limit)
	}
	if len(selected) < minimum {
		return Snapshot{}, ErrInsufficientMessages
	}
	return makeSnapshot(sessionID, selected), nil
}

// BuildAnchorSnapshot returns the newest eligible source message even when a
// full four-message window is not available. It is used only to reserve a
// durable failed memory attempt so insufficient context is observable and can
// be retried after the next successful turn.
func BuildAnchorSnapshot(ctx context.Context, store *db.Store, sessionID string) (Snapshot, error) {
	if store == nil {
		return Snapshot{}, errors.New("recap: store is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return Snapshot{}, errors.New("recap: session id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("recap: get session: %w", err)
	}
	if sess.Kind == db.SessionKindSubagent || sess.ParentSessionID != nil {
		return Snapshot{}, ErrSubagentSession
	}
	graph, err := store.LoadSessionGraph(ctx, sessionID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("recap: load session graph: %w", err)
	}
	candidates := snapshotCandidates(graph)
	if len(candidates) == 0 {
		return Snapshot{}, ErrInsufficientMessages
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].message.TimeCreated != candidates[j].message.TimeCreated {
			return candidates[i].message.TimeCreated > candidates[j].message.TimeCreated
		}
		return candidates[i].message.Seq > candidates[j].message.Seq
	})
	return makeSnapshot(sessionID, candidates[:1]), nil
}

func findAnchor(candidates []snapshotCandidate, id string) (snapshotCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.message.ID == id {
			return candidate, true
		}
	}
	return snapshotCandidate{}, false
}

type snapshotCandidate struct {
	message db.Message
	text    string
	parts   []db.Part
	facts   []ToolFact
}

func snapshotCandidates(graph db.SessionGraph) []snapshotCandidate {
	out := make([]snapshotCandidate, 0, len(graph.Entries))
	for _, entry := range graph.Entries {
		message := entry.Message
		if !message.Visible || message.Agent == "compaction" || isCompactionEntry(entry.Parts) {
			continue
		}
		text := snapshotText(entry.Parts)
		if text == "" || (message.Role == "assistant" && !hasCompletedAssistant(entry.Parts)) {
			continue
		}
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		out = append(out, snapshotCandidate{
			message: message,
			text:    text,
			parts:   entry.Parts,
			facts:   toolFacts(entry.Parts, graph.ToolCallsByPart),
		})
	}
	return out
}

func selectWindow(candidates []snapshotCandidate, cutoff int64, limit int) []snapshotCandidate {
	selected := make([]snapshotCandidate, 0, minInt(limit, len(candidates)))
	for _, candidate := range candidates {
		if candidate.message.TimeCreated < cutoff {
			continue
		}
		selected = append(selected, candidate)
		if len(selected) == limit {
			break
		}
	}
	return selected
}

func makeSnapshot(sessionID string, selected []snapshotCandidate) Snapshot {
	out := Snapshot{SessionID: sessionID, Messages: make([]SnapshotMessage, 0, len(selected))}
	for _, candidate := range selected {
		out.Messages = append(out.Messages, SnapshotMessage{
			ID:          candidate.message.ID,
			SessionID:   candidate.message.SessionID,
			Role:        candidate.message.Role,
			Seq:         candidate.message.Seq,
			TimeCreated: candidate.message.TimeCreated,
			Text:        candidate.text,
			ToolFacts:   candidate.facts,
		})
	}
	oldest := selected[len(selected)-1].message
	newest := selected[0].message
	out.SourceStartSeq = oldest.Seq
	out.SourceEndSeq = newest.Seq
	out.SourceStartTime = oldest.TimeCreated
	out.SourceEndTime = newest.TimeCreated
	out.SourceEndMessageID = newest.ID
	return out
}

func snapshotText(parts []db.Part) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "text" || part.Text == nil {
			continue
		}
		b.WriteString(*part.Text)
	}
	return strings.TrimSpace(b.String())
}

func hasCompletedAssistant(parts []db.Part) bool {
	for _, part := range parts {
		if part.Type == "step-finish" && part.FinishReason != nil && strings.TrimSpace(*part.FinishReason) != "" {
			return true
		}
	}
	return false
}

func isCompactionEntry(parts []db.Part) bool {
	for _, part := range parts {
		if part.Type == "compaction" {
			return true
		}
	}
	return false
}

func toolFacts(parts []db.Part, byPart map[string]db.ToolCall) []ToolFact {
	facts := make([]ToolFact, 0)
	for _, part := range parts {
		if part.Type != "tool" || part.ToolCallID == nil || len(facts) >= maxToolFactsPerMessage {
			continue
		}
		call, ok := byPart[part.ID]
		if !ok {
			continue
		}
		status := strings.TrimSpace(call.Status)
		switch status {
		case "completed", "denied", "error", "failed":
		default:
			continue
		}
		fact := ToolFact{Tool: call.Tool, Status: status, ExitCode: call.ExitCode}
		if call.Title != nil {
			fact.Title = truncate(call.Title, maxToolFactTitle)
		}
		if call.Output != nil {
			fact.Output = truncate(call.Output, maxToolFactOutput)
		}
		facts = append(facts, fact)
	}
	return facts
}

func truncate(value *string, max int) string {
	if value == nil {
		return ""
	}
	runes := []rune(*value)
	if len(runes) <= max {
		return *value
	}
	return string(runes[:max])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
