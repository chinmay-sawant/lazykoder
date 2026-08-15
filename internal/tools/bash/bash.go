package bash

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/policy"
)

// ErrDenied is returned when the policy gate refuses a command.
var ErrDenied = errors.New("bash: command denied by policy")

// Result holds the outcome of a command run.
type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	StartTime int64
	EndTime   int64
}

// Runner executes a command; Exec is the production implementation.
type Runner interface {
	Run(ctx context.Context, command, workdir string) (Result, error)
}

// Run is the single gate: it refuses denied or unconfirmed commands without touching the runner.
func Run(ctx context.Context, command, workdir string, dec policy.Decision, confirmed bool, runner Runner) (Result, error) {
	if dec.Action == policy.ActionDeny {
		return Result{}, ErrDenied
	}
	if dec.Action == policy.ActionAsk && !confirmed {
		return Result{}, ErrDenied
	}
	return runner.Run(ctx, command, workdir)
}

// Exec is the real runner (satisfies Runner).
type Exec struct{}

// Run executes the command, capturing stdout, stderr, exit code, and timing.
func (Exec) Run(ctx context.Context, command, workdir string) (Result, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if !needsShell(command) {
		if argv, ok := tokenize(command); ok {
			//nolint:gosec // The bash tool exists to run model-supplied commands; policy gating happens in Run before this code.
			cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
		}
	}
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		StartTime: start.UnixMilli(),
		EndTime:   time.Now().UnixMilli(),
	}
	if err == nil {
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
	}
	if isCtxErr(err) {
		return res, err
	}
	if exitErr != nil {
		// A non-zero exit code is a normal tool outcome (the caller reads
		// Result.ExitCode), not a Go error.
		return res, nil //nolint:nilerr // Non-zero exit is data, carried in Result.ExitCode.
	}
	res.ExitCode = 127
	return res, err
}

func needsShell(command string) bool {
	if command != strings.TrimSpace(command) {
		return true
	}
	for i := 0; i < len(command); i++ {
		switch command[i] {
		case '|', '&', ';', '<', '>', '$', '`', '(', ')', '*', '?', '[', ']', '~', '\'', '"', '\n':
			return true
		}
	}
	return false
}

func tokenize(command string) ([]string, bool) {
	var argv []string
	var cur strings.Builder
	var quote byte
	inToken := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inToken = true
		case c == ' ' || c == '\t' || c == '\r':
			if inToken {
				argv = append(argv, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inToken {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, false
	}
	return argv, true
}

func isCtxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
