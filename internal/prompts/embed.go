// Package prompts loads project-editable prompt templates and tool schemas.
package prompts

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// The embedded files are the fallback for workspaces that have not been
// initialized yet, and the seed copied into each workspace on initialization.
// User edits are always read from the workspace first.
//
//go:embed defaults/**
var defaultFiles embed.FS

const defaultsRoot = "defaults"

// DefaultFile is one prompt or tool-schema file copied into a workspace.
type DefaultFile struct {
	Name string
	Data []byte
}

// Store resolves prompt files for one workspace. A zero Store reads only the
// embedded defaults, which keeps package-level tests and callers safe.
type Store struct {
	dir string
}

// New returns a store rooted at <workdir>/.lazykoder/prompts.
func New(workdir string) Store {
	if strings.TrimSpace(workdir) == "" {
		return Store{}
	}
	return NewDir(filepath.Join(workdir, ".lazykoder", "prompts"))
}

// NewDir returns a store rooted at an explicit prompt directory.
func NewDir(dir string) Store {
	return Store{dir: strings.TrimSpace(dir)}
}

// Dir returns the configured workspace prompt directory, or an empty string
// when this store uses only embedded defaults.
func (s Store) Dir() string { return s.dir }

// DefaultFiles returns a deterministic copy of every seeded workspace file.
func DefaultFiles() []DefaultFile {
	var names []string
	if err := fs.WalkDir(defaultFiles, defaultsRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			names = append(names, strings.TrimPrefix(path, defaultsRoot+"/"))
		}
		return nil
	}); err != nil {
		panic(fmt.Sprintf("prompts: list defaults: %v", err))
	}
	sort.Strings(names)
	out := make([]DefaultFile, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(defaultFiles, filepath.ToSlash(filepath.Join(defaultsRoot, name)))
		if err != nil {
			panic(fmt.Sprintf("prompts: read default %q: %v", name, err))
		}
		out = append(out, DefaultFile{Name: name, Data: append([]byte(nil), data...)})
	}
	return out
}

// Must returns a prompt file. A malformed or empty custom file falls back to
// its embedded default so a user edit cannot silently remove a safety prompt.
func (s Store) Must(name string) string {
	text, err := s.load(name)
	if err != nil {
		panic(fmt.Sprintf("prompts: missing %q: %v", name, err))
	}
	return text
}

// Must is kept as a package-level convenience for embedded-default callers.
func Must(name string) string { return Store{}.Must(name) }

// Render parses a prompt as a text/template. Invalid custom templates fall
// back to the embedded default. Prompt data is supplied by the caller so the
// template has no access to filesystem or process capabilities.
func (s Store) Render(name string, data any) string {
	text, custom, err := s.loadWithSource(name)
	if err != nil {
		panic(fmt.Sprintf("prompts: missing %q: %v", name, err))
	}
	rendered, err := renderTemplate(name, text, data)
	if err != nil && custom {
		fallback, fallbackErr := defaultText(name)
		if fallbackErr != nil {
			panic(fmt.Sprintf("prompts: render %q: %v", name, fallbackErr))
		}
		rendered, err = renderTemplate(name, fallback, data)
	}
	if err != nil {
		panic(fmt.Sprintf("prompts: render %q: %v", name, err))
	}
	return rendered
}

// ToolSpec loads one built-in tool's editable JSON schema. Invalid custom
// schemas fall back to the embedded schema so the handler contract remains
// available even when a user is experimenting with the file.
func (s Store) ToolSpec(name string) opencode.ToolSpec {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\\`) {
		panic("prompts: invalid tool name")
	}
	path := "tools/" + name + ".json"
	text, custom, err := s.loadWithSource(path)
	if err != nil {
		panic(fmt.Sprintf("prompts: missing tool spec %q: %v", name, err))
	}
	spec, err := decodeToolSpec(text, name)
	if err != nil && custom {
		text, err = defaultText(path)
		if err == nil {
			spec, err = decodeToolSpec(text, name)
		}
	}
	if err != nil {
		panic(fmt.Sprintf("prompts: invalid tool spec %q: %v", name, err))
	}
	return spec
}

func (s Store) load(name string) (string, error) {
	text, _, err := s.loadWithSource(name)
	return text, err
}

func (s Store) loadWithSource(name string) (string, bool, error) {
	if err := validateName(name); err != nil {
		return "", false, err
	}
	if s.dir != "" {
		path := filepath.Join(s.dir, filepath.FromSlash(name))
		if raw, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(raw)) != "" {
			return string(raw), true, nil
		}
	}
	text, err := defaultText(name)
	return text, false, err
}

func defaultText(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	raw, err := fs.ReadFile(defaultFiles, filepath.ToSlash(filepath.Join(defaultsRoot, name)))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", fmt.Errorf("default is empty")
	}
	return string(raw), nil
}

func renderTemplate(name, text string, data any) (string, error) {
	tmpl, err := template.New(filepath.Base(name)).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, data); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func decodeToolSpec(text, wantName string) (opencode.ToolSpec, error) {
	var spec opencode.ToolSpec
	if err := json.Unmarshal([]byte(text), &spec); err != nil {
		return opencode.ToolSpec{}, err
	}
	if normalized, ok := normalizeJSONValue(spec.Parameters).(map[string]any); ok {
		spec.Parameters = normalized
	}
	if strings.TrimSpace(spec.Name) != wantName {
		return opencode.ToolSpec{}, fmt.Errorf("name is %q, want %q", spec.Name, wantName)
	}
	if strings.TrimSpace(spec.Description) == "" {
		return opencode.ToolSpec{}, fmt.Errorf("description is empty")
	}
	if spec.Parameters == nil {
		return opencode.ToolSpec{}, fmt.Errorf("parameters are required")
	}
	if kind, ok := spec.Parameters["type"].(string); !ok || kind != "object" {
		return opencode.ToolSpec{}, fmt.Errorf("parameters.type must be object")
	}
	return spec, nil
}

func normalizeJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = normalizeJSONValue(child)
		}
		return value
	case []any:
		out := make([]any, len(value))
		allStrings := true
		for index, child := range value {
			out[index] = normalizeJSONValue(child)
			if _, ok := out[index].(string); !ok {
				allStrings = false
			}
		}
		if allStrings {
			stringsOut := make([]string, len(out))
			for index, child := range out {
				stringsOut[index] = child.(string)
			}
			return stringsOut
		}
		return out
	default:
		return value
	}
}

func validateName(name string) error {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) {
		return fmt.Errorf("invalid relative name %q", name)
	}
	name = filepath.ToSlash(filepath.Clean(name))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return fmt.Errorf("invalid relative name %q", name)
	}
	return nil
}
