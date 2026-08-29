// Package tools loads bounded declarative shell-tool descriptors.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
	"github.com/chinmay-sawant/lazykoder/internal/catalog"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/bash"
	"github.com/chinmay-sawant/lazykoder/internal/workspace"
)

// Descriptor is a file-loaded shell tool. Command is display data until the
// user enables discovered tools and a model selects this descriptor.
type Descriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Command     string         `json:"command"`
	Binaries    []string       `json:"binaries"`
	Scope       catalog.Scope  `json:"-"`
}

// Catalog is one bounded discovered-tool result.
type Catalog struct {
	Tools       []Descriptor
	Diagnostics []catalog.Diagnostic
}

// Register adds a compiled tool to the shared agent registry. Tool packages
// can use this seam from init without editing the agent dispatcher.
func Register(name string, tool toolplugin.Tool) error {
	return toolplugin.Register(name, tool)
}

// Load reads local and global tools.json files. Missing files are ignored and
// local IDs replace global IDs. No command is parsed by a shell during load.
func Load(workdir string, includeLocal, includeGlobal bool, maxDescriptors int) (Catalog, error) {
	if strings.TrimSpace(workdir) == "" {
		return Catalog{}, errors.New("tools: workdir is required")
	}
	if maxDescriptors <= 0 || maxDescriptors > catalog.DefaultMaxDescriptors {
		maxDescriptors = catalog.DefaultMaxDescriptors
	}
	roots, diagnostics := catalog.ResolveRoots(workdir, includeLocal, includeGlobal, nil)
	entries := make(map[string]Descriptor)
	for _, root := range roots {
		path := filepath.Join(root.Path, "tools.json")
		data, err := catalog.ReadBoundedFile(path, catalog.DefaultMaxDescriptorSize)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: path, Error: err.Error()})
			continue
		}
		items, itemDiagnostics := parse(data, root, maxDescriptors)
		diagnostics = append(diagnostics, itemDiagnostics...)
		for _, item := range items {
			if existing, ok := entries[item.Name]; ok && root.Scope == catalog.ScopeGlobal && existing.Scope == catalog.ScopeLocal {
				continue
			}
			entries[item.Name] = item
		}
	}
	items := make([]Descriptor, 0, len(entries))
	for _, item := range entries {
		item.Binaries = append([]string{}, item.Binaries...)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return Catalog{Tools: items, Diagnostics: diagnostics}, nil
}

func parse(data []byte, root catalog.Root, maxDescriptors int) ([]Descriptor, []catalog.Diagnostic) {
	path := filepath.Join(root.Path, "tools.json")
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []catalog.Diagnostic{{Scope: root.Scope, Path: path, Error: "invalid JSON: " + err.Error()}}
	}
	items := make([]Descriptor, 0, minInt(len(raw), maxDescriptors))
	var diagnostics []catalog.Diagnostic
	seen := make(map[string]struct{}, len(raw))
	for index, item := range raw {
		if index >= maxDescriptors {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: path, Error: "descriptor count limit exceeded"})
			break
		}
		var descriptor Descriptor
		if err := json.Unmarshal(item, &descriptor); err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: err.Error()})
			continue
		}
		descriptor, err := normalize(descriptor)
		if err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: err.Error()})
			continue
		}
		if _, exists := seen[descriptor.Name]; exists {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: "duplicate tool name"})
			continue
		}
		seen[descriptor.Name] = struct{}{}
		descriptor.Scope = root.Scope
		items = append(items, descriptor)
	}
	return items, diagnostics
}

func normalize(descriptor Descriptor) (Descriptor, error) {
	descriptor.Name = strings.ToLower(strings.TrimSpace(descriptor.Name))
	if descriptor.Name == "" {
		return Descriptor{}, errors.New("tools: name is required")
	}
	for _, char := range descriptor.Name {
		valid := char >= 'a' && char <= 'z'
		valid = valid || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.'
		if !valid {
			return Descriptor{}, fmt.Errorf("tools: invalid name %q", descriptor.Name)
		}
	}
	descriptor.Description = strings.TrimSpace(descriptor.Description)
	descriptor.Command = strings.TrimSpace(descriptor.Command)
	if descriptor.Command == "" {
		return Descriptor{}, errors.New("tools: command is required")
	}
	descriptor.Binaries = cleanStrings(descriptor.Binaries)
	if len(descriptor.Binaries) == 0 {
		return Descriptor{}, errors.New("tools: binaries allowlist is required")
	}
	return descriptor, nil
}

// Plugin adapts a discovered descriptor to the compiled tool contract.
func (descriptor Descriptor) Plugin() toolplugin.Tool {
	return shellTool{descriptor: descriptor}
}

type shellTool struct {
	descriptor Descriptor
}

func (tool shellTool) Spec() opencode.ToolSpec {
	return opencode.ToolSpec{
		Name:        tool.descriptor.Name,
		Description: tool.descriptor.Description,
		Parameters:  cloneParameters(tool.descriptor.Parameters),
	}
}

func (tool shellTool) Title(argsJSON string) string {
	var args map[string]any
	if json.Unmarshal([]byte(argsJSON), &args) == nil {
		for _, key := range []string{"command", "path", "filePath", "query"} {
			if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return tool.descriptor.Name
}

func (tool shellTool) Run(ctx context.Context, argsJSON string, c toolplugin.Context) (string, string, string, error) {
	command, err := expandCommand(tool.descriptor.Command, argsJSON)
	if err != nil {
		return "", "", "error", err
	}
	decision := policy.ClassifyWithAllowlist(command, tool.descriptor.Binaries, true)
	confirmed := false
	if decision.Action == policy.ActionAsk {
		if c.Confirm == nil {
			return "", "", "denied", bash.ErrDenied
		}
		confirmed, err = c.Confirm(decision, command)
		if err != nil || !confirmed {
			if err == nil {
				err = bash.ErrDenied
			}
			return "", "", "denied", err
		}
	}
	workdir, err := workspace.Resolve(c.Workdir, c.Workdir)
	if err != nil {
		return "", "", "denied", err
	}
	result, err := bash.Run(ctx, command, workdir, decision, confirmed, bash.Exec{})
	if err != nil {
		return result.Stdout + result.Stderr, "", "error", err
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "error"
	}
	return result.Stdout + result.Stderr, fmt.Sprintf(`{"exit_code":%d}`, result.ExitCode), status, nil
}

func expandCommand(template, argsJSON string) (string, error) {
	var values map[string]any
	decoder := json.NewDecoder(strings.NewReader(argsJSON))
	if err := decoder.Decode(&values); err != nil {
		return "", fmt.Errorf("tools: invalid arguments: %w", err)
	}
	command := template
	for key, value := range values {
		text, ok := value.(string)
		if !ok {
			encoded, err := json.Marshal(value)
			if err != nil {
				return "", err
			}
			text = string(encoded)
		}
		text = shellQuote(text)
		command = strings.ReplaceAll(command, "{{"+key+"}}", text)
		command = strings.ReplaceAll(command, "{"+key+"}", text)
	}
	return command, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func cloneParameters(parameters map[string]any) map[string]any {
	if parameters == nil {
		return map[string]any{"type": "object"}
	}
	raw, err := json.Marshal(parameters)
	if err != nil {
		return map[string]any{"type": "object"}
	}
	var clone map[string]any
	if json.Unmarshal(raw, &clone) != nil {
		return map[string]any{"type": "object"}
	}
	return clone
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
