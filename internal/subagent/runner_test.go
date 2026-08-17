package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestAgentRunnerCreatesChildSession(t *testing.T) {
	st := openStore(t)
	var mu sync.Mutex
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		n++
		mu.Unlock()
		body := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"content":      "explored the tree",
					"finish_reason": "stop",
				},
			}},
		}
		raw, _ := json.Marshal(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)

	client := opencode.NewClient("test", opencode.WithBaseURL(srv.URL))
	runner := AgentRunner{Store: st, Client: client}
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{
		Title:     "parent",
		Directory: workdir,
	})
	if err != nil {
		t.Fatalf("CreateSession parent: %v", err)
	}

	res, err := runner.Run(context.Background(), Job{
		ID:              "sub_1",
		Name:            "explore-1",
		Role:            RoleExplore,
		Prompt:          "look around",
		ParentSessionID: parent.ID,
		Workdir:         workdir,
		MaxSteps:        4,
		Tools:           []string{"read", "webfetch"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != string(StatusCompleted) {
		t.Fatalf("status = %q err=%q", res.Status, res.Err)
	}
	if !strings.Contains(res.Summary, "explored") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if res.ChildSessionID == "" {
		t.Fatal("missing child session id")
	}
	child, err := st.GetSession(context.Background(), res.ChildSessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if child.Kind != db.SessionKindSubagent {
		t.Fatalf("kind = %q", child.Kind)
	}
	if child.ParentSessionID == nil || *child.ParentSessionID != parent.ID {
		t.Fatalf("parent link = %v", child.ParentSessionID)
	}

	// Resume list hides child sessions.
	list, err := st.ListSessionsByDir(context.Background(), workdir)
	if err != nil {
		t.Fatalf("ListSessionsByDir: %v", err)
	}
	for _, s := range list {
		if s.ID == child.ID {
			t.Fatalf("child session visible in resume list")
		}
	}
	if len(list) != 1 || list[0].ID != parent.ID {
		t.Fatalf("list = %+v, want only parent", list)
	}
	_ = filepath.Join // keep path import if needed
}

func TestAgentRunnerStepLimitIsPartialSuccess(t *testing.T) {
	// Children used to surface "step limit reached" as status=failed, which the
	// parent UI and model treated as a crash even when tools had already run.
	st := openStore(t)
	var mu sync.Mutex
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		// Keep requesting tools until the child budget is exhausted.
		body := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"content": "partial findings so far",
					"tool_calls": []any{map[string]any{
						"id":   fmt.Sprintf("call_%d", call),
						"type": "function",
						"function": map[string]any{
							"name":      "bash",
							"arguments": `{"command":"echo loop"}`,
						},
					}},
				},
				"finish_reason": "tool-calls",
			}},
		}
		raw, _ := json.Marshal(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)

	client := opencode.NewClient("test", opencode.WithBaseURL(srv.URL))
	runner := AgentRunner{Store: st, Client: client}
	workdir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{
		Title:     "parent",
		Directory: workdir,
	})
	if err != nil {
		t.Fatalf("CreateSession parent: %v", err)
	}

	res, err := runner.Run(context.Background(), Job{
		ID:              "sub_lim",
		Name:            "explore-lim",
		Role:            RoleExplore,
		Prompt:          "dig forever",
		ParentSessionID: parent.ID,
		Workdir:         workdir,
		MaxSteps:        3,
		Tools:           []string{"bash", "read"},
	})
	if err != nil {
		t.Fatalf("Run: %v (want soft success, not hard error)", err)
	}
	if res.Status != string(StatusCompleted) {
		t.Fatalf("status = %q err=%q, want completed partial success", res.Status, res.Err)
	}
	if res.Err != "" {
		t.Fatalf("err field should be empty on soft step-limit complete, got %q", res.Err)
	}
	if !strings.Contains(res.Summary, "step limit") {
		t.Fatalf("summary missing step-limit note: %q", res.Summary)
	}
	if !strings.Contains(res.Summary, "partial findings") {
		t.Fatalf("summary missing child text: %q", res.Summary)
	}
	mu.Lock()
	calls := n
	mu.Unlock()
	if calls != 3 {
		t.Fatalf("provider calls = %d, want 3", calls)
	}
}

func openStore(t *testing.T) *db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}
