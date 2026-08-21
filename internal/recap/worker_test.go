package recap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestRelatedAvoidSearchIsScopedAndRegexQuoted(t *testing.T) {
	root := t.TempDir()
	writeRecapFile(t, root, "knowledge-base/recaps/things-to-avoid/old.md", "Never use force reset after a user has made changes.\n")
	snapshot := Snapshot{Messages: []SnapshotMessage{
		{ID: "msg_2", Text: `force reset (dangerous)`, Role: "user"},
		{ID: "msg_1", Text: "made changes", Role: "assistant"},
	}}

	result, err := RelatedAvoid(context.Background(), root, snapshot, nil)
	if err != nil {
		t.Fatalf("RelatedAvoid: %v", err)
	}
	if !strings.Contains(result, "force reset") {
		t.Fatalf("related output = %q, want matching avoid rule", result)
	}
}

func TestWorkerUsesConfiguredModelEndpointAndNoTools(t *testing.T) {
	var got struct {
		Model    string             `json:"model"`
		Endpoint string             `json:"-"`
		Tools    []json.RawMessage  `json:"tools"`
		Messages []opencode.Message `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Endpoint = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"recap_markdown\":\"A decision was made.\",\"questions\":[],\"things_to_avoid\":[]}","finish_reason":"stop"}}]}`)
	}))
	t.Cleanup(server.Close)

	client := opencode.NewClient("test-key", opencode.WithBaseURL(server.URL+"/v1"))
	worker := Worker{Client: client, Model: "deepseek-v4-flash", Endpoint: server.URL + "/v1/chat/completions"}
	snapshot := workerTestSnapshot()
	envelope, err := worker.Generate(context.Background(), snapshot, "related avoid output")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if envelope.RecapMarkdown != "A decision was made." {
		t.Fatalf("envelope = %+v", envelope)
	}
	if got.Model != "deepseek-v4-flash" || got.Endpoint != "/v1/chat/completions" {
		t.Fatalf("request route = %q model=%q", got.Endpoint, got.Model)
	}
	if got.Tools != nil {
		t.Fatalf("tools = %v, want omitted", got.Tools)
	}
	if len(got.Messages) < 2 || got.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v, want system prompt and snapshot", got.Messages)
	}
}

func TestWorkerRejectsProviderToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"grep","arguments":"{}"}}]},"finish_reason":"tool-calls"}]}`)
	}))
	t.Cleanup(server.Close)
	worker := Worker{
		Client:   opencode.NewClient("test-key", opencode.WithBaseURL(server.URL+"/v1")),
		Model:    "deepseek-v4-flash",
		Endpoint: server.URL + "/v1/chat/completions",
	}
	_, err := worker.Generate(context.Background(), workerTestSnapshot(), "")
	if err == nil || !strings.Contains(err.Error(), "tool") {
		t.Fatalf("error = %v, want tool-call rejection", err)
	}
}

func workerTestSnapshot() Snapshot {
	return Snapshot{
		SessionID:          "ses_worker",
		SourceStartSeq:     1,
		SourceEndSeq:       4,
		SourceStartTime:    1,
		SourceEndTime:      4,
		SourceEndMessageID: "msg_4",
		Messages: []SnapshotMessage{
			{ID: "msg_4", Role: "user", Text: "Choose the local path.", Seq: 4},
			{ID: "msg_3", Role: "assistant", Text: "The local path is selected.", Seq: 3},
			{ID: "msg_2", Role: "user", Text: "Keep it bounded.", Seq: 2},
			{ID: "msg_1", Role: "assistant", Text: "It stays bounded.", Seq: 1},
		},
	}
}

func writeRecapFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
