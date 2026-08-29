package provider

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
	// IDOpenCode is the default provider shipped with lazykoder.
	IDOpenCode = "opencode"
	// IDOpenAI identifies the OpenAI chat-completions provider.
	IDOpenAI = "openai"
	// IDGrok identifies the xAI Grok provider.
	IDGrok = "grok"
	// IDCodex identifies the OpenAI Codex model provider.
	IDCodex = "codex"
	// IDXAI identifies the xAI provider.
	IDXAI = "xai"

	providerOrderOpenCode = (iota + 1) * 10
	providerOrderOpenAI
	providerOrderGrok
	providerOrderCodex
	providerOrderXAI
)

// Descriptor is the user-facing metadata for one selectable provider.
type Descriptor struct {
	ID           string              `json:"id"`
	Label        string              `json:"label"`
	AuthMethod   AuthMethod          `json:"auth_method"`
	EnvKey       string              `json:"env_key,omitempty"`
	EnvKeys      []string            `json:"env_keys,omitempty"`
	CLI          string              `json:"cli,omitempty"`
	BaseURL      string              `json:"base_url,omitempty"`
	Model        string              `json:"model,omitempty"`
	Supported    bool                `json:"supported"`
	DisplayOrder int                 `json:"display_order,omitempty"`
	Aliases      []string            `json:"aliases,omitempty"`
	Factory      DescriptorFactory   `json:"-"`
	AuthChecker  AuthChecker         `json:"-"`
	LoginCommand LoginCommandFactory `json:"-"`
}

// ProviderDescriptor is the declarative JSON form of Descriptor.
type ProviderDescriptor = Descriptor

// DescriptorFactory builds a provider client from its validated descriptor.
type DescriptorFactory func(Descriptor) (Client, error)

// Catalog is one bounded provider discovery result.
type Catalog struct {
	Providers   []Descriptor
	Diagnostics []catalog.Diagnostic
	ScannedAt   time.Time
}

var providerRegistry = struct {
	sync.RWMutex
	entries map[string]Descriptor
}{entries: make(map[string]Descriptor)}

var builtinProviders = []Descriptor{
	{
		ID: IDOpenCode, Label: "OpenCode", AuthMethod: AuthMethodAPIKey,
		EnvKey: "OPENCODE_API_KEY", EnvKeys: []string{"OPENCODE_API_KEY", "OPENCODE_ZEN_API_KEY"},
		Model: "deepseek-v4-flash", Supported: true, DisplayOrder: providerOrderOpenCode,
		Aliases: []string{"opencode-go", "opencode-zen"},
	},
	{
		ID: IDOpenAI, Label: "OpenAI", AuthMethod: AuthMethodAPIKey,
		EnvKey: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1",
		Model: "gpt-4.1-mini", Supported: true, DisplayOrder: providerOrderOpenAI,
		Aliases: []string{"openei", "open-ai"},
	},
	{
		ID: IDGrok, Label: "Grok", AuthMethod: AuthMethodGrok,
		CLI: "grok", EnvKey: "XAI_API_KEY", EnvKeys: []string{"XAI_API_KEY"}, Model: "grok-4.6", Supported: true, DisplayOrder: providerOrderGrok,
	},
	{
		ID: IDCodex, Label: "Codex", AuthMethod: AuthMethodCodex,
		CLI: "codex", EnvKey: "OPENAI_API_KEY", EnvKeys: []string{"OPENAI_API_KEY"}, Supported: true, DisplayOrder: providerOrderCodex,
	},
	{
		ID: IDXAI, Label: "xAI", AuthMethod: AuthMethodAPIKey,
		EnvKey: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1",
		Model: "grok-4.6", Supported: true, DisplayOrder: providerOrderXAI,
		Aliases: []string{"x-ai"},
	},
}

// Register adds one compiled provider descriptor. Duplicate IDs return an
// error, so registration mistakes fail at the caller instead of panicking.
func Register(descriptor Descriptor) error {
	descriptor, err := normalizeDescriptor(descriptor)
	if err != nil {
		return err
	}
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	if _, exists := providerRegistry.entries[descriptor.ID]; exists {
		return fmt.Errorf("provider: duplicate provider %q", descriptor.ID)
	}
	providerRegistry.entries[descriptor.ID] = descriptor
	return nil
}

// Descriptors returns a copy of the current provider catalog.
func Descriptors() []Descriptor {
	providerRegistry.RLock()
	defer providerRegistry.RUnlock()
	return sortedDescriptors(providerRegistry.entries)
}

// IDs returns canonical provider IDs in display order.
func IDs() []string {
	descriptors := Descriptors()
	ids := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		ids = append(ids, descriptor.ID)
	}
	return ids
}

// DescriptorFor finds a provider by canonical ID or alias.
func DescriptorFor(id string) (Descriptor, bool) {
	canonical := canonicalID(id)
	if canonical == "" {
		return Descriptor{}, false
	}
	providerRegistry.RLock()
	defer providerRegistry.RUnlock()
	descriptor, ok := providerRegistry.entries[canonical]
	return descriptor, ok
}

// DefaultModel returns the configured default model for id.
func DefaultModel(id string) string {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return ""
	}
	return descriptor.Model
}

// Normalize canonicalizes known IDs and returns a trimmed lower-case value for
// unknown IDs. Callers that need a usable provider must check DescriptorFor.
func Normalize(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if canonical := canonicalID(id); canonical != "" {
		return canonical
	}
	return id
}

func canonicalID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ""
	}
	providerRegistry.RLock()
	defer providerRegistry.RUnlock()
	if _, ok := providerRegistry.entries[id]; ok {
		return id
	}
	for canonical, descriptor := range providerRegistry.entries {
		for _, alias := range descriptor.Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), id) {
				return canonical
			}
		}
	}
	return ""
}

func sortedDescriptors(entries map[string]Descriptor) []Descriptor {
	out := make([]Descriptor, 0, len(entries))
	for _, descriptor := range entries {
		descriptor.EnvKeys = append([]string{}, descriptor.EnvKeys...)
		descriptor.Aliases = append([]string{}, descriptor.Aliases...)
		out = append(out, descriptor)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DisplayOrder != out[j].DisplayOrder {
			return out[i].DisplayOrder < out[j].DisplayOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func normalizeDescriptor(descriptor Descriptor) (Descriptor, error) {
	descriptor.ID = strings.ToLower(strings.TrimSpace(descriptor.ID))
	if descriptor.ID == "" {
		return Descriptor{}, errors.New("provider: provider ID is required")
	}
	for _, char := range descriptor.ID {
		valid := char >= 'a' && char <= 'z'
		valid = valid || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.'
		if !valid {
			return Descriptor{}, fmt.Errorf("provider: invalid provider ID %q", descriptor.ID)
		}
	}
	if descriptor.Label == "" {
		descriptor.Label = descriptor.ID
	}
	descriptor.Label = strings.TrimSpace(descriptor.Label)
	descriptor.AuthMethod = normalizeAuthMethod(descriptor.AuthMethod)
	if descriptor.AuthMethod != AuthMethodAPIKey && descriptor.AuthMethod != AuthMethodCodex && descriptor.AuthMethod != AuthMethodGrok {
		return Descriptor{}, fmt.Errorf("provider: unsupported auth method %q", descriptor.AuthMethod)
	}
	descriptor.EnvKey = strings.TrimSpace(descriptor.EnvKey)
	descriptor.EnvKeys = cleanStrings(append([]string{descriptor.EnvKey}, descriptor.EnvKeys...))
	if len(descriptor.EnvKeys) > 0 {
		descriptor.EnvKey = descriptor.EnvKeys[0]
	}
	descriptor.CLI = strings.TrimSpace(descriptor.CLI)
	descriptor.BaseURL = strings.TrimRight(strings.TrimSpace(descriptor.BaseURL), "/")
	descriptor.Model = strings.TrimSpace(descriptor.Model)
	descriptor.Aliases = cleanStrings(descriptor.Aliases)
	if descriptor.DisplayOrder == 0 {
		descriptor.DisplayOrder = 1000
	}
	return descriptor, nil
}

func normalizeAuthMethod(method AuthMethod) AuthMethod {
	switch strings.ToLower(strings.TrimSpace(string(method))) {
	case "api_key", "api-key", "apikey", "":
		return AuthMethodAPIKey
	case "codex", "codex-login":
		return AuthMethodCodex
	case "grok", "grok-login":
		return AuthMethodGrok
	default:
		return method
	}
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

// LoadProviders merges built-ins and compiled registrations with bounded JSON
// descriptors from local and global .lazykoder directories. It also installs
// the merged catalog so NewClient and the TUI use the same snapshot.
func LoadProviders(workdir string) (Catalog, error) {
	if strings.TrimSpace(workdir) == "" {
		return Catalog{}, errors.New("provider: workdir is required")
	}
	roots, diagnostics := catalog.ResolveRoots(workdir, true, true, nil)
	entries := baseProviderEntries()
	for _, root := range roots {
		path := filepath.Join(root.Path, "providers.json")
		data, err := catalog.ReadBoundedFile(path, catalog.DefaultMaxDescriptorSize)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: path, Error: err.Error()})
			continue
		}
		items, itemDiagnostics := parseProviderFile(data, root)
		diagnostics = append(diagnostics, itemDiagnostics...)
		for _, item := range items {
			if item.Descriptor.Factory == nil {
				if factory, ok := builtinFactories[item.Descriptor.ID]; ok {
					item.Descriptor.Factory = factory
				} else if item.Descriptor.AuthMethod == AuthMethodAPIKey && isOpenCodeDescriptor(item.Descriptor) {
					item.Descriptor.Factory = builtinFactories[IDOpenCode]
				}
			}
			if item.Descriptor.AuthMethod != AuthMethodAPIKey && item.Descriptor.AuthChecker == nil {
				item.Descriptor.AuthChecker = cliAuthChecker
				item.Descriptor.LoginCommand = cliLoginCommand
			}
			if previous, exists := entries[item.Descriptor.ID]; exists && root.Scope == catalog.ScopeGlobal && previous.Source == "local" {
				continue
			}
			entries[item.Descriptor.ID] = item
		}
	}
	providerRegistry.Lock()
	providerRegistry.entries = make(map[string]Descriptor, len(entries))
	for id, entry := range entries {
		providerRegistry.entries[id] = entry.Descriptor
	}
	providers := sortedDescriptors(providerRegistry.entries)
	providerRegistry.Unlock()
	return Catalog{Providers: providers, Diagnostics: diagnostics, ScannedAt: time.Now().UTC()}, nil
}

type providerEntry struct {
	Descriptor Descriptor
	Source     string
}

func baseProviderEntries() map[string]providerEntry {
	providerRegistry.RLock()
	defer providerRegistry.RUnlock()
	entries := make(map[string]providerEntry, len(providerRegistry.entries))
	builtinIDs := make(map[string]struct{}, len(builtinProviders))
	for _, descriptor := range builtinProviders {
		builtinIDs[descriptor.ID] = struct{}{}
	}
	for id, descriptor := range providerRegistry.entries {
		source := "compiled"
		if _, ok := builtinIDs[id]; ok {
			source = "builtin"
		}
		entries[id] = providerEntry{Descriptor: descriptor, Source: source}
	}
	return entries
}

type providerJSON struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	AuthMethod   AuthMethod `json:"auth_method"`
	EnvKey       string     `json:"env_key"`
	EnvKeys      []string   `json:"env_keys"`
	CLI          string     `json:"cli"`
	BaseURL      string     `json:"base_url"`
	Model        string     `json:"model"`
	Supported    *bool      `json:"supported"`
	DisplayOrder int        `json:"display_order"`
	Aliases      []string   `json:"aliases"`
}

func parseProviderFile(data []byte, root catalog.Root) ([]providerEntry, []catalog.Diagnostic) {
	path := filepath.Join(root.Path, "providers.json")
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []catalog.Diagnostic{{Scope: root.Scope, Path: path, Error: "invalid JSON: " + err.Error()}}
	}
	items := make([]providerEntry, 0, minInt(len(raw), catalog.DefaultMaxDescriptors))
	var diagnostics []catalog.Diagnostic
	seen := make(map[string]struct{}, len(raw))
	for index, item := range raw {
		if index >= catalog.DefaultMaxDescriptors {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: path, Error: "descriptor count limit exceeded"})
			break
		}
		var value providerJSON
		if err := json.Unmarshal(item, &value); err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: err.Error()})
			continue
		}
		supported := true
		if value.Supported != nil {
			supported = *value.Supported
		}
		descriptor, err := normalizeDescriptor(Descriptor{
			ID: value.ID, Label: value.Label, AuthMethod: value.AuthMethod,
			EnvKey: value.EnvKey, EnvKeys: value.EnvKeys, CLI: value.CLI,
			BaseURL: value.BaseURL, Model: value.Model, Supported: supported,
			DisplayOrder: value.DisplayOrder, Aliases: value.Aliases,
		})
		if err != nil {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: err.Error()})
			continue
		}
		if descriptor.AuthMethod == AuthMethodAPIKey && descriptor.BaseURL == "" {
			if _, builtin := builtinFactories[descriptor.ID]; !builtin && !isOpenCodeDescriptor(descriptor) {
				diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: "base_url is required for declarative API-key providers"})
				continue
			}
		}
		if _, exists := seen[descriptor.ID]; exists {
			diagnostics = append(diagnostics, catalog.Diagnostic{Scope: root.Scope, Path: fmt.Sprintf("%s[%d]", path, index), Error: "duplicate provider ID"})
			continue
		}
		seen[descriptor.ID] = struct{}{}
		items = append(items, providerEntry{Descriptor: descriptor, Source: string(root.Scope)})
	}
	return items, diagnostics
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isOpenCodeDescriptor(descriptor Descriptor) bool {
	if strings.HasPrefix(descriptor.ID, IDOpenCode) {
		return true
	}
	if strings.Contains(strings.ToLower(descriptor.BaseURL), "opencode.ai") {
		return true
	}
	return false
}

// ResetForTest restores the built-in provider set.
func ResetForTest() {
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providerRegistry.entries = make(map[string]Descriptor, len(builtinProviders))
	for _, raw := range builtinProviders {
		descriptor, err := normalizeDescriptor(raw)
		if err != nil {
			continue
		}
		descriptor.Factory = builtinFactories[descriptor.ID]
		if descriptor.AuthMethod != AuthMethodAPIKey {
			descriptor.AuthChecker = cliAuthChecker
			descriptor.LoginCommand = cliLoginCommand
		}
		providerRegistry.entries[descriptor.ID] = descriptor
	}
}
