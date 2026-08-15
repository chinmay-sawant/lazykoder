package read

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxReadBytes = 1 << 20

// Result holds the file contents and metadata for a read.
type Result struct {
	Output   string
	Metadata map[string]any
}

// Run reads filePath from within rootDir, capping the output at 1MiB.
func Run(filePath, rootDir string) (Result, error) {
	abs, err := resolve(filePath, rootDir)
	if err != nil {
		return Result{}, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return Result{}, fmt.Errorf("read: %s: %w", abs, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Result{}, fmt.Errorf("read: %s: %w", abs, err)
	}
	var data []byte
	truncated := false
	if st.Size() > maxReadBytes {
		truncated = true
		data = make([]byte, maxReadBytes)
		if _, err := io.ReadFull(f, data); err != nil {
			return Result{}, fmt.Errorf("read: %s: %w", abs, err)
		}
	} else {
		if data, err = io.ReadAll(f); err != nil {
			return Result{}, fmt.Errorf("read: %s: %w", abs, err)
		}
	}
	content := string(data)
	meta := map[string]any{"lines": countLines(content)}
	if truncated {
		meta["truncated"] = true
		content += "\n... truncated at 1MiB"
	}
	return Result{Output: content, Metadata: meta}, nil
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
		return "", fmt.Errorf("read: %s: %w", filePath, err)
	}
	abs = filepath.Clean(abs)
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("read: %s: %w", filePath, err)
	}
	root = filepath.Clean(root)
	if !inside(abs, root) {
		return "", fmt.Errorf("read: %s: path escapes session directory", abs)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("read: %s: %w", abs, err)
	}
	resolved, err := evalResolved(abs)
	if err != nil {
		return "", fmt.Errorf("read: %s: %w", abs, err)
	}
	if !inside(resolved, rootReal) {
		return "", fmt.Errorf("read: %s: path escapes session directory via symlink", abs)
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

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
