package recap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/prompts"
)

const (
	memoryFormatVersion             = 2
	memoryMaxBytes                  = 64 * 1024
	memoryMaxEntriesPerPart         = 32
	memoryMaxSourceEntries          = 128
	memoryMaxEntryText              = 1_200
	memoryMaxEntryEvidence          = 1_000
	memoryMaxSourceIDs              = 8
	memoryMaxPromptDocument         = 24_000
	memoryPromptCompactionThreshold = 16_000
	memorySectionPreferences        = "preferences"
	memorySectionDecisions          = "decisions"
	memorySectionAvoid              = "things_to_avoid"
	memorySectionQuestions          = "questions"
	memorySectionRecent             = "recent_context"
	memorySectionSkills             = "skills"
	memoryMaxSkillEntries           = 64
	memoryContextMaxRunes           = 16_000
)

// MemoryEntry is one source-backed fact in the project aggregate.
type MemoryEntry struct {
	ID               string   `json:"id"`
	State            string   `json:"state"`
	Text             string   `json:"text"`
	Evidence         string   `json:"evidence"`
	SourceMessageIDs []string `json:"source_message_ids"`
	FirstSeenUTC     string   `json:"first_seen_utc"`
	LastSeenUTC      string   `json:"last_seen_utc"`
}

// MemorySource records the durable message anchors that support the file.
type MemorySource struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Seq       int    `json:"seq"`
	SeenAtUTC string `json:"seen_at_utc"`
}

// MemorySkillReference records a discovered or explicitly activated skill.
// It stores provenance, not the descriptor body.
type MemorySkillReference struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Scope            string   `json:"scope"`
	Path             string   `json:"path"`
	ContentHash      string   `json:"content_hash"`
	UseCount         int      `json:"use_count"`
	LastUsedUTC      string   `json:"last_used_utc"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

// MemoryDocument is the application-owned representation of memories.md.
// Recap artifacts remain the detailed session evidence ledger.
type MemoryDocument struct {
	FormatVersion  int
	UpdatedAtUTC   string
	LastSessionID  string
	LastMessageID  string
	LastMessageSeq int
	Preferences    []MemoryEntry
	Decisions      []MemoryEntry
	ThingsToAvoid  []MemoryEntry
	Questions      []MemoryEntry
	RecentContext  []MemoryEntry
	Skills         []MemorySkillReference
	Sources        []MemorySource
}

// MemoryFact is the model-facing shape for one proposed aggregate entry.
type MemoryFact struct {
	Text             string   `json:"text"`
	Evidence         string   `json:"evidence"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

// MemorySupersession explicitly replaces an older fact with source-backed text.
type MemorySupersession struct {
	Category         string   `json:"category"`
	ExistingText     string   `json:"existing_text"`
	ReplacementText  string   `json:"replacement_text"`
	Evidence         string   `json:"evidence"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

// MemoryEnvelope is the only model output accepted by the memory worker.
type MemoryEnvelope struct {
	Preferences   []MemoryFact         `json:"preferences"`
	Decisions     []MemoryFact         `json:"decisions"`
	ThingsToAvoid []MemoryFact         `json:"things_to_avoid"`
	Questions     []MemoryFact         `json:"questions"`
	RecentContext []MemoryFact         `json:"recent_context"`
	Supersessions []MemorySupersession `json:"supersessions"`
}

// NewMemoryDocument returns an empty, valid aggregate.
func NewMemoryDocument() MemoryDocument {
	return MemoryDocument{
		FormatVersion: memoryFormatVersion,
		Preferences:   make([]MemoryEntry, 0),
		Decisions:     make([]MemoryEntry, 0),
		ThingsToAvoid: make([]MemoryEntry, 0),
		Questions:     make([]MemoryEntry, 0),
		RecentContext: make([]MemoryEntry, 0),
		Skills:        make([]MemorySkillReference, 0),
		Sources:       make([]MemorySource, 0),
	}
}

// ParseMemoryEnvelope decodes strict JSON and validates all cited messages.
func ParseMemoryEnvelope(raw []byte, snapshot Snapshot) (MemoryEnvelope, error) {
	return parseMemoryEnvelope(raw, snapshot, false)
}

func parseMemoryEnvelopeWithRecentContextRecovery(raw []byte, snapshot Snapshot) (MemoryEnvelope, error) {
	return parseMemoryEnvelope(raw, snapshot, true)
}

func parseMemoryEnvelope(raw []byte, snapshot Snapshot, recoverRecentContext bool) (MemoryEnvelope, error) {
	raw = escapeLiteralJSONControls(raw)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return MemoryEnvelope{}, fmt.Errorf("memory: decode envelope: %w", err)
	}
	for _, key := range []string{
		"preferences", "decisions", "things_to_avoid", "questions", "recent_context", "supersessions",
	} {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			return MemoryEnvelope{}, fmt.Errorf("memory: %s is required", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope MemoryEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return MemoryEnvelope{}, fmt.Errorf("memory: decode envelope: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return MemoryEnvelope{}, errors.New("memory: envelope has trailing JSON")
		}
		return MemoryEnvelope{}, fmt.Errorf("memory: envelope has trailing data: %w", err)
	}
	if recoverRecentContext {
		discardInvalidRecentContext(&envelope, snapshot)
	}
	if err := validateMemoryEnvelope(envelope, snapshot); err != nil {
		return MemoryEnvelope{}, err
	}
	return envelope, nil
}

// discardInvalidRecentContext removes only short-lived context that the model
// could not anchor to the current snapshot. Durable sections remain strict.
func discardInvalidRecentContext(envelope *MemoryEnvelope, snapshot Snapshot) {
	known := snapshotMessageIDs(snapshot)
	filtered := envelope.RecentContext[:0]
	for _, fact := range envelope.RecentContext {
		if validCurrentMemorySources(fact.SourceMessageIDs, known) {
			filtered = append(filtered, fact)
		}
	}
	envelope.RecentContext = filtered
}

func validCurrentMemorySources(sourceIDs []string, known map[string]struct{}) bool {
	if len(sourceIDs) == 0 || len(sourceIDs) > memoryMaxSourceIDs {
		return false
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			return false
		}
		if _, ok := known[sourceID]; !ok {
			return false
		}
		if _, ok := seen[sourceID]; ok {
			return false
		}
		seen[sourceID] = struct{}{}
	}
	return true
}

func validateMemoryEnvelope(envelope MemoryEnvelope, snapshot Snapshot) error {
	known := snapshotMessageIDs(snapshot)
	seen := make(map[string]struct{})
	sections := []struct {
		name  string
		facts []MemoryFact
	}{
		{memorySectionPreferences, envelope.Preferences},
		{memorySectionDecisions, envelope.Decisions},
		{memorySectionAvoid, envelope.ThingsToAvoid},
		{memorySectionQuestions, envelope.Questions},
		{memorySectionRecent, envelope.RecentContext},
	}
	for _, section := range sections {
		if len(section.facts) > memoryMaxEntriesPerPart {
			return fmt.Errorf("memory: too many %s entries", section.name)
		}
		for index, fact := range section.facts {
			if err := validateMemoryFact(section.name, index, fact, known, seen); err != nil {
				return err
			}
		}
	}
	if len(envelope.Supersessions) > memoryMaxEntriesPerPart {
		return errors.New("memory: too many supersessions")
	}
	for index, supersession := range envelope.Supersessions {
		if err := validateSupersession(index, supersession, known); err != nil {
			return err
		}
	}
	return nil
}

func validateMemoryFact(section string, index int, fact MemoryFact, known, seen map[string]struct{}) error {
	text := strings.TrimSpace(fact.Text)
	evidence := strings.TrimSpace(fact.Evidence)
	if text == "" || len([]rune(text)) > memoryMaxEntryText {
		return fmt.Errorf("memory: %s entry %d has invalid text", section, index)
	}
	if evidence == "" || len([]rune(evidence)) > memoryMaxEntryEvidence {
		return fmt.Errorf("memory: %s entry %d has invalid evidence", section, index)
	}
	if asksForSecret(text) || asksForSecret(evidence) || containsSecretMaterial(text) || containsSecretMaterial(evidence) {
		return fmt.Errorf("memory: %s entry %d contains secret-like material", section, index)
	}
	if err := validateSources(fact.SourceMessageIDs, known, section, index); err != nil {
		return err
	}
	key := memoryEntryKey(section, text)
	if _, exists := seen[key]; exists {
		return fmt.Errorf("memory: duplicate %s entry %d", section, index)
	}
	seen[key] = struct{}{}
	return nil
}

func validateSupersession(index int, value MemorySupersession, known map[string]struct{}) error {
	if !validMemorySection(value.Category) {
		return fmt.Errorf("memory: supersession %d has invalid category", index)
	}
	if strings.TrimSpace(value.ExistingText) == "" || strings.TrimSpace(value.ReplacementText) == "" {
		return fmt.Errorf("memory: supersession %d requires existing and replacement text", index)
	}
	if len([]rune(value.ExistingText)) > memoryMaxEntryText || len([]rune(value.ReplacementText)) > memoryMaxEntryText || len([]rune(value.Evidence)) > memoryMaxEntryEvidence {
		return fmt.Errorf("memory: supersession %d exceeds text limits", index)
	}
	if asksForSecret(value.ExistingText) || asksForSecret(value.ReplacementText) || asksForSecret(value.Evidence) || containsSecretMaterial(value.ExistingText) || containsSecretMaterial(value.ReplacementText) || containsSecretMaterial(value.Evidence) {
		return fmt.Errorf("memory: supersession %d contains secret-like material", index)
	}
	if strings.TrimSpace(value.Evidence) == "" {
		return fmt.Errorf("memory: supersession %d requires evidence", index)
	}
	return validateSources(value.SourceMessageIDs, known, "supersession", index)
}

func validMemorySection(value string) bool {
	switch value {
	case memorySectionPreferences, memorySectionDecisions, memorySectionAvoid, memorySectionQuestions, memorySectionRecent:
		return true
	default:
		return false
	}
}

// MergeMemoryWithSkills applies model facts and request-time skill provenance.
func MergeMemoryWithSkills(previous MemoryDocument, envelope MemoryEnvelope, snapshot Snapshot, skillRefs []MemorySkillReference, now time.Time) (MemoryDocument, error) {
	if err := validateMemoryEnvelope(envelope, snapshot); err != nil {
		return MemoryDocument{}, err
	}
	document := cloneMemoryDocument(previous)
	if document.FormatVersion == 0 {
		document.FormatVersion = memoryFormatVersion
	}
	if document.FormatVersion == 1 {
		document.FormatVersion = memoryFormatVersion
	}
	if document.FormatVersion != memoryFormatVersion {
		return MemoryDocument{}, fmt.Errorf("memory: unsupported format version %d", document.FormatVersion)
	}
	if now.IsZero() {
		now = time.Now()
	}
	nowUTC := now.UTC().Format(time.RFC3339Nano)
	document.UpdatedAtUTC = nowUTC
	document.LastSessionID = snapshot.SessionID
	document.LastMessageID = snapshot.SourceEndMessageID
	document.LastMessageSeq = snapshot.SourceEndSeq
	document.Sources = mergeMemorySources(document.Sources, snapshot, nowUTC)

	for _, supersession := range envelope.Supersessions {
		markSuperseded(&document, supersession, nowUTC)
	}
	mergeMemoryFacts(&document.Preferences, memorySectionPreferences, envelope.Preferences, nowUTC)
	mergeMemoryFacts(&document.Decisions, memorySectionDecisions, envelope.Decisions, nowUTC)
	mergeMemoryFacts(&document.ThingsToAvoid, memorySectionAvoid, envelope.ThingsToAvoid, nowUTC)
	mergeMemoryFacts(&document.Questions, memorySectionQuestions, envelope.Questions, nowUTC)
	mergeMemoryFacts(&document.RecentContext, memorySectionRecent, envelope.RecentContext, nowUTC)
	mergeMemorySkills(&document.Skills, skillRefs, snapshot.SourceEndMessageID, nowUTC)
	trimMemoryDocument(&document)
	if _, err := RenderMemoryDocument(document); err != nil {
		return MemoryDocument{}, err
	}
	return document, nil
}

func mergeMemorySkills(target *[]MemorySkillReference, refs []MemorySkillReference, sourceMessageID, nowUTC string) {
	for _, ref := range refs {
		ref.Name = strings.TrimSpace(ref.Name)
		ref.Scope = strings.TrimSpace(ref.Scope)
		ref.Path = strings.TrimSpace(ref.Path)
		if ref.Name == "" || ref.Path == "" {
			continue
		}
		if ref.ID == "" {
			ref.ID = memorySkillKey(ref.Name, ref.Scope, ref.Path)
		}
		if ref.LastUsedUTC == "" {
			ref.LastUsedUTC = nowUTC
		}
		if len(ref.SourceMessageIDs) == 0 && sourceMessageID != "" {
			ref.SourceMessageIDs = []string{sourceMessageID}
		}
		found := -1
		for index := range *target {
			if (*target)[index].ID == ref.ID {
				found = index
				break
			}
		}
		if found < 0 {
			if ref.UseCount <= 0 {
				ref.UseCount = 1
			}
			*target = append(*target, ref)
			continue
		}
		entry := &(*target)[found]
		entry.Name = ref.Name
		entry.Scope = ref.Scope
		entry.Path = ref.Path
		entry.ContentHash = ref.ContentHash
		entry.UseCount += max(1, ref.UseCount)
		entry.LastUsedUTC = ref.LastUsedUTC
		entry.SourceMessageIDs = mergeStrings(entry.SourceMessageIDs, ref.SourceMessageIDs)
	}
}

func memorySkillKey(name, scope, path string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(scope)) + "\x00" + strings.ToLower(strings.TrimSpace(name)) + "\x00" + filepath.Clean(path)))
	return fmt.Sprintf("skill_%x", digest[:8])
}

// SkillReferenceID returns the stable identifier used in memories.md.
func SkillReferenceID(name, scope, path string) string {
	return memorySkillKey(name, scope, path)
}

func mergeMemoryFacts(target *[]MemoryEntry, category string, facts []MemoryFact, nowUTC string) {
	for _, fact := range facts {
		key := memoryEntryKey(category, fact.Text)
		found := -1
		for index := range *target {
			if (*target)[index].ID == key {
				found = index
				break
			}
		}
		if found < 0 {
			*target = append(*target, MemoryEntry{
				ID:               key,
				State:            "active",
				Text:             strings.TrimSpace(fact.Text),
				Evidence:         strings.TrimSpace(fact.Evidence),
				SourceMessageIDs: uniqueStrings(fact.SourceMessageIDs),
				FirstSeenUTC:     nowUTC,
				LastSeenUTC:      nowUTC,
			})
			continue
		}
		entry := &(*target)[found]
		entry.State = "active"
		entry.Evidence = strings.TrimSpace(fact.Evidence)
		entry.LastSeenUTC = nowUTC
		entry.SourceMessageIDs = mergeStrings(entry.SourceMessageIDs, fact.SourceMessageIDs)
	}
}

func markSuperseded(document *MemoryDocument, value MemorySupersession, nowUTC string) {
	target := memorySection(document, value.Category)
	if target == nil {
		return
	}
	canonical := memoryCanonical(value.ExistingText)
	for index := range *target {
		if memoryCanonical((*target)[index].Text) == canonical {
			(*target)[index].State = "superseded"
		}
	}
	mergeMemoryFacts(target, value.Category, []MemoryFact{{
		Text:             value.ReplacementText,
		Evidence:         value.Evidence,
		SourceMessageIDs: value.SourceMessageIDs,
	}}, nowUTC)
}

func memorySection(document *MemoryDocument, category string) *[]MemoryEntry {
	switch category {
	case memorySectionPreferences:
		return &document.Preferences
	case memorySectionDecisions:
		return &document.Decisions
	case memorySectionAvoid:
		return &document.ThingsToAvoid
	case memorySectionQuestions:
		return &document.Questions
	case memorySectionRecent:
		return &document.RecentContext
	default:
		return nil
	}
}

func mergeMemorySources(existing []MemorySource, snapshot Snapshot, seenAtUTC string) []MemorySource {
	seen := make(map[string]struct{}, len(existing)+len(snapshot.Messages))
	out := make([]MemorySource, 0, len(existing)+len(snapshot.Messages))
	for _, source := range existing {
		key := source.SessionID + "\x00" + source.MessageID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, source)
	}
	for _, message := range snapshot.Messages {
		key := message.SessionID + "\x00" + message.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, MemorySource{
			SessionID: message.SessionID,
			MessageID: message.ID,
			Seq:       message.Seq,
			SeenAtUTC: seenAtUTC,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		if out[i].SessionID != out[j].SessionID {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].MessageID < out[j].MessageID
	})
	if len(out) > memoryMaxSourceEntries {
		out = out[len(out)-memoryMaxSourceEntries:]
	}
	return out
}

func trimMemoryDocument(document *MemoryDocument) {
	for _, target := range []*[]MemoryEntry{
		&document.Preferences,
		&document.Decisions,
		&document.ThingsToAvoid,
		&document.Questions,
		&document.RecentContext,
	} {
		sort.SliceStable(*target, func(i, j int) bool {
			if (*target)[i].State != (*target)[j].State {
				return (*target)[i].State == "active"
			}
			if (*target)[i].LastSeenUTC != (*target)[j].LastSeenUTC {
				return (*target)[i].LastSeenUTC > (*target)[j].LastSeenUTC
			}
			return (*target)[i].ID < (*target)[j].ID
		})
		if len(*target) > memoryMaxEntriesPerPart {
			*target = (*target)[:memoryMaxEntriesPerPart]
		}
	}
	sort.SliceStable(document.Skills, func(i, j int) bool {
		if document.Skills[i].LastUsedUTC != document.Skills[j].LastUsedUTC {
			return document.Skills[i].LastUsedUTC > document.Skills[j].LastUsedUTC
		}
		return document.Skills[i].ID < document.Skills[j].ID
	})
	if len(document.Skills) > memoryMaxSkillEntries {
		document.Skills = document.Skills[:memoryMaxSkillEntries]
	}
}

func cloneMemoryDocument(document MemoryDocument) MemoryDocument {
	clone := document
	clone.Preferences = append([]MemoryEntry{}, document.Preferences...)
	clone.Decisions = append([]MemoryEntry{}, document.Decisions...)
	clone.ThingsToAvoid = append([]MemoryEntry{}, document.ThingsToAvoid...)
	clone.Questions = append([]MemoryEntry{}, document.Questions...)
	clone.RecentContext = append([]MemoryEntry{}, document.RecentContext...)
	clone.Skills = append([]MemorySkillReference{}, document.Skills...)
	clone.Sources = append([]MemorySource{}, document.Sources...)
	for _, entries := range []*[]MemoryEntry{
		&clone.Preferences,
		&clone.Decisions,
		&clone.ThingsToAvoid,
		&clone.Questions,
		&clone.RecentContext,
	} {
		for index := range *entries {
			(*entries)[index].SourceMessageIDs = append([]string{}, (*entries)[index].SourceMessageIDs...)
		}
	}
	for index := range clone.Skills {
		clone.Skills[index].SourceMessageIDs = append([]string{}, clone.Skills[index].SourceMessageIDs...)
	}
	return clone
}

func memoryEntryKey(category, text string) string {
	digest := sha256.Sum256([]byte(category + "\x00" + memoryCanonical(text)))
	return fmt.Sprintf("mem_%x", digest[:8])
}

func memoryCanonical(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeStrings(first, second []string) []string {
	return uniqueStrings(append(append([]string{}, first...), second...))
}

// RenderMemoryDocument renders the fixed human-readable Markdown layout.
func RenderMemoryDocument(document MemoryDocument) ([]byte, error) {
	document, err := prepareMemoryDocument(document)
	if err != nil {
		return nil, err
	}
	return renderMemoryDocumentRaw(document), nil
}

func prepareMemoryDocument(document MemoryDocument) (MemoryDocument, error) {
	if document.FormatVersion == 0 {
		document.FormatVersion = memoryFormatVersion
	}
	if document.FormatVersion == 1 {
		document.FormatVersion = memoryFormatVersion
	}
	if document.FormatVersion != memoryFormatVersion {
		return MemoryDocument{}, fmt.Errorf("memory: unsupported format version %d", document.FormatVersion)
	}
	if err := validateMemoryDocument(document); err != nil {
		return MemoryDocument{}, err
	}
	document = cloneMemoryDocument(document)
	trimMemoryDocument(&document)
	if err := fitMemoryDocument(&document); err != nil {
		return MemoryDocument{}, err
	}
	return document, nil
}

// renderMemoryPromptDocument removes durable provenance before the aggregate
// is shown to the model. The current snapshot below the aggregate is the only
// valid source for new message citations.
func renderMemoryPromptDocument(document MemoryDocument) ([]byte, error) {
	document, err := prepareMemoryDocument(document)
	if err != nil {
		return nil, err
	}
	document.LastSessionID = ""
	document.LastMessageID = ""
	document.LastMessageSeq = 0
	document.Sources = nil
	for _, entries := range []*[]MemoryEntry{
		&document.Preferences,
		&document.Decisions,
		&document.ThingsToAvoid,
		&document.Questions,
		&document.RecentContext,
	} {
		for index := range *entries {
			(*entries)[index].SourceMessageIDs = nil
		}
	}
	for index := range document.Skills {
		document.Skills[index].SourceMessageIDs = nil
	}
	return renderMemoryDocumentRaw(document), nil
}

func renderMemoryDocumentRaw(document MemoryDocument) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "format_version: %d\n", document.FormatVersion)
	fmt.Fprintf(&b, "updated_at_utc: %s\n", quoteMemoryValue(document.UpdatedAtUTC))
	fmt.Fprintf(&b, "last_session_id: %s\n", quoteMemoryValue(document.LastSessionID))
	fmt.Fprintf(&b, "last_message_id: %s\n", quoteMemoryValue(document.LastMessageID))
	fmt.Fprintf(&b, "last_message_seq: %d\n", document.LastMessageSeq)
	b.WriteString("---\n\n# Project memory\n\n")
	renderMemorySection(&b, "User preferences", document.Preferences)
	renderMemorySection(&b, "Decisions and constraints", document.Decisions)
	renderMemorySection(&b, "Things to avoid", document.ThingsToAvoid)
	renderMemorySection(&b, "Open questions", document.Questions)
	renderMemorySection(&b, "Recent context", document.RecentContext)
	b.WriteString("## Skills\n\n")
	for _, skill := range document.Skills {
		raw, _ := json.Marshal(skill)
		fmt.Fprintf(&b, "- %s\n", raw)
	}
	b.WriteString("\n")
	b.WriteString("## Source ledger\n\n")
	for _, source := range document.Sources {
		raw, _ := json.Marshal(source)
		fmt.Fprintf(&b, "- %s\n", raw)
	}
	return []byte(b.String())
}

func fitMemoryDocument(document *MemoryDocument) error {
	for {
		if len(renderMemoryDocumentRaw(*document)) <= memoryMaxBytes {
			return nil
		}
		bestSection := -1
		bestReduction := 0
		sections := []*[]MemoryEntry{
			&document.Preferences,
			&document.Decisions,
			&document.ThingsToAvoid,
			&document.Questions,
			&document.RecentContext,
		}
		for index, section := range sections {
			if len(*section) == 0 {
				continue
			}
			candidate := cloneMemoryDocument(*document)
			candidateSections := []*[]MemoryEntry{
				&candidate.Preferences,
				&candidate.Decisions,
				&candidate.ThingsToAvoid,
				&candidate.Questions,
				&candidate.RecentContext,
			}
			candidateSection := candidateSections[index]
			*candidateSection = (*candidateSection)[:len(*candidateSection)-1]
			reduction := len(renderMemoryDocumentRaw(*document)) - len(renderMemoryDocumentRaw(candidate))
			if reduction > bestReduction {
				bestSection = index
				bestReduction = reduction
			}
		}
		if bestSection >= 0 {
			*sections[bestSection] = (*sections[bestSection])[:len(*sections[bestSection])-1]
			continue
		}
		if len(document.Skills) > 0 {
			document.Skills = document.Skills[:len(document.Skills)-1]
			continue
		}
		if len(document.Sources) > 0 {
			document.Sources = document.Sources[:len(document.Sources)-1]
			continue
		}
		return fmt.Errorf("memory: rendered document exceeds %d bytes", memoryMaxBytes)
	}
}

func renderMemorySection(builder *strings.Builder, heading string, entries []MemoryEntry) {
	fmt.Fprintf(builder, "## %s\n\n", heading)
	for _, entry := range entries {
		fmt.Fprintf(builder, "- id: %s\n  state: %s\n  text: %s\n  evidence: %s\n  source_message_ids: %s\n  first_seen_utc: %s\n  last_seen_utc: %s\n",
			quoteMemoryValue(entry.ID), quoteMemoryValue(entry.State), quoteMemoryValue(entry.Text), quoteMemoryValue(entry.Evidence), quoteMemoryStrings(entry.SourceMessageIDs), quoteMemoryValue(entry.FirstSeenUTC), quoteMemoryValue(entry.LastSeenUTC))
	}
	builder.WriteString("\n")
}

func quoteMemoryValue(value string) string {
	return strconv.Quote(strings.ReplaceAll(strings.TrimSpace(value), "\n", " "))
}

func quoteMemoryStrings(values []string) string {
	raw, _ := json.Marshal(uniqueStrings(values))
	return string(raw)
}

// ParseMemoryDocument parses only the application-owned layout emitted above.
func ParseMemoryDocument(raw []byte) (MemoryDocument, error) {
	if len(raw) > memoryMaxBytes {
		return MemoryDocument{}, fmt.Errorf("memory: document exceeds %d bytes", memoryMaxBytes)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 7 || strings.TrimSpace(lines[0]) != "---" {
		return MemoryDocument{}, errors.New("memory: invalid front matter")
	}
	document := NewMemoryDocument()
	frontEnd := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			frontEnd = index
			break
		}
		key, value, ok := strings.Cut(lines[index], ":")
		if !ok {
			return MemoryDocument{}, errors.New("memory: invalid front matter field")
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "format_version":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return MemoryDocument{}, fmt.Errorf("memory: format version: %w", err)
			}
			document.FormatVersion = parsed
		case "updated_at_utc":
			document.UpdatedAtUTC = unquoteMemoryValue(value)
		case "last_session_id":
			document.LastSessionID = unquoteMemoryValue(value)
		case "last_message_id":
			document.LastMessageID = unquoteMemoryValue(value)
		case "last_message_seq":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return MemoryDocument{}, fmt.Errorf("memory: last message sequence: %w", err)
			}
			document.LastMessageSeq = parsed
		default:
			return MemoryDocument{}, fmt.Errorf("memory: unknown front matter field %q", key)
		}
	}
	if frontEnd < 0 {
		return MemoryDocument{}, errors.New("memory: missing front matter terminator")
	}
	if document.FormatVersion == 1 {
		document.FormatVersion = memoryFormatVersion
	}
	section := ""
	var current *MemoryEntry
	for _, line := range lines[frontEnd+1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			section = memoryHeadingSection(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			current = nil
			if section == "" {
				return MemoryDocument{}, fmt.Errorf("memory: unknown section %q", trimmed)
			}
			continue
		}
		if trimmed == "" || strings.TrimSpace(line) == "# Project memory" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			if section == "source_ledger" {
				source, err := parseMemorySource(line)
				if err != nil {
					return MemoryDocument{}, err
				}
				document.Sources = append(document.Sources, source)
				continue
			}
			if section == memorySectionSkills {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				var skill MemorySkillReference
				if err := json.Unmarshal([]byte(value), &skill); err != nil {
					return MemoryDocument{}, fmt.Errorf("memory: skill entry: %w", err)
				}
				document.Skills = append(document.Skills, skill)
				continue
			}
			entries := memorySection(&document, section)
			if entries == nil {
				return MemoryDocument{}, fmt.Errorf("memory: invalid entry section %q", section)
			}
			*entries = append(*entries, MemoryEntry{})
			current = &(*entries)[len(*entries)-1]
			if err := parseMemoryField(current, strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))); err != nil {
				return MemoryDocument{}, err
			}
			continue
		}
		if current == nil {
			return MemoryDocument{}, errors.New("memory: field without entry")
		}
		if err := parseMemoryField(current, trimmed); err != nil {
			return MemoryDocument{}, err
		}
	}
	if err := validateMemoryDocument(document); err != nil {
		return MemoryDocument{}, err
	}
	return document, nil
}

func memoryHeadingSection(heading string) string {
	switch heading {
	case "User preferences":
		return memorySectionPreferences
	case "Decisions and constraints":
		return memorySectionDecisions
	case "Things to avoid":
		return memorySectionAvoid
	case "Open questions":
		return memorySectionQuestions
	case "Recent context":
		return memorySectionRecent
	case "Skills":
		return memorySectionSkills
	case "Source ledger":
		return "source_ledger"
	default:
		return ""
	}
}

func parseMemoryField(entry *MemoryEntry, line string) error {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return errors.New("memory: invalid entry field")
	}
	value = strings.TrimSpace(value)
	switch strings.TrimSpace(key) {
	case "id":
		entry.ID = unquoteMemoryValue(value)
	case "state":
		entry.State = unquoteMemoryValue(value)
	case "text":
		entry.Text = unquoteMemoryValue(value)
	case "evidence":
		entry.Evidence = unquoteMemoryValue(value)
	case "source_message_ids":
		if err := json.Unmarshal([]byte(value), &entry.SourceMessageIDs); err != nil {
			return fmt.Errorf("memory: source message IDs: %w", err)
		}
	case "first_seen_utc":
		entry.FirstSeenUTC = unquoteMemoryValue(value)
	case "last_seen_utc":
		entry.LastSeenUTC = unquoteMemoryValue(value)
	default:
		return fmt.Errorf("memory: unknown entry field %q", key)
	}
	return nil
}

func parseMemorySource(first string) (MemorySource, error) {
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(first), "- "))
	var source MemorySource
	if err := json.Unmarshal([]byte(value), &source); err != nil {
		return MemorySource{}, fmt.Errorf("memory: source ledger entry: %w", err)
	}
	return source, nil
}

func unquoteMemoryValue(value string) string {
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return strings.Trim(value, "\"")
	}
	return parsed
}

func validateMemoryDocument(document MemoryDocument) error {
	if document.FormatVersion != memoryFormatVersion {
		return fmt.Errorf("memory: unsupported format version %d", document.FormatVersion)
	}
	for _, section := range []struct {
		name    string
		entries []MemoryEntry
	}{
		{memorySectionPreferences, document.Preferences},
		{memorySectionDecisions, document.Decisions},
		{memorySectionAvoid, document.ThingsToAvoid},
		{memorySectionQuestions, document.Questions},
		{memorySectionRecent, document.RecentContext},
	} {
		if len(section.entries) > memoryMaxEntriesPerPart {
			return fmt.Errorf("memory: too many %s entries", section.name)
		}
		seen := make(map[string]struct{}, len(section.entries))
		for index, entry := range section.entries {
			if err := validateMemoryEntry(section.name, index, entry, seen); err != nil {
				return err
			}
		}
	}
	if len(document.Skills) > memoryMaxSkillEntries {
		return errors.New("memory: too many skills")
	}
	seenSkills := make(map[string]struct{}, len(document.Skills))
	for index, skill := range document.Skills {
		if strings.TrimSpace(skill.ID) == "" || strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Scope) == "" || strings.TrimSpace(skill.Path) == "" {
			return fmt.Errorf("memory: incomplete skill entry %d", index)
		}
		if skill.UseCount <= 0 || strings.TrimSpace(skill.LastUsedUTC) == "" || len(skill.SourceMessageIDs) == 0 || len(skill.SourceMessageIDs) > memoryMaxSourceIDs {
			return fmt.Errorf("memory: invalid skill entry %d provenance", index)
		}
		if _, exists := seenSkills[skill.ID]; exists {
			return fmt.Errorf("memory: duplicate skill entry %d", index)
		}
		seenSkills[skill.ID] = struct{}{}
	}
	if len(document.Sources) > memoryMaxSourceEntries {
		return errors.New("memory: too many source entries")
	}
	seenSources := make(map[string]struct{}, len(document.Sources))
	for index, source := range document.Sources {
		if strings.TrimSpace(source.SessionID) == "" || strings.TrimSpace(source.MessageID) == "" || source.Seq <= 0 || strings.TrimSpace(source.SeenAtUTC) == "" {
			return fmt.Errorf("memory: incomplete source entry %d", index)
		}
		key := source.SessionID + "\x00" + source.MessageID
		if _, exists := seenSources[key]; exists {
			return fmt.Errorf("memory: duplicate source entry %d", index)
		}
		seenSources[key] = struct{}{}
	}
	return nil
}

func validateMemoryEntry(category string, index int, entry MemoryEntry, seen map[string]struct{}) error {
	if strings.TrimSpace(entry.ID) == "" || len([]rune(entry.ID)) > 128 {
		return fmt.Errorf("memory: invalid %s entry %d id", category, index)
	}
	if entry.State != "active" && entry.State != "superseded" {
		return fmt.Errorf("memory: invalid %s entry %d state", category, index)
	}
	if strings.TrimSpace(entry.Text) == "" || len([]rune(entry.Text)) > memoryMaxEntryText {
		return fmt.Errorf("memory: invalid %s entry %d text", category, index)
	}
	if strings.TrimSpace(entry.Evidence) == "" || len([]rune(entry.Evidence)) > memoryMaxEntryEvidence {
		return fmt.Errorf("memory: invalid %s entry %d evidence", category, index)
	}
	if len(entry.SourceMessageIDs) == 0 || len(entry.SourceMessageIDs) > memoryMaxSourceIDs {
		return fmt.Errorf("memory: %s entry %d needs one to %d source message IDs", category, index, memoryMaxSourceIDs)
	}
	if asksForSecret(entry.Text) || asksForSecret(entry.Evidence) || containsSecretMaterial(entry.Text) || containsSecretMaterial(entry.Evidence) {
		return fmt.Errorf("memory: %s entry %d contains secret-like material", category, index)
	}
	key := memoryCanonical(entry.Text)
	if _, exists := seen[key]; exists {
		return fmt.Errorf("memory: duplicate %s entry %d", category, index)
	}
	seen[key] = struct{}{}
	seenSources := make(map[string]struct{}, len(entry.SourceMessageIDs))
	for _, sourceID := range entry.SourceMessageIDs {
		if strings.TrimSpace(sourceID) == "" {
			return fmt.Errorf("memory: %s entry %d has an empty source message ID", category, index)
		}
		if _, exists := seenSources[sourceID]; exists {
			return fmt.Errorf("memory: %s entry %d has a duplicate source message ID", category, index)
		}
		seenSources[sourceID] = struct{}{}
	}
	return nil
}

// ReadMemoryDocument reads the project aggregate. Missing memory is an empty
// document so first-use setup does not require a placeholder file.
func ReadMemoryDocument(workdir string) (MemoryDocument, error) {
	root, err := filepath.Abs(workdir)
	if err != nil {
		return MemoryDocument{}, fmt.Errorf("memory: resolve workdir: %w", err)
	}
	path, err := safeJoin(root, filepath.Join("knowledge-base"), "memories.md")
	if err != nil {
		return MemoryDocument{}, err
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewMemoryDocument(), nil
	}
	if err != nil {
		return MemoryDocument{}, fmt.Errorf("memory: read document: %w", err)
	}
	return ParseMemoryDocument(body)
}

// LoadMemoryContext reads and bounds the safe aggregate block used in a
// parent request. Missing memories produce an empty string. The rendered
// block intentionally omits durable source IDs because only the current
// request snapshot may be used for new citations.
func LoadMemoryContext(workdir string) (string, error) {
	document, err := ReadMemoryDocument(workdir)
	if err != nil {
		return "", err
	}
	raw, err := renderMemoryPromptDocument(document)
	if err != nil {
		return "", err
	}
	context := strings.TrimSpace(string(raw))
	if context == "" || context == "---" {
		return "", nil
	}
	return truncateMemoryContext(context), nil
}

func truncateMemoryContext(value string) string {
	runes := []rune(value)
	if len(runes) <= memoryContextMaxRunes {
		return value
	}
	return string(runes[:memoryContextMaxRunes]) + "\n[aggregate truncated]"
}

// WriteMemoryDocument atomically replaces the project aggregate.
func WriteMemoryDocument(ctx context.Context, workdir string, document MemoryDocument) (string, error) {
	if err := requireContext(ctx); err != nil {
		return "", err
	}
	body, err := RenderMemoryDocument(document)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(workdir)
	if err != nil {
		return "", fmt.Errorf("memory: resolve workdir: %w", err)
	}
	path, err := safeJoin(root, filepath.Join("knowledge-base"), "memories.md")
	if err != nil {
		return "", err
	}
	return writeAtomic(ctx, path, body)
}

func BuildMemoryPrompt(snapshot Snapshot, document MemoryDocument, relatedRecaps string) (string, error) {
	return BuildMemoryPromptFor("", snapshot, document, relatedRecaps)
}

// BuildMemoryPromptFor renders the memory request using prompt files from
// workdir while keeping evidence serialization typed in Go.
func BuildMemoryPromptFor(workdir string, snapshot Snapshot, document MemoryDocument, relatedRecaps string) (string, error) {
	// Keep the prompt focused: only snapshot messages plus recent_context
	// (and skills) are sent for memory creation. Preferences, decisions,
	// things_to_avoid, questions and source ledger are dropped to keep the
	// prompt at ~5000 tokens with priority to recent_context.
	filtered := filterMemoryDocumentForPrompt(document)
	current, err := renderMemoryPromptDocument(filtered)
	if err != nil {
		return "", err
	}
	if len([]rune(string(current))) > memoryPromptCompactionThreshold {
		compact := compactMemoryPromptDocument(filtered, snapshot)
		current, err = renderMemoryPromptDocument(compact)
		if err != nil {
			return "", err
		}
	}
	// Enforce ~5000 tokens (~20000 runes) with priority to snapshot + recent_context
	const memoryPromptMaxRunes = 20000
	promptLimit := memoryPromptLimitFor(prompts.New(workdir).Must("recap/memory-repair.md"))
	if promptLimit > memoryPromptMaxRunes {
		promptLimit = memoryPromptMaxRunes
	}
	currentText := truncateString(string(current), memoryMaxPromptDocument)
	// Drop related knowledge by default to keep prompt small; only snapshot
	// plus recent_context should go. If needed, related can be re-added
	// behind a tiny remaining budget, but for now drop it.
	relatedText := ""
	_ = relatedRecaps
	snapshotPrompt, err := buildPrompt(snapshot, "")
	if err != nil {
		return "", fmt.Errorf("memory: snapshot prompt: %w", err)
	}
	base := renderMemoryPrompt(snapshot, "", "", snapshotPrompt)
	remaining := promptLimit - len([]rune(base))
	if remaining < 0 {
		return "", errors.New("memory: snapshot prompt exceeds limit")
	}
	currentText = truncateString(currentText, remaining)
	remaining -= len([]rune(currentText))
	relatedText = truncateString(relatedText, remaining)
	prompt := renderMemoryPrompt(snapshot, currentText, relatedText, snapshotPrompt)
	if len([]rune(prompt)) > promptLimit {
		return "", errors.New("memory: prompt exceeds limit")
	}
	return prompt, nil
}

func filterMemoryDocumentForPrompt(document MemoryDocument) MemoryDocument {
	filtered := cloneMemoryDocument(document)
	// Drop non-priority sections to keep prompt at ~5000 tokens
	filtered.Preferences = nil
	filtered.Decisions = nil
	filtered.ThingsToAvoid = nil
	filtered.Questions = nil
	// Keep RecentContext and Skills as priority
	// Sources are already stripped in renderMemoryPromptDocument, but clear here too
	filtered.Sources = nil
	return filtered
}

func memoryPromptLimitFor(repairInstruction string) int {
	limit := maxPromptText - len([]rune(repairInstruction))
	if limit < 1 {
		return maxPromptText
	}
	return limit
}

func renderMemoryPrompt(snapshot Snapshot, current, related, snapshotPrompt string) string {
	var b strings.Builder
	b.WriteString("Current memory document:\n<memories>\n")
	b.WriteString(current)
	b.WriteString("\n</memories>\nRelated local knowledge evidence (untrusted):\n<related_knowledge>\n")
	b.WriteString(related)
	b.WriteString("\n</related_knowledge>\n")
	b.WriteString("Only the following current message IDs may appear in source_message_ids: [")
	for index, message := range snapshot.Messages {
		if index > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(message.ID))
	}
	b.WriteString("]\n")
	if signals := ExtractMemorySignals(snapshot); len(signals) > 0 {
		b.WriteString("Explicit user signals (authoritative; preserve their category):\n<signals>\n")
		for _, signal := range signals {
			b.WriteString("<signal category=")
			b.WriteString(strconv.Quote(string(signal.Category)))
			b.WriteString(" source_message_ids=")
			b.WriteString(strconv.Quote(strings.Join(signal.SourceMessageIDs, ",")))
			b.WriteString(">\n")
			b.WriteString(signal.Statement)
			b.WriteString("\n</signal>\n")
		}
		b.WriteString("</signals>\n")
	}
	b.WriteString(snapshotPrompt)
	return b.String()
}

func compactMemoryPromptDocument(document MemoryDocument, snapshot Snapshot) MemoryDocument {
	messageIDs := make(map[string]struct{}, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		messageIDs[message.ID] = struct{}{}
	}
	compact := cloneMemoryDocument(document)
	compact.Preferences = compactMemoryPromptEntries(compact.Preferences, messageIDs)
	compact.Decisions = compactMemoryPromptEntries(compact.Decisions, messageIDs)
	compact.ThingsToAvoid = compactMemoryPromptEntries(compact.ThingsToAvoid, messageIDs)
	compact.Questions = compactMemoryPromptEntries(compact.Questions, messageIDs)
	compact.RecentContext = compactMemoryPromptEntries(compact.RecentContext, messageIDs)
	compact.Skills = compactMemoryPromptSkills(compact.Skills, messageIDs)
	return compact
}

func compactMemoryPromptEntries(entries []MemoryEntry, messageIDs map[string]struct{}) []MemoryEntry {
	compact := make([]MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.State != "active" || !memoryEntryUsesMessage(entry.SourceMessageIDs, messageIDs) {
			continue
		}
		compact = append(compact, entry)
	}
	return compact
}

func compactMemoryPromptSkills(skills []MemorySkillReference, messageIDs map[string]struct{}) []MemorySkillReference {
	compact := make([]MemorySkillReference, 0, len(skills))
	for _, skill := range skills {
		if memoryEntryUsesMessage(skill.SourceMessageIDs, messageIDs) {
			compact = append(compact, skill)
		}
	}
	return compact
}

func memoryEntryUsesMessage(sourceIDs []string, messageIDs map[string]struct{}) bool {
	for _, sourceID := range sourceIDs {
		if _, ok := messageIDs[sourceID]; ok {
			return true
		}
	}
	return false
}
