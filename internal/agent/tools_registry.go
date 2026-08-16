package agent

import (
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

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
)

// DefaultParentTools is the full parent allowlist without task tools.
var DefaultParentTools = []string{
	toolBash, toolRead, toolWrite, toolEdit, toolGrep, toolWebfetch, toolQuestion, toolTodowrite,
}

// DefaultChildTools is the general-role child allowlist (no question, no task).
var DefaultChildTools = []string{
	toolBash, toolRead, toolWrite, toolEdit, toolGrep, toolWebfetch,
}

var allBaseToolSpecs = map[string]opencode.ToolSpec{
	toolBash: {
		Name:        toolBash,
		Description: "Run a shell command. Dangerous commands are gated by a human confirm.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "shell command to run"},
				"workdir": map[string]any{"type": "string", "description": "working directory"},
			},
			"required": []string{"command"},
		},
	},
	toolRead: {
		Name:        toolRead,
		Description: "Read a file under the session workdir.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filePath": map[string]any{"type": "string", "description": "path relative to or under workdir"},
			},
			"required": []string{"filePath"},
		},
	},
	toolGrep: {
		Name:        toolGrep,
		Description: "Search file contents under the workdir with ripgrep (fast). Prefer this over reading many files. Returns path:line:match hits.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "regex pattern to search for",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "file or directory under workdir (default: workdir root)",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "filename glob filter, e.g. *.go",
				},
				"caseInsensitive": map[string]any{
					"type":        "boolean",
					"description": "case-insensitive search",
				},
				"maxMatches": map[string]any{
					"type":        "integer",
					"description": "max hits to return (default 50, max 200)",
				},
			},
			"required": []string{"pattern"},
		},
	},
	toolWrite: {
		Name:        toolWrite,
		Description: "Write a file under the session workdir (parent dirs must exist).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filePath": map[string]any{"type": "string", "description": "path relative to or under workdir"},
				"contents": map[string]any{"type": "string", "description": "full file contents"},
			},
			"required": []string{"filePath", "contents"},
		},
	},
	toolEdit: {
		Name:        toolEdit,
		Description: "Replace one unique occurrence of oldString with newString in a file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filePath":  map[string]any{"type": "string"},
				"oldString": map[string]any{"type": "string"},
				"newString": map[string]any{"type": "string"},
			},
			"required": []string{"filePath", "oldString", "newString"},
		},
	},
	toolWebfetch: {
		Name:        toolWebfetch,
		Description: "Fetch an http(s) URL and return the body as text or markdown.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":    map[string]any{"type": "string"},
				"format": map[string]any{"type": "string", "description": "markdown or text"},
			},
			"required": []string{"url"},
		},
	},
	toolQuestion: {
		Name:        toolQuestion,
		Description: "Ask the human a multiple-choice question via the TUI.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"question": map[string]any{"type": "string"},
							"header":   map[string]any{"type": "string"},
							"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"question", "options"},
					},
				},
			},
			"required": []string{"questions"},
		},
	},
	toolTodowrite: {
		Name: toolTodowrite,
		Description: "Replace the session todo checklist with the full list you pass. " +
			"Send every item every time (replace-all). Status: pending, in_progress, completed, cancelled. " +
			"Use this to plan multi-step work and keep the user-visible tracker in sync.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{"type": "string", "description": "short checklist item"},
							"status":  map[string]any{"type": "string", "description": "pending|in_progress|completed|cancelled"},
						},
						"required": []string{"content"},
					},
				},
			},
			"required": []string{"todos"},
		},
	},
}

// toolSpecsFor returns provider ToolSpecs for the given allowlist names.
// Unknown names are skipped. When names is empty, DefaultParentTools is used.
func toolSpecsFor(names []string, host SubagentHost) []opencode.ToolSpec {
	if len(names) == 0 {
		names = DefaultParentTools
	}
	out := make([]opencode.ToolSpec, 0, len(names)+5)
	seen := make(map[string]struct{}, len(names)+5)
	for _, n := range names {
		if _, ok := seen[n]; ok {
			continue
		}
		if isTaskToolName(n) {
			continue // task tools come only from Host
		}
		spec, ok := allBaseToolSpecs[n]
		if !ok {
			continue
		}
		seen[n] = struct{}{}
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

func isTaskToolName(name string) bool {
	switch name {
	case toolTask, toolTaskList, toolTaskStatus, toolTaskWait, toolTaskCancel:
		return true
	default:
		return false
	}
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
