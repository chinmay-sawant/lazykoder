package webfetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const safeTestURL = "http://example.test/"

type sequenceResolver struct {
	mu        sync.Mutex
	responses [][]net.IPAddr
	calls     int
}

func (r *sequenceResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := r.calls
	r.calls++
	if idx >= len(r.responses) {
		idx = len(r.responses) - 1
	}
	return append([]net.IPAddr(nil), r.responses[idx]...), nil
}

func publicIP() net.IPAddr {
	return net.IPAddr{IP: net.ParseIP("203.0.113.11")}
}

func loopbackIP() net.IPAddr {
	return net.IPAddr{IP: net.ParseIP("127.0.0.1")}
}

func serverNetwork(srv *httptest.Server, resolver resolver) networkDeps {
	return networkDeps{
		resolver: resolver,
		dial: func(ctx context.Context, networkName, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, networkName, srv.Listener.Addr().String())
		},
	}
}

func TestRunBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello body"))
	}))
	defer srv.Close()
	res, err := run(context.Background(), safeTestURL, "", nil, serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}))
	if err != nil || !strings.Contains(res.Output, "hello body") {
		t.Fatalf("Run() = %#v, %v", res, err)
	}
	if res.Metadata["content_type"] != "text/plain; charset=utf-8" {
		t.Errorf("content_type = %v", res.Metadata["content_type"])
	}
}

func TestRunRejectsNonHTTPScheme(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com/x"} {
		_, err := run(context.Background(), url, "", nil, defaultNetwork)
		if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
			t.Errorf("Run(%q) = %v", url, err)
		}
	}
}

func TestRunNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	_, err := run(context.Background(), safeTestURL, "", nil, serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v", err)
	}
}

func TestRunTruncatesLargeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 6*1024*1024))) }))
	defer srv.Close()
	res, err := run(context.Background(), safeTestURL, "", nil, serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}))
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
		_, err := run(context.Background(), safeTestURL, tt.format, nil, serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}))
		if err != nil {
			t.Fatal(err)
		}
		if v := <-got; v != tt.want {
			t.Errorf("format = %q, want %q", v, tt.want)
		}
	}
}

func TestRunContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(2 * time.Second) }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := run(ctx, safeTestURL, "", nil, serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v", err)
	}
}

func TestRunRejectsPrivateBeforeDial(t *testing.T) {
	dialed := false
	_, err := run(context.Background(), "http://127.0.0.1/", "", nil, networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{loopbackIP()}}},
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "local or private") {
		t.Errorf("error = %v", err)
	}
	if dialed {
		t.Fatal("dial occurred for private target")
	}
}

func TestRunRejectsPrivateRedirectAndPreservesClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/", http.StatusFound)
	}))
	defer srv.Close()
	called := false
	callback := func(*http.Request, []*http.Request) error {
		called = true
		return nil
	}
	client := &http.Client{CheckRedirect: callback}
	_, err := run(context.Background(), safeTestURL, "", client, serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}))
	if err == nil || !strings.Contains(err.Error(), "redirect to local") {
		t.Errorf("error = %v", err)
	}
	if called {
		t.Fatal("caller redirect callback ran after rejected destination")
	}
	if client.CheckRedirect == nil {
		t.Fatal("Run cleared the caller redirect callback")
	}
	if err := client.CheckRedirect(nil, nil); err != nil {
		t.Fatalf("caller redirect callback changed: %v", err)
	}
	if !called {
		t.Fatal("caller redirect callback no longer works")
	}
}

func TestRunRejectsDNSRebindingAtDial(t *testing.T) {
	dialed := false
	resolver := &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}, {loopbackIP()}}}
	_, err := run(context.Background(), safeTestURL, "", nil, networkDeps{
		resolver: resolver,
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "local or private") {
		t.Fatalf("error = %v", err)
	}
	if dialed {
		t.Fatal("dial occurred after a rebinding result to loopback")
	}
}

func TestRunRejectsCustomTransport(t *testing.T) {
	_, err := run(context.Background(), safeTestURL, "", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })}, networkDeps{resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}})
	if err == nil || !strings.Contains(err.Error(), "custom transports") {
		t.Fatalf("error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
