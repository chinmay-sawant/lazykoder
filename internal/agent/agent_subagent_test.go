package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestNewCopiesCapabilityPolicySlices(t *testing.T) {
	tools := []string{"read", "bash"}
	allowlist := []string{"git", "go"}
	a := New(nil, nil, t.TempDir(), Options{ToolNames: tools, BashAllowlist: allowlist})
	tools[0] = "write"
	allowlist[0] = "rm"
	if got := a.opts.ToolNames[0]; got != "read" {
		t.Fatalf("ToolNames changed through caller slice: %q", got)
	}
	if got := a.opts.BashAllowlist[0]; got != "git" {
		t.Fatalf("BashAllowlist changed through caller slice: %q", got)
	}
	withoutPolicy := New(nil, nil, t.TempDir(), Options{})
	if withoutPolicy.opts.ToolNames != nil || withoutPolicy.opts.BashAllowlist != nil {
		t.Fatal("nil policy slices should stay nil")
	}
}

// TestAdvertiseBaseTools ensures the provider sees more than bash.
func TestAdvertiseBaseTools(t *testing.T) {
	specs := toolSpecsFor(nil, nil)
	tools := make([]string, 0, len(specs))
	for _, s := range specs {
		tools = append(tools, s.Name)
	}
	for _, want := range []string{"bash", "read", "grep", "write", "edit", "webfetch", "question"} {
		found := false
		for _, n := range tools {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tools %v missing %q", tools, want)
		}
	}
}

// fakeHost implements SubagentHost for unit tests.
type fakeHost struct {
	calls int
}

func (f *fakeHost) Specs() []opencode.ToolSpec {
	return []opencode.ToolSpec{{
		Name:        "task",
		Description: "test task",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func (f *fakeHost) Execute(ctx context.Context, parentSessionID, name, argsJSON, partID string) (string, string, string, error) {
	f.calls++
	out, _ := json.Marshal(map[string]any{
		"id": "sub_test", "status": "completed", "summary": "child done",
	})
	return string(out), string(out), "completed", nil
}

func TestAdvertiseTaskToolsWithHost(t *testing.T) {
	h := &fakeHost{}
	specs := toolSpecsFor(nil, h)
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	for _, want := range []string{"task", "bash", "read"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

func TestParentTaskToolViaHost(t *testing.T) {
	taskArgs, _ := json.Marshal(map[string]any{"prompt": "explore foo", "name": "exp"})
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{
			{ID: "c_task", Name: "task", Args: string(taskArgs)},
		}, testUsage),
		respBody("parent done", "", "stop", nil, nil),
	)
	st, path := newTestEnv(t)
	h := &fakeHost{}
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Host:             h,
		DisableStreaming: true,
	})
	if _, err := sendAndCollect(t, a, "spawn a child"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if h.calls != 1 {
		t.Fatalf("host calls = %d, want 1", h.calls)
	}
	rows := queryToolCalls(t, path)
	var taskRow *rowTC
	for i := range rows {
		if rows[i].Tool == "task" {
			taskRow = &rows[i]
			break
		}
	}
	if taskRow == nil {
		t.Fatalf("no task tool_call in %+v", rows)
	}
	if taskRow.Status != "completed" {
		t.Fatalf("task status = %q", taskRow.Status)
	}
	if taskRow.Output == nil || !strings.Contains(*taskRow.Output, "child done") {
		t.Fatalf("task output = %v", taskRow.Output)
	}
}
