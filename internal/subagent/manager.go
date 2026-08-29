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

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/roles"
	"github.com/chinmay-sawant/lazykoder/internal/skills"
)

// Poll tuning for recovering a job that is open in the store but not yet in
// memory (Recover/race window).
const (
	// handlePollStep is the pause between handle-map polls.
	handlePollStep = 5 * time.Millisecond
	// handlePollDeadline bounds how long Wait polls before giving up.
	handlePollDeadline = 50 * time.Millisecond
)

// Manager owns the sub-agent job table, concurrency semaphore, and writer lock.
// When a Store is attached, jobs are durable: task_list/status/wait work after
// restart, and open jobs are resumed via Recover.
type Manager struct {
	cfg    Config
	runner Runner
	store  *db.Store

	mu      sync.Mutex
	handles map[string]*handle
	sem     chan struct{}
	// writerGate serializes general-role children without a non-cancellable
	// mutex wait when parallel writers are off.
	writerGate chan struct{}

	rt Runtime

	persistMu  sync.Mutex
	persistErr error
	writeJob   func(context.Context, db.SubagentJob) error
}

type handle struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}

	// mu guards snap/result after spawn returns the initial snapshot.
	mu     sync.Mutex
	snap   Snapshot
	result Result
	// prompt/description/model/etc for crash recovery re-spawn.
	spec Spec
}

// NewManager builds a Manager with normalized config. runner may be nil until set.
func NewManager(cfg Config, runner Runner) *Manager {
	cfg = cfg.Normalize()
	return &Manager{
		cfg:        cfg,
		runner:     runner,
		handles:    make(map[string]*handle),
		sem:        make(chan struct{}, cfg.MaxConcurrent),
		writerGate: newWriterGate(),
	}
}

func newWriterGate() chan struct{} {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return gate
}

// SetStore attaches the SQLite store used for durable job records.
func (m *Manager) SetStore(store *db.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
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

// Boot attaches store/runtime and optionally replaces the runner, then recovers
// open durable jobs. Pass a nil runner when NewManager already set one.
func (m *Manager) Boot(ctx context.Context, store *db.Store, rt Runtime, runner Runner) error {
	if runner != nil {
		m.SetRunner(runner)
	}
	m.SetStore(store)
	m.SetRuntime(rt)
	return m.Recover(ctx)
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

	// Foreground jobs follow the parent turn. Background jobs are explicitly
	// detached so a completed parent turn does not cancel work the user asked
	// to continue, while Manager.Cancel and Shutdown still stop them.
	base := ctx
	if spec.Background {
		base = context.WithoutCancel(ctx)
	}
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
		spec:   spec,
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
	job := m.buildJob(id, parentSessionID, parentPartID, name, role, spec, timeout, rt, "", false)
	h.snap.Model = job.Model
	h.snap.Variant = job.Variant
	// Bind session id as soon as the runner creates it (stable drawer identity).
	job.OnSession = func(childSessionID string) {
		h.mu.Lock()
		h.snap.ChildSessionID = childSessionID
		h.mu.Unlock()
		_ = m.persistHandle(h)
	}
	if err := m.persistHandleLocked(h); err != nil {
		m.mu.Unlock()
		cancel()
		return Snapshot{}, fmt.Errorf("subagent: persist queued job: %w", err)
	}
	m.handles[id] = h
	m.mu.Unlock()

	go m.execute(jobCtx, h, job, role, runner)

	if spec.Background {
		return h.snapshot(), nil
	}
	return m.waitSnapshot(ctx, h)
}

// Recover restarts any queued/running jobs left in the store after a crash or
// process exit. Safe to call once after SetStore/SetRuntime/SetRunner.
func (m *Manager) Recover(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	store := m.store
	runner := m.runner
	rt := m.rt
	cfg := m.cfg
	m.mu.Unlock()
	if store == nil || runner == nil || !cfg.Enabled {
		return nil
	}
	open, err := store.ListOpenSubagentJobs(ctx)
	if err != nil {
		return err
	}
	var recoverErr error
	for _, row := range open {
		if err := m.resumeJob(ctx, row, runner, rt); err != nil {
			// Mark failed so a bad row does not loop forever on every start.
			if persistErr := m.writeSubagentJob(ctx, rowWithStatus(row, string(StatusFailed), "", err.Error())); persistErr != nil {
				persistErr = fmt.Errorf("subagent: persist recovery failure for %q: %w", row.ID, persistErr)
				m.recordPersistenceError(persistErr)
				return persistErr
			}
			recoverErr = err
		}
	}
	return recoverErr
}

func (m *Manager) resumeJob(ctx context.Context, row db.SubagentJob, runner Runner, rt Runtime) error {
	if m.store != nil {
		latest, err := m.store.GetSubagentJob(ctx, row.ID)
		if err != nil {
			return err
		}
		if IsTerminalStatus(latest.Status) {
			return nil
		}
		row = latest
	}
	m.mu.Lock()
	if _, exists := m.handles[row.ID]; exists {
		m.mu.Unlock()
		return nil
	}
	if m.nonTerminalLocked() >= m.cfg.MaxQueued {
		m.mu.Unlock()
		return fmt.Errorf("subagent: queue full during recover (%d)", m.cfg.MaxQueued)
	}
	timeout := time.Duration(row.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = m.cfg.Timeout
	}
	var jobCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		jobCtx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		jobCtx, cancel = context.WithCancel(context.Background())
	}
	spec := Spec{
		Name:        row.Name,
		Prompt:      row.Prompt,
		Description: row.Description,
		Role:        row.Role,
		Model:       row.Model,
		Variant:     row.Variant,
		MaxSteps:    row.MaxSteps,
		Background:  true,
		Timeout:     timeout,
	}
	role := normalizeRole(row.Role, m.cfg.DefaultRole)
	started := row.TimeStarted
	if started == 0 {
		started = row.TimeCreated
	}
	h := &handle{
		id:     row.ID,
		cancel: cancel,
		done:   make(chan struct{}),
		spec:   spec,
		snap: Snapshot{
			ID:              row.ID,
			Name:            row.Name,
			Role:            role,
			Status:          string(StatusQueued),
			ParentSessionID: row.ParentSessionID,
			ParentPartID:    row.ParentPartID,
			ChildSessionID:  row.ChildSessionID,
			StartedAt:       started,
		},
	}
	job := m.buildJob(row.ID, row.ParentSessionID, row.ParentPartID, row.Name, role, spec, timeout, rt, row.ChildSessionID, true)
	h.snap.Model = job.Model
	h.snap.Variant = job.Variant
	job.OnSession = func(childSessionID string) {
		h.mu.Lock()
		h.snap.ChildSessionID = childSessionID
		h.mu.Unlock()
		_ = m.persistHandle(h)
	}
	if err := m.persistHandleLocked(h); err != nil {
		m.mu.Unlock()
		cancel()
		return err
	}
	m.handles[row.ID] = h
	m.mu.Unlock()

	go m.execute(jobCtx, h, job, role, runner)
	_ = ctx
	return nil
}

func rowWithStatus(row db.SubagentJob, status, summary, errText string) db.SubagentJob {
	now := time.Now().UnixMilli()
	row.Status = status
	row.Summary = summary
	row.Error = errText
	row.TimeUpdated = now
	if IsTerminalStatus(status) {
		row.TimeFinished = now
	}
	return row
}

func (m *Manager) buildJob(id, parentSessionID, parentPartID, name, role string, spec Spec, timeout time.Duration, rt Runtime, childSessionID string, resume bool) Job {
	cfg := m.cfg
	model := strings.TrimSpace(spec.Model)
	if model == "" {
		if roleModel := strings.TrimSpace(cfg.ModelByRole[role]); roleModel != "" {
			model = roleModel
		}
		if model == "" && role == RoleExplore {
			model = strings.TrimSpace(cfg.ExploreModel)
		}
		if model == "" {
			model = cfg.Model
		}
		if model == "" {
			class := strings.TrimSpace(spec.ModelClass)
			if class == "" {
				if descriptor, ok := roles.DescriptorFor(role); ok {
					class = descriptor.DefaultModelClass
				}
			}
			model = resolveClass(class, role, cfg, rt)
		}
	}
	model = firstNonEmpty(model, rt.Model, opencode.DefaultModelID)
	profile := rt.profile(model)
	maxSteps := spec.MaxSteps
	if maxSteps < 1 {
		maxSteps = cfg.ChildMaxSteps
	}
	confirm := rt.Confirm
	if cfg.BashConfirm == BashConfirmDeny {
		confirm = nil // ask/deny-class bash is denied without a UI confirm
	}
	endpoint := profile.Endpoint
	if endpoint == "" && model == rt.Model {
		endpoint = rt.Endpoint
	}
	return Job{
		ID:              id,
		Name:            name,
		Role:            role,
		Prompt:          spec.Prompt,
		Description:     spec.Description,
		ParentSessionID: parentSessionID,
		ParentPartID:    parentPartID,
		ChildSessionID:  childSessionID,
		Resume:          resume,
		Workdir:         rt.Workdir,
		Model:           model,
		Endpoint:        endpoint,
		Variant:         profile.variant(spec.Variant, cfg.Variant, rt.Variant),
		ContextWindow:   profile.ContextWindow,
		MaxSteps:        maxSteps,
		Timeout:         timeout,
		Depth:           1, // Manager only builds direct children of the chat parent
		MaxDepth:        cfg.MaxDepth,
		Tools:           roles.Tools(role),
		Skills:          append([]skills.Context{}, rt.Skills...),
		Confirm:         confirm,
		Ask:             nil, // children never get the question tool
	}
}

func resolveClass(class, role string, cfg Config, rt Runtime) string {
	class = strings.ToLower(strings.TrimSpace(class))
	if class == "" {
		return ""
	}
	for _, profile := range rt.Profiles {
		id := strings.ToLower(profile.ID)
		if strings.Contains(id, class) {
			return profile.ID
		}
	}
	if configured := strings.TrimSpace(cfg.ModelClassByRole[role]); configured == class {
		if model := strings.TrimSpace(cfg.ModelByRole[role]); model != "" {
			return model
		}
	}
	switch role {
	case RoleExplore:
		if cfg.ExploreClass == class {
			return cfg.ExploreModel
		}
	case RolePlan:
		if cfg.PlanClass == class {
			return cfg.Model
		}
	case RoleGeneral:
		if cfg.GeneralClass == class {
			return cfg.Model
		}
	}
	return ""
}

func (rt Runtime) profile(model string) ModelProfile {
	for _, profile := range rt.Profiles {
		if profile.ID == model {
			profile.Variants = append([]string{}, profile.Variants...)
			return profile
		}
	}
	return ModelProfile{ID: model}
}

func (profile ModelProfile) variant(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if len(profile.Variants) == 0 {
			return candidate
		}
		for _, supported := range profile.Variants {
			if candidate == supported {
				return candidate
			}
		}
	}
	for _, supported := range profile.Variants {
		if variant := strings.TrimSpace(supported); variant != "" {
			return variant
		}
	}
	return ""
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
	descriptor, known := roles.DescriptorFor(role)
	if known && descriptor.SingleWriter && !m.cfg.AllowParallelWriters {
		if err := m.acquireWriter(jobCtx); err != nil {
			m.finish(h, terminalFromCtx(jobCtx, Result{
				ID: job.ID, Name: job.Name, Role: job.Role,
			}))
			return
		}
		defer m.releaseWriter()
	}

	h.mu.Lock()
	h.snap.Status = string(StatusRunning)
	if h.snap.StartedAt == 0 {
		h.snap.StartedAt = time.Now().UnixMilli()
	}
	h.mu.Unlock()
	_ = m.persistHandle(h)

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

// acquireWriter waits for the single-writer slot or returns ctx.Err().
func (m *Manager) acquireWriter(ctx context.Context) error {
	select {
	case <-m.writerGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) releaseWriter() {
	m.writerGate <- struct{}{}
}

func (m *Manager) finish(h *handle, res Result) {
	now := time.Now().UnixMilli()
	h.mu.Lock()
	if IsTerminalStatus(h.snap.Status) {
		h.mu.Unlock()
		return
	}
	h.result = res
	h.snap.Status = res.Status
	h.snap.Summary = res.Summary
	h.snap.Err = res.Err
	if res.ChildSessionID != "" {
		h.snap.ChildSessionID = res.ChildSessionID
	}
	if h.snap.StartedAt == 0 {
		h.snap.StartedAt = now
	}
	h.snap.FinishedAt = now
	h.mu.Unlock()
	_ = m.persistHandle(h)
	// Keep parent activity fresh for age labels and the child drawer. History
	// ordering uses the conversation timestamp and does not move the parent.
	m.mu.Lock()
	store := m.store
	parentID := h.snapshot().ParentSessionID
	m.mu.Unlock()
	if store != nil && parentID != "" {
		if err := store.TouchSession(context.Background(), parentID); err != nil {
			m.recordPersistenceError(fmt.Errorf("subagent: touch parent session %q: %w", parentID, err))
		}
	}
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

// Status returns a snapshot for id (live handle, else durable store).
func (m *Manager) Status(id string) (Snapshot, bool) {
	m.mu.Lock()
	h, ok := m.handles[id]
	store := m.store
	m.mu.Unlock()
	if ok {
		return h.snapshot(), true
	}
	if store == nil {
		return Snapshot{}, false
	}
	row, err := store.GetSubagentJob(context.Background(), id)
	if err != nil {
		return Snapshot{}, false
	}
	return snapshotFromRow(row), true
}

// List returns snapshots for parentSessionID (empty parent lists none).
// Live handles win over store rows. Order is stable: StartedAt ascending, then id.
func (m *Manager) List(parentSessionID string) []Snapshot {
	if parentSessionID == "" {
		return nil
	}
	m.mu.Lock()
	store := m.store
	byID := make(map[string]Snapshot)
	for _, h := range m.handles {
		h.mu.Lock()
		if h.snap.ParentSessionID == parentSessionID {
			byID[h.snap.ID] = h.snap
		}
		h.mu.Unlock()
	}
	m.mu.Unlock()

	if store != nil {
		rows, err := store.ListSubagentJobs(context.Background(), parentSessionID)
		if err == nil {
			for _, row := range rows {
				if _, ok := byID[row.ID]; ok {
					continue
				}
				byID[row.ID] = snapshotFromRow(row)
			}
		}
	}
	out := make([]Snapshot, 0, len(byID))
	for _, snap := range byID {
		out = append(out, snap)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt < out[j].StartedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// RequestCancel signals a live job without waiting for worker cleanup. A
// durable-only open job is marked cancelled immediately because it has no
// in-memory worker to signal. It is safe to call repeatedly and is intended
// for UI and task-tool callers that return before cleanup completes.
func (m *Manager) RequestCancel(id string) (Snapshot, error) {
	m.mu.Lock()
	h, ok := m.handles[id]
	store := m.store
	m.mu.Unlock()
	if ok {
		h.cancel()
		return h.snapshot(), nil
	}
	if store != nil {
		changed, err := store.CancelSubagentJob(context.Background(), id)
		if err != nil {
			m.recordPersistenceError(fmt.Errorf("subagent: persist cancellation for %q: %w", id, err))
			return Snapshot{}, err
		}
		if row, err := store.GetSubagentJob(context.Background(), id); err == nil && (changed || IsTerminalStatus(row.Status)) {
			return snapshotFromRow(row), nil
		}
	}
	return Snapshot{}, fmt.Errorf("subagent: unknown id %q", id)
}

// Cancel requests cancellation of a job and waits until it is terminal.
func (m *Manager) Cancel(id string) (Snapshot, error) {
	if _, err := m.RequestCancel(id); err != nil {
		return Snapshot{}, err
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	store := m.store
	m.mu.Unlock()
	if ok {
		<-h.done
		return h.snapshot(), nil
	}
	if store != nil {
		changed, err := store.CancelSubagentJob(context.Background(), id)
		if err != nil {
			m.recordPersistenceError(fmt.Errorf("subagent: persist cancellation for %q: %w", id, err))
			return Snapshot{}, err
		}
		row, err := store.GetSubagentJob(context.Background(), id)
		if err != nil {
			return Snapshot{}, err
		}
		if changed || IsTerminalStatus(row.Status) {
			return snapshotFromRow(row), nil
		}
	}
	return Snapshot{}, fmt.Errorf("subagent: unknown id %q", id)
}

// Wait blocks until the job is terminal or ctx is done.
// Terminal results are also loaded from the durable store after restart.
func (m *Manager) Wait(ctx context.Context, id string) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	store := m.store
	m.mu.Unlock()
	if ok {
		select {
		case <-h.done:
			h.mu.Lock()
			res := h.result
			// If finish raced without filling result (should not), use snap.
			if res.ID == "" {
				res = resultFromSnapshot(h.snapshot())
			}
			h.mu.Unlock()
			return res, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	if store != nil {
		if row, err := store.GetSubagentJob(context.Background(), id); err == nil {
			if IsTerminalStatus(row.Status) {
				return resultFromRow(row), nil
			}
			// Open job not in memory yet: wait briefly for Recover/race, else error.
			deadline := time.Now().Add(handlePollDeadline)
			for time.Now().Before(deadline) {
				m.mu.Lock()
				_, ok = m.handles[id]
				m.mu.Unlock()
				if ok {
					return m.Wait(ctx, id)
				}
				time.Sleep(handlePollStep)
			}
			return Result{}, fmt.Errorf("subagent: job %q is open but not running (try again after recover)", id)
		}
	}
	return Result{}, fmt.Errorf("subagent: unknown id %q", id)
}

// WaitAll waits for every job under parentSessionID (live + durable terminal).
func (m *Manager) WaitAll(ctx context.Context, parentSessionID string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Snapshot the set of job ids we must wait for (memory + store).
	ids := make(map[string]struct{})
	m.mu.Lock()
	store := m.store
	var hs []*handle
	for _, h := range m.handles {
		h.mu.Lock()
		if h.snap.ParentSessionID == parentSessionID {
			hs = append(hs, h)
			ids[h.snap.ID] = struct{}{}
		}
		h.mu.Unlock()
	}
	m.mu.Unlock()

	if store != nil {
		rows, err := store.ListSubagentJobs(context.Background(), parentSessionID)
		if err == nil {
			for _, row := range rows {
				ids[row.ID] = struct{}{}
			}
		}
	}

	out := make([]Result, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	// Wait live handles first.
	for _, h := range hs {
		select {
		case <-h.done:
			h.mu.Lock()
			res := h.result
			if res.ID == "" {
				res = resultFromSnapshot(h.snapshot())
			}
			h.mu.Unlock()
			out = append(out, res)
			seen[res.ID] = true
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	// Fill remaining from store (completed after restart, or finished offline).
	for id := range ids {
		if seen[id] {
			continue
		}
		res, err := m.Wait(ctx, id)
		if err != nil {
			return out, err
		}
		out = append(out, res)
		seen[id] = true
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Manager) requestCancelHandles(parentSessionID string) []*handle {
	m.mu.Lock()
	var hs []*handle
	for _, h := range m.handles {
		h.mu.Lock()
		match := h.snap.ParentSessionID == parentSessionID && !IsTerminalStatus(h.snap.Status)
		h.mu.Unlock()
		if match {
			hs = append(hs, h)
		}
	}
	m.mu.Unlock()
	return hs
}

// RequestCancelAll signals every non-terminal job for a parent without
// waiting for live workers. Durable-only open rows are marked cancelled.
func (m *Manager) RequestCancelAll(parentSessionID string) int {
	hs := m.requestCancelHandles(parentSessionID)
	for _, h := range hs {
		h.cancel()
	}
	count := len(hs)
	liveIDs := make(map[string]struct{}, len(hs))
	for _, h := range hs {
		liveIDs[h.id] = struct{}{}
	}
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return count
	}
	rows, err := store.ListSubagentJobs(context.Background(), parentSessionID)
	if err != nil {
		m.recordPersistenceError(fmt.Errorf("subagent: list jobs for cancellation: %w", err))
		return count
	}
	for _, row := range rows {
		if _, ok := liveIDs[row.ID]; ok || IsTerminalStatus(row.Status) {
			continue
		}
		changed, err := store.CancelSubagentJob(context.Background(), row.ID)
		if err != nil {
			m.recordPersistenceError(fmt.Errorf("subagent: persist cancellation for %q: %w", row.ID, err))
			continue
		}
		if changed {
			count++
		}
	}
	return count
}

// CancelAll cancels every non-terminal job for parentSessionID and waits for
// live workers. Durable-only rows are conditionally cancelled as well.
func (m *Manager) CancelAll(parentSessionID string) int {
	hs := m.requestCancelHandles(parentSessionID)
	for _, h := range hs {
		h.cancel()
	}
	for _, h := range hs {
		<-h.done
	}
	count := len(hs)
	liveIDs := make(map[string]struct{}, len(hs))
	for _, h := range hs {
		liveIDs[h.id] = struct{}{}
	}
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return count
	}
	rows, err := store.ListSubagentJobs(context.Background(), parentSessionID)
	if err != nil {
		m.recordPersistenceError(fmt.Errorf("subagent: list jobs for cancellation: %w", err))
		return count
	}
	for _, row := range rows {
		if _, ok := liveIDs[row.ID]; ok || IsTerminalStatus(row.Status) {
			continue
		}
		changed, err := store.CancelSubagentJob(context.Background(), row.ID)
		if err != nil {
			m.recordPersistenceError(fmt.Errorf("subagent: persist cancellation for %q: %w", row.ID, err))
			continue
		}
		if changed {
			count++
		}
	}
	return count
}

// Shutdown cancels every in-flight job and waits for them to finish.
// Used before replacing the Manager so Recover does not double-run work.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	var hs []*handle
	for _, h := range m.handles {
		h.mu.Lock()
		if !IsTerminalStatus(h.snap.Status) {
			hs = append(hs, h)
		}
		h.mu.Unlock()
	}
	m.mu.Unlock()
	for _, h := range hs {
		h.cancel()
	}
	for _, h := range hs {
		<-h.done
	}
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return
	}
	rows, err := store.ListOpenSubagentJobs(context.Background())
	if err != nil {
		m.recordPersistenceError(fmt.Errorf("subagent: list open jobs during shutdown: %w", err))
		return
	}
	for _, row := range rows {
		if _, err := store.CancelSubagentJob(context.Background(), row.ID); err != nil {
			m.recordPersistenceError(fmt.Errorf("subagent: persist shutdown cancellation for %q: %w", row.ID, err))
		}
	}
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
		if !IsTerminalStatus(h.snap.Status) {
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

func (m *Manager) persistHandle(h *handle) error {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil || h == nil {
		return nil
	}
	if err := m.upsertSubagentJobRelaxed(store, m.rowFromHandle(h)); err != nil {
		m.recordPersistenceError(err)
		return err
	}
	return nil
}

// persistHandleLocked must be called with m.mu held.
func (m *Manager) persistHandleLocked(h *handle) error {
	if m.store == nil || h == nil {
		return nil
	}
	if err := m.upsertSubagentJobRelaxed(m.store, m.rowFromHandle(h)); err != nil {
		m.recordPersistenceError(err)
		return err
	}
	return nil
}

// upsertSubagentJobRelaxed persists a job even when optional FK targets
// (parent part / child session) are missing - e.g. tests or stale ids.
// Required parent_session_id must still exist.
func (m *Manager) upsertSubagentJobRelaxed(store *db.Store, row db.SubagentJob) error {
	if store == nil || row.ID == "" {
		return nil
	}
	ctx := context.Background()
	if err := m.writeSubagentJob(ctx, row); err == nil {
		return nil
	}
	row.ParentPartID = ""
	if err := m.writeSubagentJob(ctx, row); err == nil {
		return nil
	}
	row.ChildSessionID = ""
	if err := m.writeSubagentJob(ctx, row); err != nil {
		return fmt.Errorf("subagent: persist job %q: %w", row.ID, err)
	}
	return nil
}

func (m *Manager) writeSubagentJob(ctx context.Context, row db.SubagentJob) error {
	if m.writeJob != nil {
		return m.writeJob(ctx, row)
	}
	if m.store == nil {
		return nil
	}
	return m.store.UpsertSubagentJob(ctx, row)
}

// TakePersistenceError returns the latest durable-state failure and clears it.
// Live jobs keep running, but callers can show the failure instead of claiming
// that a state transition survived a restart.
func (m *Manager) TakePersistenceError() error {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	err := m.persistErr
	m.persistErr = nil
	return err
}

func (m *Manager) recordPersistenceError(err error) {
	if err == nil {
		return
	}
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	m.persistErr = err
}

func (m *Manager) rowFromHandle(h *handle) db.SubagentJob {
	h.mu.Lock()
	defer h.mu.Unlock()
	timeoutMS := h.spec.Timeout.Milliseconds()
	if timeoutMS <= 0 {
		timeoutMS = m.cfg.Timeout.Milliseconds()
	}
	maxSteps := h.spec.MaxSteps
	if maxSteps < 1 {
		maxSteps = m.cfg.ChildMaxSteps
	}
	now := time.Now().UnixMilli()
	created := h.snap.StartedAt
	if created == 0 {
		created = now
	}
	return db.SubagentJob{
		ID:              h.snap.ID,
		ParentSessionID: h.snap.ParentSessionID,
		ParentPartID:    h.snap.ParentPartID,
		ChildSessionID:  h.snap.ChildSessionID,
		Name:            h.snap.Name,
		Role:            h.snap.Role,
		Status:          h.snap.Status,
		Prompt:          h.spec.Prompt,
		Description:     firstNonEmpty(h.spec.Description, h.snap.Name),
		Model:           firstNonEmpty(h.snap.Model, h.spec.Model),
		Variant:         firstNonEmpty(h.snap.Variant, h.spec.Variant),
		MaxSteps:        maxSteps,
		TimeoutMS:       timeoutMS,
		Summary:         h.snap.Summary,
		Error:           h.snap.Err,
		TimeCreated:     created,
		TimeUpdated:     now,
		TimeStarted:     h.snap.StartedAt,
		TimeFinished:    h.snap.FinishedAt,
	}
}

func snapshotFromRow(row db.SubagentJob) Snapshot {
	started := row.TimeStarted
	if started == 0 {
		started = row.TimeCreated
	}
	return Snapshot{
		ID:              row.ID,
		Name:            row.Name,
		Role:            row.Role,
		Model:           row.Model,
		Variant:         row.Variant,
		Status:          row.Status,
		ParentSessionID: row.ParentSessionID,
		ChildSessionID:  row.ChildSessionID,
		ParentPartID:    row.ParentPartID,
		Summary:         row.Summary,
		Err:             row.Error,
		StartedAt:       started,
		FinishedAt:      row.TimeFinished,
	}
}

func resultFromRow(row db.SubagentJob) Result {
	return Result{
		ID:             row.ID,
		Name:           row.Name,
		Role:           row.Role,
		Status:         row.Status,
		Summary:        row.Summary,
		Err:            row.Error,
		ChildSessionID: row.ChildSessionID,
	}
}

func resultFromSnapshot(s Snapshot) Result {
	return Result{
		ID:             s.ID,
		Name:           s.Name,
		Role:           s.Role,
		Status:         s.Status,
		Summary:        s.Summary,
		Err:            s.Err,
		ChildSessionID: s.ChildSessionID,
	}
}

// IsTerminalStatus reports whether status is a finished Manager job state.
// Live drawer rows are the complement (!IsTerminalStatus).
func IsTerminalStatus(s string) bool {
	switch Status(s) {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	default:
		return false
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
