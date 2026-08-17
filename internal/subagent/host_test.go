package subagent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestTaskSpecOmitsTimeoutFields(t *testing.T) {
	h := NewHost(NewManager(NewConfig(), newBlockingRunner("ok")))
	for _, s := range h.Specs() {
		if s.Name != "task" {
			continue
		}
		props, ok := s.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatal("task properties missing")
		}
		if _, ok := props["timeout_ms"]; ok {
			t.Fatal("public task schema still has timeout_ms")
		}
		if _, ok := props["timeout_sec"]; ok {
			t.Fatal("public task schema still has timeout_sec")
		}
		return
	}
	t.Fatal("task tool not found in Host.Specs")
}

// captureRunner records the Job.Timeout the manager applied.
type captureRunner struct {
	mu      sync.Mutex
	timeout time.Duration
	release chan struct{}
}

func (r *captureRunner) Run(ctx context.Context, job Job) (Result, error) {
	r.mu.Lock()
	r.timeout = job.Timeout
	r.mu.Unlock()
	select {
	case <-r.release:
		return Result{
			ID: job.ID, Name: job.Name, Role: job.Role,
			Status: string(StatusCompleted), Summary: "ok",
		}, nil
	case <-ctx.Done():
		return Result{ID: job.ID, Name: job.Name, Role: job.Role}, ctx.Err()
	}
}

// TestHostIgnoresModelTimeoutMS ensures a model-invented 1s budget cannot
// override settings-owned Config.Timeout (the failure mode from the UI audit
// session where every child got timeout_ms=1000).
func TestHostIgnoresModelTimeoutMS(t *testing.T) {
	r := &captureRunner{release: make(chan struct{})}
	close(r.release)

	cfg := NewConfig()
	cfg.Timeout = 600 * time.Second
	cfg.MaxConcurrent = 2
	m := NewManager(cfg, r)
	m.SetRuntime(Runtime{Workdir: t.TempDir()})
	h := NewHost(m)
	h.ParentSessionID = "ses_parent"

	args := `{
		"prompt":"audit the ui",
		"name":"ui-architecture",
		"role":"explore",
		"background":true,
		"timeout_ms":1000,
		"timeout_sec":1,
		"max_steps":32
	}`
	result, _, status, err := h.Execute(context.Background(), "ses_parent", "task", args, "prt_1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status = %q result = %s", status, result)
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(result), &snap); err != nil {
		t.Fatalf("result json: %v %s", err, result)
	}
	// Wait for runner to finish so capture is set.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, ok := m.Status(snap.ID)
		if ok && (st.Status == string(StatusCompleted) || st.Status == string(StatusTimedOut) ||
			st.Status == string(StatusFailed) || st.Status == string(StatusCancelled)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	r.mu.Lock()
	got := r.timeout
	r.mu.Unlock()
	if got != cfg.Timeout {
		t.Fatalf("Job.Timeout = %v, want config %v (model timeout_ms=1000 must be ignored)", got, cfg.Timeout)
	}
}

func TestInternalSpecTimeoutStillHonored(t *testing.T) {
	// Programmatic Spawn (tests/debug) may still set Spec.Timeout.
	r := &captureRunner{release: make(chan struct{})}
	close(r.release)
	cfg := NewConfig()
	cfg.Timeout = 600 * time.Second
	m := NewManager(cfg, r)
	m.SetRuntime(Runtime{Workdir: t.TempDir()})

	want := 45 * time.Second
	snap, err := m.Spawn(context.Background(), "ses_p", "prt_1", Spec{
		Prompt:  "internal",
		Name:    "unit",
		Timeout: want,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, ok := m.Status(snap.ID)
		if ok && st.Status == string(StatusCompleted) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	got := r.timeout
	r.mu.Unlock()
	if got != want {
		t.Fatalf("Job.Timeout = %v, want internal Spec.Timeout %v", got, want)
	}
}
