package task

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSpecsHasFiveTools(t *testing.T) {
	specs := Specs()
	if len(specs) != 5 {
		t.Fatalf("Specs() len = %d, want 5", len(specs))
	}
	want := []string{ToolTask, ToolTaskList, ToolTaskStatus, ToolTaskWait, ToolTaskCancel}
	got := make(map[string]bool, len(specs))
	for _, s := range specs {
		if s.Name == "" {
			t.Error("Spec has empty Name")
		}
		if s.Description == "" {
			t.Errorf("Spec %q has empty Description", s.Name)
		}
		if s.Parameters == nil {
			t.Errorf("Spec %q has nil Parameters", s.Name)
		}
		got[s.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("Specs() missing tool %q", name)
		}
	}
}

func TestSpecsTaskRequiresPrompt(t *testing.T) {
	for _, s := range Specs() {
		if s.Name != ToolTask {
			continue
		}
		req, ok := s.Parameters["required"].([]string)
		if !ok {
			t.Fatal("task Parameters.required is not []string")
		}
		found := false
		for _, r := range req {
			if r == "prompt" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("task required = %v, want prompt", req)
		}
		return
	}
	t.Fatal("task tool not found in Specs()")
}

func TestIsTaskTool(t *testing.T) {
	for _, name := range []string{ToolTask, ToolTaskList, ToolTaskStatus, ToolTaskWait, ToolTaskCancel} {
		if !IsTaskTool(name) {
			t.Errorf("IsTaskTool(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "bash", "read", "task_spawn", "Task"} {
		if IsTaskTool(name) {
			t.Errorf("IsTaskTool(%q) = true, want false", name)
		}
	}
}

func TestNormalizeRole(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", RoleExplore},
		{"  ", RoleExplore},
		{"explore", RoleExplore},
		{"Explore", RoleExplore},
		{" PLAN ", RolePlan},
		{"general", RoleGeneral},
		{"GENERAL", RoleGeneral},
		{"writer", ""},
		{"unknown", ""},
	}
	for _, tt := range tests {
		if got := NormalizeRole(tt.in); got != tt.want {
			t.Errorf("NormalizeRole(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseTaskArgs(t *testing.T) {
	raw := []byte(`{
		"name": "scan",
		"prompt": "list go files",
		"description": "explore repo",
		"role": "Explore",
		"model": "m1",
		"variant": "high",
		"max_steps": 8,
		"background": true,
		"timeout_sec": 60
	}`)
	a, err := ParseTaskArgs(raw)
	if err != nil {
		t.Fatalf("ParseTaskArgs() error = %v", err)
	}
	if a.Name != "scan" || a.Prompt != "list go files" || a.Description != "explore repo" {
		t.Errorf("ParseTaskArgs() = %+v, unexpected name/prompt/description", a)
	}
	if a.Role != RoleExplore {
		t.Errorf("Role = %q, want %q", a.Role, RoleExplore)
	}
	if a.Model != "m1" || a.Variant != "high" || a.MaxSteps != 8 || !a.Background || a.TimeoutSec != 60 {
		t.Errorf("ParseTaskArgs() = %+v, unexpected optional fields", a)
	}
}

func TestParseTaskArgsRequiresPrompt(t *testing.T) {
	_, err := ParseTaskArgs([]byte(`{"name":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("ParseTaskArgs() error = %v, want prompt required", err)
	}
	_, err = ParseTaskArgs([]byte(`{"prompt":"  "}`))
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("ParseTaskArgs() whitespace prompt error = %v, want prompt required", err)
	}
}

func TestParseTaskArgsDefaultRole(t *testing.T) {
	a, err := ParseTaskArgs([]byte(`{"prompt":"do work"}`))
	if err != nil {
		t.Fatalf("ParseTaskArgs() error = %v", err)
	}
	if a.Role != RoleExplore {
		t.Errorf("Role = %q, want %q", a.Role, RoleExplore)
	}
}

func TestParseTaskArgsInvalidRole(t *testing.T) {
	_, err := ParseTaskArgs([]byte(`{"prompt":"x","role":"writer"}`))
	if err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("ParseTaskArgs() error = %v, want invalid role", err)
	}
}

func TestParseTaskArgsInvalidJSON(t *testing.T) {
	_, err := ParseTaskArgs([]byte(`{`))
	if err == nil {
		t.Fatal("ParseTaskArgs() error = nil, want invalid json")
	}
}

func TestParseListArgs(t *testing.T) {
	a, err := ParseListArgs([]byte(`{"status":" running "}`))
	if err != nil {
		t.Fatalf("ParseListArgs() error = %v", err)
	}
	if a.Status != "running" {
		t.Errorf("Status = %q, want running", a.Status)
	}
	a, err = ParseListArgs(nil)
	if err != nil {
		t.Fatalf("ParseListArgs(nil) error = %v", err)
	}
	if a.Status != "" {
		t.Errorf("Status = %q, want empty", a.Status)
	}
}

func TestParseStatusArgs(t *testing.T) {
	a, err := ParseStatusArgs([]byte(`{"id":" abc "}`))
	if err != nil {
		t.Fatalf("ParseStatusArgs() error = %v", err)
	}
	if a.ID != "abc" {
		t.Errorf("ID = %q, want abc", a.ID)
	}
	_, err = ParseStatusArgs([]byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("ParseStatusArgs() error = %v, want id required", err)
	}
}

func TestParseWaitArgs(t *testing.T) {
	a, err := ParseWaitArgs([]byte(`{"id":"t1","timeout_sec":30}`))
	if err != nil {
		t.Fatalf("ParseWaitArgs() error = %v", err)
	}
	if a.ID != "t1" || a.TimeoutSec != 30 {
		t.Errorf("ParseWaitArgs() = %+v", a)
	}
	a, err = ParseWaitArgs([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseWaitArgs({}) error = %v", err)
	}
	if a.ID != "" {
		t.Errorf("ID = %q, want empty (wait all)", a.ID)
	}
}

func TestParseCancelArgs(t *testing.T) {
	a, err := ParseCancelArgs([]byte(`{"id":"t1"}`))
	if err != nil {
		t.Fatalf("ParseCancelArgs() error = %v", err)
	}
	if a.ID != "t1" || a.CancelAll {
		t.Errorf("ParseCancelArgs() = %+v", a)
	}
	a, err = ParseCancelArgs([]byte(`{"cancel_all":true}`))
	if err != nil {
		t.Fatalf("ParseCancelArgs(cancel_all) error = %v", err)
	}
	if !a.CancelAll {
		t.Error("CancelAll = false, want true")
	}
	_, err = ParseCancelArgs([]byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("ParseCancelArgs() error = %v, want id or cancel_all", err)
	}
}

func TestEncodeResults(t *testing.T) {
	spawn, err := EncodeSpawnResult(SpawnResult{ID: "1", Status: "running", Background: true})
	if err != nil {
		t.Fatalf("EncodeSpawnResult() error = %v", err)
	}
	var sr SpawnResult
	if err := json.Unmarshal([]byte(spawn), &sr); err != nil {
		t.Fatalf("spawn json: %v", err)
	}
	if sr.ID != "1" || sr.Status != "running" || !sr.Background {
		t.Errorf("spawn = %+v", sr)
	}

	list, err := EncodeListResult(ListResult{Tasks: []TaskInfo{{ID: "1", Status: "completed"}}})
	if err != nil {
		t.Fatalf("EncodeListResult() error = %v", err)
	}
	if !strings.Contains(list, `"id":"1"`) {
		t.Errorf("list = %s", list)
	}

	status, err := EncodeStatusResult(StatusResult{Task: TaskInfo{ID: "2", Status: "failed", Error: "boom"}})
	if err != nil {
		t.Fatalf("EncodeStatusResult() error = %v", err)
	}
	if !strings.Contains(status, "boom") {
		t.Errorf("status = %s", status)
	}

	wait, err := EncodeWaitResult(WaitResult{Tasks: []TaskInfo{{ID: "3", Status: "completed", Summary: "done"}}})
	if err != nil {
		t.Fatalf("EncodeWaitResult() error = %v", err)
	}
	if !strings.Contains(wait, "done") {
		t.Errorf("wait = %s", wait)
	}

	cancel, err := EncodeCancelResult(CancelResult{ID: "4", Cancelled: []string{"4"}})
	if err != nil {
		t.Fatalf("EncodeCancelResult() error = %v", err)
	}
	if !strings.Contains(cancel, `"id":"4"`) {
		t.Errorf("cancel = %s", cancel)
	}
}


