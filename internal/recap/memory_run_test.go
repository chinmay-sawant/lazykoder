package recap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestRunMemoryUpdateWritesAggregateAndCompletesLedger(t *testing.T) {
	store := openRecapStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir, ID: "ses_memory_run"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 4; i++ {
		addRecapMessage(t, store, session.ID, "user", "keep plans focused", sessionTime(i), true)
	}
	snapshot, err := BuildSnapshot(context.Background(), store, session.ID, SnapshotOptions{Now: sessionTime(4)})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	record, created, err := store.ReserveMemoryUpdate(context.Background(), db.MemoryUpdate{
		Workdir:            workdir,
		SourceSessionID:    snapshot.SessionID,
		SourceEndSeq:       snapshot.SourceEndSeq,
		SourceEndMessageID: snapshot.SourceEndMessageID,
		Model:              "deepseek-v4-flash",
	})
	if err != nil || !created {
		t.Fatalf("ReserveMemoryUpdate: record=%+v created=%v err=%v", record, created, err)
	}
	worker := MemoryWorker{
		Client: recapClientFunc(func(context.Context, opencode.ChatRequest) (*opencode.ChatResponse, error) {
			return &opencode.ChatResponse{
				Content:      `{"preferences":[{"text":"Keep plans focused","evidence":"The recent work uses focused phases","source_message_ids":["` + snapshot.Messages[0].ID + `"]}],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`,
				FinishReason: "stop",
			}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	if err := RunMemoryUpdate(context.Background(), MemoryRunInput{
		Store:    store,
		Record:   record,
		Snapshot: snapshot,
		Workdir:  workdir,
		Worker:   worker,
	}); err != nil {
		t.Fatalf("RunMemoryUpdate: %v", err)
	}
	got, err := store.GetMemoryUpdate(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("GetMemoryUpdate: %v", err)
	}
	if got.Status != db.MemoryUpdateStatusCompleted || len(got.SHA256) != 64 {
		t.Fatalf("memory update = %+v", got)
	}
	body, err := os.ReadFile(filepath.Join(workdir, "knowledge-base", "memories.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(body), "Keep plans focused") || !strings.Contains(string(body), "## User preferences") {
		t.Fatalf("memory body = %q", body)
	}
}

func TestRunMemoryUpdateDoesNotOverwriteNewerAggregate(t *testing.T) {
	store := openRecapStore(t)
	workdir := t.TempDir()
	session, err := store.CreateSession(context.Background(), db.Session{Directory: workdir, ID: "ses_memory_order"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 4; i++ {
		addRecapMessage(t, store, session.ID, "user", "older context", sessionTime(i), true)
	}
	snapshot, err := BuildSnapshot(context.Background(), store, session.ID, SnapshotOptions{Now: sessionTime(4)})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	newer := NewMemoryDocument()
	newer.UpdatedAtUTC = "2026-08-22T12:00:00Z"
	newer.LastSessionID = snapshot.SessionID
	newer.LastMessageID = "msg_newer"
	newer.LastMessageSeq = snapshot.SourceEndSeq + 10
	newer.Questions = append(newer.Questions, MemoryEntry{
		ID:               "mem_newer",
		State:            "active",
		Text:             "Keep the newer decision.",
		Evidence:         "A later source message superseded this window.",
		SourceMessageIDs: []string{"msg_newer"},
		FirstSeenUTC:     newer.UpdatedAtUTC,
		LastSeenUTC:      newer.UpdatedAtUTC,
	})
	if _, err := WriteMemoryDocument(context.Background(), workdir, newer); err != nil {
		t.Fatalf("WriteMemoryDocument: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(workdir, "knowledge-base", "memories.md"))
	if err != nil {
		t.Fatalf("ReadFile before: %v", err)
	}
	record, _, err := store.ReserveMemoryUpdate(context.Background(), db.MemoryUpdate{
		Workdir:            workdir,
		SourceSessionID:    snapshot.SessionID,
		SourceEndSeq:       snapshot.SourceEndSeq,
		SourceEndMessageID: snapshot.SourceEndMessageID,
		Model:              "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("ReserveMemoryUpdate: %v", err)
	}
	worker := MemoryWorker{
		Client: recapClientFunc(func(context.Context, opencode.ChatRequest) (*opencode.ChatResponse, error) {
			return &opencode.ChatResponse{Content: `{"preferences":[],"decisions":[],"things_to_avoid":[],"questions":[],"recent_context":[],"supersessions":[]}`, FinishReason: "stop"}, nil
		}),
		Model: "deepseek-v4-flash",
	}
	if err := RunMemoryUpdate(context.Background(), MemoryRunInput{Store: store, Record: record, Snapshot: snapshot, Workdir: workdir, Worker: worker}); err != nil {
		t.Fatalf("RunMemoryUpdate: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(workdir, "knowledge-base", "memories.md"))
	if err != nil {
		t.Fatalf("ReadFile after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("older update replaced newer aggregate")
	}
}

func sessionTime(offset int) time.Time {
	return time.UnixMilli(int64(10_000 + offset*60_000))
}
