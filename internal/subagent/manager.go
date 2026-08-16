package subagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager owns the sub-agent job table, concurrency semaphore, and writer lock.
type Manager struct {
	cfg    Config
	runner Runner

	mu      sync.Mutex
	handles map[string]*handle
	sem     chan struct{}
	// writerMu serializes general-role children when parallel writers are off.
	writerMu sync.Mutex

	rt Runtime
}

type handle struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}

	// mu guards snap/result after spawn returns the initial snapshot.
	mu     sync.Mutex
	snap   Snapshot
	result Result
}

// NewManager builds a Manager with normalized config. runner may be nil until set.
func NewManager(cfg Config, runner Runner) *Manager {
	cfg = cfg.Normalize()
	return &Manager{
		cfg:     cfg,
		runner:  runner,
		handles: make(map[string]*handle),
		sem:     make(chan struct{}, cfg.MaxConcurrent),
	}
}

// SetRuntime stores parent-side deps used when building Jobs in Spawn.
func (m *Manager) SetRuntime(rt Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rt = rt
}

// SetRunner replaces the Runner (tests and late wiring).
func (m *Manager) SetRunner(r Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runner = r
}

// MaxConcurrent returns the configured slot count.
func (m *Manager) MaxConcurrent() int {
	return m.cfg.MaxConcurrent
}

// Active returns the number of jobs currently running.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, h := range m.handles {
		h.mu.Lock()
		if h.snap.Status == string(StatusRunning) {
			n++
		}
		h.mu.Unlock()
	}
	return n
}

// Spawn enqueues a child job. Background returns immediately; wait mode blocks
// until the job reaches a terminal status (or ctx is done).
func (m *Manager) Spawn(ctx context.Context, parentSessionID, parentPartID string, spec Spec) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := m.cfg
	if !cfg.Enabled {
		return Snapshot{}, errors.New("subagent: disabled")
	}
	m.mu.Lock()
	runner := m.runner
	rt := m.rt
	if runner == nil {
		m.mu.Unlock()
		return Snapshot{}, errors.New("subagent: no runner")
	}
	if m.nonTerminalLocked() >= cfg.MaxQueued {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("subagent: queue full (%d)", cfg.MaxQueued)
	}

	role := normalizeRole(spec.Role, cfg.DefaultRole)
	id := newID("sub_")
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = cfg.Timeout
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = strings.TrimSpace(spec.Description)
	}
	if name == "" {
		name = id
	}

	// Derive from parent ctx when present so turn cancel cascades; still apply wall timeout.
	base := ctx
	if base == nil {
		base = context.Background()
	}
	var jobCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		jobCtx, cancel = context.WithTimeout(base, timeout)
	} else {
		jobCtx, cancel = context.WithCancel(base)
	}
	now := time.Now().UnixMilli()
	h := &handle{
		id:     id,
		cancel: cancel,
		done:   make(chan struct{}),
		snap: Snapshot{
			ID:              id,
			Name:            name,
			Role:            role,
			Status:          string(StatusQueued),
			ParentSessionID: parentSessionID,
			ParentPartID:    parentPartID,
			// StartedAt at spawn so drawer order is fixed for the job's life.
			StartedAt: now,
		},
	}
	m.handles[id] = h

	job := m.buildJob(id, parentSessionID, parentPartID, name, role, spec, timeout, rt)
	// Bind session id as soon as the runner creates it (stable drawer identity).
	job.OnSession = func(childSessionID string) {
		h.mu.Lock()
		h.snap.ChildSessionID = childSessionID
		h.mu.Unlock()
	}
	m.mu.Unlock()

	go m.execute(jobCtx, h, job, role, runner)

	if spec.Background {
		return h.snapshot(), nil
	}
	return m.waitSnapshot(ctx, h)
}

func (m *Manager) buildJob(id, parentSessionID, parentPartID, name, role string, spec Spec, timeout time.Duration, rt Runtime) Job {
	cfg := m.cfg
	model := firstNonEmpty(spec.Model, cfg.Model, rt.Model)
	if role == RoleExplore && cfg.ExploreModel != "" {
		model = firstNonEmpty(spec.Model, cfg.ExploreModel, model)
	}
	maxSteps := spec.MaxSteps
	if maxSteps < 1 {
		maxSteps = cfg.ChildMaxSteps
	}
	confirm := rt.Confirm
	if cfg.BashConfirm == BashConfirmDeny {
		confirm = nil // ask/deny-class bash is denied without a UI confirm
	}
	return Job{
		ID:              id,
		Name:            name,
		Role:            role,
		Prompt:          spec.Prompt,
		Description:     spec.Description,
		ParentSessionID: parentSessionID,
		ParentPartID:    parentPartID,
		Workdir:         rt.Workdir,
		Model:           model,
		Endpoint:        firstNonEmpty(cfg.Endpoint, rt.Endpoint),
		Variant:         firstNonEmpty(spec.Variant, cfg.Variant, rt.Variant),
		MaxSteps:        maxSteps,
		Timeout:         timeout,
		Tools:           toolsForRole(role),
		Confirm:         confirm,
		Ask:             nil, // children never get the question tool
	}
}

func (m *Manager) execute(jobCtx context.Context, h *handle, job Job, role string, runner Runner) {
	defer close(h.done)
	defer h.cancel()

	// Acquire concurrency slot (or exit if cancelled while waiting).
	select {
	case m.sem <- struct{}{}:
		// held
	case <-jobCtx.Done():
		m.finish(h, terminalFromCtx(jobCtx, Result{
			ID: job.ID, Name: job.Name, Role: job.Role,
		}))
		return
	}
	defer func() { <-m.sem }()

	// Optional single-writer lock for general role (cancelable wait).
	if role == RoleGeneral && !m.cfg.AllowParallelWriters {
		if err := m.acquireWriter(jobCtx); err != nil {
			m.finish(h, terminalFromCtx(jobCtx, Result{
				ID: job.ID, Name: job.Name, Role: job.Role,
			}))
			return
		}
		defer m.writerMu.Unlock()
	}

	h.mu.Lock()
	h.snap.Status = string(StatusRunning)
	if h.snap.StartedAt == 0 {
		h.snap.StartedAt = time.Now().UnixMilli()
	}
	h.mu.Unlock()

	res, err := runner.Run(jobCtx, job)
	if res.ID == "" {
		res.ID = job.ID
	}
	if res.Name == "" {
		res.Name = job.Name
	}
	if res.Role == "" {
		res.Role = job.Role
	}
	if jobCtx.Err() != nil {
		m.finish(h, terminalFromCtx(jobCtx, res))
		return
	}
	if err != nil {
		if res.Err == "" {
			res.Err = err.Error()
		}
		if res.Status == "" {
			res.Status = string(StatusFailed)
		}
		m.finish(h, res)
		return
	}
	if res.Status == "" {
		res.Status = string(StatusCompleted)
	}
	m.finish(h, res)
}

// acquireWriter locks writerMu or returns ctx.Err() if cancelled while waiting.
func (m *Manager) acquireWriter(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		m.writerMu.Lock()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		go func() {
			<-done
			m.writerMu.Unlock()
		}()
		return ctx.Err()
	}
}

func (m *Manager) finish(h *handle, res Result) {
	now := time.Now().UnixMilli()
	h.mu.Lock()
	defer h.mu.Unlock()
	if isTerminalStatus(h.snap.Status) {
		return
	}
	h.result = res
	h.snap.Status = res.Status
	h.snap.Summary = res.Summary
	h.snap.Err = res.Err
	h.snap.ChildSessionID = res.ChildSessionID
	if h.snap.StartedAt == 0 {
		h.snap.StartedAt = now
	}
	h.snap.FinishedAt = now
}

func terminalFromCtx(ctx context.Context, res Result) Result {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Status = string(StatusTimedOut)
		if res.Err == "" {
			res.Err = "subagent: timed out"
		}
		return res
	}
	res.Status = string(StatusCancelled)
	if res.Err == "" {
		res.Err = "subagent: cancelled"
	}
	return res
}

// Status returns a snapshot for id.
func (m *Manager) Status(id string) (Snapshot, bool) {
	m.mu.Lock()
	h, ok := m.handles[id]
	m.mu.Unlock()
	if !ok {
		return Snapshot{}, false
	}
	return h.snapshot(), true
}

// List returns snapshots for parentSessionID (empty parent lists none).
// Order is stable: StartedAt ascending, then id (map iteration is never used as order).
func (m *Manager) List(parentSessionID string) []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Snapshot, 0)
	for _, h := range m.handles {
		h.mu.Lock()
		if h.snap.ParentSessionID == parentSessionID {
			out = append(out, h.snap)
		}
		h.mu.Unlock()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt < out[j].StartedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Cancel requests cancellation of a job and waits until it is terminal.
func (m *Manager) Cancel(id string) (Snapshot, error) {
	m.mu.Lock()
	h, ok := m.handles[id]
	m.mu.Unlock()
	if !ok {
		return Snapshot{}, fmt.Errorf("subagent: unknown id %q", id)
	}
	h.cancel()
	<-h.done
	return h.snapshot(), nil
}

// Wait blocks until the job is terminal or ctx is done.
func (m *Manager) Wait(ctx context.Context, id string) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	m.mu.Unlock()
	if !ok {
		return Result{}, fmt.Errorf("subagent: unknown id %q", id)
	}
	select {
	case <-h.done:
		h.mu.Lock()
		res := h.result
		h.mu.Unlock()
		return res, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// WaitAll waits for every job under parentSessionID.
func (m *Manager) WaitAll(ctx context.Context, parentSessionID string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	var hs []*handle
	for _, h := range m.handles {
		h.mu.Lock()
		if h.snap.ParentSessionID == parentSessionID {
			hs = append(hs, h)
		}
		h.mu.Unlock()
	}
	m.mu.Unlock()

	out := make([]Result, 0, len(hs))
	for _, h := range hs {
		select {
		case <-h.done:
			h.mu.Lock()
			out = append(out, h.result)
			h.mu.Unlock()
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	return out, nil
}

// CancelAll cancels every non-terminal job for parentSessionID. Returns count cancelled.
func (m *Manager) CancelAll(parentSessionID string) int {
	m.mu.Lock()
	var hs []*handle
	for _, h := range m.handles {
		h.mu.Lock()
		match := h.snap.ParentSessionID == parentSessionID && !isTerminalStatus(h.snap.Status)
		h.mu.Unlock()
		if match {
			hs = append(hs, h)
		}
	}
	m.mu.Unlock()
	for _, h := range hs {
		h.cancel()
	}
	for _, h := range hs {
		<-h.done
	}
	return len(hs)
}

func (m *Manager) waitSnapshot(ctx context.Context, h *handle) (Snapshot, error) {
	select {
	case <-h.done:
		return h.snapshot(), nil
	case <-ctx.Done():
		return h.snapshot(), ctx.Err()
	}
}

func (m *Manager) nonTerminalLocked() int {
	n := 0
	for _, h := range m.handles {
		h.mu.Lock()
		if !isTerminalStatus(h.snap.Status) {
			n++
		}
		h.mu.Unlock()
	}
	return n
}

func (h *handle) snapshot() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap
}

func isTerminalStatus(s string) bool {
	switch Status(s) {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	default:
		return false
	}
}

func toolsForRole(role string) []string {
	switch role {
	case RoleGeneral:
		return []string{"bash", "read", "write", "edit", "webfetch"}
	case RolePlan:
		return []string{"read", "webfetch"}
	default: // explore
		return []string{"read", "webfetch"}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// newID returns prefix plus 16 lowercase hex characters from crypto/rand.
func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
