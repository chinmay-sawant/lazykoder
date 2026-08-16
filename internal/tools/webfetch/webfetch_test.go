package webfetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello body"))
	}))
	defer srv.Close()

	res, err := Run(context.Background(), srv.URL, "", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(res.Output, "hello body") {
		t.Errorf("Output = %q, want it to contain %q", res.Output, "hello body")
	}
	if res.Metadata["content_type"] != "text/plain; charset=utf-8" {
		t.Errorf("content_type = %v, want %q", res.Metadata["content_type"], "text/plain; charset=utf-8")
	}
}

func TestRunRejectsNonHTTPScheme(t *testing.T) {
	tests := []struct{ name, url string }{
		{"file", "file:///etc/passwd"},
		{"ftp", "ftp://example.com/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(context.Background(), tt.url, "", nil)
			if err == nil {
				t.Fatalf("Run(%q) error = nil, want scheme rejection", tt.url)
			}
			if !strings.Contains(err.Error(), `webfetch: unsupported scheme`) {
				t.Errorf("error = %q, want unsupported scheme message", err)
			}
		})
	}
}

func TestRunNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Run(context.Background(), srv.URL, "", nil)
	if err == nil {
		t.Fatal("Run() error = nil, want status error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain 500", err)
	}
}

func TestRunTruncatesLargeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 6*1024*1024)))
	}))
	defer srv.Close()

	res, err := Run(context.Background(), srv.URL, "", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Metadata["truncated"] != true {
		t.Errorf("truncated = %v, want true", res.Metadata["truncated"])
	}
	if len(res.Output) != 5*1024*1024 {
		t.Errorf("Output length = %d, want %d", len(res.Output), 5*1024*1024)
	}
}

func TestRunFormatParam(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query().Get("format")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tests := []struct{ name, format, want string }{
		{"markdown passed through", "markdown", "markdown"},
		{"text is default omitted", "text", ""},
		{"empty omitted", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Run(context.Background(), srv.URL, tt.format, nil); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if v := <-got; v != tt.want {
				t.Errorf("format query = %q, want %q", v, tt.want)
			}
		})
	}
}

func TestRunContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, srv.URL, "", nil)
	if err == nil {
		t.Fatal("Run() error = nil, want cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
