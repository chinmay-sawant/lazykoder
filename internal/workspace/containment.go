// Package workspace resolves session-relative paths without allowing escapes.
package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Resolve returns filePath as an absolute path within rootDir. It rejects
// lexical escapes and destinations that leave rootDir through an existing
// symlink, including a missing leaf under a symlinked directory.
func Resolve(filePath, rootDir string) (string, error) {
	path := filePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootDir, filepath.Clean(path))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if !inside(abs, root) {
		return "", fmt.Errorf("path escapes session directory")
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := resolveExistingParent(abs)
	if err != nil {
		return "", err
	}
	if !inside(resolved, rootReal) {
		return "", fmt.Errorf("path escapes session directory via symlink")
	}
	return abs, nil
}

func inside(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func resolveExistingParent(path string) (string, error) {
	current := path
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return resolved, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		current = parent
	}
}
