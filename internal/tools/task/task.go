// Package task holds pure schemas and JSON helpers for parent task tools.
// It does not run sub-agents; agent and subagent packages consume these types.
package task

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
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
	Variant     string `json:"variant"`
	MaxSteps    int    `json:"max_steps"`
	Background  bool   `json:"background"`
}

// ListArgs is the argument object for task_list.
type ListArgs struct {
	Status string `json:"status"`
}

// StatusArgs is the argument object for task_status.
type StatusArgs struct {
	ID string `json:"id"`
}

// WaitArgs is the argument object for task_wait.
type WaitArgs struct {
	ID         string `json:"id"`
	TimeoutSec int    `json:"timeout_sec"`
}

// CancelArgs is the argument object for task_cancel.
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
	ID        string   `json:"id,omitempty"`
	Cancelled []string `json:"cancelled,omitempty"`
	CancelAll bool     `json:"cancel_all,omitempty"`
}

// Specs returns the five task tool definitions for provider advertising.
func Specs() []opencode.ToolSpec {
	return []opencode.ToolSpec{
		specTask(),
		specTaskList(),
		specTaskStatus(),
		specTaskWait(),
		specTaskCancel(),
	}
}

func specTask() opencode.ToolSpec {
	return opencode.ToolSpec{
		Name:        ToolTask,
		Description: "Spawn a sub-agent to work on a prompt. Roles: explore and plan are read-only; general may write. Set background true to return immediately; otherwise wait for completion.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "short display name for the sub-agent"},
				"prompt":      map[string]any{"type": "string", "description": "full task prompt for the sub-agent"},
				"description": map[string]any{"type": "string", "description": "one-line summary shown in the parent UI"},
				"role": map[string]any{
					"type":        "string",
					"description": "sub-agent role",
					"enum":        []string{RoleExplore, RolePlan, RoleGeneral},
				},
				"model":      map[string]any{"type": "string", "description": "model id override; empty uses parent model"},
				"variant":    map[string]any{"type": "string", "description": "reasoning effort / variant override"},
				"max_steps":  map[string]any{"type": "integer", "description": "max agent steps for the child"},
				"background": map[string]any{"type": "boolean", "description": "if true, return after spawn without waiting"},
			},
			"required": []string{"prompt"},
		},
	}
}

func specTaskList() opencode.ToolSpec {
	return opencode.ToolSpec{
		Name:        ToolTaskList,
		Description: "List child sub-agents of the current parent. Optionally filter by status.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{"type": "string", "description": "optional status filter (e.g. running, completed, failed, cancelled)"},
			},
		},
	}
}

func specTaskStatus() opencode.ToolSpec {
	return opencode.ToolSpec{
		Name:        ToolTaskStatus,
		Description: "Get status and summary for one child sub-agent by id.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "child task id"},
			},
			"required": []string{"id"},
		},
	}
}

func specTaskWait() opencode.ToolSpec {
	return opencode.ToolSpec{
		Name:        ToolTaskWait,
		Description: "Wait for a child to finish, or for all children when id is empty.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":          map[string]any{"type": "string", "description": "child task id; empty waits for all"},
				"timeout_sec": map[string]any{"type": "integer", "description": "max seconds to wait"},
			},
		},
	}
}

func specTaskCancel() opencode.ToolSpec {
	return opencode.ToolSpec{
		Name:        ToolTaskCancel,
		Description: "Cancel a running child by id, or all children when cancel_all is true.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":         map[string]any{"type": "string", "description": "child task id to cancel"},
				"cancel_all": map[string]any{"type": "boolean", "description": "if true, cancel every running child"},
			},
		},
	}
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
	switch strings.ToLower(strings.TrimSpace(s)) {
	case RoleExplore, "":
		return RoleExplore
	case RolePlan:
		return RolePlan
	case RoleGeneral:
		return RoleGeneral
	default:
		return ""
	}
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
			return TaskArgs{}, fmt.Errorf("task: invalid role %q (want explore, plan, or general)", a.Role)
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
	a.Status = strings.TrimSpace(a.Status)
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

// ParseCancelArgs unmarshals and validates task_cancel arguments.
// Either a non-empty id or cancel_all=true is required.
func ParseCancelArgs(raw []byte) (CancelArgs, error) {
	var a CancelArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return CancelArgs{}, fmt.Errorf("task_cancel: %w", err)
	}
	a.ID = strings.TrimSpace(a.ID)
	if a.ID == "" && !a.CancelAll {
		return CancelArgs{}, fmt.Errorf("task_cancel: id is required unless cancel_all is true")
	}
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
