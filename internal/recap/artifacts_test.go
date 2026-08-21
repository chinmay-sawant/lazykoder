package recap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaterializeWritesOrderedAtomicArtifactsAndFrontMatter(t *testing.T) {
	root := t.TempDir()
	snapshot := Snapshot{
		SessionID:          "ses_artifact",
		SourceStartSeq:     8,
		SourceEndSeq:       12,
		SourceStartTime:    1_000,
		SourceEndTime:      2_000,
		SourceEndMessageID: "msg_end",
		Messages: []SnapshotMessage{
			{ID: "msg_end", SessionID: "ses_artifact", Role: "user", Text: "latest decision", Seq: 12, TimeCreated: 2_000},
			{ID: "msg_11", SessionID: "ses_artifact", Role: "user", Text: "context", Seq: 11, TimeCreated: 1_800},
			{ID: "msg_10", SessionID: "ses_artifact", Role: "user", Text: "context", Seq: 10, TimeCreated: 1_500},
			{ID: "msg_old", SessionID: "ses_artifact", Role: "assistant", Text: "prior context", Seq: 8, TimeCreated: 1_000},
		},
	}
	envelope := Envelope{
		RecapMarkdown: "# Decision\nUse the local memory folder.",
		Questions: []Question{{
			Question:         "Should old files be compacted?",
			Reason:           "The retention rule is not decided.",
			SourceMessageIDs: []string{"msg_end"},
		}},
		ThingsToAvoid: []AvoidRule{{
			Rule:             "Do not write outside the workspace.",
			Reason:           "The artifact path must remain local.",
			SourceMessageIDs: []string{"msg_old"},
		}},
	}

	manifest, err := Materialize(context.Background(), root, MaterializeInput{
		RecapID:     "rec_0000000000000001",
		Model:       "deepseek-v4-flash",
		GeneratedAt: time.Unix(10, 0).UTC(),
		Snapshot:    snapshot,
		Envelope:    envelope,
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	for _, artifact := range []Artifact{manifest.Sessions, *manifest.Questions, *manifest.ThingsToAvoid} {
		if artifact.Path == "" || artifact.SHA256 == "" {
			t.Fatalf("incomplete artifact = %+v", artifact)
		}
		if filepath.IsAbs(artifact.Path) {
			t.Fatalf("artifact path should be relative: %+v", artifact)
		}
		if _, err := os.Stat(filepath.Join(root, artifact.Path)); err != nil {
			t.Fatalf("artifact %q missing: %v", artifact.Path, err)
		}
	}
	wantStem := "knowledge-base/recaps/sessions/ses_artifact/000000000012-msg_end.md"
	if manifest.Sessions.Path != wantStem {
		t.Fatalf("session path = %q, want %q", manifest.Sessions.Path, wantStem)
	}
	raw, err := os.ReadFile(filepath.Join(root, manifest.Sessions.Path))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"recap_id: rec_0000000000000001",
		"session_id: ses_artifact",
		"source_start_seq: 8",
		"source_end_seq: 12",
		"source_end_message_id: msg_end",
		"source_end_unix_millis: 2000",
		"generated_at_utc: 1970-01-01T00:00:10Z",
		"model: deepseek-v4-flash",
		"source_content_sha256:",
		"# Decision",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("session artifact missing %q:\n%s", want, body)
		}
	}
}

func TestMaterializeOmitsEmptyOptionalArtifactsAndRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	snapshot := Snapshot{
		SessionID:          "ses_optional",
		SourceStartSeq:     1,
		SourceEndSeq:       4,
		SourceStartTime:    1,
		SourceEndTime:      4,
		SourceEndMessageID: "msg_4",
		Messages: []SnapshotMessage{
			{ID: "msg_4", Text: "x", Role: "user", Seq: 4},
			{ID: "msg_3", Text: "x", Role: "user", Seq: 3},
			{ID: "msg_2", Text: "x", Role: "user", Seq: 2},
			{ID: "msg_1", Text: "x", Role: "user", Seq: 1},
		},
	}
	manifest, err := Materialize(context.Background(), root, MaterializeInput{
		RecapID:     "rec_optional",
		Model:       "deepseek-v4-flash",
		GeneratedAt: time.Unix(0, 0).UTC(),
		Snapshot:    snapshot,
		Envelope:    Envelope{RecapMarkdown: "x", Questions: []Question{}, ThingsToAvoid: []AvoidRule{}},
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if manifest.Questions != nil || manifest.ThingsToAvoid != nil {
		t.Fatalf("optional manifest = %+v, want omitted", manifest)
	}
	if _, err := os.Stat(filepath.Join(root, "knowledge-base/recaps/questions")); !os.IsNotExist(err) {
		t.Fatalf("questions directory exists, err = %v", err)
	}

	escape := snapshot
	escape.SessionID = "../outside"
	if _, err := Materialize(context.Background(), root, MaterializeInput{
		RecapID:     "rec_escape",
		Model:       "deepseek-v4-flash",
		GeneratedAt: time.Unix(0, 0).UTC(),
		Snapshot:    escape,
		Envelope:    Envelope{RecapMarkdown: "x"},
	}); err == nil || !strings.Contains(err.Error(), "session") {
		t.Fatalf("escape error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "knowledge-base/recaps"), 0o755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "knowledge-base/recaps/questions")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := Materialize(context.Background(), root, MaterializeInput{
		RecapID:     "rec_symlink",
		Model:       "deepseek-v4-flash",
		GeneratedAt: time.Unix(0, 0).UTC(),
		Snapshot:    snapshot,
		Envelope:    Envelope{RecapMarkdown: "x"},
	}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}
