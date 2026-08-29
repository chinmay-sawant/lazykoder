package webfetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	browserTimeout      = 30 * time.Second
	browserSettleDelay  = 250 * time.Millisecond
	maxBrowserDOMBytes  = 8 * 1024 * 1024
	maxBrowserBodyBytes = 5 * 1024 * 1024
	browserWaitDelay    = time.Second

	// maxBrowserStderrBytes caps captured stderr from the browser process.
	maxBrowserStderrBytes = 256 * 1024
	// maxBrowserErrorMessageRunes caps how much stderr text is embedded in an error.
	maxBrowserErrorMessageRunes = 500
	// browserHeaderTimeout bounds how long the local proxy waits for request headers.
	browserHeaderTimeout = 5 * time.Second
	// tunnelConns is the number of directions a CONNECT tunnel copies between.
	tunnelConns = 2
)

// BrowserErrorCategory identifies the browser failure boundary reported to a
// caller.
type BrowserErrorCategory string

const (
	BrowserMissingBinary     BrowserErrorCategory = "missing_binary"
	BrowserStartupFailure    BrowserErrorCategory = "startup_failure"
	BrowserBlockedPage       BrowserErrorCategory = "blocked_page"
	BrowserEmptyDocument     BrowserErrorCategory = "empty_valid_document"
	BrowserRendererCrash     BrowserErrorCategory = "renderer_crash"
	BrowserNavigationTimeout BrowserErrorCategory = "navigation_timeout"
	BrowserCancellation      BrowserErrorCategory = "cancellation"
)

// BrowserError keeps browser lifecycle failures distinguishable while
// preserving context cancellation and unsafe-destination errors for callers.
type BrowserError struct {
	Category BrowserErrorCategory
	Err      error
}

func (e *BrowserError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("webfetch browser: %s", e.Category)
	}
	return fmt.Sprintf("webfetch browser: %s: %v", e.Category, e.Err)
}

func (e *BrowserError) Unwrap() error { return e.Err }

func browserError(category BrowserErrorCategory, err error) error {
	return &BrowserError{Category: category, Err: err}
}

// ChromeBrowser renders a page with an isolated system Chrome or Chromium
// process. Browser requests go through a local validating proxy so redirects
// and subresource origins receive the same public-destination checks as HTTP.
type ChromeBrowser struct {
	command string
	network networkDeps
	find    func() (string, error)
}

// NewChromeBrowser returns a lazy browser reader. It does not start Chrome or
// inspect the machine until Read is called.
func NewChromeBrowser() *ChromeBrowser {
	return &ChromeBrowser{
		network: defaultNetwork,
		find:    findChrome,
	}
}

// Read renders one public HTTP(S) URL and extracts its readable document.
func (b *ChromeBrowser) Read(ctx context.Context, urlStr string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, browserContextError(err)
	}
	network := normalizedNetwork(b.network)
	u, err := parseHTTPURL(urlStr)
	if err != nil {
		return Result{}, err
	}
	if err := validateURL(ctx, u, network.resolver); err != nil {
		if ctx.Err() != nil {
			return Result{}, browserContextError(ctx.Err())
		}
		return Result{}, err
	}

	command := b.command
	if command == "" {
		finder := b.find
		if finder == nil {
			finder = findChrome
		}
		command, err = finder()
		if err != nil {
			return Result{}, browserError(BrowserMissingBinary, err)
		}
	}

	profile, err := os.MkdirTemp("", "lazykoder-chrome-")
	if err != nil {
		return Result{}, browserError(BrowserStartupFailure, fmt.Errorf("create profile: %w", err))
	}
	defer os.RemoveAll(profile)

	readCtx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()
	proxy, err := newBrowserProxy(readCtx, network)
	if err != nil {
		return Result{}, browserError(BrowserStartupFailure, fmt.Errorf("start egress proxy: %w", err))
	}
	defer proxy.Close()

	args := browserArgs(profile, proxy.Address(), urlStr)
	cmd := exec.CommandContext(readCtx, command, args...)
	configureBrowserCommand(cmd)
	stderr := &limitedBuffer{limit: maxBrowserStderrBytes}
	cmd.Stderr = stderr
	cmd.WaitDelay = browserWaitDelay
	if err := cmd.Start(); err != nil {
		return Result{}, browserError(BrowserStartupFailure, withBrowserStderr(err, stderr))
	}
	process := newBrowserProcess(cmd)
	defer func() {
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), browserWaitDelay)
		defer cleanupCancel()
		_ = process.wait(cleanupCtx)
	}()

	devtools, err := connectDevTools(readCtx, profile, process)
	if err != nil {
		return Result{}, classifyBrowserRuntimeError(readCtx, process, err, stderr)
	}
	defer devtools.Close()
	snapshot, err := devtools.waitForDocument(readCtx, process)
	if err != nil {
		return Result{}, classifyBrowserRuntimeError(readCtx, process, err, stderr)
	}
	finalURL, err := parseHTTPURL(snapshot.URL)
	if err != nil {
		return Result{}, browserError(BrowserBlockedPage, fmt.Errorf("final navigation URL: %w", err))
	}
	if err := validateURL(readCtx, finalURL, network.resolver); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(readCtx.Err(), context.DeadlineExceeded) {
			return Result{}, browserError(BrowserNavigationTimeout, context.DeadlineExceeded)
		}
		if errors.Is(err, context.Canceled) || errors.Is(readCtx.Err(), context.Canceled) {
			return Result{}, browserError(BrowserCancellation, context.Canceled)
		}
		return Result{}, err
	}
	dom, domTruncated := truncateUTF8(snapshot.HTML, maxBrowserBodyBytes)
	document, err := extractHTML([]byte(dom), finalURL.String(), "text")
	if err != nil {
		return Result{}, browserError(BrowserBlockedPage, fmt.Errorf("extract document: %w", err))
	}
	if document.Metadata == nil {
		document.Metadata = map[string]any{}
	}
	document.Metadata["browser_rendered"] = true
	document.Metadata["browser_command"] = filepath.Base(command)
	document.Metadata["browser_truncated"] = domTruncated
	document.Metadata["content_type"] = "text/html"
	document.Metadata["final_url"] = finalURL.String()
	document.Metadata["final_url_source"] = "browser"
	document.Metadata["mode"] = string(ModeBrowser)
	if strings.TrimSpace(document.Output) == "" {
		return Result{}, browserError(BrowserEmptyDocument, nil)
	}
	return document, nil
}

func browserArgs(profile, proxyAddress, urlStr string) []string {
	return []string{
		"--headless=new",
		"--disable-gpu",
		"--disable-extensions",
		"--disable-component-extensions-with-background-pages",
		"--disable-sync",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-popup-blocking",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + profile,
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=0",
		"--proxy-server=http://" + proxyAddress,
		"--proxy-bypass-list=<-loopback>",
		urlStr,
	}
}

func browserContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return browserError(BrowserNavigationTimeout, context.DeadlineExceeded)
	}
	return browserError(BrowserCancellation, context.Canceled)
}

func findChrome() (string, error) {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Google Chrome or Chromium is not installed")
}

type limitedBuffer struct {
	mu        sync.Mutex
	data      bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.data.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(value), nil
	}
	if len(value) > remaining {
		b.data.Write(value[:remaining])
		b.truncated = true
		return len(value), nil
	}
	b.data.Write(value)
	return len(value), nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte{}, b.data.Bytes()...)
}

func (b *limitedBuffer) String() string {
	return string(b.Bytes())
}

func (b *limitedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Len()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

type browserProxy struct {
	server   *http.Server
	listener net.Listener
	network  networkDeps
	ctx      context.Context

	mu        sync.Mutex
	closed    bool
	conns     map[net.Conn]struct{}
	closeOnce sync.Once
}

func newBrowserProxy(ctx context.Context, network networkDeps) (*browserProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	proxy := &browserProxy{
		listener: listener,
		network:  normalizedNetwork(network),
		ctx:      ctx,
		conns:    make(map[net.Conn]struct{}),
	}
	proxy.server = &http.Server{
		Handler:           http.HandlerFunc(proxy.handle),
		ReadHeaderTimeout: browserHeaderTimeout,
	}
	go func() {
		_ = proxy.server.Serve(listener)
	}()
	go proxy.closeOnContext()
	return proxy, nil
}

func (p *browserProxy) Address() string {
	return p.listener.Addr().String()
}

func (p *browserProxy) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		connections := make([]net.Conn, 0, len(p.conns))
		for connection := range p.conns {
			connections = append(connections, connection)
		}
		p.mu.Unlock()
		_ = p.server.Close()
		_ = p.listener.Close()
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
}

func (p *browserProxy) closeOnContext() {
	if p.ctx == nil {
		return
	}
	<-p.ctx.Done()
	p.Close()
}

func (p *browserProxy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	if r.URL == nil || (r.URL.Scheme != "http" && r.URL.Scheme != "https") {
		http.Error(w, "proxy: only http and https are allowed", http.StatusForbidden)
		return
	}
	if err := validateURL(r.Context(), r.URL, p.network.resolver); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	transport, err := validatedTransport(nil, p.network)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	r.RequestURI = ""
	r.Header.Del("Proxy-Connection")
	response, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.CopyN(w, response.Body, maxBrowserBodyBytes)
}

func (p *browserProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
		port = "443"
	}
	ips, err := lookupPublicIPs(r.Context(), host, p.network.resolver)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var remote net.Conn
	for _, ip := range ips {
		remote, err = p.network.dial(r.Context(), "tcp", net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			break
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = remote.Close()
		http.Error(w, "proxy: hijacking is unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		_ = remote.Close()
		return
	}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		_ = client.Close()
		_ = remote.Close()
		return
	}
	if !p.track(client, remote) {
		_ = client.Close()
		_ = remote.Close()
		return
	}
	go p.tunnel(client, remote)
}

func (p *browserProxy) track(connections ...net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	if p.conns == nil {
		p.conns = make(map[net.Conn]struct{})
	}
	for _, connection := range connections {
		p.conns[connection] = struct{}{}
	}
	return true
}

func (p *browserProxy) untrack(connections ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, connection := range connections {
		delete(p.conns, connection)
	}
}

func (p *browserProxy) tunnel(left, right net.Conn) {
	defer p.untrack(left, right)
	defer left.Close()
	defer right.Close()
	done := make(chan struct{}, tunnelConns)
	copyConn := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go copyConn(left, right)
	go copyConn(right, left)
	<-done
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

var _ BrowserReader = (*ChromeBrowser)(nil)
