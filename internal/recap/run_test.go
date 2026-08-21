package recap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestRunClaimsCompletesAndPersistsOneRecap(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_run"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(7_000_000)
	for i := 4; i >= 1; i-- {
		addRecapMessage(t, store, sess.ID, "user", "Keep the recap local.", now.Add(-time.Duration(i)*time.Minute), true)
	}
	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	record, created, err := store.ReserveRecap(context.Background(), db.RecapRecord{
		SessionID:          sess.ID,
		SourceStartSeq:     snapshot.SourceStartSeq,
		SourceEndSeq:       snapshot.SourceEndSeq,
		SourceStartTime:    snapshot.SourceStartTime,
		SourceEndTime:      snapshot.SourceEndTime,
		SourceEndMessageID: snapshot.SourceEndMessageID,
		Model:              "deepseek-v4-flash",
	})
	if err != nil || !created {
		t.Fatalf("ReserveRecap: record=%+v created=%t err=%v", record, created, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var request struct {
			Tools []json.RawMessage `json:"tools"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("request JSON: %v", err)
		}
		if request.Tools != nil {
			t.Errorf("tools = %v, want omitted", request.Tools)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"recap_markdown\":\"The recap remains local.\",\"questions\":[],\"things_to_avoid\":[]}","finish_reason":"stop"}}]}`)
	}))
	t.Cleanup(server.Close)
	workdir := t.TempDir()
	manifest, err := Run(context.Background(), RunInput{
		Store:    store,
		Record:   record,
		Snapshot: snapshot,
		Workdir:  workdir,
		Worker: Worker{
			Client:   opencode.NewClient("test-key", opencode.WithBaseURL(server.URL+"/v1")),
			Model:    record.Model,
			Endpoint: server.URL + "/v1/chat/completions",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if manifest.Sessions.Path == "" {
		t.Fatalf("manifest = %+v", manifest)
	}
	completed, err := store.GetRecap(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("GetRecap: %v", err)
	}
	if completed.Status != db.RecapStatusCompleted || completed.Attempts != 1 {
		t.Fatalf("completed record = %+v", completed)
	}
	if _, err := filepath.Abs(filepath.Join(workdir, manifest.Sessions.Path)); err != nil {
		t.Fatalf("artifact path: %v", err)
	}
}

func TestRunRecordsStrictWorkerFailure(t *testing.T) {
	store := openRecapStore(t)
	sess, err := store.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), ID: "ses_run_failure"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.UnixMilli(8_000_000)
	for i := 4; i >= 1; i-- {
		addRecapMessage(t, store, sess.ID, "user", "context", now.Add(-time.Duration(i)*time.Minute), true)
	}
	snapshot, err := BuildSnapshot(context.Background(), store, sess.ID, SnapshotOptions{Now: now})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	record, _, err := store.ReserveRecap(context.Background(), db.RecapRecord{
		SessionID:          sess.ID,
		SourceStartSeq:     snapshot.SourceStartSeq,
		SourceEndSeq:       snapshot.SourceEndSeq,
		SourceStartTime:    snapshot.SourceStartTime,
		SourceEndTime:      snapshot.SourceEndTime,
		SourceEndMessageID: snapshot.SourceEndMessageID,
		Model:              "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("ReserveRecap: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"not-json","finish_reason":"stop"}}]}`)
	}))
	t.Cleanup(server.Close)
	_, err = Run(context.Background(), RunInput{
		Store:    store,
		Record:   record,
		Snapshot: snapshot,
		Workdir:  t.TempDir(),
		Worker: Worker{
			Client:   opencode.NewClient("test-key", opencode.WithBaseURL(server.URL+"/v1")),
			Model:    record.Model,
			Endpoint: server.URL + "/v1/chat/completions",
		},
	})
	if err == nil {
		t.Fatal("Run succeeded with malformed worker output")
	}
	failed, err := store.GetRecap(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("GetRecap: %v", err)
	}
	if failed.Status != db.RecapStatusFailed || failed.Attempts != 1 || failed.Error == "" {
		t.Fatalf("failed record = %+v", failed)
	}
}
