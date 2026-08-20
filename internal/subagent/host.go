package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/task"
)

// Host is the parent-side control plane for task tools.
// It does not import internal/agent; chat/agent wire Mgr and ParentSessionID.
// Specs and arg parsing live in internal/tools/task; Host executes them.
type Host struct {
	Mgr             *Manager
	ParentSessionID string
}

// NewHost wraps a Manager for tool dispatch.
func NewHost(mgr *Manager) *Host {
	return &Host{Mgr: mgr}
}

// Specs returns the parent task tool advertisements (owned by tools/task).
func (h *Host) Specs() []opencode.ToolSpec {
	return task.Specs()
}

// IsTaskTool reports whether name is a parent task control-plane tool.
func IsTaskTool(name string) bool {
	return task.IsTaskTool(name)
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
	case task.ToolTask:
		return h.execTask(ctx, argsJSON, partID)
	case task.ToolTaskList:
		return h.execList()
	case task.ToolTaskStatus:
		return h.execStatus(argsJSON)
	case task.ToolTaskWait:
		return h.execWait(ctx, argsJSON)
	case task.ToolTaskCancel:
		return h.execCancel(argsJSON)
	default:
		msg := "unknown tool: " + name
		return msg, `{"denied":true}`, "denied", nil
	}
}

func (h *Host) execTask(ctx context.Context, argsJSON, partID string) (string, string, string, error) {
	args, err := task.ParseTaskArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
	}
	// Lifetime is settings-owned (Config.Timeout / default_timeout_sec).
	// Leaving Spec.Timeout at 0 makes Manager.Spawn apply the config default.
	spec := Spec{
		Name:        args.Name,
		Prompt:      args.Prompt,
		Description: args.Description,
		Role:        args.Role,
		Model:       args.Model,
		Variant:     args.Variant,
		MaxSteps:    args.MaxSteps,
		Background:  args.Background,
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
	args, err := task.ParseStatusArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
	}
	snap, ok := h.Mgr.Status(args.ID)
	if !ok {
		return toolError(fmt.Sprintf("task_status: unknown id %q", args.ID))
	}
	return toolJSON(snap, "completed")
}

func (h *Host) execWait(ctx context.Context, argsJSON string) (string, string, string, error) {
	args, err := task.ParseWaitArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
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
	args, err := task.ParseCancelArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
	}
	if args.CancelAll || strings.TrimSpace(args.ID) == "" {
		n := h.Mgr.CancelAll(h.ParentSessionID)
		return toolJSON(map[string]any{"cancelled": n}, "completed")
	}
	snap, err := h.Mgr.Cancel(args.ID)
	if err != nil {
		return toolError(err.Error())
	}
	return toolJSON(snap, "completed")
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
