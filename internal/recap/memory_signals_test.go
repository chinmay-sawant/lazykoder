package recap

import (
	"testing"
)

func TestExtractMemorySignalsUsesUserMessagesOnly(t *testing.T) {
	snapshot := Snapshot{Messages: []SnapshotMessage{
		{ID: "msg_assistant", Role: "assistant", Text: "Always use the old setting."},
		{ID: "msg_user", Role: "user", Text: "I prefer the local model by default. Avoid hiding failed memory updates."},
		{ID: "msg_decision", Role: "user", Text: "We decided to keep the memory file project-scoped. The open question is how to display it."},
	}}
	signals := ExtractMemorySignals(snapshot)
	if len(signals) != 4 {
		t.Fatalf("signals = %+v, want four explicit user signals", signals)
	}
	for _, signal := range signals {
		if signal.SourceMessageIDs[0] == "msg_assistant" {
			t.Fatalf("assistant text became a signal: %+v", signal)
		}
	}
	if !hasSignal(signals, MemorySignalPreference, "I prefer the local model by default. Avoid hiding failed memory updates.") {
		t.Fatal("preference signal missing")
	}
	if !hasSignal(signals, MemorySignalAvoid, "I prefer the local model by default. Avoid hiding failed memory updates.") {
		t.Fatal("avoid signal missing")
	}
	if !hasSignal(signals, MemorySignalDecision, "We decided to keep the memory file project-scoped. The open question is how to display it.") {
		t.Fatal("decision signal missing")
	}
	if !hasSignal(signals, MemorySignalQuestion, "We decided to keep the memory file project-scoped. The open question is how to display it.") {
		t.Fatal("question signal missing")
	}
}

func TestApplyExplicitMemorySignalsRestoresCorrectCategory(t *testing.T) {
	snapshot := memoryTestSnapshot()
	snapshot.Messages[0] = SnapshotMessage{ID: "msg_user", SessionID: snapshot.SessionID, Role: "user", Seq: 1, Text: "I prefer the local memory file."}
	envelope := MemoryEnvelope{RecentContext: []MemoryFact{{
		Text:             "I prefer the local memory file.",
		Evidence:         "The user stated it.",
		SourceMessageIDs: []string{"msg_user"},
	}}}
	got, err := ApplyExplicitMemorySignals(envelope, ExtractMemorySignals(snapshot), snapshot)
	if err != nil {
		t.Fatalf("ApplyExplicitMemorySignals: %v", err)
	}
	if len(got.Preferences) != 1 || got.Preferences[0].Text != "I prefer the local memory file." {
		t.Fatalf("preferences = %+v", got.Preferences)
	}
	if len(got.RecentContext) != 0 {
		t.Fatalf("recent context = %+v, want miscategorized signal removed", got.RecentContext)
	}
}

func hasSignal(signals []MemorySignal, category MemorySignalCategory, statement string) bool {
	for _, signal := range signals {
		if signal.Category == category && signal.Statement == statement {
			return true
		}
	}
	return false
}
