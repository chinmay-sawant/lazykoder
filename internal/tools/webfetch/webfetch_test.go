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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func safeTestClient(srv *httptest.Server) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		u.URL.RawQuery = r.URL.RawQuery
		copy.URL, copy.Host = u.URL, u.Host
		return srv.Client().Transport.RoundTrip(copy)
	})}
}

const safeTestURL = "http://example.test/"

func TestRunBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello body"))
	}))
	defer srv.Close()
	res, err := Run(context.Background(), safeTestURL, "", safeTestClient(srv))
	if err != nil || !strings.Contains(res.Output, "hello body") {
		t.Fatalf("Run() = %#v, %v", res, err)
	}
	if res.Metadata["content_type"] != "text/plain; charset=utf-8" {
		t.Errorf("content_type = %v", res.Metadata["content_type"])
	}
}

func TestRunRejectsNonHTTPScheme(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com/x"} {
		_, err := Run(context.Background(), url, "", nil)
		if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
			t.Errorf("Run(%q) = %v", url, err)
		}
	}
}

func TestRunNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	_, err := Run(context.Background(), safeTestURL, "", safeTestClient(srv))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v", err)
	}
}

func TestRunTruncatesLargeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 6*1024*1024))) }))
	defer srv.Close()
	res, err := Run(context.Background(), safeTestURL, "", safeTestClient(srv))
	if err != nil {
		t.Fatal(err)
	}
	if res.Metadata["truncated"] != true || len(res.Output) != 5*1024*1024 {
		t.Errorf("truncation = %v, len %d", res.Metadata["truncated"], len(res.Output))
	}
}

func TestRunFormatParam(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query().Get("format")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	for _, tt := range []struct{ format, want string }{{"markdown", "markdown"}, {"text", ""}, {"", ""}} {
		if _, err := Run(context.Background(), safeTestURL, tt.format, safeTestClient(srv)); err != nil {
			t.Fatal(err)
		}
		if v := <-got; v != tt.want {
			t.Errorf("format = %q, want %q", v, tt.want)
		}
	}
}

func TestRunContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, safeTestURL, "", safeTestClient(srv))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v", err)
	}
}

func TestRunRejectsPrivateEvenWithCustomClient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("transport must not be called")
		return nil, nil
	})}
	_, err := Run(context.Background(), "http://127.0.0.1/", "", client)
	if err == nil || !strings.Contains(err.Error(), "local or private") {
		t.Errorf("error = %v", err)
	}
}

func TestRunRejectsPrivateRedirect(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/", http.StatusFound)
	}))
	defer public.Close()
	_, err := Run(context.Background(), safeTestURL, "", safeTestClient(public))
	if err == nil || !strings.Contains(err.Error(), "redirect to local") {
		t.Errorf("error = %v", err)
	}
}
