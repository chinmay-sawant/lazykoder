package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/orchestrator"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/task"
)

// Host is the parent-side control plane for task tools.
// It does not import internal/agent; chat/agent wire Mgr and ParentSessionID.
// Specs and arg parsing live in internal/tools/task; Host executes them.
type Host struct {
	Mgr             *Manager
	ParentSessionID string
	plan            orchestrator.Plan
	reviewPlan      bool
	retried         map[string]bool
}

// NewHost wraps a Manager for tool dispatch.
func NewHost(mgr *Manager) *Host {
	return &Host{Mgr: mgr, retried: make(map[string]bool)}
}

// SetPlan gives the host the current turn's bounded plan so task_wait can
// review terminal child summaries and retry one failed assignment once.
func (h *Host) SetPlan(plan orchestrator.Plan, review bool) {
	h.plan = plan
	h.reviewPlan = review
	if h.retried == nil {
		h.retried = make(map[string]bool)
	}
}

// Specs returns the parent task tool advertisements (owned by tools/task).
func (h *Host) Specs() []opencode.ToolSpec {
	return task.Specs()
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
	resolvedParentID := h.ParentSessionID
	if parentSessionID != "" {
		resolvedParentID = parentSessionID
	}
	switch name {
	case task.ToolTask:
		return h.execTask(ctx, resolvedParentID, argsJSON, partID)
	case task.ToolTaskList:
		return h.execList(resolvedParentID, argsJSON)
	case task.ToolTaskStatus:
		return h.execStatus(resolvedParentID, argsJSON)
	case task.ToolTaskWait:
		return h.execWait(ctx, resolvedParentID, argsJSON)
	case task.ToolTaskCancel:
		return h.execCancel(resolvedParentID, argsJSON)
	default:
		msg := "unknown tool: " + name
		return msg, `{"denied":true}`, "denied", nil
	}
}

func (h *Host) execTask(ctx context.Context, parentSessionID, argsJSON, partID string) (string, string, string, error) {
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
		ModelClass:  args.ModelClass,
		Variant:     args.Variant,
		MaxSteps:    args.MaxSteps,
		Background:  args.Background,
	}
	snap, err := h.Mgr.Spawn(ctx, parentSessionID, partID, spec)
	if err != nil {
		return toolError(err.Error())
	}
	result, err := task.EncodeSpawnResult(task.SpawnResult{
		ID:             snap.ID,
		Name:           snap.Name,
		Role:           snap.Role,
		Status:         snap.Status,
		Background:     args.Background,
		Summary:        snap.Summary,
		Error:          snap.Err,
		ChildSessionID: snap.ChildSessionID,
	})
	return completedTaskResult(result, err)
}

func (h *Host) execList(parentSessionID, argsJSON string) (string, string, string, error) {
	if _, err := task.ParseListArgs([]byte(argsJSON)); err != nil {
		return toolError(err.Error())
	}
	result, err := task.EncodeListResult(task.ListResult{Tasks: taskInfos(h.Mgr.List(parentSessionID))})
	return completedTaskResult(result, err)
}

func (h *Host) execStatus(parentSessionID, argsJSON string) (string, string, string, error) {
	args, err := task.ParseStatusArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
	}
	snap, ok := h.Mgr.Status(args.ID)
	if !ok || snap.ParentSessionID != parentSessionID {
		return toolError(fmt.Sprintf("task_status: unknown id %q", args.ID))
	}
	result, err := task.EncodeStatusResult(task.StatusResult{Task: taskInfo(snap)})
	return completedTaskResult(result, err)
}

func (h *Host) execWait(ctx context.Context, parentSessionID, argsJSON string) (string, string, string, error) {
	args, err := task.ParseWaitArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
	}
	if strings.TrimSpace(args.ID) != "" {
		if snap, ok := h.Mgr.Status(args.ID); !ok || snap.ParentSessionID != parentSessionID {
			return toolError(fmt.Sprintf("task_wait: unknown id %q", args.ID))
		}
		res, err := h.Mgr.Wait(ctx, args.ID)
		if err != nil {
			return toolError(err.Error())
		}
		result, err := task.EncodeWaitResult(task.WaitResult{Tasks: []task.TaskInfo{taskInfoFromResult(res)}})
		return completedTaskResult(result, err)
	}
	results, err := h.Mgr.WaitAll(ctx, parentSessionID)
	if err != nil {
		return toolError(err.Error())
	}
	if h.reviewPlan && len(h.plan.Subtasks) > 0 {
		results = h.reviewAndRespawn(ctx, parentSessionID, results)
	}
	result, err := task.EncodeWaitResult(task.WaitResult{Tasks: taskInfosFromResults(results)})
	return completedTaskResult(result, err)
}

func (h *Host) reviewAndRespawn(ctx context.Context, parentSessionID string, results []Result) []Result {
	for index, result := range results {
		if result.Status == string(StatusCompleted) && strings.TrimSpace(result.Summary) != "" {
			continue
		}
		if h.retried[result.ID] {
			continue
		}
		h.retried[result.ID] = true
		spec := retrySpec(h.plan, result)
		if spec.Prompt == "" {
			continue
		}
		snap, err := h.Mgr.Spawn(ctx, parentSessionID, "", spec)
		if err != nil {
			continue
		}
		retry, err := h.Mgr.Wait(ctx, snap.ID)
		if err != nil {
			continue
		}
		results[index] = retry
	}
	return results
}

func retrySpec(plan orchestrator.Plan, result Result) Spec {
	for _, task := range plan.Subtasks {
		if task.ID == result.Name || task.Name == result.Name {
			return Spec{
				Name:        result.Name + " retry",
				Prompt:      task.Prompt + "\n\nThis is the single allowed retry. Re-check the work and finish with a concise summary.",
				Description: task.Name,
				Role:        task.Role,
				ModelClass:  task.ModelClass,
			}
		}
	}
	return Spec{
		Name:        result.Name + " retry",
		Prompt:      "Re-check the assigned sub-agent task and finish with a concise summary.\n\nPrevious error: " + result.Err,
		Description: result.Name,
		Role:        result.Role,
	}
}

func (h *Host) execCancel(parentSessionID, argsJSON string) (string, string, string, error) {
	args, err := task.ParseCancelArgs([]byte(argsJSON))
	if err != nil {
		return toolError(err.Error())
	}
	if args.CancelAll || strings.TrimSpace(args.ID) == "" {
		n := h.Mgr.RequestCancelAll(parentSessionID)
		result, err := task.EncodeCancelResult(task.CancelResult{CancelAll: true, CancelledCount: n})
		return completedTaskResult(result, err)
	}
	if snap, ok := h.Mgr.Status(args.ID); !ok || snap.ParentSessionID != parentSessionID {
		return toolError(fmt.Sprintf("task_cancel: unknown id %q", args.ID))
	}
	snap, err := h.Mgr.RequestCancel(args.ID)
	if err != nil {
		return toolError(err.Error())
	}
	result, err := task.EncodeCancelResult(task.CancelResult{ID: snap.ID, Cancelled: []string{snap.ID}})
	return completedTaskResult(result, err)
}

func completedTaskResult(result string, err error) (string, string, string, error) {
	if err != nil {
		return toolError("encode: " + err.Error())
	}
	return result, result, "completed", nil
}

func taskInfo(snap Snapshot) task.TaskInfo {
	return task.TaskInfo{
		ID:             snap.ID,
		Name:           snap.Name,
		Role:           snap.Role,
		Status:         snap.Status,
		Summary:        snap.Summary,
		Error:          snap.Err,
		ChildSessionID: snap.ChildSessionID,
		StartedAt:      snap.StartedAt,
		FinishedAt:     snap.FinishedAt,
	}
}

func taskInfoFromResult(res Result) task.TaskInfo {
	return task.TaskInfo{
		ID:             res.ID,
		Name:           res.Name,
		Role:           res.Role,
		Status:         res.Status,
		Summary:        res.Summary,
		Error:          res.Err,
		ChildSessionID: res.ChildSessionID,
	}
}

func taskInfos(snaps []Snapshot) []task.TaskInfo {
	if snaps == nil {
		return []task.TaskInfo{}
	}
	out := make([]task.TaskInfo, 0, len(snaps))
	for _, snap := range snaps {
		out = append(out, taskInfo(snap))
	}
	return out
}

func taskInfosFromResults(results []Result) []task.TaskInfo {
	if results == nil {
		return []task.TaskInfo{}
	}
	out := make([]task.TaskInfo, 0, len(results))
	for _, res := range results {
		out = append(out, taskInfoFromResult(res))
	}
	return out
}

func toolError(msg string) (string, string, string, error) {
	meta, _ := json.Marshal(map[string]string{"error": msg})
	return msg, string(meta), "error", nil
}
