// Package toolplugin defines the narrow contract for compiled and discovered
// agent tools. It does not import the agent package, which keeps registration
// usable by tool implementations without an import cycle.
package toolplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// BuiltinIDs is the stable set of tools shipped by the parent agent.
var BuiltinIDs = []string{"bash", "read", "write", "edit", "grep", "webfetch", "question", "todowrite"}

// Tool is the executable boundary for a registered tool.
type Tool interface {
	Spec() opencode.ToolSpec
	Run(context.Context, string, Context) (output string, metadata string, status string, err error)
	Title(string) string
}

// Context carries only the capabilities a tool needs. Descriptor loading never
// constructs or executes a Tool.
type Context struct {
	Workdir string
	Store   *db.Store
	Events  chan<- any
	Ask     func(string, []string) (int, error)
	Confirm func(policy.Decision, string) (bool, error)
}

var registry = struct {
	sync.RWMutex
	entries    map[string]Tool
	discovered map[string]struct{}
}{entries: make(map[string]Tool)}

// Register adds one compiled tool. The registration name must match its spec.
func Register(name string, tool Tool) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validate(name, tool); err != nil {
		return err
	}
	registry.Lock()
	defer registry.Unlock()
	if _, exists := registry.entries[name]; exists {
		return fmt.Errorf("toolplugin: duplicate tool %q", name)
	}
	registry.entries[name] = tool
	return nil
}

func validate(name string, tool Tool) error {
	if name == "" || tool == nil {
		return errors.New("toolplugin: name and tool are required")
	}
	spec := tool.Spec()
	if strings.TrimSpace(spec.Name) != name {
		return fmt.Errorf("toolplugin: spec name %q does not match %q", spec.Name, name)
	}
	return nil
}

// Replace installs a discovered tool snapshot. Discovery is allowed to
// refresh an existing descriptor while compiled registrations remain strict.
func Replace(name string, tool Tool) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validate(name, tool); err != nil {
		return err
	}
	if isBuiltin(name) {
		return fmt.Errorf("toolplugin: discovered tool %q cannot replace a built-in", name)
	}
	registry.Lock()
	defer registry.Unlock()
	if _, exists := registry.entries[name]; exists {
		if _, discovered := registry.discovered[name]; !discovered {
			return fmt.Errorf("toolplugin: discovered tool %q conflicts with compiled tool", name)
		}
	}
	registry.entries[name] = tool
	if registry.discovered == nil {
		registry.discovered = make(map[string]struct{})
	}
	registry.discovered[name] = struct{}{}
	return nil
}

// ReplaceDiscovered atomically replaces the file-loaded tool snapshot while
// preserving compiled registrations. An empty snapshot removes stale entries.
func ReplaceDiscovered(entries map[string]Tool) error {
	normalized := make(map[string]Tool, len(entries))
	for name, tool := range entries {
		name = strings.ToLower(strings.TrimSpace(name))
		if err := validate(name, tool); err != nil {
			return err
		}
		if isBuiltin(name) {
			return fmt.Errorf("toolplugin: discovered tool %q cannot replace a built-in", name)
		}
		normalized[name] = tool
	}

	registry.Lock()
	defer registry.Unlock()
	for name := range normalized {
		if _, exists := registry.entries[name]; exists {
			if _, discovered := registry.discovered[name]; !discovered {
				return fmt.Errorf("toolplugin: discovered tool %q conflicts with compiled tool", name)
			}
		}
	}
	for name := range registry.discovered {
		delete(registry.entries, name)
	}
	registry.discovered = make(map[string]struct{}, len(normalized))
	for name, tool := range normalized {
		registry.entries[name] = tool
		registry.discovered[name] = struct{}{}
	}
	return nil
}

// Lookup returns a registered tool.
func Lookup(name string) (Tool, bool) {
	registry.RLock()
	defer registry.RUnlock()
	tool, ok := registry.entries[strings.ToLower(strings.TrimSpace(name))]
	return tool, ok
}

// IDs returns built-in and compiled plugin IDs in stable order.
func IDs() []string {
	return IDsFor(true)
}

// IDsFor returns stable IDs, optionally excluding file-loaded tools.
func IDsFor(includeDiscovered bool) []string {
	registry.RLock()
	defer registry.RUnlock()
	seen := make(map[string]struct{}, len(BuiltinIDs)+len(registry.entries))
	ids := make([]string, 0, len(BuiltinIDs)+len(registry.entries))
	for _, id := range BuiltinIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range registry.entries {
		if !includeDiscovered {
			if _, ok := registry.discovered[id]; ok {
				continue
			}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// IsDiscovered reports whether id came from a declarative descriptor.
func IsDiscovered(id string) bool {
	registry.RLock()
	defer registry.RUnlock()
	_, ok := registry.discovered[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

func isBuiltin(id string) bool {
	for _, builtin := range BuiltinIDs {
		if builtin == id {
			return true
		}
	}
	return false
}

// Specs returns registered specs for the requested names.
func Specs(names []string) []opencode.ToolSpec {
	registry.RLock()
	defer registry.RUnlock()
	seen := make(map[string]struct{}, len(names))
	out := make([]opencode.ToolSpec, 0, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if _, ok := seen[name]; ok {
			continue
		}
		tool, ok := registry.entries[name]
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, cloneSpec(tool.Spec()))
	}
	return out
}

func cloneSpec(spec opencode.ToolSpec) opencode.ToolSpec {
	if spec.Parameters == nil {
		return spec
	}
	raw, err := json.Marshal(spec.Parameters)
	if err != nil {
		return spec
	}
	var parameters map[string]any
	if json.Unmarshal(raw, &parameters) == nil {
		spec.Parameters = parameters
	}
	return spec
}

// ResetForTest clears compiled registrations.
func ResetForTest() {
	registry.Lock()
	defer registry.Unlock()
	registry.entries = make(map[string]Tool)
	registry.discovered = make(map[string]struct{})
}
