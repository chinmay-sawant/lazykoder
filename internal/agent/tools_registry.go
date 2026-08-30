package agent

import (
	"fmt"

	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
	"github.com/chinmay-sawant/lazykoder/internal/prompts"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/task"
)

// Tool is the extension contract for compiled and discovered tools.
type Tool = toolplugin.Tool

// ToolContext is the capability set passed to extension tools.
type ToolContext = toolplugin.Context

// Register adds a compiled extension tool to the shared registry.
func Register(name string, tool Tool) error {
	return toolplugin.Register(name, tool)
}

// RegisteredToolIDs returns the current extension IDs.
func RegisteredToolIDs() []string {
	return toolplugin.IDs()
}

// Base tool names advertised to parent and child agents (role allowlists filter further).
const (
	toolBash       = "bash"
	toolRead       = "read"
	toolWrite      = "write"
	toolEdit       = "edit"
	toolGrep       = "grep"
	toolWebfetch   = "webfetch"
	toolQuestion   = "question"
	toolTodowrite  = "todowrite"
	toolTask       = "task"
	toolTaskList   = "task_list"
	toolTaskStatus = "task_status"
	toolTaskWait   = "task_wait"
	toolTaskCancel = "task_cancel"

	// toolRegistryExtraSlots is the extra capacity reserved beyond len(names)
	// when building tool spec slices and dedup maps.
	toolRegistryExtraSlots = 5
)

// DefaultParentTools is the full parent allowlist without task tools.
var DefaultParentTools = []string{
	toolBash, toolRead, toolWrite, toolEdit, toolGrep, toolWebfetch, toolQuestion, toolTodowrite,
}

type baseToolRegistration struct {
	name   string
	spec   opencode.ToolSpec
	runner baseToolRunner
}

var baseToolRegistry = map[string]baseToolRegistration{
	toolBash:      {name: toolBash, runner: (*Agent).execBash},
	toolRead:      {name: toolRead, runner: (*Agent).execRead},
	toolGrep:      {name: toolGrep, runner: (*Agent).execGrep},
	toolWrite:     {name: toolWrite, runner: (*Agent).execWrite},
	toolEdit:      {name: toolEdit, runner: (*Agent).execEdit},
	toolWebfetch:  {name: toolWebfetch, runner: (*Agent).execWebfetch},
	toolQuestion:  {name: toolQuestion, runner: (*Agent).execQuestion},
	toolTodowrite: {name: toolTodowrite, runner: (*Agent).execTodowrite},
}

func init() {
	for name, registration := range baseToolRegistry {
		registration.spec = prompts.Store{}.ToolSpec(name)
		baseToolRegistry[name] = registration
	}
}

// toolSpecsFor returns provider ToolSpecs for the given allowlist names.
// Unknown names are skipped. When names is empty, DefaultParentTools is used.
func toolSpecsFor(names []string, host SubagentHost) []opencode.ToolSpec {
	return toolSpecsForWorkdir("", names, host)
}

func toolSpecsForWorkdir(workdir string, names []string, host SubagentHost) []opencode.ToolSpec {
	if len(names) == 0 {
		names = DefaultParentTools
	}
	promptStore := prompts.New(workdir)
	out := make([]opencode.ToolSpec, 0, len(names)+toolRegistryExtraSlots)
	seen := make(map[string]struct{}, len(names)+toolRegistryExtraSlots)
	for _, n := range names {
		if _, ok := seen[n]; ok {
			continue
		}
		if isTaskToolName(n) {
			continue // task tools come only from Host
		}
		registration, ok := baseToolRegistry[n]
		if !ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, promptStore.ToolSpec(registration.name))
	}
	for _, spec := range toolplugin.Specs(names) {
		if _, ok := seen[spec.Name]; ok {
			continue
		}
		seen[spec.Name] = struct{}{}
		out = append(out, spec)
	}
	if host != nil {
		for _, v := range host.Specs() {
			if _, ok := seen[v.Name]; ok {
				continue
			}
			seen[v.Name] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func registeredTool(name string) (Tool, bool) {
	tool, ok := toolplugin.Lookup(name)
	return tool, ok
}

func validateRegisteredTool(name string, tool Tool) error {
	if tool == nil {
		return fmt.Errorf("agent: nil tool %q", name)
	}
	return nil
}

func isTaskToolName(name string) bool {
	return task.IsTaskTool(name)
}

func toolAllowed(names []string, name string) bool {
	if isTaskToolName(name) {
		return true // gated by Host presence in executeTool
	}
	if len(names) == 0 {
		for _, n := range DefaultParentTools {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
