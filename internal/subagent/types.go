package subagent

import (
	"context"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/policy"
)

// Status is the lifecycle state of a sub-agent job.
type Status string

const (
	// StatusQueued means the job is waiting for a concurrency slot.
	StatusQueued Status = "queued"
	// StatusRunning means the runner is executing the job.
	StatusRunning Status = "running"
	// StatusCompleted means the job finished successfully.
	StatusCompleted Status = "completed"
	// StatusFailed means the job finished with an error.
	StatusFailed Status = "failed"
	// StatusCancelled means the job was cancelled by the parent.
	StatusCancelled Status = "cancelled"
	// StatusTimedOut means the job exceeded its timeout.
	StatusTimedOut Status = "timed_out"
)

// Role names used for tool allowlists and writer locking.
const (
	RoleExplore = "explore"
	RolePlan    = "plan"
	RoleGeneral = "general"
)

// Spec is the parent-facing spawn request (task tool args).
type Spec struct {
	Name        string
	Prompt      string
	Description string
	Role        string
	Model       string
	Variant     string
	MaxSteps    int
	Background  bool
	// Timeout is 0 to use Config.Timeout.
	Timeout time.Duration
}

// Snapshot is a point-in-time view of a job for status/list tools.
type Snapshot struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	ChildSessionID  string `json:"child_session_id,omitempty"`
	ParentPartID    string `json:"parent_part_id,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Err             string `json:"error,omitempty"`
	StartedAt       int64  `json:"started_at,omitempty"`
	FinishedAt      int64  `json:"finished_at,omitempty"`
}

// Result is the terminal outcome of a job.
type Result struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	Summary        string `json:"summary,omitempty"`
	Err            string `json:"error,omitempty"`
	ChildSessionID string `json:"child_session_id,omitempty"`
}

// Job is the runner input. Store/client stay off this type to avoid import cycles.
type Job struct {
	ID              string
	Name            string
	Role            string
	Prompt          string
	Description     string
	ParentSessionID string
	ParentPartID    string
	// ChildSessionID is set by the runner as soon as the child session row exists.
	// When non-empty on entry, the runner resumes that session instead of creating one.
	ChildSessionID string
	// Resume is true when Recover restarts a job after a process restart.
	Resume   bool
	Workdir  string
	Model    string
	Endpoint string
	Variant  string
	MaxSteps int
	Timeout  time.Duration
	// Tools is the child tool allowlist (no task tools at depth 1).
	Tools   []string
	Confirm func(dec policy.Decision, subject string) (bool, error)
	// Ask may be nil when the parent has no question UI.
	Ask func(question string, options []string) (int, error)
	// OnSession is invoked once the child session id is known (before the
	// agent loop). Used so the TUI can key live rows to the same session
	// the store already has and avoid duplicate drawer entries.
	OnSession func(childSessionID string)
}

// Runner executes a child job. Production uses an AgentRunner; tests use fakes.
type Runner interface {
	Run(ctx context.Context, job Job) (Result, error)
}

// Runtime holds parent-side deps shared across spawns (set via Manager.SetRuntime).
type Runtime struct {
	Workdir  string
	Model    string
	Endpoint string
	Variant  string
	Confirm  func(dec policy.Decision, subject string) (bool, error)
	Ask      func(question string, options []string) (int, error)
}
