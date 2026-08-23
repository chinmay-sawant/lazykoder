package webfetch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestExtractHTML(t *testing.T) {
	body := `<html><head><title>Article title</title><script>ignore()</script></head><body>
<nav>Ignore navigation</nav><main><h1>Article title</h1><p>Useful article text.</p>
<p>Contact contact@example.com.</p><a href="/next">Next page</a>
<a href="mailto:writer@example.com?subject=Hello%20there&body=Read%20this">Email writer</a>
<a href="javascript:alert(1)">Unsafe script link</a><p hidden>hidden@example.com</p></main></body></html>`
	result, err := extractHTML([]byte(body), "https://example.com/articles/start", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "# Article title") || !strings.Contains(result.Output, "Useful article text") {
		t.Errorf("output = %q, want readable markdown text", result.Output)
	}
	if strings.Contains(result.Output, "Ignore navigation") || strings.Contains(result.Output, "hidden@example.com") {
		t.Errorf("output contains excluded content: %q", result.Output)
	}
	links, ok := result.Metadata["links"].([]extractedLink)
	if !ok || len(links) != 1 || links[0].URL != "https://example.com/next" {
		t.Errorf("links = %#v, want one resolved HTTP link", result.Metadata["links"])
	}
	emailLinks, ok := result.Metadata["email_links"].([]extractedEmailLink)
	if !ok || len(emailLinks) != 1 || emailLinks[0].Subject != "Hello there" || emailLinks[0].Body != "Read this" {
		t.Errorf("email links = %#v, want decoded mailto fields", result.Metadata["email_links"])
	}
	emails, ok := result.Metadata["emails"].([]string)
	if !ok || len(emails) != 1 || emails[0] != "contact@example.com" {
		t.Errorf("emails = %#v, want visible email only", result.Metadata["emails"])
	}
	if result.Metadata["needs_browser"] != false {
		t.Errorf("needs_browser = %v, want false", result.Metadata["needs_browser"])
	}
}

func TestRunWithOptionsAutoFallsBackToBrowser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	fake := &fakeBrowserReader{result: Result{
		Output:   "rendered browser content",
		Metadata: map[string]any{"title": "Rendered"},
	}}
	result, err := RunWithOptions(context.Background(), Options{
		URL:     safeTestURL,
		Mode:    ModeAuto,
		Browser: fake,
		network: serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "rendered browser content" || fake.url != safeTestURL {
		t.Errorf("result = %#v, browser URL = %q", result, fake.url)
	}
	if result.Metadata["mode"] != string(ModeBrowser) || result.Metadata["browser_fallback"] == nil {
		t.Errorf("metadata = %#v, want browser fallback metadata", result.Metadata)
	}
}

func TestRunWithOptionsRejectsUnknownMode(t *testing.T) {
	_, err := RunWithOptions(context.Background(), Options{URL: safeTestURL, Mode: Mode("sideways")})
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestChromeBrowserRendersLocalFixture(t *testing.T) {
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 to run the system browser integration")
	}
	command, err := findChrome()
	if err != nil {
		t.Skip(err.Error())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head><title>Shell</title></head><body><div id="app">Loading</div><script>document.getElementById("app").innerHTML = '<main><h1>Rendered fixture</h1><p>JavaScript content.</p><a href="mailto:writer@example.com">Email</a></main>'</script></body></html>`)
	}))
	defer srv.Close()
	browser := NewChromeBrowser()
	browser.command = command
	browser.network = serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}})
	result, err := browser.Read(context.Background(), safeTestURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Rendered fixture") || !strings.Contains(result.Output, "JavaScript content") {
		t.Errorf("output = %q, want rendered JavaScript content", result.Output)
	}
	if result.Metadata["browser_rendered"] != true {
		t.Errorf("metadata = %#v, want browser_rendered", result.Metadata)
	}
	if result.Metadata["title"] != "Shell" || result.Metadata["final_url"] != safeTestURL {
		t.Errorf("metadata = %#v, want title and final URL", result.Metadata)
	}
	if links, ok := result.Metadata["links"].([]extractedLink); !ok || len(links) != 0 {
		t.Errorf("links = %#v, want no HTTP links in fixture", result.Metadata["links"])
	}
	emailLinks, ok := result.Metadata["email_links"].([]extractedEmailLink)
	if !ok || len(emailLinks) != 1 || emailLinks[0].Address != "writer@example.com" {
		t.Errorf("email_links = %#v, output = %q, want extracted writer address", result.Metadata["email_links"], result.Output)
	}
}

func TestChromeBrowserRendersConfiguredURL(t *testing.T) {
	urlStr := os.Getenv("LAZYKODER_BROWSER_URL")
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" || urlStr == "" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 and LAZYKODER_BROWSER_URL to run a live browser read")
	}
	result, err := NewChromeBrowser().Read(context.Background(), urlStr)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) == "" {
		t.Fatal("browser returned empty readable content")
	}
	t.Logf("browser title=%q output_runes=%d", result.Metadata["title"], len([]rune(result.Output)))
}

func TestBrowserProxyRejectsPrivateHTTPDestination(t *testing.T) {
	dialed := false
	proxy := &browserProxy{network: networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{loopbackIP()}}},
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusForbidden || dialed {
		t.Fatalf("proxy response = %d, dialed = %v, want forbidden without dial", recorder.Code, dialed)
	}
}

func TestBrowserProxyRejectsPrivateConnectDestination(t *testing.T) {
	dialed := false
	proxy := &browserProxy{network: networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{loopbackIP()}}},
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodConnect, "http://127.0.0.1:443", nil)
	request.Host = "127.0.0.1:443"
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusForbidden || dialed {
		t.Fatalf("proxy response = %d, dialed = %v, want forbidden without dial", recorder.Code, dialed)
	}
}

type fakeBrowserReader struct {
	result Result
	err    error
	url    string
}

func (f *fakeBrowserReader) Read(_ context.Context, urlStr string) (Result, error) {
	f.url = urlStr
	return f.result, f.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
