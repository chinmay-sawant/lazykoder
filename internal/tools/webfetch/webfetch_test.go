package webfetch

import (
	"context"
	"errors"
	"fmt"
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

func TestRunCapturesPublicRedirectURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://example.test/final", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "final body")
	}))
	defer srv.Close()
	result, err := run(
		context.Background(),
		"http://example.test/start",
		"",
		nil,
		serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["final_url"] != "http://example.test/final" {
		t.Errorf("final_url = %v, want redirected URL", result.Metadata["final_url"])
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

func TestExtractHTMLCapsReadableText(t *testing.T) {
	result, err := extractHTML(
		[]byte("<main><p>"+strings.Repeat("x", maxReadableTextBytes+100)+"</p></main>"),
		"https://example.com/long",
		"text",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) > maxReadableTextBytes || result.Metadata["content_truncated"] != true {
		t.Errorf("output bytes = %d, content_truncated = %v", len(result.Output), result.Metadata["content_truncated"])
	}
}

func TestExtractHTMLCapsMetadata(t *testing.T) {
	var links strings.Builder
	for index := range maxExtractedLinks + 20 {
		links.WriteString(fmt.Sprintf(
			`<a href="https://example.com/%d%s">link</a>`,
			index,
			strings.Repeat("x", maxLinkURLRunes-30),
		))
	}
	body := "<main>" + links.String() + "</main>"
	result, err := extractHTML([]byte(body), "https://example.com/long", "text")
	if err != nil {
		t.Fatal(err)
	}
	if size := metadataSize(result.Metadata); size > maxMetadataBytes {
		t.Errorf("metadata bytes = %d, want at most %d", size, maxMetadataBytes)
	}
	if result.Metadata["metadata_truncated"] != true {
		t.Errorf("metadata_truncated = %v, want true", result.Metadata["metadata_truncated"])
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

func TestRunWithOptionsHTTPNeverStartsBrowser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "static response")
	}))
	defer srv.Close()
	reader := &fakeBrowserReader{result: Result{Output: "browser response"}}
	result, err := RunWithOptions(context.Background(), Options{
		URL:     safeTestURL,
		Mode:    ModeHTTP,
		Browser: reader,
		network: serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "static response" || reader.url != "" {
		t.Fatalf("result = %#v, browser URL = %q", result, reader.url)
	}
	if result.Metadata["mode"] != string(ModeHTTP) {
		t.Errorf("mode = %v, want http", result.Metadata["mode"])
	}
}

func TestRunWithOptionsBrowserValidatesInjectedReader(t *testing.T) {
	reader := &fakeBrowserReader{result: Result{Output: "browser response"}}
	_, err := RunWithOptions(context.Background(), Options{
		URL:     "http://127.0.0.1/private",
		Mode:    ModeBrowser,
		Browser: reader,
		network: networkDeps{
			resolver: &sequenceResolver{responses: [][]net.IPAddr{{loopbackIP()}}},
			dial:     func(context.Context, string, string) (net.Conn, error) { return nil, nil },
		},
	})
	if !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("error = %v, want unsafe destination", err)
	}
	if reader.url != "" {
		t.Fatalf("browser URL = %q, want no browser call", reader.url)
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
		_, _ = io.WriteString(w, `<html><head><title>Shell</title></head><body><div id="app">Loading</div><script>setTimeout(() => { document.getElementById("app").innerHTML = '<main><h1>Rendered fixture</h1><p>JavaScript content.</p><a href="mailto:writer@example.com">Email</a></main>' }, 50)</script></body></html>`)
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

func TestChromeBrowserReportsMissingBinary(t *testing.T) {
	browser := &ChromeBrowser{
		network: networkDeps{
			resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}},
			dial:     func(context.Context, string, string) (net.Conn, error) { return nil, nil },
		},
		find: func() (string, error) { return "", errors.New("no browser") },
	}
	_, err := browser.Read(context.Background(), safeTestURL)
	var browserErr *BrowserError
	if !errors.As(err, &browserErr) || browserErr.Category != BrowserMissingBinary {
		t.Fatalf("error = %v, want missing binary category", err)
	}
}

func TestChromeBrowserReportsStartupFailure(t *testing.T) {
	browser := &ChromeBrowser{
		command: "/definitely/missing/lazykoder-browser",
		network: networkDeps{
			resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}},
			dial:     func(context.Context, string, string) (net.Conn, error) { return nil, nil },
		},
	}
	_, err := browser.Read(context.Background(), safeTestURL)
	var browserErr *BrowserError
	if !errors.As(err, &browserErr) || browserErr.Category != BrowserStartupFailure {
		t.Fatalf("error = %v, want startup failure category", err)
	}
}

func TestChromeBrowserReportsRendererCrash(t *testing.T) {
	browser := &ChromeBrowser{
		command: "/usr/bin/false",
		network: networkDeps{
			resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}},
			dial:     func(context.Context, string, string) (net.Conn, error) { return nil, nil },
		},
	}
	_, err := browser.Read(context.Background(), safeTestURL)
	var browserErr *BrowserError
	if !errors.As(err, &browserErr) || browserErr.Category != BrowserRendererCrash {
		t.Fatalf("error = %v, want renderer crash category", err)
	}
}

func TestChromeBrowserCapturesRedirectURL(t *testing.T) {
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 to run the system browser integration")
	}
	command, err := findChrome()
	if err != nil {
		t.Skip(err.Error())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://example.test/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head><title>Final page</title></head><body><main><h1>Final page</h1><p>Redirected content.</p></main></body></html>`)
	}))
	defer srv.Close()
	browser := NewChromeBrowser()
	browser.command = command
	browser.network = serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}})
	result, err := browser.Read(context.Background(), "http://example.test/start")
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["final_url"] != "http://example.test/final" {
		t.Errorf("final_url = %v, want redirected URL", result.Metadata["final_url"])
	}
	if result.Metadata["final_url_source"] != "browser" {
		t.Errorf("final_url_source = %v, want browser", result.Metadata["final_url_source"])
	}
}

func TestChromeBrowserBlocksPrivateSubresource(t *testing.T) {
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 to run the system browser integration")
	}
	command, err := findChrome()
	if err != nil {
		t.Skip(err.Error())
	}
	privateHit := make(chan struct{}, 1)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/private") {
			privateHit <- struct{}{}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		privateURL := fmt.Sprintf("http://%s/private", srv.Listener.Addr().String())
		_, _ = io.WriteString(w, `<html><body><main><h1>Public page</h1><img src="`+privateURL+`/image"><iframe src="`+privateURL+`/iframe"></iframe><script>fetch('`+privateURL+`/fetch')</script></main></body></html>`)
	}))
	defer srv.Close()
	browser := NewChromeBrowser()
	browser.command = command
	browser.network = serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}})
	if _, err := browser.Read(context.Background(), safeTestURL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-privateHit:
		t.Fatal("browser reached a private subresource")
	case <-time.After(250 * time.Millisecond):
	}
}

func TestChromeBrowserCancellationStopsProcessAndProxy(t *testing.T) {
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 to run the system browser integration")
	}
	command, err := findChrome()
	if err != nil {
		t.Skip(err.Error())
	}
	started := make(chan struct{})
	var startOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer srv.Close()
	browser := NewChromeBrowser()
	browser.command = command
	browser.network = serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, readErr := browser.Read(ctx, safeTestURL)
		result <- readErr
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("browser did not reach the fixture")
	}
	select {
	case err := <-result:
		var browserErr *BrowserError
		if !errors.As(err, &browserErr) || browserErr.Category != BrowserCancellation {
			t.Fatalf("error = %v, want cancellation category", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("browser did not stop after cancellation")
	}
}

func TestChromeBrowserReportsEmptyValidDocument(t *testing.T) {
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 to run the system browser integration")
	}
	command, err := findChrome()
	if err != nil {
		t.Skip(err.Error())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head><title>Empty</title></head><body></body></html>`)
	}))
	defer srv.Close()
	browser := NewChromeBrowser()
	browser.command = command
	browser.network = serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}})
	_, err = browser.Read(context.Background(), safeTestURL)
	var browserErr *BrowserError
	if !errors.As(err, &browserErr) || browserErr.Category != BrowserEmptyDocument {
		t.Fatalf("error = %v, want empty document category", err)
	}
}

func TestChromeBrowserReportsNavigationTimeout(t *testing.T) {
	if os.Getenv("LAZYKODER_BROWSER_TEST") != "1" {
		t.Skip("set LAZYKODER_BROWSER_TEST=1 to run the system browser integration")
	}
	command, err := findChrome()
	if err != nil {
		t.Skip(err.Error())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	browser := NewChromeBrowser()
	browser.command = command
	browser.network = serverNetwork(srv, &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}})
	_, err = browser.Read(context.Background(), safeTestURL)
	var browserErr *BrowserError
	if !errors.As(err, &browserErr) || browserErr.Category != BrowserNavigationTimeout {
		t.Fatalf("error = %v, want navigation timeout category", err)
	}
}

func TestChromeBrowserReportsCancellationCategory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	browser := &ChromeBrowser{network: networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}},
		dial:     func(context.Context, string, string) (net.Conn, error) { return nil, nil },
	}}
	_, err := browser.Read(ctx, safeTestURL)
	var browserErr *BrowserError
	if !errors.As(err, &browserErr) || browserErr.Category != BrowserCancellation {
		t.Fatalf("error = %v, want cancellation category", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
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
	t.Logf(
		"browser command=%q fixture_url=%q title=%q final_url=%q output_runes=%d email_links=%v",
		result.Metadata["browser_command"],
		urlStr,
		result.Metadata["title"],
		result.Metadata["final_url"],
		len([]rune(result.Output)),
		result.Metadata["email_links"],
	)
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

func TestBrowserProxyRejectsIPv6PrivateDestination(t *testing.T) {
	dialed := false
	proxy := &browserProxy{network: networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{{IP: net.ParseIP("::1")}}}},
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://[::1]/private", nil)
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusForbidden || dialed {
		t.Fatalf("proxy response = %d, dialed = %v, want forbidden without dial", recorder.Code, dialed)
	}
}

func TestBrowserProxyRejectsLinkLocalAndMetadataDestinations(t *testing.T) {
	for _, target := range []string{"169.254.169.254", "fe80::1"} {
		t.Run(target, func(t *testing.T) {
			dialed := false
			proxy := &browserProxy{network: networkDeps{
				resolver: &sequenceResolver{responses: [][]net.IPAddr{{{IP: net.ParseIP(target)}}}},
				dial: func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, nil
				},
			}}
			host := target
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://"+host+"/private", nil)
			proxy.handle(recorder, request)
			if recorder.Code != http.StatusForbidden || dialed {
				t.Fatalf("proxy response = %d, dialed = %v, want forbidden without dial", recorder.Code, dialed)
			}
		})
	}
}

func TestBrowserProxyRejectsUnsupportedScheme(t *testing.T) {
	proxy := &browserProxy{network: networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}},
		dial:     func(context.Context, string, string) (net.Conn, error) { return nil, nil },
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "file:///etc/passwd", nil)
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("proxy response = %d, want forbidden", recorder.Code)
	}
}

func TestBrowserProxyRejectsDNSRebindingBeforeDial(t *testing.T) {
	dialed := false
	proxy := &browserProxy{network: networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}, {loopbackIP()}}},
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/rebind", nil)
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusBadGateway || dialed {
		t.Fatalf("proxy response = %d, dialed = %v, want bad gateway without dial", recorder.Code, dialed)
	}
}

func TestBrowserProxyCloseClosesTrackedTunnel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy, err := newBrowserProxy(ctx, networkDeps{
		resolver: &sequenceResolver{responses: [][]net.IPAddr{{publicIP()}}},
		dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("unused")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	if !proxy.track(left, right) {
		t.Fatal("track returned false")
	}
	proxy.Close()
	for name, connection := range map[string]net.Conn{"left": left, "right": right} {
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := connection.Read(make([]byte, 1)); err == nil {
			t.Errorf("%s tunnel endpoint remained open", name)
		}
		_ = connection.Close()
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
