package write

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Result holds the write outcome and metadata.
type Result struct {
	Output   string
	Metadata map[string]any
}

// Run writes contents to filePath inside rootDir, overwriting any existing file.
func Run(filePath, contents, rootDir string) (Result, error) {
	abs, err := resolve(filePath, rootDir)
	if err != nil {
		return Result{}, err
	}
	parent := filepath.Dir(abs)
	st, err := os.Stat(parent)
	if err != nil {
		return Result{}, fmt.Errorf("write: %s: parent directory does not exist: %w", abs, err)
	}
	if !st.IsDir() {
		return Result{}, fmt.Errorf("write: %s: parent is not a directory", abs)
	}
	if err := os.WriteFile(abs, []byte(contents), 0o644); err != nil {
		return Result{}, fmt.Errorf("write: %s: %w", abs, err)
	}
	n := len(contents)
	return Result{
		Output:   fmt.Sprintf("wrote %d bytes to %s", n, abs),
		Metadata: map[string]any{"bytes": n, "path": abs},
	}, nil
}

// resolve returns the absolute cleaned path of filePath inside rootDir, rejecting escapes.
func resolve(filePath, rootDir string) (string, error) {
	var p string
	if filepath.IsAbs(filePath) {
		p = filepath.Clean(filePath)
	} else {
		p = filepath.Join(rootDir, filepath.Clean(filePath))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("write: %s: %w", filePath, err)
	}
	abs = filepath.Clean(abs)
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("write: %s: %w", filePath, err)
	}
	root = filepath.Clean(root)
	if !inside(abs, root) {
		return "", fmt.Errorf("write: %s: path escapes session directory", abs)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("write: %s: %w", abs, err)
	}
	resolved, err := evalResolved(abs)
	if err != nil {
		return "", fmt.Errorf("write: %s: %w", abs, err)
	}
	if !inside(resolved, rootReal) {
		return "", fmt.Errorf("write: %s: path escapes session directory via symlink", abs)
	}
	return abs, nil
}

func inside(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func evalResolved(p string) (string, error) {
	cur := p
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return resolved, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		cur = parent
	}
}
