package webfetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	browserTimeout      = 30 * time.Second
	browserVirtualTime  = 5 * time.Second
	maxBrowserDOMBytes  = 8 * 1024 * 1024
	maxBrowserBodyBytes = 5 * 1024 * 1024
)

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
	u, err := url.Parse(urlStr)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch browser: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return Result{}, fmt.Errorf("webfetch browser: unsupported scheme %q", u.Scheme)
	}
	if err := validateURL(ctx, u, b.network.resolver); err != nil {
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
			return Result{}, err
		}
	}
	proxy, err := newBrowserProxy(b.network)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch browser: start egress proxy: %w", err)
	}
	defer proxy.Close()

	profile, err := os.MkdirTemp("", "lazykoder-chrome-")
	if err != nil {
		return Result{}, fmt.Errorf("webfetch browser: create profile: %w", err)
	}
	defer os.RemoveAll(profile)

	readCtx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()
	args := browserArgs(profile, proxy.Address(), urlStr)
	cmd := exec.CommandContext(readCtx, command, args...)
	configureBrowserCommand(cmd)
	stdout := &limitedBuffer{limit: maxBrowserDOMBytes}
	stderr := &limitedBuffer{limit: 256 * 1024}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if readCtx.Err() != nil {
			return Result{}, fmt.Errorf("webfetch browser: %w", readCtx.Err())
		}
		if stdout.Len() == 0 {
			message := strings.TrimSpace(stderr.String())
			if message != "" {
				return Result{}, fmt.Errorf("webfetch browser: %w: %s", err, truncateRunes(message, 500))
			}
			return Result{}, fmt.Errorf("webfetch browser: %w", err)
		}
	}
	if stdout.Len() == 0 {
		return Result{}, fmt.Errorf("webfetch browser: empty rendered document")
	}
	document, err := extractHTML(stdout.Bytes(), urlStr, "text")
	if err != nil {
		return Result{}, fmt.Errorf("webfetch browser: extract document: %w", err)
	}
	if document.Metadata == nil {
		document.Metadata = map[string]any{}
	}
	document.Metadata["browser_rendered"] = true
	document.Metadata["browser_command"] = filepath.Base(command)
	document.Metadata["browser_truncated"] = stdout.Truncated()
	document.Metadata["content_type"] = "text/html"
	document.Metadata["final_url_source"] = "requested"
	return document, nil
}

func browserArgs(profile, proxyAddress, urlStr string) []string {
	virtualTime := strconv.FormatInt(browserVirtualTime.Milliseconds(), 10)
	return []string{
		"--headless=new",
		"--dump-dom",
		"--disable-gpu",
		"--disable-extensions",
		"--disable-sync",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-popup-blocking",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + profile,
		"--proxy-server=http://" + proxyAddress,
		"--proxy-bypass-list=<-loopback>",
		"--virtual-time-budget=" + virtualTime,
		"--timeout=" + strconv.FormatInt(browserTimeout.Milliseconds(), 10),
		urlStr,
	}
}

func findChrome() (string, error) {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("webfetch browser: Google Chrome or Chromium is not installed")
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
}

func newBrowserProxy(network networkDeps) (*browserProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	proxy := &browserProxy{listener: listener, network: network}
	proxy.server = &http.Server{
		Handler:           http.HandlerFunc(proxy.handle),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = proxy.server.Serve(listener)
	}()
	return proxy, nil
}

func (p *browserProxy) Address() string {
	return p.listener.Addr().String()
}

func (p *browserProxy) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.server.Shutdown(ctx)
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
	go tunnel(client, remote)
}

func tunnel(left, right net.Conn) {
	defer left.Close()
	defer right.Close()
	done := make(chan struct{}, 2)
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
