package recap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	maxRecapMarkdown = 12_000
	maxQuestionText  = 800
	maxReasonText    = 1_500
	maxAvoidRule     = 800
	maxQuestions     = 20
	maxAvoidRules    = 20
	maxSourceIDs     = 8
)

// Envelope is the only model output accepted by a recap worker.
type Envelope struct {
	RecapMarkdown string      `json:"recap_markdown"`
	Questions     []Question  `json:"questions"`
	ThingsToAvoid []AvoidRule `json:"things_to_avoid"`
}

// Question is an unresolved, source-backed question for a future turn.
type Question struct {
	Question         string   `json:"question"`
	Reason           string   `json:"reason"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

// AvoidRule is a concrete source-backed rule that should prevent a repeat
// failure or user correction.
type AvoidRule struct {
	Rule             string   `json:"rule"`
	Reason           string   `json:"reason"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

// ParseEnvelope decodes strict JSON and validates every claim's source IDs.
func ParseEnvelope(raw []byte, snapshot Snapshot) (Envelope, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Envelope{}, fmt.Errorf("recap: decode envelope: %w", err)
	}
	for _, key := range []string{"recap_markdown", "questions", "things_to_avoid"} {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			return Envelope{}, fmt.Errorf("recap: %s is required", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("recap: decode envelope: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Envelope{}, errors.New("recap: envelope has trailing JSON")
		}
		return Envelope{}, fmt.Errorf("recap: envelope has trailing data: %w", err)
	}
	if err := validateEnvelope(envelope, snapshot); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func validateEnvelope(envelope Envelope, snapshot Snapshot) error {
	if asksForSecret(envelope.RecapMarkdown) {
		return errors.New("recap: recap requests a secret")
	}
	if containsSecretMaterial(envelope.RecapMarkdown) {
		return errors.New("recap: recap contains secret-like material")
	}
	if unsupportedFailureClaim(envelope.RecapMarkdown, snapshot) {
		return errors.New("recap: recap contains an unsupported failure claim")
	}
	if strings.TrimSpace(envelope.RecapMarkdown) == "" {
		return errors.New("recap: recap_markdown is required")
	}
	if len([]rune(envelope.RecapMarkdown)) > maxRecapMarkdown {
		return fmt.Errorf("recap: recap_markdown exceeds %d characters", maxRecapMarkdown)
	}
	if len(envelope.Questions) > maxQuestions {
		return fmt.Errorf("recap: too many questions")
	}
	if len(envelope.ThingsToAvoid) > maxAvoidRules {
		return fmt.Errorf("recap: too many things_to_avoid rules")
	}
	knownIDs := snapshotMessageIDs(snapshot)
	seenRules := make(map[string]struct{}, len(envelope.ThingsToAvoid))
	for i, question := range envelope.Questions {
		if err := validateQuestion(i, question, knownIDs); err != nil {
			return err
		}
	}
	for i, rule := range envelope.ThingsToAvoid {
		if err := validateAvoidRule(i, rule, knownIDs, seenRules); err != nil {
			return err
		}
	}
	return nil
}

func snapshotMessageIDs(snapshot Snapshot) map[string]struct{} {
	known := make(map[string]struct{}, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		if message.ID != "" {
			known[message.ID] = struct{}{}
		}
	}
	return known
}

func validateQuestion(index int, question Question, known map[string]struct{}) error {
	if strings.TrimSpace(question.Question) == "" || len([]rune(question.Question)) > maxQuestionText {
		return fmt.Errorf("recap: question %d has invalid question text", index)
	}
	if strings.TrimSpace(question.Reason) == "" || len([]rune(question.Reason)) > maxReasonText {
		return fmt.Errorf("recap: question %d has invalid reason", index)
	}
	if err := validateSources(question.SourceMessageIDs, known, "question", index); err != nil {
		return err
	}
	if asksForSecret(question.Question) || asksForSecret(question.Reason) {
		return fmt.Errorf("recap: question %d requests a secret", index)
	}
	if containsSecretMaterial(question.Question) || containsSecretMaterial(question.Reason) {
		return fmt.Errorf("recap: question %d contains secret-like material", index)
	}
	return nil
}

func validateAvoidRule(index int, rule AvoidRule, known map[string]struct{}, seen map[string]struct{}) error {
	canonical := strings.ToLower(strings.Join(strings.Fields(rule.Rule), " "))
	if canonical == "" || len([]rune(rule.Rule)) > maxAvoidRule {
		return fmt.Errorf("recap: thing_to_avoid %d has invalid rule", index)
	}
	if _, ok := seen[canonical]; ok {
		return fmt.Errorf("recap: duplicate thing_to_avoid rule %d", index)
	}
	seen[canonical] = struct{}{}
	if strings.TrimSpace(rule.Reason) == "" || len([]rune(rule.Reason)) > maxReasonText {
		return fmt.Errorf("recap: thing_to_avoid %d has invalid reason", index)
	}
	if err := validateSources(rule.SourceMessageIDs, known, "thing_to_avoid", index); err != nil {
		return err
	}
	if asksForSecret(rule.Rule) || asksForSecret(rule.Reason) {
		return fmt.Errorf("recap: thing_to_avoid %d requests a secret", index)
	}
	if containsSecretMaterial(rule.Rule) || containsSecretMaterial(rule.Reason) {
		return fmt.Errorf("recap: thing_to_avoid %d contains secret-like material", index)
	}
	return nil
}

func validateSources(sourceIDs []string, known map[string]struct{}, kind string, index int) error {
	if len(sourceIDs) == 0 || len(sourceIDs) > maxSourceIDs {
		return fmt.Errorf("recap: %s %d needs one to %d source message IDs", kind, index, maxSourceIDs)
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("recap: %s %d has an empty source message ID", kind, index)
		}
		if _, ok := known[id]; !ok {
			return fmt.Errorf("recap: %s %d cites unknown message %q", kind, index, id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("recap: %s %d cites duplicate message %q", kind, index, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

var secretRequestPattern = regexp.MustCompile(`(?i)\b(?:password|passwd|secret|api[ _-]?key|access[ _-]?token|private[ _-]?key)\b`)
var secretMaterialPattern = regexp.MustCompile(`(?i)(?:\bsk-[a-z0-9]{16,}\b|\bgh[pousr]_[a-z0-9_]{20,}\b|\bbearer\s+[a-z0-9._-]{20,}\b|\b(?:password|passwd|api[ _-]?key|access[ _-]?token|private[ _-]?key)\s*[:=]\s*\S+)`)

var failureClaimPattern = regexp.MustCompile(`(?i)\b(?:failed|failure|error|panic|crash|broken)\b`)

func asksForSecret(text string) bool {
	if !secretRequestPattern.MatchString(text) {
		return false
	}
	words := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, verb := range []string{"share", "send", "provide", "paste", "give", "reveal", "expose", "store", "remember", "record"} {
		if strings.Contains(words, verb) {
			return true
		}
	}
	return false
}

func containsSecretMaterial(text string) bool {
	return secretMaterialPattern.MatchString(text)
}

func unsupportedFailureClaim(recap string, snapshot Snapshot) bool {
	if !failureClaimPattern.MatchString(recap) {
		return false
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(recap), " "))
	for _, phrase := range []string{
		"no failure", "no failures", "without failure", "without failures",
		"no error", "no errors", "without error", "without errors",
		"no crash", "no crashes",
	} {
		if strings.Contains(normalized, phrase) {
			return false
		}
	}
	var evidence strings.Builder
	for _, message := range snapshot.Messages {
		evidence.WriteString(" ")
		evidence.WriteString(message.Text)
		for _, fact := range message.ToolFacts {
			evidence.WriteString(" ")
			evidence.WriteString(fact.Status)
			evidence.WriteString(" ")
			evidence.WriteString(fact.Output)
		}
	}
	return !failureClaimPattern.MatchString(evidence.String())
}
