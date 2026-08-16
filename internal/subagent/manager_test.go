package subagent

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

// blockingRunner holds each Run until release is closed (or ctx done).
type blockingRunner struct {
	started chan struct{}
	release chan struct{}
	summary string
	// concurrent tracks in-flight Run calls.
	concurrent atomic.Int32
	maxSeen    atomic.Int32
}

func newBlockingRunner(summary string) *blockingRunner {
	return &blockingRunner{
		started: make(chan struct{}, 64),
		release: make(chan struct{}),
		summary: summary,
	}
}

func (r *blockingRunner) Run(ctx context.Context, job Job) (Result, error) {
	n := r.concurrent.Add(1)
	for {
		old := r.maxSeen.Load()
		if n <= old || r.maxSeen.CompareAndSwap(old, n) {
			break
		}
	}
	defer r.concurrent.Add(-1)

	select {
	case r.started <- struct{}{}:
	default:
	}

	select {
	case <-r.release:
		return Result{
			ID:      job.ID,
			Name:    job.Name,
			Role:    job.Role,
			Status:  string(StatusCompleted),
			Summary: r.summary,
		}, nil
	case <-ctx.Done():
		return Result{ID: job.ID, Name: job.Name, Role: job.Role}, ctx.Err()
	}
}

func TestSpawnWaitReturnsSummary(t *testing.T) {
	r := newBlockingRunner("done-work")
	close(r.release)
	cfg := NewConfig()
	cfg.MaxConcurrent = 2
	m := NewManager(cfg, r)
	m.SetRuntime(Runtime{Workdir: t.TempDir()})

	snap, err := m.Spawn(context.Background(), "ses_parent", "prt_1", Spec{
		Prompt: "do it",
		Name:   "unit",
		Role:   RoleExplore,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if snap.Status != string(StatusCompleted) {
		t.Fatalf("status = %q, want completed", snap.Status)
	}
	if snap.Summary != "done-work" {
		t.Fatalf("summary = %q", snap.Summary)
	}
	res, err := m.Wait(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Summary != "done-work" {
		t.Fatalf("Wait summary = %q", res.Summary)
	}
}

func TestConcurrentCap(t *testing.T) {
	r := newBlockingRunner("ok")
	cfg := NewConfig()
	cfg.MaxConcurrent = 2
	cfg.MaxQueued = 40
	m := NewManager(cfg, r)

	var snaps []Snapshot
	for i := 0; i < 3; i++ {
		snap, err := m.Spawn(context.Background(), "ses_a", "prt_x", Spec{
			Prompt:     "work",
			Name:       "bg",
			Background: true,
		})
		if err != nil {
			t.Fatalf("Spawn %d: %v", i, err)
		}
		snaps = append(snaps, snap)
	}

	// Wait until two runners are in flight.
	deadline := time.After(2 * time.Second)
	for r.concurrent.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for 2 concurrent runs; got %d", r.concurrent.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}

	if got := m.Active(); got > m.MaxConcurrent() {
		t.Fatalf("Active() = %d > MaxConcurrent %d", got, m.MaxConcurrent())
	}
	if got := r.maxSeen.Load(); got > 2 {
		t.Fatalf("max concurrent runs = %d, want <= 2", got)
	}

	close(r.release)
	for _, s := range snaps {
		if _, err := m.Wait(context.Background(), s.ID); err != nil {
			t.Fatalf("Wait %s: %v", s.ID, err)
		}
	}
	if got := r.maxSeen.Load(); got > 2 {
		t.Fatalf("after drain max concurrent = %d", got)
	}
}

func TestCancelBeforeFinish(t *testing.T) {
	r := newBlockingRunner("never")
	cfg := NewConfig()
	m := NewManager(cfg, r)

	snap, err := m.Spawn(context.Background(), "ses_c", "prt_c", Spec{
		Prompt:     "slow",
		Background: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Ensure the job is at least queued/running.
	select {
	case <-r.started:
	case <-time.After(2 * time.Second):
		// may still be queued; cancel should still work
	}

	out, err := m.Cancel(snap.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if out.Status != string(StatusCancelled) {
		t.Fatalf("status = %q, want cancelled", out.Status)
	}
	res, err := m.Wait(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("Wait after cancel: %v", err)
	}
	if res.Status != string(StatusCancelled) {
		t.Fatalf("result status = %q", res.Status)
	}
}

func TestListFiltersByParent(t *testing.T) {
	r := newBlockingRunner("sum")
	close(r.release)
	m := NewManager(NewConfig(), r)

	a, err := m.Spawn(context.Background(), "ses_a", "p1", Spec{Prompt: "a", Name: "A"})
	if err != nil {
		t.Fatalf("Spawn A: %v", err)
	}
	b, err := m.Spawn(context.Background(), "ses_b", "p2", Spec{Prompt: "b", Name: "B"})
	if err != nil {
		t.Fatalf("Spawn B: %v", err)
	}
	_ = b

	listA := m.List("ses_a")
	if len(listA) != 1 || listA[0].ID != a.ID {
		t.Fatalf("List ses_a = %+v", listA)
	}
	listB := m.List("ses_b")
	if len(listB) != 1 || listB[0].ID != b.ID {
		t.Fatalf("List ses_b = %+v", listB)
	}
	if got := m.List("ses_none"); len(got) != 0 {
		t.Fatalf("List empty parent = %+v", got)
	}
}

func TestQueueFull(t *testing.T) {
	r := newBlockingRunner("x")
	cfg := NewConfig()
	cfg.MaxConcurrent = 1
	cfg.MaxQueued = 2
	m := NewManager(cfg, r)

	for i := 0; i < 2; i++ {
		if _, err := m.Spawn(context.Background(), "ses_q", "p", Spec{
			Prompt:     "q",
			Background: true,
		}); err != nil {
			t.Fatalf("Spawn %d: %v", i, err)
		}
	}
	_, err := m.Spawn(context.Background(), "ses_q", "p", Spec{
		Prompt:     "overflow",
		Background: true,
	})
	if err == nil {
		t.Fatal("expected queue full error")
	}
	close(r.release)
	m.CancelAll("ses_q")
}

func TestDisabled(t *testing.T) {
	cfg := NewConfig()
	cfg.Enabled = false
	m := NewManager(cfg, newBlockingRunner("x"))
	_, err := m.Spawn(context.Background(), "s", "p", Spec{Prompt: "nope"})
	if err == nil {
		t.Fatal("expected disabled error")
	}
}

func TestHostTaskWaitAndList(t *testing.T) {
	r := newBlockingRunner("host-sum")
	// Hold briefly so background status is observable, then complete.
	var once sync.Once
	releaseSoon := func() {
		once.Do(func() {
			go func() {
				time.Sleep(20 * time.Millisecond)
				close(r.release)
			}()
		})
	}

	m := NewManager(NewConfig(), r)
	h := NewHost(m)
	h.ParentSessionID = "ses_host"

	releaseSoon()
	res, meta, status, err := h.Execute(context.Background(), "ses_parent", "task", `{
		"prompt":"scan files","description":"scan","role":"explore","background":true
	}`, "prt_host")
	if err != nil {
		t.Fatalf("Execute task: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status=%q result=%s", status, res)
	}
	if meta == "" {
		t.Fatal("empty meta")
	}

	listRes, _, status, err := h.Execute(context.Background(), "ses_parent", "task_list", `{}`, "")
	if err != nil || status != "completed" {
		t.Fatalf("task_list: status=%q err=%v res=%s", status, err, listRes)
	}
	if !containsID(listRes, "sub_") {
		t.Fatalf("task_list missing sub id: %s", listRes)
	}

	waitRes, _, status, err := h.Execute(context.Background(), "ses_parent", "task_wait", `{}`, "")
	if err != nil || status != "completed" {
		t.Fatalf("task_wait: status=%q err=%v res=%s", status, err, waitRes)
	}
	if !containsID(waitRes, "host-sum") {
		t.Fatalf("wait result missing summary: %s", waitRes)
	}
}

func TestHostUnknownToolDenied(t *testing.T) {
	h := NewHost(NewManager(NewConfig(), newBlockingRunner("x")))
	_, _, status, err := h.Execute(context.Background(), "ses_parent", "not_a_tool", `{}`, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if status != "denied" {
		t.Fatalf("status=%q", status)
	}
	if !IsTaskTool("task") || IsTaskTool("bash") {
		t.Fatal("IsTaskTool mismatch")
	}
}

func TestConfigNormalize(t *testing.T) {
	c := Config{MaxConcurrent: 100, MaxQueued: 0, Timeout: 0, DefaultRole: "nope"}.Normalize()
	if c.MaxConcurrent != HardMaxConcurrent {
		t.Fatalf("MaxConcurrent=%d", c.MaxConcurrent)
	}
	if c.MaxQueued != DefaultMaxQueued {
		t.Fatalf("MaxQueued=%d", c.MaxQueued)
	}
	if c.Timeout != DefaultTimeout {
		t.Fatalf("Timeout=%v", c.Timeout)
	}
	if c.DefaultRole != DefaultRole {
		t.Fatalf("DefaultRole=%q", c.DefaultRole)
	}
	if c.BashConfirm != BashConfirmParent {
		t.Fatalf("BashConfirm=%q", c.BashConfirm)
	}
}

func containsID(s, sub string) bool {
	return strings.Contains(s, sub)
}

func TestDurableListAndWaitAfterManagerDrop(t *testing.T) {
	ctx := context.Background()
	st := openSubagentTestStore(t)
	parent, err := st.CreateSession(ctx, db.Session{Directory: t.TempDir(), Title: "p"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	r := newBlockingRunner("durable-summary")
	close(r.release)
	cfg := NewConfig()
	m := NewManager(cfg, r)
	m.SetStore(st)
	m.SetRuntime(Runtime{Workdir: t.TempDir()})

	snap, err := m.Spawn(ctx, parent.ID, "prt_1", Spec{
		Prompt: "do durable work", Name: "durable-job", Role: RoleExplore, Background: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	// Wait until terminal in memory, then drop the manager (simulates process exit).
	res, err := m.Wait(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Wait live: %v", err)
	}
	if res.Summary != "durable-summary" {
		t.Fatalf("summary = %q", res.Summary)
	}

	// New manager, same store: no live handles.
	m2 := NewManager(cfg, r)
	m2.SetStore(st)
	m2.SetRuntime(Runtime{Workdir: t.TempDir()})

	list := m2.List(parent.ID)
	if len(list) != 1 {
		t.Fatalf("List after restart: %#v", list)
	}
	if list[0].Status != string(StatusCompleted) || list[0].Summary != "durable-summary" {
		t.Fatalf("list snap: %+v", list[0])
	}
	stSnap, ok := m2.Status(snap.ID)
	if !ok || stSnap.Summary != "durable-summary" {
		t.Fatalf("Status after restart: ok=%v %+v", ok, stSnap)
	}
	waitRes, err := m2.Wait(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Wait durable: %v", err)
	}
	if waitRes.Summary != "durable-summary" {
		t.Fatalf("Wait summary after restart: %q", waitRes.Summary)
	}
	all, err := m2.WaitAll(ctx, parent.ID)
	if err != nil || len(all) != 1 || all[0].Summary != "durable-summary" {
		t.Fatalf("WaitAll after restart: %v %#v", err, all)
	}
}

func TestRecoverResumesOpenJob(t *testing.T) {
	ctx := context.Background()
	st := openSubagentTestStore(t)
	parent, err := st.CreateSession(ctx, db.Session{Directory: t.TempDir(), Title: "p"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Seed an open job as if the process crashed mid-run.
	job := db.SubagentJob{
		ID:              "sub_recover01aabbcc",
		ParentSessionID: parent.ID,
		Name:            "resume-me",
		Role:            RoleExplore,
		Status:          string(StatusRunning),
		Prompt:          "finish the audit",
		MaxSteps:        8,
		TimeCreated:     time.Now().UnixMilli(),
		TimeUpdated:     time.Now().UnixMilli(),
		TimeStarted:     time.Now().UnixMilli(),
	}
	if err := st.UpsertSubagentJob(ctx, job); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	r := newBlockingRunner("recovered-ok")
	close(r.release)
	m := NewManager(NewConfig(), r)
	m.SetStore(st)
	m.SetRuntime(Runtime{Workdir: t.TempDir()})
	if err := m.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	res, err := m.Wait(ctx, job.ID)
	if err != nil {
		t.Fatalf("Wait recovered: %v", err)
	}
	if res.Summary != "recovered-ok" || res.Status != string(StatusCompleted) {
		t.Fatalf("recovered result: %+v", res)
	}
	// Durable row updated.
	row, err := st.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != string(StatusCompleted) || row.Summary != "recovered-ok" {
		t.Fatalf("store after recover: %+v", row)
	}
}

func openSubagentTestStore(t *testing.T) *db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub.db")
	st, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}
