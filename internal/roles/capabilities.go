// Package roles owns registered sub-agent roles and their effective policies.
package roles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/catalog"
)

const (
	Explore = "explore"
	Plan    = "plan"
	General = "general"

	roleOrderExplore = (iota + 1) * 10
	roleOrderPlan
	roleOrderGeneral
)

// Role describes one sub-agent policy. Prompt is optional display-only context.
type Role struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	Tools             []string `json:"tools"`
	SingleWriter      bool     `json:"single_writer"`
	DefaultModelClass string   `json:"model_class"`
	Prompt            string   `json:"prompt,omitempty"`
	DisplayOrder      int      `json:"display_order,omitempty"`
}

// Catalog is one bounded role discovery result.
type Catalog struct {
	Roles       []Role
	Diagnostics []catalog.Diagnostic
	ScannedAt   time.Time
}

var roleRegistry = struct {
	sync.RWMutex
	entries map[string]Role
}{entries: make(map[string]Role)}

var builtinRoles = []Role{
	{ID: Explore, Label: "Explore", Tools: []string{"bash", "read", "grep", "webfetch"}, DefaultModelClass: "flash", DisplayOrder: roleOrderExplore},
	{ID: Plan, Label: "Plan", Tools: []string{"bash", "read", "grep", "webfetch"}, DefaultModelClass: "pro", DisplayOrder: roleOrderPlan},
	{ID: General, Label: "General", Tools: []string{"bash", "read", "grep", "write", "edit", "webfetch"}, SingleWriter: true, DefaultModelClass: "pro", DisplayOrder: roleOrderGeneral},
}

// Register adds one compiled role descriptor.
func Register(role Role) error {
	role, err := normalize(role)
	if err != nil {
		return err
	}
	roleRegistry.Lock()
	defer roleRegistry.Unlock()
	if _, exists := roleRegistry.entries[role.ID]; exists {
		return fmt.Errorf("roles: duplicate role %q", role.ID)
	}
	roleRegistry.entries[role.ID] = role
	return nil
}

func init() {
	for _, role := range builtinRoles {
		if err := Register(role); err != nil {
			panic(err)
		}
	}
}

// Roles returns the registered roles in display order.
func Roles() []Role {
	roleRegistry.RLock()
	defer roleRegistry.RUnlock()
	out := make([]Role, 0, len(roleRegistry.entries))
	for _, role := range roleRegistry.entries {
		role.Tools = append([]string{}, role.Tools...)
		out = append(out, role)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DisplayOrder != out[j].DisplayOrder {
			return out[i].DisplayOrder < out[j].DisplayOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// IDs returns registered role IDs in display order.
func IDs() []string {
	roles := Roles()
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	return ids
}

// DescriptorFor returns a registered role.
func DescriptorFor(id string) (Role, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	roleRegistry.RLock()
	defer roleRegistry.RUnlock()
	role, ok := roleRegistry.entries[id]
	return role, ok
}

// IsKnown reports whether id names a registered role.
func IsKnown(id string) bool {
	_, ok := DescriptorFor(id)
	return ok
}

// Normalize returns a registered role or the registered fallback.
func Normalize(role, fallback string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if IsKnown(role) {
		return role
	}
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if IsKnown(fallback) {
		return fallback
	}
	if IsKnown(Explore) {
		return Explore
	}
	return ""
}

// Tools returns a copy of the registered role allowlist.
func Tools(role string) []string {
	descriptor, ok := DescriptorFor(Normalize(role, Explore))
	if !ok {
		return []string{}
	}
	return append([]string{}, descriptor.Tools...)
}

// Load merges bounded local and global role descriptors and installs them.
func Load(workdir string) (Catalog, error) {
	if strings.TrimSpace(workdir) == "" {
		return Catalog{}, errors.New("roles: workdir is required")
	}
	roots, diagnostics := catalog.ResolveRoots(workdir, true, true, nil)
	entries := baseEntries()
	for _, root := range roots {
		path := filepath.Join(root.Path, "roles.json")
		data, err := catalog.ReadBoundedFile(path, catalog.DefaultMaxDescriptorSize)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: path, Error: err.Error()})
			continue
		}
		items, itemDiagnostics := parseFile(data, root)
		diagnostics = append(diagnostics, itemDiagnostics...)
		for _, item := range items {
			if previous, exists := entries[item.ID]; exists && root.Scope == catalog.ScopeGlobal && previous.Source == "local" {
				continue
			}
			entries[item.ID] = roleEntry{Role: item, Source: string(root.Scope)}
		}
	}
	roleRegistry.Lock()
	roleRegistry.entries = make(map[string]Role, len(entries))
	for id, entry := range entries {
		roleRegistry.entries[id] = entry.Role
	}
	roles := make([]Role, 0, len(entries))
	for _, entry := range entries {
		roles = append(roles, entry.Role)
	}
	sort.SliceStable(roles, func(i, j int) bool {
		if roles[i].DisplayOrder != roles[j].DisplayOrder {
			return roles[i].DisplayOrder < roles[j].DisplayOrder
		}
		return roles[i].ID < roles[j].ID
	})
	roleRegistry.Unlock()
	return Catalog{Roles: roles, Diagnostics: diagnostics, ScannedAt: time.Now().UTC()}, nil
}

type roleEntry struct {
	Role   Role
	Source string
}

func baseEntries() map[string]roleEntry {
	roleRegistry.RLock()
	defer roleRegistry.RUnlock()
	builtinIDs := make(map[string]struct{}, len(builtinRoles))
	for _, role := range builtinRoles {
		builtinIDs[role.ID] = struct{}{}
	}
	entries := make(map[string]roleEntry, len(roleRegistry.entries))
	for id, role := range roleRegistry.entries {
		source := "compiled"
		if _, ok := builtinIDs[id]; ok {
			source = "builtin"
		}
		entries[id] = roleEntry{Role: role, Source: source}
	}
	return entries
}

func parseFile(data []byte, root catalog.Root) ([]Role, []catalog.Diagnostic) {
	path := filepath.Join(root.Path, "roles.json")
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []catalog.Diagnostic{{Scope: root.Scope, Path: path, Error: "invalid JSON: " + err.Error()}}
	}
	items := make([]Role, 0, minInt(len(raw), catalog.DefaultMaxDescriptors))
	var diagnostics []catalog.Diagnostic
	seen := make(map[string]struct{}, len(raw))
	for index, item := range raw {
		if index >= catalog.DefaultMaxDescriptors {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: path, Error: "descriptor count limit exceeded"})
			break
		}
		var role Role
		if err := json.Unmarshal(item, &role); err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: err.Error()})
			continue
		}
		role, err := normalize(role)
		if err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: err.Error()})
			continue
		}
		if _, exists := seen[role.ID]; exists {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: "duplicate role ID"})
			continue
		}
		seen[role.ID] = struct{}{}
		items = append(items, role)
	}
	return items, diagnostics
}

func normalize(role Role) (Role, error) {
	role.ID = strings.ToLower(strings.TrimSpace(role.ID))
	if role.ID == "" {
		return Role{}, errors.New("roles: role ID is required")
	}
	for _, char := range role.ID {
		valid := char >= 'a' && char <= 'z'
		valid = valid || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.'
		if !valid {
			return Role{}, fmt.Errorf("roles: invalid role ID %q", role.ID)
		}
	}
	if strings.TrimSpace(role.Label) == "" {
		role.Label = role.ID
	}
	role.Label = strings.TrimSpace(role.Label)
	role.DefaultModelClass = strings.ToLower(strings.TrimSpace(role.DefaultModelClass))
	if role.DefaultModelClass == "" {
		role.DefaultModelClass = "general"
	}
	role.Tools = cleanStrings(role.Tools)
	role.Prompt = strings.TrimSpace(role.Prompt)
	if role.DisplayOrder == 0 {
		role.DisplayOrder = 1000
	}
	return role, nil
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

// ResetForTest restores built-in roles.
func ResetForTest() {
	roleRegistry.Lock()
	defer roleRegistry.Unlock()
	roleRegistry.entries = make(map[string]Role, len(builtinRoles))
	for _, role := range builtinRoles {
		roleRegistry.entries[role.ID] = role
	}
}
