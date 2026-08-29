// Package catalog contains the bounded file-discovery primitives shared by
// provider, tool, and role catalogs.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultMaxDepth          = 4
	DefaultMaxDescriptors    = 256
	DefaultMaxDescriptorSize = 256 * 1024
	DefaultMaxBody           = 48 * 1024
	GlobalConfigDirEnv       = "LAZYKODER_GLOBAL_CONFIG_DIR"
	globalPriorityStart      = 10
)

// Scope identifies the source of a descriptor.
type Scope string

const (
	ScopeLocal  Scope = "local"
	ScopeGlobal Scope = "global"
)

// Root is an approved catalog directory.
type Root struct {
	Scope    Scope
	Label    string
	Path     string
	Priority int
}

// Diagnostic records a source that could not be used. Diagnostics are
// returned with partial catalogs so one bad source does not hide good entries.
type Diagnostic struct {
	Scope Scope
	Path  string
	Error string
}

// ResolveRoots returns approved local and global config directories. Missing
// directories are ignored. Existing symlinks and non-directories are rejected.
func ResolveRoots(workdir string, includeLocal, includeGlobal bool, explicitGlobal []string) ([]Root, []Diagnostic) {
	var roots []Root
	var diagnostics []Diagnostic
	seen := make(map[string]struct{})
	add := func(scope Scope, label, path string, priority int) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: path, Error: err.Error()})
			return
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		info, err := os.Lstat(abs)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: abs, Error: err.Error()})
			return
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: abs, Error: "root is not a real directory"})
			return
		}
		roots = append(roots, Root{Scope: scope, Label: label, Path: abs, Priority: priority})
	}

	if includeLocal {
		add(ScopeLocal, "project .lazykoder", filepath.Join(workdir, ".lazykoder"), 0)
	}
	if includeGlobal {
		paths := append([]string{}, explicitGlobal...)
		if len(paths) == 0 {
			if configured := strings.TrimSpace(os.Getenv(GlobalConfigDirEnv)); configured != "" {
				paths = []string{configured}
			} else if configDir, err := os.UserConfigDir(); err == nil {
				paths = []string{filepath.Join(configDir, "lazykoder")}
			}
		}
		for index, path := range paths {
			add(ScopeGlobal, "global config", path, globalPriorityStart+index)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].Priority != roots[j].Priority {
			return roots[i].Priority < roots[j].Priority
		}
		return roots[i].Path < roots[j].Path
	})
	return roots, diagnostics
}

// ReadBoundedFile verifies and reads one regular, non-symlink file.
func ReadBoundedFile(path string, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("catalog: invalid max size %d", maxBytes)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("catalog: descriptor is not a regular file")
	}
	if info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("catalog: descriptor exceeds %d bytes", maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("catalog: descriptor exceeds %d bytes", maxBytes)
	}
	return data, nil
}
