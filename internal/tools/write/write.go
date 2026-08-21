package write

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chinmay-sawant/lazykoder/internal/workspace"
)

// fileMode is the mode of files written by the write tool.
const fileMode = 0o600

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
	if err := os.WriteFile(abs, []byte(contents), fileMode); err != nil {
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
	abs, err := workspace.Resolve(filePath, rootDir)
	if err != nil {
		return "", fmt.Errorf("write: %s: %w", filePath, err)
	}
	return abs, nil
}
