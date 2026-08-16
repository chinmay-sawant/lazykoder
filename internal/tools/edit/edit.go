package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxDiffChars = 4000
	diffContext  = 3
	maxLCSCells  = 4_000_000
	maxTrunc     = 60
	// fileMode is the mode of files written by the edit tool.
	fileMode = 0o600
)

// Result holds the edit outcome and metadata.
type Result struct {
	Output   string
	Metadata map[string]any
}

// Run replaces the unique occurrence of oldString with newString in filePath inside rootDir.
func Run(filePath, oldString, newString, rootDir string) (Result, error) {
	abs, err := resolve(filePath, rootDir)
	if err != nil {
		return Result{}, err
	}
	oldData, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, fmt.Errorf("edit: %s: %w", abs, err)
	}
	content := string(oldData)
	if oldString == "" {
		return Result{}, fmt.Errorf("edit: %s: oldString is empty", abs)
	}
	count := strings.Count(content, oldString)
	switch {
	case count == 0:
		return Result{}, fmt.Errorf("edit: %s: oldString not found", abs)
	case count > 1:
		return Result{}, fmt.Errorf("edit: oldString is not unique in %s (%d occurrences)", abs, count)
	}
	idx := strings.Index(content, oldString)
	newContent := content[:idx] + newString + content[idx+len(oldString):]
	if err := os.WriteFile(abs, []byte(newContent), fileMode); err != nil {
		return Result{}, fmt.Errorf("edit: %s: %w", abs, err)
	}
	return Result{
		Output: fmt.Sprintf("edited %s: %s -> %s", abs, truncate(oldString), truncate(newString)),
		Metadata: map[string]any{
			"diff":          diffLines(strings.Split(content, "\n"), strings.Split(newContent, "\n")),
			"bytes_changed": len(newContent) - len(content),
		},
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
		return "", fmt.Errorf("edit: %s: %w", filePath, err)
	}
	abs = filepath.Clean(abs)
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("edit: %s: %w", filePath, err)
	}
	root = filepath.Clean(root)
	if !inside(abs, root) {
		return "", fmt.Errorf("edit: %s: path escapes session directory", abs)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("edit: %s: %w", abs, err)
	}
	resolved, err := evalResolved(abs)
	if err != nil {
		return "", fmt.Errorf("edit: %s: %w", abs, err)
	}
	if !inside(resolved, rootReal) {
		return "", fmt.Errorf("edit: %s: path escapes session directory via symlink", abs)
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

func truncate(s string) string {
	r := []rune(s)
	if len(r) <= maxTrunc {
		return s
	}
	return string(r[:maxTrunc]) + "..."
}

type diffOp struct {
	kind byte
	text string
}

func diffLines(a, b []string) string {
	n, m := len(a), len(b)
	var ops []diffOp
	if n*m <= maxLCSCells {
		ops = lcsOps(a, b)
	} else {
		for _, line := range a {
			ops = append(ops, diffOp{kind: 'd', text: line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{kind: 'a', text: line})
		}
	}
	return formatDiff(ops)
}

func lcsOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{kind: 'k', text: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{kind: 'd', text: a[i]})
			i++
		default:
			ops = append(ops, diffOp{kind: 'a', text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{kind: 'd', text: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{kind: 'a', text: b[j]})
	}
	return ops
}

func formatDiff(ops []diffOp) string {
	var out strings.Builder
	var hunk []diffOp
	var aPos, bPos int
	var hunkStartA, hunkStartB int
	flush := func() {
		if len(hunk) == 0 {
			return
		}
		aCount, bCount := 0, 0
		for _, op := range hunk {
			if op.kind != 'a' {
				aCount++
			}
			if op.kind != 'd' {
				bCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", hunkStartA, aCount, hunkStartB, bCount)
		for _, op := range hunk {
			switch op.kind {
			case 'k':
				out.WriteByte(' ')
			case 'd':
				out.WriteByte('-')
			case 'a':
				out.WriteByte('+')
			}
			out.WriteString(op.text)
			out.WriteByte('\n')
		}
		hunk = hunk[:0]
	}
	appendOp := func(op diffOp) {
		if len(hunk) == 0 {
			hunkStartA, hunkStartB = aPos+1, bPos+1
		}
		hunk = append(hunk, op)
		switch op.kind {
		case 'k':
			aPos++
			bPos++
		case 'd':
			aPos++
		case 'a':
			bPos++
		}
	}
	i := 0
	for i < len(ops) {
		if ops[i].kind == 'k' {
			i++
			continue
		}
		start := i
		for start > 0 && ops[start-1].kind == 'k' && i-start < diffContext {
			start--
		}
		end := i
		for end < len(ops) {
			if ops[end].kind != 'k' {
				end++
				continue
			}
			k := end
			for k < len(ops) && ops[k].kind == 'k' {
				k++
			}
			if k < len(ops) && k-end <= diffContext {
				end = k
				continue
			}
			if k-end > diffContext {
				end += diffContext
			} else {
				end = k
			}
			break
		}
		for _, op := range ops[start:end] {
			appendOp(op)
		}
		flush()
		i = end
	}
	flush()
	if out.Len() > maxDiffChars {
		s := out.String()
		note := "\n... diff truncated"
		out.Reset()
		out.WriteString(s[:maxDiffChars-len(note)])
		out.WriteString(note)
	}
	return out.String()
}
