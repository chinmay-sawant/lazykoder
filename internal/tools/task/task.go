// Package task holds pure schemas and JSON helpers for parent task tools.
// It does not run sub-agents; subagent.Host consumes these types for ads/parse.
package task

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/prompts"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/roles"
)

// Tool names advertised to the model.
const (
	ToolTask       = "task"
	ToolTaskList   = "task_list"
	ToolTaskStatus = "task_status"
	ToolTaskWait   = "task_wait"
	ToolTaskCancel = "task_cancel"
)

// Child role identifiers.
const (
	RoleExplore = "explore"
	RolePlan    = "plan"
	RoleGeneral = "general"
)

// TaskArgs is the argument object for the task tool (spawn a sub-agent).
// Child wall-clock lifetime is not a model arg; it comes from project
// settings (agents.default_timeout_sec) via the subagent manager config.
type TaskArgs struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Role        string `json:"role"`
	Model       string `json:"model"`
	ModelClass  string `json:"model_class"`
	Variant     string `json:"variant"`
	MaxSteps    int    `json:"max_steps"`
	Background  bool   `json:"background"`
}

// ListArgs is the argument object for task_list (no filters today).
type ListArgs struct{}

// StatusArgs is the argument object for task_status.
type StatusArgs struct {
	ID string `json:"id"`
}

// WaitArgs is the argument object for task_wait.
// Empty ID waits for all jobs under the parent session.
type WaitArgs struct {
	ID string `json:"id"`
}

// CancelArgs is the argument object for task_cancel.
// Empty ID or CancelAll cancels every non-terminal job for the parent.
type CancelArgs struct {
	ID        string `json:"id"`
	CancelAll bool   `json:"cancel_all"`
}

// TaskInfo is a single child snapshot returned by list/status/wait.
// Field names match subagent.Snapshot JSON tags for easy conversion.
type TaskInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	Role           string `json:"role,omitempty"`
	Status         string `json:"status"`
	Summary        string `json:"summary,omitempty"`
	Error          string `json:"error,omitempty"`
	ChildSessionID string `json:"child_session_id,omitempty"`
	StartedAt      int64  `json:"started_at,omitempty"`
	FinishedAt     int64  `json:"finished_at,omitempty"`
}

// SpawnResult is returned by a successful task (spawn) call.
type SpawnResult struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	Role           string `json:"role,omitempty"`
	Status         string `json:"status"`
	Background     bool   `json:"background"`
	Summary        string `json:"summary,omitempty"`
	Error          string `json:"error,omitempty"`
	ChildSessionID string `json:"child_session_id,omitempty"`
}

// ListResult is returned by task_list.
type ListResult struct {
	Tasks []TaskInfo `json:"tasks"`
}

// StatusResult is returned by task_status.
type StatusResult struct {
	Task TaskInfo `json:"task"`
}

// WaitResult is returned by task_wait (one task when id set, else all).
type WaitResult struct {
	Tasks []TaskInfo `json:"tasks"`
}

// CancelResult is returned by task_cancel.
type CancelResult struct {
	ID             string   `json:"id,omitempty"`
	Cancelled      []string `json:"cancelled,omitempty"`
	CancelledCount int      `json:"cancelled_count,omitempty"`
	CancelAll      bool     `json:"cancel_all,omitempty"`
}

// Specs returns the five task tool definitions for provider advertising.
// Keep in sync with what subagent.Host actually executes.
func Specs() []opencode.ToolSpec {
	return SpecsFor("")
}

// SpecsFor returns task tool definitions customized by the workspace prompt
// files. Execution and argument validation remain in this package.
func SpecsFor(workdir string) []opencode.ToolSpec {
	store := prompts.New(workdir)
	names := []string{ToolTask, ToolTaskList, ToolTaskStatus, ToolTaskWait, ToolTaskCancel}
	specs := make([]opencode.ToolSpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, store.ToolSpec(name))
	}
	return specs
}

// IsTaskTool reports whether name is one of the five task tools.
func IsTaskTool(name string) bool {
	switch name {
	case ToolTask, ToolTaskList, ToolTaskStatus, ToolTaskWait, ToolTaskCancel:
		return true
	default:
		return false
	}
}

// NormalizeRole returns a canonical role, or empty when s is not a known role.
// Empty input defaults to RoleExplore (matches settings/subagent defaults).
func NormalizeRole(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return roles.Explore
	}
	if roles.IsKnown(s) {
		return s
	}
	return ""
}

// ParseTaskArgs unmarshals and validates task (spawn) arguments.
func ParseTaskArgs(raw []byte) (TaskArgs, error) {
	var a TaskArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return TaskArgs{}, fmt.Errorf("task: %w", err)
	}
	a.Prompt = strings.TrimSpace(a.Prompt)
	if a.Prompt == "" {
		return TaskArgs{}, fmt.Errorf("task: prompt is required")
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Description = strings.TrimSpace(a.Description)
	a.Model = strings.TrimSpace(a.Model)
	a.Variant = strings.TrimSpace(a.Variant)
	if strings.TrimSpace(a.Role) == "" {
		a.Role = RoleExplore
	} else {
		role := NormalizeRole(a.Role)
		if role == "" {
			return TaskArgs{}, fmt.Errorf("task: invalid role %q (want one of %s)", a.Role, strings.Join(roles.IDs(), ", "))
		}
		a.Role = role
	}
	return a, nil
}

// ParseListArgs unmarshals task_list arguments.
func ParseListArgs(raw []byte) (ListArgs, error) {
	var a ListArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return ListArgs{}, fmt.Errorf("task_list: %w", err)
	}
	return a, nil
}

// ParseStatusArgs unmarshals and validates task_status arguments.
func ParseStatusArgs(raw []byte) (StatusArgs, error) {
	var a StatusArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return StatusArgs{}, fmt.Errorf("task_status: %w", err)
	}
	a.ID = strings.TrimSpace(a.ID)
	if a.ID == "" {
		return StatusArgs{}, fmt.Errorf("task_status: id is required")
	}
	return a, nil
}

// ParseWaitArgs unmarshals task_wait arguments (id may be empty to wait for all).
func ParseWaitArgs(raw []byte) (WaitArgs, error) {
	var a WaitArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return WaitArgs{}, fmt.Errorf("task_wait: %w", err)
	}
	a.ID = strings.TrimSpace(a.ID)
	return a, nil
}

// ParseCancelArgs unmarshals task_cancel arguments.
// Empty id (or cancel_all=true) means cancel all non-terminal jobs.
func ParseCancelArgs(raw []byte) (CancelArgs, error) {
	var a CancelArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return CancelArgs{}, fmt.Errorf("task_cancel: %w", err)
	}
	a.ID = strings.TrimSpace(a.ID)
	return a, nil
}

// EncodeSpawnResult marshals a spawn result for a tool message.
func EncodeSpawnResult(r SpawnResult) (string, error) {
	return encode(r)
}

// EncodeListResult marshals a list result for a tool message.
func EncodeListResult(r ListResult) (string, error) {
	return encode(r)
}

// EncodeStatusResult marshals a status result for a tool message.
func EncodeStatusResult(r StatusResult) (string, error) {
	return encode(r)
}

// EncodeWaitResult marshals a wait result for a tool message.
func EncodeWaitResult(r WaitResult) (string, error) {
	return encode(r)
}

// EncodeCancelResult marshals a cancel result for a tool message.
func EncodeCancelResult(r CancelResult) (string, error) {
	return encode(r)
}

func unmarshalArgs(raw []byte, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
