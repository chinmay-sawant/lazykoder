// Package grep searches file contents under a workdir using ripgrep when
// available, with a pure-Go fallback.
package grep

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chinmay-sawant/lazykoder/internal/workspace"
)

const (
	// DefaultMaxMatches caps returned hits when the caller does not set a limit.
	DefaultMaxMatches = 50
	// MaxMaxMatches hard-caps matches returned to the model.
	MaxMaxMatches = 200
	// maxOutputBytes caps the tool payload (roughly).
	maxOutputBytes = 256 << 10 // 256 KiB
	// maxFileBytes skips files larger than this in the Go fallback.
	maxFileBytes = 1 << 20 // 1 MiB
	// searchTimeout bounds a single search invocation.
	searchTimeout = 30 * time.Second
)

// Result is the search output returned to the agent.
type Result struct {
	Output   string
	Metadata map[string]any
}

// Options configure one search.
type Options struct {
	// Pattern is the search pattern (required). Treated as a regex by rg / regexp.
	Pattern string
	// Path is a file or directory under rootDir. Empty means rootDir.
	Path string
	// Glob limits files (rg --glob style), e.g. "*.go". Empty = all text files.
	Glob string
	// CaseInsensitive maps to rg -i.
	CaseInsensitive bool
	// MaxMatches caps hits (default DefaultMaxMatches, max MaxMaxMatches).
	MaxMatches int
}

// Runner finds and runs ripgrep. Tests may override LookPath / CommandContext.
type Runner struct {
	// LookPath defaults to exec.LookPath.
	LookPath func(file string) (string, error)
	// CommandContext defaults to exec.CommandContext.
	CommandContext func(ctx context.Context, name string, arg ...string) *exec.Cmd
}

// Run searches under rootDir. Prefers `rg`; falls back to a pure-Go walk if rg
// is not on PATH.
func Run(ctx context.Context, rootDir string, opts Options, r *Runner) (Result, error) {
	if r == nil {
		r = &Runner{}
	}
	if r.LookPath == nil {
		r.LookPath = exec.LookPath
	}
	if r.CommandContext == nil {
		r.CommandContext = exec.CommandContext
	}
	pattern := strings.TrimSpace(opts.Pattern)
	if pattern == "" {
		return Result{}, fmt.Errorf("grep: pattern is required")
	}
	max := opts.MaxMatches
	if max <= 0 {
		max = DefaultMaxMatches
	}
	if max > MaxMaxMatches {
		max = MaxMaxMatches
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return Result{}, fmt.Errorf("grep: root: %w", err)
	}
	root = filepath.Clean(root)
	searchPath, err := resolvePath(opts.Path, root)
	if err != nil {
		return Result{}, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	meta := map[string]any{
		"pattern":          pattern,
		"path":             relOrSelf(searchPath, root),
		"max_matches":      max,
		"engine":           "rg",
		"case_insensitive": opts.CaseInsensitive,
	}
	if opts.Glob != "" {
		meta["glob"] = opts.Glob
	}

	if rgPath, err := r.LookPath("rg"); err == nil {
		out, hits, truncated, err := runRG(ctx, r, rgPath, root, searchPath, pattern, opts.Glob, opts.CaseInsensitive, max)
		if err != nil {
			return Result{}, err
		}
		meta["matches"] = hits
		if truncated {
			meta["truncated"] = true
		}
		return Result{Output: out, Metadata: meta}, nil
	}

	meta["engine"] = "go"
	out, hits, truncated, err := runGo(ctx, root, searchPath, pattern, opts.Glob, opts.CaseInsensitive, max)
	if err != nil {
		return Result{}, err
	}
	meta["matches"] = hits
	if truncated {
		meta["truncated"] = true
	}
	return Result{Output: out, Metadata: meta}, nil
}

func runRG(ctx context.Context, r *Runner, rgPath, root, searchPath, pattern, glob string, ci bool, max int) (string, int, bool, error) {
	args := []string{
		"--line-number",
		"--no-heading",
		"--color", "never",
		"--max-count", fmt.Sprintf("%d", max), // per-file; we also cap total below
		"--max-filesize", "1M",
	}
	if ci {
		args = append(args, "-i")
	}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	// Prefer relative paths when searching the root for cleaner agent output.
	args = append(args, "--", pattern)
	if searchPath == root {
		args = append(args, ".")
	} else {
		args = append(args, searchPath)
	}
	cmd := r.CommandContext(ctx, rgPath, args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// rg: 0 = matches, 1 = no matches, 2 = error
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ee.ExitCode() == 1 {
				return "no matches", 0, false, nil
			}
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return "", 0, false, fmt.Errorf("grep: rg: %s", msg)
		}
		return "", 0, false, fmt.Errorf("grep: rg: %w", err)
	}
	return formatHits(stdout.String(), root, max)
}

func runGo(ctx context.Context, root, searchPath, pattern, glob string, ci bool, max int) (string, int, bool, error) {
	pat := pattern
	if ci {
		pat = "(?i)" + pattern
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return "", 0, false, fmt.Errorf("grep: invalid pattern: %w", err)
	}
	var globRe *regexp.Regexp
	if glob != "" {
		// Simple glob: * and ? only, matched against base name.
		globRe, err = globToRegexp(glob)
		if err != nil {
			return "", 0, false, fmt.Errorf("grep: invalid glob: %w", err)
		}
	}

	var b strings.Builder
	hits := 0
	truncated := false
	walkErr := filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".lazykoder" {
				if path != searchPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if globRe != nil && !globRe.MatchString(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes || info.Size() == 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return nil // skip binary
		}
		rel := relOrSelf(path, root)
		lines := strings.Split(string(data), "\n")
		fileHits := 0
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			hits++
			fileHits++
			fmt.Fprintf(&b, "%s:%d:%s\n", rel, i+1, line)
			if hits >= max || b.Len() >= maxOutputBytes {
				truncated = true
				return errStop
			}
			// Per-file soft cap matching rg --max-count.
			if fileHits >= max {
				break
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStop) {
		return "", 0, false, fmt.Errorf("grep: %w", walkErr)
	}
	if hits == 0 {
		return "no matches", 0, false, nil
	}
	out := strings.TrimSuffix(b.String(), "\n")
	if truncated {
		out += "\n…(truncated)"
	}
	return out, hits, truncated, nil
}

var errStop = fmt.Errorf("stop")

func formatHits(raw, root string, max int) (string, int, bool, error) {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return "no matches", 0, false, nil
	}
	lines := strings.Split(raw, "\n")
	truncated := false
	if len(lines) > max {
		lines = lines[:max]
		truncated = true
	}
	// Rewrite absolute paths under root to relative when present.
	for i, line := range lines {
		if !strings.HasPrefix(line, root) {
			continue
		}
		rest := strings.TrimPrefix(line, root)
		rest = strings.TrimPrefix(rest, string(filepath.Separator))
		lines[i] = rest
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes]
		truncated = true
	}
	if truncated {
		out += "\n…(truncated)"
	}
	return out, len(lines), truncated, nil
}

func resolvePath(path, root string) (string, error) {
	path = strings.TrimSpace(path)
	abs, err := workspace.Resolve(path, root)
	if err != nil {
		return "", fmt.Errorf("grep: %s: %w", path, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("grep: %s: %w", abs, err)
	}
	if !st.IsDir() && !st.Mode().IsRegular() {
		return "", fmt.Errorf("grep: %s: not a regular file or directory", abs)
	}
	return abs, nil
}

func relOrSelf(path, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	if rel == "." {
		return "."
	}
	return rel
}

func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
