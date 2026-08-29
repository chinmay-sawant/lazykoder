package opencode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.MaxRetries != 5 {
		t.Fatalf("MaxRetries = %d, want 5", policy.MaxRetries)
	}
	if policy.Delay != 10*time.Second {
		t.Fatalf("Delay = %v, want 10s", policy.Delay)
	}
}

func TestChatRetriesTransientServerErrors(t *testing.T) {
	var calls atomic.Int32
	var waits []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, `{"error":"temporarily unavailable"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"ok","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL), WithRetryPolicy(RetryPolicy{
		MaxRetries: 2,
		Delay:      10 * time.Second,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	}))
	resp, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("Content = %q, want ok", resp.Content)
	}
	if calls.Load() != 3 {
		t.Fatalf("requests = %d, want 3", calls.Load())
	}
	if len(waits) != 2 || waits[0] != 10*time.Second || waits[1] != 10*time.Second {
		t.Fatalf("retry waits = %v, want [10s 10s]", waits)
	}
}

func TestChatRetryWaitStopsOnCancellation(t *testing.T) {
	var calls atomic.Int32
	waitStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `temporary failure`)
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL), WithRetryPolicy(RetryPolicy{
		MaxRetries: 2,
		Wait: func(ctx context.Context, _ time.Duration) error {
			close(waitStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := c.Chat(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
		result <- err
	}()
	select {
	case <-waitStarted:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("retry wait did not start")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Chat error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Chat did not stop after cancellation")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestChatStreamRetriesTransientServerErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `temporary failure`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"stream ok","finish_reason":"stop"}}]}`)
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL), WithRetryPolicy(RetryPolicy{
		MaxRetries: 1,
		Wait:       func(context.Context, time.Duration) error { return nil },
	}))
	resp, err := c.ChatStream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp.Content != "stream ok" {
		t.Fatalf("Content = %q, want stream ok", resp.Content)
	}
	if calls.Load() != 2 {
		t.Fatalf("requests = %d, want 2", calls.Load())
	}
}

func TestChatDoesNotRetryAuthenticationFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized status", status: http.StatusUnauthorized, body: `{"error":"unauthorized"}`},
		{name: "api key body", status: http.StatusInternalServerError, body: `{"error":"invalid api key"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()

			c := NewClient("key", WithBaseURL(srv.URL), WithRetryPolicy(RetryPolicy{
				MaxRetries: 3,
				Delay:      time.Second,
				Wait: func(context.Context, time.Duration) error {
					t.Fatal("authentication failure should not wait before retry")
					return nil
				},
			}))
			_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
			if err == nil {
				t.Fatal("Chat() error = nil, want authentication error")
			}
			if calls.Load() != 1 {
				t.Fatalf("requests = %d, want 1", calls.Load())
			}
		})
	}
}

func TestChatRetryLimitReturnsLastServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `server exploded`)
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL), WithRetryPolicy(RetryPolicy{
		MaxRetries: 2,
		Wait:       func(context.Context, time.Duration) error { return nil },
	}))
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Fatal("Chat() error = nil, want server error")
	}
	if calls.Load() != 3 {
		t.Fatalf("requests = %d, want initial request plus 2 retries", calls.Load())
	}
	if got := err.Error(); !strings.Contains(got, "status 500") || !strings.Contains(got, "server exploded") {
		t.Fatalf("error = %q, want status and response body", got)
	}
}
