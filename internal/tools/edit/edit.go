package edit

import (
	"fmt"
	"os"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/workspace"
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
			"diff":          UnifiedDiff(content, newContent),
			"bytes_changed": len(newContent) - len(content),
		},
	}, nil
}

// UnifiedDiff returns a unified diff of oldContent vs newContent with correct
// 1-based file line numbers in the @@ hunk headers.
func UnifiedDiff(oldContent, newContent string) string {
	return diffLines(splitContentLines(oldContent), splitContentLines(newContent))
}

func splitContentLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// resolve returns the absolute cleaned path of filePath inside rootDir, rejecting escapes.
func resolve(filePath, rootDir string) (string, error) {
	abs, err := workspace.Resolve(filePath, rootDir)
	if err != nil {
		return "", fmt.Errorf("edit: %s: %w", filePath, err)
	}
	return abs, nil
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
			// Unchanged lines outside a hunk still advance file positions so
			// later @@ headers use real 1-based line numbers, not "1".
			aPos++
			bPos++
			i++
			continue
		}
		// Change at i. Pull a few keep-ops of context before it; those keep
		// ops were already counted while skipping, so rewind positions first.
		start := i
		for start > 0 && ops[start-1].kind == 'k' && i-start < diffContext {
			start--
		}
		if rewind := i - start; rewind > 0 {
			aPos -= rewind
			bPos -= rewind
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
