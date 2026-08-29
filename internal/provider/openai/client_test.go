package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestAPIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := APIKeyFromEnv(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("APIKeyFromEnv() error = %v", err)
	}
	t.Setenv("OPENAI_API_KEY", " key ")
	key, err := APIKeyFromEnv()
	if err != nil || key != "key" {
		t.Fatalf("APIKeyFromEnv() = %q, %v", key, err)
	}
	_ = os.Unsetenv("OPENAI_API_KEY")
}

func TestChatToolCallsAndStreaming(t *testing.T) {
	var stream bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != DefaultModelID {
			t.Fatalf("model = %v", body["model"])
		}
		stream, _ = body["stream"].(bool)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	client := NewClient("key", WithBaseURL(server.URL), WithRetryPolicy(opencode.RetryPolicy{MaxRetries: 0}))
	response, err := client.ChatStream(context.Background(), opencode.ChatRequest{
		Messages: []opencode.Message{{Role: "user", Content: "hello"}},
		Tools:    []opencode.ToolSpec{{Name: "read", Description: "read"}},
	}, nil)
	if err != nil || response.Content != "ok" || !stream {
		t.Fatalf("ChatStream() = %+v, %v, stream=%v", response, err, stream)
	}
}

func TestChatErrorsAndModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/models") {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"data":[{"id":"gpt-test"}]}`))
			return
		}
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()
	client := NewClient("key", WithBaseURL(server.URL), WithRetryPolicy(opencode.RetryPolicy{MaxRetries: 0}))
	if _, err := client.Chat(context.Background(), opencode.ChatRequest{}); err == nil {
		t.Fatal("Chat() error = nil")
	}
	infos, err := client.ModelInfos(context.Background())
	if err != nil || len(infos) != 1 || infos[0].Provider != ProviderName {
		t.Fatalf("ModelInfos() = %+v, %v", infos, err)
	}
}
