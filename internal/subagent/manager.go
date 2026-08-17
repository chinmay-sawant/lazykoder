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
	// prompt/description/model/etc for crash recovery re-spawn.
	spec Spec
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
	m.handles[id] = h

	job := m.buildJob(id, parentSessionID, parentPartID, name, role, spec, timeout, rt, "", false)
	h.snap.Model = job.Model
	h.snap.Variant = job.Variant
	// Bind session id as soon as the runner creates it (stable drawer identity).
	job.OnSession = func(childSessionID string) {
		h.mu.Lock()
		h.snap.ChildSessionID = childSessionID
		h.mu.Unlock()
		m.persistHandle(h)
	}
	m.persistHandleLocked(h) // still holding m.mu
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
	for _, row := range open {
		if err := m.resumeJob(ctx, row, runner, rt); err != nil {
			// Mark failed so a bad row does not loop forever on every start.
			_ = store.UpsertSubagentJob(ctx, rowWithStatus(row, string(StatusFailed), "", err.Error()))
		}
	}
	return nil
}

func (m *Manager) resumeJob(ctx context.Context, row db.SubagentJob, runner Runner, rt Runtime) error {
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
	m.handles[row.ID] = h
	job := m.buildJob(row.ID, row.ParentSessionID, row.ParentPartID, row.Name, role, spec, timeout, rt, row.ChildSessionID, true)
	h.snap.Model = job.Model
	h.snap.Variant = job.Variant
	job.OnSession = func(childSessionID string) {
		h.mu.Lock()
		h.snap.ChildSessionID = childSessionID
		h.mu.Unlock()
		m.persistHandle(h)
	}
	m.persistHandleLocked(h)
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
	if isTerminalStatus(status) {
		row.TimeFinished = now
	}
	return row
}

func (m *Manager) buildJob(id, parentSessionID, parentPartID, name, role string, spec Spec, timeout time.Duration, rt Runtime, childSessionID string, resume bool) Job {
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
		ChildSessionID:  childSessionID,
		Resume:          resume,
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
	m.persistHandle(h)

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
	if isTerminalStatus(h.snap.Status) {
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
	m.persistHandle(h)
	// Bump parent session so resume lists show activity.
	m.mu.Lock()
	store := m.store
	parentID := h.snapshot().ParentSessionID
	m.mu.Unlock()
	if store != nil && parentID != "" {
		_ = store.TouchSession(context.Background(), parentID)
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

// Cancel requests cancellation of a job and waits until it is terminal.
func (m *Manager) Cancel(id string) (Snapshot, error) {
	m.mu.Lock()
	h, ok := m.handles[id]
	store := m.store
	m.mu.Unlock()
	if ok {
		h.cancel()
		<-h.done
		return h.snapshot(), nil
	}
	// Durable-only terminal or unknown job.
	if store != nil {
		if row, err := store.GetSubagentJob(context.Background(), id); err == nil {
			if isTerminalStatus(row.Status) {
				return snapshotFromRow(row), nil
			}
			// Mark cancelled in store when not live (crashed mid-flight, never recovered).
			row = rowWithStatus(row, string(StatusCancelled), row.Summary, "subagent: cancelled")
			_ = store.UpsertSubagentJob(context.Background(), row)
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
			if isTerminalStatus(row.Status) {
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

// Shutdown cancels every in-flight job and waits for them to finish.
// Used before replacing the Manager so Recover does not double-run work.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	var hs []*handle
	for _, h := range m.handles {
		h.mu.Lock()
		if !isTerminalStatus(h.snap.Status) {
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

func (m *Manager) persistHandle(h *handle) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil || h == nil {
		return
	}
	upsertSubagentJobRelaxed(store, m.rowFromHandle(h))
}

// persistHandleLocked must be called with m.mu held.
func (m *Manager) persistHandleLocked(h *handle) {
	if m.store == nil || h == nil {
		return
	}
	upsertSubagentJobRelaxed(m.store, m.rowFromHandle(h))
}

// upsertSubagentJobRelaxed persists a job even when optional FK targets
// (parent part / child session) are missing - e.g. tests or stale ids.
// Required parent_session_id must still exist.
func upsertSubagentJobRelaxed(store *db.Store, row db.SubagentJob) {
	if store == nil || row.ID == "" {
		return
	}
	ctx := context.Background()
	if err := store.UpsertSubagentJob(ctx, row); err == nil {
		return
	}
	row.ParentPartID = ""
	if err := store.UpsertSubagentJob(ctx, row); err == nil {
		return
	}
	row.ChildSessionID = ""
	_ = store.UpsertSubagentJob(ctx, row)
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
		return []string{"bash", "read", "grep", "write", "edit", "webfetch"}
	case RolePlan:
		// Plan/explore: search + read; shell for listing (policy still gates rm).
		return []string{"bash", "read", "grep", "webfetch"}
	default: // explore
		return []string{"bash", "read", "grep", "webfetch"}
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
