package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/tools/task"
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

// captureRunner records Job fields the manager applied.
type captureRunner struct {
	mu       sync.Mutex
	timeout  time.Duration
	depth    int
	maxDepth int
	endpoint string
	variant  string
	window   int64
	tools    []string
	ran      bool
	release  chan struct{}
}

func (r *captureRunner) Run(ctx context.Context, job Job) (Result, error) {
	r.mu.Lock()
	r.timeout = job.Timeout
	r.depth = job.Depth
	r.maxDepth = job.MaxDepth
	r.endpoint = job.Endpoint
	r.variant = job.Variant
	r.window = job.ContextWindow
	r.tools = append([]string{}, job.Tools...)
	r.ran = true
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
	var snap task.SpawnResult
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

func TestBuildJobSetsDepthGate(t *testing.T) {
	r := &captureRunner{release: make(chan struct{})}
	close(r.release)
	cfg := NewConfig()
	cfg.MaxDepth = 1
	m := NewManager(cfg, r)
	m.SetRuntime(Runtime{Workdir: t.TempDir()})
	snap, err := m.Spawn(context.Background(), "ses_p", "prt_1", Spec{
		Prompt: "depth check", Name: "d", Background: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, ok := m.Status(snap.ID)
		if ok && IsTerminalStatus(st.Status) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	depth, maxDepth := r.depth, r.maxDepth
	r.mu.Unlock()
	if depth != 1 || maxDepth != 1 {
		t.Fatalf("Job depth/max = %d/%d, want 1/1", depth, maxDepth)
	}
}

func TestSnapshotReportsResolvedChildModel(t *testing.T) {
	r := &captureRunner{release: make(chan struct{})}
	close(r.release)
	cfg := NewConfig()
	cfg.Model = "configured-child-model"
	cfg.Variant = "high"
	m := NewManager(cfg, r)
	m.SetRuntime(Runtime{Workdir: t.TempDir(), Model: "parent-model", Variant: "parent-thinking"})
	h := NewHost(m)
	h.ParentSessionID = "ses_parent"

	result, _, status, err := h.Execute(context.Background(), "ses_parent", "task", `{
		"prompt":"inspect the project",
		"name":"worker",
		"background":true
	}`, "prt_1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status != "queued" && status != "running" && status != "completed" {
		t.Fatalf("status = %q", status)
	}
	var spawned task.SpawnResult
	if err := json.Unmarshal([]byte(result), &spawned); err != nil {
		t.Fatalf("result json: %v %s", err, result)
	}
	snap, ok := m.Status(spawned.ID)
	if !ok {
		t.Fatalf("status missing for %q", spawned.ID)
	}
	if snap.Model != "configured-child-model" {
		t.Fatalf("snapshot model = %q, want resolved configured-child-model", snap.Model)
	}
	if snap.Variant != "high" {
		t.Fatalf("snapshot variant = %q, want resolved high", snap.Variant)
	}
}

func TestOrchestrationModelClassHonorsConfiguredChildModels(t *testing.T) {
	tests := []struct {
		name         string
		role         string
		modelClass   string
		configured   string
		planClass    string
		exploreClass string
	}{
		{
			name:       "plan",
			role:       RolePlan,
			modelClass: "pro",
			configured: "settings-child-model",
			planClass:  "pro",
		},
		{
			name:         "explore",
			role:         RoleExplore,
			modelClass:   "flash",
			configured:   "settings-explore-model",
			exploreClass: "flash",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &captureRunner{release: make(chan struct{})}
			close(r.release)
			cfg := NewConfig()
			cfg.Model = test.configured
			cfg.ExploreModel = ""
			cfg.PlanClass = test.planClass
			cfg.ExploreClass = test.exploreClass
			if test.role == RoleExplore {
				cfg.ExploreModel = test.configured
				cfg.Model = "other-common-child-model"
			}
			m := NewManager(cfg, r)
			m.SetRuntime(Runtime{
				Workdir: t.TempDir(),
				Model:   "deepseek-v4-flash",
				Profiles: []ModelProfile{
					{ID: "deepseek-v4-flash"},
					{ID: "deepseek-v4-pro"},
				},
			})
			snap, err := m.Spawn(context.Background(), "ses_parent", "prt_1", Spec{
				Prompt:     "inspect the project",
				Role:       test.role,
				ModelClass: test.modelClass,
				Background: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if snap.Model != test.configured {
				t.Fatalf("snapshot model = %q, want configured model %q", snap.Model, test.configured)
			}
		})
	}
}

func TestBuildJobUsesSelectedChildModelProfile(t *testing.T) {
	r := &captureRunner{release: make(chan struct{})}
	close(r.release)
	cfg := NewConfig()
	cfg.Model = "zen-child"
	cfg.Variant = "high"
	m := NewManager(cfg, r)
	m.SetRuntime(Runtime{
		Workdir:  t.TempDir(),
		Model:    "go-parent",
		Endpoint: "https://opencode.ai/zen/go/v1/chat/completions",
		Variant:  "medium",
		Profiles: []ModelProfile{{
			ID:            "zen-child",
			Endpoint:      "https://opencode.ai/zen/v1/chat/completions",
			ContextWindow: 200_000,
			Variants:      []string{"low", "high"},
		}},
	})
	if _, err := m.Spawn(context.Background(), "ses_parent", "prt_1", Spec{Prompt: "inspect", Background: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		endpoint, variant, window := r.endpoint, r.variant, r.window
		r.mu.Unlock()
		if endpoint != "" {
			if endpoint != "https://opencode.ai/zen/v1/chat/completions" || variant != "high" || window != 200_000 {
				t.Fatalf("child profile = %q / %q / %d", endpoint, variant, window)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("runner did not receive the child profile")
}

func TestManagerUsesSharedRoleCapabilities(t *testing.T) {
	for _, role := range []string{RoleExplore, RolePlan, RoleGeneral} {
		t.Run(role, func(t *testing.T) {
			r := &captureRunner{release: make(chan struct{})}
			close(r.release)
			m := NewManager(NewConfig(), r)
			if _, err := m.Spawn(context.Background(), "ses_parent", "", Spec{Prompt: "work", Role: role, Background: true}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				r.mu.Lock()
				got := append([]string{}, r.tools...)
				ran := r.ran
				r.mu.Unlock()
				if ran {
					want := (settings.Agents{}).ToolsForRole(role)
					if strings.Join(got, ",") != strings.Join(want, ",") {
						t.Fatalf("Job.Tools = %v, settings tools = %v", got, want)
					}
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatal("runner did not receive role tools")
		})
	}
}

func TestHostKeepsParentSessionStateInvocationLocal(t *testing.T) {
	r := newBlockingRunner("done")
	m := NewManager(NewConfig(), r)
	h := NewHost(m)
	h.ParentSessionID = "ses_default"

	type result struct {
		parent string
		err    error
	}
	results := make(chan result, 2)
	for _, parent := range []string{"ses_a", "ses_b"} {
		go func(parent string) {
			_, _, _, err := h.Execute(context.Background(), parent, task.ToolTask, `{"prompt":"work","background":true}`, "prt_1")
			results <- result{parent: parent, err: err}
		}(parent)
	}
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("Execute(%q): %v", got.parent, got.err)
		}
	}
	if h.ParentSessionID != "ses_default" {
		t.Fatalf("Host.ParentSessionID = %q, want unchanged default", h.ParentSessionID)
	}
	for _, parent := range []string{"ses_a", "ses_b"} {
		if jobs := m.List(parent); len(jobs) != 1 || jobs[0].ParentSessionID != parent {
			t.Fatalf("List(%q) = %+v", parent, jobs)
		}
	}
	close(r.release)
	for _, parent := range []string{"ses_a", "ses_b"} {
		if _, err := m.WaitAll(context.Background(), parent); err != nil {
			t.Fatalf("WaitAll(%q): %v", parent, err)
		}
	}
}

func TestHostEncodesDeclaredTaskResults(t *testing.T) {
	r := newBlockingRunner("finished")
	close(r.release)
	m := NewManager(NewConfig(), r)
	h := NewHost(m)
	parentID := "ses_parent"

	spawnJSON, _, status, err := h.Execute(context.Background(), parentID, task.ToolTask, `{"prompt":"inspect","background":true}`, "prt_1")
	if err != nil || status != "completed" {
		t.Fatalf("task: status=%q err=%v", status, err)
	}
	var spawn task.SpawnResult
	if err := json.Unmarshal([]byte(spawnJSON), &spawn); err != nil || spawn.ID == "" {
		t.Fatalf("spawn result = %s, err=%v", spawnJSON, err)
	}

	listJSON, _, _, err := h.Execute(context.Background(), parentID, task.ToolTaskList, `{}`, "")
	if err != nil {
		t.Fatal(err)
	}
	var list task.ListResult
	if err := json.Unmarshal([]byte(listJSON), &list); err != nil || len(list.Tasks) != 1 {
		t.Fatalf("list result = %s, err=%v", listJSON, err)
	}

	statusJSON, _, _, err := h.Execute(context.Background(), parentID, task.ToolTaskStatus, `{"id":"`+spawn.ID+`"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	var taskStatus task.StatusResult
	if err := json.Unmarshal([]byte(statusJSON), &taskStatus); err != nil || taskStatus.Task.ID != spawn.ID {
		t.Fatalf("status result = %s, err=%v", statusJSON, err)
	}

	waitJSON, _, _, err := h.Execute(context.Background(), parentID, task.ToolTaskWait, `{}`, "")
	if err != nil {
		t.Fatal(err)
	}
	var wait task.WaitResult
	if err := json.Unmarshal([]byte(waitJSON), &wait); err != nil || len(wait.Tasks) != 1 {
		t.Fatalf("wait result = %s, err=%v", waitJSON, err)
	}

	cancelJSON, _, _, err := h.Execute(context.Background(), parentID, task.ToolTaskCancel, `{"id":"`+spawn.ID+`"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	var cancel task.CancelResult
	if err := json.Unmarshal([]byte(cancelJSON), &cancel); err != nil || cancel.ID != spawn.ID {
		t.Fatalf("cancel result = %s, err=%v", cancelJSON, err)
	}
}
