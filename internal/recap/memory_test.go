package recap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryDocumentRoundTrip(t *testing.T) {
	document := NewMemoryDocument()
	document.UpdatedAtUTC = "2026-08-22T12:00:00Z"
	document.LastSessionID = "ses_memory"
	document.LastMessageID = "msg_4"
	document.LastMessageSeq = 4
	document.Preferences = append(document.Preferences, MemoryEntry{
		ID:               "mem_pref",
		State:            "active",
		Text:             "Keep implementation plans evidence-backed.",
		Evidence:         "The user asked for phase gates.",
		SourceMessageIDs: []string{"msg_4"},
		FirstSeenUTC:     "2026-08-22T11:00:00Z",
		LastSeenUTC:      "2026-08-22T12:00:00Z",
	})
	document.Sources = append(document.Sources, MemorySource{
		SessionID: "ses_memory",
		MessageID: "msg_4",
		Seq:       4,
		SeenAtUTC: "2026-08-22T12:00:00Z",
	})
	document.Skills = append(document.Skills, MemorySkillReference{
		ID:               "skill_review",
		Name:             "review",
		Scope:            "local",
		Path:             "skills/review/SKILL.md",
		ContentHash:      "abc123",
		UseCount:         1,
		LastUsedUTC:      "2026-08-22T12:00:00Z",
		SourceMessageIDs: []string{"msg_4"},
	})
	raw, err := RenderMemoryDocument(document)
	if err != nil {
		t.Fatalf("RenderMemoryDocument: %v", err)
	}
	got, err := ParseMemoryDocument(raw)
	if err != nil {
		t.Fatalf("ParseMemoryDocument: %v\n%s", err, raw)
	}
	if got.LastSessionID != document.LastSessionID || got.LastMessageID != document.LastMessageID || len(got.Preferences) != 1 || len(got.Sources) != 1 || len(got.Skills) != 1 {
		t.Fatalf("round trip = %+v, want %+v", got, document)
	}
	if got.Preferences[0].Text != document.Preferences[0].Text || got.Sources[0].MessageID != "msg_4" {
		t.Fatalf("round-trip content = %+v", got)
	}
	if got.Skills[0].Name != "review" || got.Skills[0].Path != "skills/review/SKILL.md" {
		t.Fatalf("round-trip skills = %+v", got.Skills)
	}
}

func TestParseMemoryEnvelopeValidatesSourcesAndUnknownFields(t *testing.T) {
	snapshot := memoryTestSnapshot()
	raw := `{"preferences":[{"text":"Use focused plans","evidence":"The user requested a checklist","source_message_ids":["msg_4"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`
	envelope, err := ParseMemoryEnvelope([]byte(raw), snapshot)
	if err != nil {
		t.Fatalf("ParseMemoryEnvelope: %v", err)
	}
	if len(envelope.Preferences) != 1 || envelope.Preferences[0].Text != "Use focused plans" {
		t.Fatalf("envelope = %+v", envelope)
	}
	unknown := strings.TrimSuffix(raw, "}") + `,"extra":true}`
	if _, err := ParseMemoryEnvelope([]byte(unknown), snapshot); err == nil {
		t.Fatal("unknown memory field was accepted")
	}
	uncited := strings.Replace(raw, `["msg_4"]`, `[]`, 1)
	if _, err := ParseMemoryEnvelope([]byte(uncited), snapshot); err == nil {
		t.Fatal("uncited memory fact was accepted")
	}
}

func TestParseMemoryEnvelopeRejectsMalformedDuplicateSecretAndUnknownSource(t *testing.T) {
	snapshot := memoryTestSnapshot()
	base := `{"preferences":[{"text":"Use focused plans","evidence":"The user requested a checklist","source_message_ids":["msg_4"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`
	cases := map[string]string{
		"malformed":      base[:len(base)-1],
		"duplicate":      strings.Replace(base, `}],"decisions"`, `},{"text":"Use  focused plans","evidence":"The same request","source_message_ids":["msg_4"]}],"decisions"`, 1),
		"secret":         strings.Replace(base, "Use focused plans", "store the password", 1),
		"unknown source": strings.Replace(base, "msg_4", "msg_missing", 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseMemoryEnvelope([]byte(raw), snapshot); err == nil {
				t.Fatalf("ParseMemoryEnvelope accepted %s input", name)
			}
		})
	}
}

func TestParseMemoryFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "memories.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	document, err := ParseMemoryDocument(raw)
	if err != nil {
		t.Fatalf("ParseMemoryDocument: %v", err)
	}
	if len(document.Preferences) != 1 || len(document.Sources) != 1 {
		t.Fatalf("fixture = %+v", document)
	}
}

func TestMergeMemorySupersedesAndRetainsSourceLedger(t *testing.T) {
	snapshot := memoryTestSnapshot()
	previous := NewMemoryDocument()
	previous.Decisions = append(previous.Decisions, MemoryEntry{
		ID:               memoryEntryKey(memorySectionDecisions, "Use the old plan"),
		State:            "active",
		Text:             "Use the old plan",
		Evidence:         "Earlier decision",
		SourceMessageIDs: []string{"msg_1"},
		FirstSeenUTC:     "2026-08-22T10:00:00Z",
		LastSeenUTC:      "2026-08-22T10:00:00Z",
	})
	envelope := MemoryEnvelope{
		Preferences:   []MemoryFact{},
		Decisions:     []MemoryFact{},
		ThingsToAvoid: []MemoryFact{},
		Questions:     []MemoryFact{},
		RecentContext: []MemoryFact{},
		Supersessions: []MemorySupersession{{
			Category:         memorySectionDecisions,
			ExistingText:     "Use the old plan",
			ReplacementText:  "Use the checked plan",
			Evidence:         "The latest decision changed the plan",
			SourceMessageIDs: []string{"msg_4"},
		}},
	}
	got, err := MergeMemory(previous, envelope, snapshot, time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MergeMemory: %v", err)
	}
	var replacement, historical *MemoryEntry
	for index := range got.Decisions {
		entry := &got.Decisions[index]
		switch entry.Text {
		case "Use the checked plan":
			replacement = entry
		case "Use the old plan":
			historical = entry
		}
	}
	if replacement == nil || replacement.State != "active" || historical == nil || historical.State != "superseded" {
		t.Fatalf("decisions = %+v, want active replacement and retained superseded source", got.Decisions)
	}
	if len(got.Sources) != len(snapshot.Messages) {
		t.Fatalf("sources = %d, want %d", len(got.Sources), len(snapshot.Messages))
	}
}

func TestMemoryFileRoundTripIsAtomic(t *testing.T) {
	workdir := t.TempDir()
	document := NewMemoryDocument()
	document.UpdatedAtUTC = "2026-08-22T12:00:00Z"
	document.LastSessionID = "ses_memory"
	document.LastMessageID = "msg_4"
	document.LastMessageSeq = 4
	document.Preferences = append(document.Preferences, MemoryEntry{
		ID:               "mem_pref",
		State:            "active",
		Text:             "Keep memory local.",
		Evidence:         "The feature is project scoped.",
		SourceMessageIDs: []string{"msg_4"},
		FirstSeenUTC:     document.UpdatedAtUTC,
		LastSeenUTC:      document.UpdatedAtUTC,
	})
	digest, err := WriteMemoryDocument(context.Background(), workdir, document)
	if err != nil {
		t.Fatalf("WriteMemoryDocument: %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("digest = %q, want SHA-256", digest)
	}
	path := filepath.Join(workdir, "knowledge-base", "memories.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("memory file: %v", err)
	}
	got, err := ReadMemoryDocument(workdir)
	if err != nil {
		t.Fatalf("ReadMemoryDocument: %v", err)
	}
	if got.Preferences[0].Text != "Keep memory local." {
		t.Fatalf("memory = %+v", got)
	}
}

func TestRenderMemoryDocumentPrunesOldEntriesToBound(t *testing.T) {
	document := NewMemoryDocument()
	for index := 0; index < memoryMaxEntriesPerPart; index++ {
		text := "preference " + string(rune('a'+index)) + " " + strings.Repeat("detail ", 150)
		entry := MemoryEntry{
			ID:               "mem_" + string(rune('a'+index)),
			State:            "active",
			Text:             text,
			Evidence:         "Observed in the recent conversation.",
			SourceMessageIDs: []string{"msg_4"},
			FirstSeenUTC:     "2026-08-22T12:00:00Z",
			LastSeenUTC:      "2026-08-22T12:00:00Z",
		}
		document.Preferences = append(document.Preferences, entry)
		document.Decisions = append(document.Decisions, entryWithCategory(entry, memorySectionDecisions, text))
		document.ThingsToAvoid = append(document.ThingsToAvoid, entryWithCategory(entry, memorySectionAvoid, text))
		document.Questions = append(document.Questions, entryWithCategory(entry, memorySectionQuestions, text))
		document.RecentContext = append(document.RecentContext, entryWithCategory(entry, memorySectionRecent, text))
	}
	raw, err := RenderMemoryDocument(document)
	if err != nil {
		t.Fatalf("RenderMemoryDocument: %v", err)
	}
	if len(raw) > memoryMaxBytes {
		t.Fatalf("rendered document is %d bytes, want <= %d", len(raw), memoryMaxBytes)
	}
	parsed, err := ParseMemoryDocument(raw)
	if err != nil {
		t.Fatalf("ParseMemoryDocument: %v", err)
	}
	totalEntries := len(parsed.Preferences) + len(parsed.Decisions) + len(parsed.ThingsToAvoid) + len(parsed.Questions) + len(parsed.RecentContext)
	if totalEntries >= memoryMaxEntriesPerPart*5 {
		t.Fatalf("entries were not pruned: %d entries", totalEntries)
	}
}

func TestBuildMemoryPromptRespectsCombinedPromptLimit(t *testing.T) {
	document := NewMemoryDocument()
	for index := 0; index < memoryMaxEntriesPerPart; index++ {
		document.Preferences = append(document.Preferences, MemoryEntry{
			ID:               fmt.Sprintf("mem_prompt_%d", index),
			State:            "active",
			Text:             fmt.Sprintf("memory detail %d %s", index, strings.Repeat("detail ", 55)),
			Evidence:         strings.Repeat("memory evidence ", 25),
			SourceMessageIDs: []string{"msg_1"},
			FirstSeenUTC:     "2026-08-22T12:00:00Z",
			LastSeenUTC:      "2026-08-22T12:00:00Z",
		})
	}
	snapshot := memoryTestSnapshot()
	for index := range snapshot.Messages {
		snapshot.Messages[index].Text = strings.Repeat("current context ", 250)
	}

	prompt, err := BuildMemoryPrompt(snapshot, document, strings.Repeat("related evidence ", 900))
	if err != nil {
		t.Fatalf("BuildMemoryPrompt: %v", err)
	}
	if got := len([]rune(prompt)); got > maxPromptText {
		t.Fatalf("prompt length = %d, want <= %d", got, maxPromptText)
	}
}

func TestBuildMemoryPromptCompactsToCurrentSourceEntries(t *testing.T) {
	document := NewMemoryDocument()
	for index := 0; index < memoryMaxEntriesPerPart-1; index++ {
		document.Preferences = append(document.Preferences, MemoryEntry{
			ID:               fmt.Sprintf("mem_old_%d", index),
			State:            "active",
			Text:             fmt.Sprintf("old memory entry %d %s", index, strings.Repeat("old detail ", 35)),
			Evidence:         "Historical evidence.",
			SourceMessageIDs: []string{"msg_old"},
			FirstSeenUTC:     "2026-08-22T12:00:00Z",
			LastSeenUTC:      "2026-08-22T12:00:00Z",
		})
	}
	document.Preferences = append(document.Preferences, MemoryEntry{
		ID:               "mem_current",
		State:            "active",
		Text:             "current memory entry",
		Evidence:         "Current evidence.",
		SourceMessageIDs: []string{"msg_1"},
		FirstSeenUTC:     "2026-08-22T12:00:00Z",
		LastSeenUTC:      "2026-08-22T12:00:00Z",
	})

	prompt, err := BuildMemoryPrompt(memoryTestSnapshot(), document, "")
	if err != nil {
		t.Fatalf("BuildMemoryPrompt: %v", err)
	}
	if !strings.Contains(prompt, "current memory entry") {
		t.Fatalf("current entry missing from prompt: %q", prompt)
	}
	if strings.Contains(prompt, "old memory entry") {
		t.Fatalf("historical entries were not compacted: %q", prompt)
	}
}

func TestBuildMemoryPromptLeavesRepairCapacity(t *testing.T) {
	prompt, err := BuildMemoryPrompt(memoryTestSnapshot(), NewMemoryDocument(), "related evidence")
	if err != nil {
		t.Fatalf("BuildMemoryPrompt: %v", err)
	}
	if got := len([]rune(prompt)) + len([]rune(memoryRepairInstruction)); got > maxPromptText {
		t.Fatalf("prompt plus repair instruction = %d, want <= %d", got, maxPromptText)
	}
}

func TestBuildMemoryPromptCountsUnicodeByRune(t *testing.T) {
	document := NewMemoryDocument()
	for index := 0; index < memoryMaxEntriesPerPart; index++ {
		document.Preferences = append(document.Preferences, MemoryEntry{
			ID:               fmt.Sprintf("mem_unicode_%d", index),
			State:            "active",
			Text:             fmt.Sprintf("记忆 %d %s", index, strings.Repeat("细节 ", 45)),
			Evidence:         strings.Repeat("证据 ", 25),
			SourceMessageIDs: []string{"msg_1"},
			FirstSeenUTC:     "2026-08-22T12:00:00Z",
			LastSeenUTC:      "2026-08-22T12:00:00Z",
		})
	}

	prompt, err := BuildMemoryPrompt(memoryTestSnapshot(), document, strings.Repeat("相关证据 ", 900))
	if err != nil {
		t.Fatalf("BuildMemoryPrompt: %v", err)
	}
	if got := len([]rune(prompt)) + len([]rune(memoryRepairInstruction)); got > maxPromptText {
		t.Fatalf("unicode prompt plus repair instruction = %d, want <= %d", got, maxPromptText)
	}
}

func entryWithCategory(entry MemoryEntry, category, text string) MemoryEntry {
	entry.ID = memoryEntryKey(category, text)
	return entry
}

func TestRenderMemoryDocumentRejectsUnsafeManualEntry(t *testing.T) {
	document := NewMemoryDocument()
	document.Preferences = append(document.Preferences, MemoryEntry{
		ID:               memoryEntryKey(memorySectionPreferences, "store the password"),
		State:            "active",
		Text:             "store the password",
		Evidence:         "The user requested that it be remembered.",
		SourceMessageIDs: []string{"msg_4"},
		FirstSeenUTC:     "2026-08-22T12:00:00Z",
		LastSeenUTC:      "2026-08-22T12:00:00Z",
	})
	if _, err := RenderMemoryDocument(document); err == nil {
		t.Fatal("unsafe manual memory entry was accepted")
	}
}

func memoryTestSnapshot() Snapshot {
	messages := make([]SnapshotMessage, 0, 4)
	for i := 1; i <= 4; i++ {
		messages = append(messages, SnapshotMessage{
			ID:          "msg_" + string(rune('0'+i)),
			SessionID:   "ses_memory",
			Role:        "user",
			Seq:         i,
			TimeCreated: int64(i),
			Text:        "memory context",
		})
	}
	return Snapshot{
		SessionID:          "ses_memory",
		SourceStartSeq:     1,
		SourceEndSeq:       4,
		SourceStartTime:    1,
		SourceEndTime:      4,
		SourceEndMessageID: "msg_4",
		Messages:           messages,
	}
}
