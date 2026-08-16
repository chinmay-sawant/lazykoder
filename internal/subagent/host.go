package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// Host is the parent-side control plane for task tools.
// It does not import internal/agent; chat/agent wire Mgr and ParentSessionID.
type Host struct {
	Mgr             *Manager
	ParentSessionID string
}

// NewHost wraps a Manager for tool dispatch.
func NewHost(mgr *Manager) *Host {
	return &Host{Mgr: mgr}
}

// Specs returns the parent task tool advertisements.
func (h *Host) Specs() []opencode.ToolSpec {
	return []opencode.ToolSpec{
		{
			Name: "task",
			Description: "Spawn a sub-agent on a focused prompt. Prefer background=true for parallel work, then task_wait. " +
				"Raise max_steps for multi-file explores (default is higher than parent chat but still finite). " +
				"Ask the child to finish with a short written report before the step budget ends.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Full instructions for the sub-agent (include: write a final summary before stopping)",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Short label shown in the UI",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Optional stable name for the job",
					},
					"role": map[string]any{
						"type":        "string",
						"description": "explore | plan | general (default explore)",
						"enum":        []string{RoleExplore, RolePlan, RoleGeneral},
					},
					"model": map[string]any{
						"type":        "string",
						"description": "Optional model override",
					},
					"variant": map[string]any{
						"type":        "string",
						"description": "Optional reasoning variant",
					},
					"max_steps": map[string]any{
						"type":        "integer",
						"description": "Child tool-round budget (0 = config default, typically 32). Use 48-64 for large audits.",
					},
					"background": map[string]any{
						"type":        "boolean",
						"description": "If true, return immediately while the job runs (recommended for parallel agents)",
					},
					"timeout_ms": map[string]any{
						"type":        "integer",
						"description": "Optional timeout in milliseconds (0 = config default)",
					},
				},
				"required": []string{"prompt"},
			},
		},
		{
			Name: "task_list",
			Description: "List sub-agent jobs for the current parent session " +
				"(includes completed jobs from SQLite after restart).",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "task_status",
			Description: "Get status for one sub-agent by id " +
				"(works for finished jobs persisted in SQLite).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Job id (sub_...)"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name: "task_wait",
			Description: "Wait for one sub-agent (id) or all jobs for this parent session. " +
				"Returns durable summaries from SQLite when jobs already finished " +
				"(including after a crash/restart). Always call this after background task spawns.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Job id; omit or empty to wait for all",
					},
				},
			},
		},
		{
			Name:        "task_cancel",
			Description: "Cancel one sub-agent (id) or all non-terminal jobs for this parent session.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Job id; omit or empty to cancel all",
					},
				},
			},
		},
	}
}

// IsTaskTool reports whether name is a parent task control-plane tool.
func IsTaskTool(name string) bool {
	switch name {
	case "task", "task_list", "task_status", "task_wait", "task_cancel":
		return true
	default:
		return false
	}
}

// Execute dispatches a task tool. status is completed, denied, or error.
// result is a JSON string for the model. err is reserved for host wiring failures.
// parentSessionID from the parent agent wins over Host.ParentSessionID.
func (h *Host) Execute(ctx context.Context, parentSessionID, name, argsJSON, partID string) (result, metaJSON, status string, err error) {
	if h == nil || h.Mgr == nil {
		return toolError("subagent host not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if parentSessionID != "" {
		h.ParentSessionID = parentSessionID
	}
	switch name {
	case "task":
		return h.execTask(ctx, argsJSON, partID)
	case "task_list":
		return h.execList()
	case "task_status":
		return h.execStatus(argsJSON)
	case "task_wait":
		return h.execWait(ctx, argsJSON)
	case "task_cancel":
		return h.execCancel(argsJSON)
	default:
		msg := "unknown tool: " + name
		return msg, `{"denied":true}`, "denied", nil
	}
}

type taskArgs struct {
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Model       string `json:"model"`
	Variant     string `json:"variant"`
	MaxSteps    int    `json:"max_steps"`
	Background  bool   `json:"background"`
	TimeoutMS   int64  `json:"timeout_ms"`
}

type idArgs struct {
	ID string `json:"id"`
}

func (h *Host) execTask(ctx context.Context, argsJSON, partID string) (string, string, string, error) {
	var args taskArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolError("invalid task arguments: " + err.Error())
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return toolError("task: prompt is required")
	}
	var timeout time.Duration
	if args.TimeoutMS > 0 {
		timeout = time.Duration(args.TimeoutMS) * time.Millisecond
	}
	spec := Spec{
		Name:        args.Name,
		Prompt:      args.Prompt,
		Description: args.Description,
		Role:        args.Role,
		Model:       args.Model,
		Variant:     args.Variant,
		MaxSteps:    args.MaxSteps,
		Background:  args.Background,
		Timeout:     timeout,
	}
	snap, err := h.Mgr.Spawn(ctx, h.ParentSessionID, partID, spec)
	if err != nil {
		return toolError(err.Error())
	}
	return toolJSON(snap, "completed")
}

func (h *Host) execList() (string, string, string, error) {
	list := h.Mgr.List(h.ParentSessionID)
	if list == nil {
		list = []Snapshot{}
	}
	return toolJSON(map[string]any{"tasks": list}, "completed")
}

func (h *Host) execStatus(argsJSON string) (string, string, string, error) {
	var args idArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolError("invalid task_status arguments: " + err.Error())
	}
	if strings.TrimSpace(args.ID) == "" {
		return toolError("task_status: id is required")
	}
	snap, ok := h.Mgr.Status(args.ID)
	if !ok {
		return toolError(fmt.Sprintf("task_status: unknown id %q", args.ID))
	}
	return toolJSON(snap, "completed")
}

func (h *Host) execWait(ctx context.Context, argsJSON string) (string, string, string, error) {
	var args idArgs
	if strings.TrimSpace(argsJSON) != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return toolError("invalid task_wait arguments: " + err.Error())
		}
	}
	if strings.TrimSpace(args.ID) != "" {
		res, err := h.Mgr.Wait(ctx, args.ID)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(res, "completed")
	}
	results, err := h.Mgr.WaitAll(ctx, h.ParentSessionID)
	if err != nil {
		return toolError(err.Error())
	}
	if results == nil {
		results = []Result{}
	}
	return toolJSON(map[string]any{"results": results}, "completed")
}

func (h *Host) execCancel(argsJSON string) (string, string, string, error) {
	var args idArgs
	if strings.TrimSpace(argsJSON) != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return toolError("invalid task_cancel arguments: " + err.Error())
		}
	}
	if strings.TrimSpace(args.ID) != "" {
		snap, err := h.Mgr.Cancel(args.ID)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(snap, "completed")
	}
	n := h.Mgr.CancelAll(h.ParentSessionID)
	return toolJSON(map[string]any{"cancelled": n}, "completed")
}

func toolJSON(v any, status string) (string, string, string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return toolError("encode: " + err.Error())
	}
	return string(b), string(b), status, nil
}

func toolError(msg string) (string, string, string, error) {
	meta, _ := json.Marshal(map[string]string{"error": msg})
	return msg, string(meta), "error", nil
}
