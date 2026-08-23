package db

import (
	"context"
	"strings"
	"testing"
)

func TestMemoryUpdateLedgerIsIdempotentAndRecoverable(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	input := MemoryUpdate{
		Workdir:            "/work/project",
		SourceSessionID:    "ses_memory",
		SourceEndSeq:       4,
		SourceEndMessageID: "msg_4",
		Model:              "deepseek-v4-flash",
	}
	reserved, created, err := store.ReserveMemoryUpdate(ctx, input)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if !created || reserved.Status != MemoryUpdateStatusQueued || reserved.ID == "" {
		t.Fatalf("reserved = %+v created=%t", reserved, created)
	}
	duplicate, created, err := store.ReserveMemoryUpdate(ctx, input)
	if err != nil {
		t.Fatalf("duplicate reserve: %v", err)
	}
	if created || duplicate.ID != reserved.ID {
		t.Fatalf("duplicate = %+v created=%t", duplicate, created)
	}
	if err := store.ClaimMemoryUpdate(ctx, reserved.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	digest := strings.Repeat("a", 64)
	if err := store.CompleteMemoryUpdate(ctx, reserved.ID, digest); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err := store.GetMemoryUpdate(ctx, reserved.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != MemoryUpdateStatusCompleted || got.SHA256 != digest || got.Attempts != 1 {
		t.Fatalf("completed = %+v", got)
	}
	if err := store.RequeueMemoryUpdate(ctx, reserved.ID); err == nil {
		t.Fatal("completed update was requeued")
	}
}

func TestMemoryUpdateFailureCanBeRequeued(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	reserved, _, err := store.ReserveMemoryUpdate(ctx, MemoryUpdate{
		Workdir:            "/work/project",
		SourceSessionID:    "ses_memory",
		SourceEndSeq:       5,
		SourceEndMessageID: "msg_5",
		Model:              "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := store.ClaimMemoryUpdate(ctx, reserved.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.FailMemoryUpdate(ctx, reserved.ID, "provider failed"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if err := store.RequeueMemoryUpdate(ctx, reserved.ID); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	open, err := store.ListOpenMemoryUpdates(ctx, "/work/project")
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(open) != 1 || open[0].Status != MemoryUpdateStatusQueued {
		t.Fatalf("open = %+v", open)
	}
	if err := store.FailMemoryUpdate(ctx, reserved.ID, "recap: fewer than four complete messages"); err != nil {
		t.Fatalf("fail insufficient: %v", err)
	}
	retryable, err := store.ListMemoryUpdatesForRecovery(ctx, "/work/project")
	if err != nil {
		t.Fatalf("list recovery: %v", err)
	}
	if len(retryable) != 1 || retryable[0].Status != MemoryUpdateStatusFailed {
		t.Fatalf("retryable = %+v", retryable)
	}
}

func TestMemoryUpdateStageDurationsPersist(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	record, _, err := store.ReserveMemoryUpdate(ctx, MemoryUpdate{
		Workdir:            "/work/project",
		SourceSessionID:    "ses_timing",
		SourceEndSeq:       6,
		SourceEndMessageID: "msg_6",
		Model:              "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := store.ClaimMemoryUpdate(ctx, record.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.RecordMemoryStageDurations(ctx, record.ID, map[string]int64{
		"aggregate_read": 12,
		"provider_call":  345,
	}); err != nil {
		t.Fatalf("record durations: %v", err)
	}
	got, err := store.GetMemoryUpdate(ctx, record.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StageDurations["aggregate_read"] != 12 || got.StageDurations["provider_call"] != 345 {
		t.Fatalf("stage durations = %+v", got.StageDurations)
	}
}
