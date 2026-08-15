package bash

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/policy"
)

type fakeRunner struct {
	calls  []string
	result Result
	err    error
}

func (f *fakeRunner) Run(_ context.Context, command, _ string) (Result, error) {
	f.calls = append(f.calls, command)
	return f.result, f.err
}

func TestRunGate(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		confirmed bool
		wantErr   bool
		wantCalls int
	}{
		{"deny recursive rm no confirm", "rm -rf /tmp/lazy-x", false, true, 0},
		{"deny rm no confirm", "rm ./README.md", false, true, 0},
		{"ask rm confirmed", "rm -rf .", true, false, 1},
		{"allow no confirm", "ls", false, false, 1},
		{"empty ask no confirm", "", false, true, 0},
		{"allow confirmed passthrough", "ls", true, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{result: Result{Stdout: "out", ExitCode: 3, StartTime: 1, EndTime: 2}}
			dec := policy.Classify(tt.command)
			res, err := Run(context.Background(), tt.command, "", dec, tt.confirmed, r)
			if tt.wantErr {
				if !errors.Is(err, ErrDenied) {
					t.Fatalf("Run() error = %v, want ErrDenied", err)
				}
				if res != (Result{}) {
					t.Errorf("Run() result = %+v, want zero Result", res)
				}
			} else if err != nil {
				t.Fatalf("Run() unexpected error = %v", err)
			}
			if len(r.calls) != tt.wantCalls {
				t.Errorf("runner calls = %d, want %d", len(r.calls), tt.wantCalls)
			}
			if !tt.wantErr {
				if res.Stdout != "out" || res.ExitCode != 3 {
					t.Errorf("Run() result = %+v, want runner result passed through", res)
				}
				if r.calls[0] != tt.command {
					t.Errorf("runner command = %q, want %q", r.calls[0], tt.command)
				}
			}
		})
	}
}

func TestPolicyProof(t *testing.T) {
	if d := policy.Classify("rm x"); d.Action != policy.ActionAsk {
		t.Fatalf("Classify(\"rm x\") Action = %v, want ActionAsk", d.Action)
	}
	if d := policy.Classify("rm -rf /tmp/x"); d.Action != policy.ActionAsk || !d.Destructive {
		t.Fatalf("Classify(\"rm -rf /tmp/x\") = %+v, want ActionAsk with Destructive", d)
	}
}

func TestExecEcho(t *testing.T) {
	res, err := (Exec{}).Run(context.Background(), "echo hi", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "hi") {
		t.Errorf("Stdout = %q, want it to contain \"hi\"", res.Stdout)
	}
}

func TestExecPipelineSeparatesStreams(t *testing.T) {
	res, err := (Exec{}).Run(context.Background(), "echo out; echo err >&2", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "out") {
		t.Errorf("Stdout = %q, want it to contain \"out\"", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "err") {
		t.Errorf("Stderr = %q, want it to contain \"err\"", res.Stderr)
	}
	if strings.Contains(res.Stdout, "err") {
		t.Errorf("Stdout = %q, must not contain stderr output", res.Stdout)
	}
}

func TestExecMissingProgram(t *testing.T) {
	res, err := (Exec{}).Run(context.Background(), "definitely-not-a-real-cmd-xyz", "")
	if err == nil {
		t.Fatal("Run() error = nil, want error for missing program")
	}
	if res.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want non-zero for missing program")
	}
}

func TestExecWorkdir(t *testing.T) {
	dir := t.TempDir()
	res, err := (Exec{}).Run(context.Background(), "pwd", dir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(res.Stdout, dir) {
		t.Errorf("Stdout = %q, want it to contain %q", res.Stdout, dir)
	}
}

func TestExecTiming(t *testing.T) {
	res, err := (Exec{}).Run(context.Background(), "echo hi", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.StartTime > res.EndTime {
		t.Errorf("StartTime %d > EndTime %d", res.StartTime, res.EndTime)
	}
}
