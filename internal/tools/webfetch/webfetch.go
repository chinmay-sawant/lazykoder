package webfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Result struct {
	Output   string
	Metadata map[string]any
}

// Mode controls how a URL is read.
type Mode string

const (
	ModeAuto    Mode = "auto"
	ModeHTTP    Mode = "http"
	ModeBrowser Mode = "browser"
)

// Options configures one URL read.
type Options struct {
	URL     string
	Format  string
	Mode    Mode
	Client  *http.Client
	Browser BrowserReader
	network networkDeps
}

// BrowserReader reads a URL with a browser-capable renderer.
type BrowserReader interface {
	Read(ctx context.Context, urlStr string) (Result, error)
}

// ErrUnsafeDestination marks a local, private, link-local, multicast, or
// metadata destination rejected by the egress policy.
var ErrUnsafeDestination = errors.New("webfetch: local or private host is not allowed")

const (
	requestTimeout = 30 * time.Second
	maxURLRunes    = 8192
)

type resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

type networkDeps struct {
	resolver resolver
	dial     dialContextFunc
}

var defaultNetwork = networkDeps{
	resolver: net.DefaultResolver,
	dial:     (&net.Dialer{}).DialContext,
}

// Run GETs an HTTP(S) URL, rejects local and private destinations (including
// redirects), and caps the response body at 5MB. A supplied client contributes
// client-level settings only. Run copies it and replaces its transport with a
// validated direct transport, so the caller's client is never changed.
func Run(ctx context.Context, urlStr, format string, client *http.Client) (Result, error) {
	return run(ctx, urlStr, format, client, defaultNetwork)
}

// RunWithOptions reads a URL through the requested HTTP or browser path. Auto
// mode keeps the guarded HTTP path first and uses a browser for blocked status
// responses or pages that are only JavaScript shells.
func RunWithOptions(ctx context.Context, options Options) (Result, error) {
	mode, err := normalizeMode(options.Mode)
	if err != nil {
		return Result{}, err
	}
	network := normalizedNetwork(options.network)
	if mode == ModeBrowser {
		u, err := parseHTTPURL(options.URL)
		if err != nil {
			return Result{}, err
		}
		if err := validateURL(ctx, u, network.resolver); err != nil {
			return Result{}, err
		}
		return readBrowser(ctx, options, "")
	}

	result, err := runHTTP(ctx, options.URL, options.Format, options.Client, true, network)
	if mode == ModeHTTP {
		return result, err
	}
	if err == nil && !needsBrowser(result) {
		return result, nil
	}
	if err != nil && !canFallbackToBrowser(err) {
		return Result{}, err
	}

	reason := "rendered content was unavailable"
	if err != nil {
		reason = err.Error()
	}
	return readBrowser(ctx, options, reason)
}

func normalizeMode(mode Mode) (Mode, error) {
	if mode == "" {
		return ModeAuto, nil
	}
	switch Mode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case ModeAuto:
		return ModeAuto, nil
	case ModeHTTP:
		return ModeHTTP, nil
	case ModeBrowser:
		return ModeBrowser, nil
	default:
		return "", fmt.Errorf("webfetch: unsupported mode %q", mode)
	}
}

func run(ctx context.Context, urlStr, format string, client *http.Client, network networkDeps) (Result, error) {
	return runHTTP(ctx, urlStr, format, client, false, network)
}

func runHTTP(ctx context.Context, urlStr, format string, client *http.Client, normalizeHTML bool, network networkDeps) (Result, error) {
	u, err := parseHTTPURL(urlStr)
	if err != nil {
		return Result{}, err
	}
	if err := validateURL(ctx, u, network.resolver); err != nil {
		return Result{}, err
	}
	if strings.EqualFold(format, "markdown") {
		q := u.Query()
		q.Set("format", format)
		u.RawQuery = q.Encode()
	}
	validatedClient, err := newClient(client, network)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	resp, err := validatedClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Result{}, &HTTPStatusError{StatusCode: resp.StatusCode}
	}
	const cap = 5 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, cap+1))
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	finalURL := u.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	contentType := resp.Header.Get("Content-Type")
	res := Result{Output: string(body), Metadata: map[string]any{
		"content_type":     contentType,
		"final_url":        finalURL,
		"final_url_source": "response",
		"mode":             string(ModeHTTP),
	}}
	if len(body) > cap {
		res.Metadata["truncated"] = true
		res.Output = string(body[:cap])
	}
	if normalizeHTML && strings.Contains(strings.ToLower(contentType), "text/html") {
		document, extractErr := extractHTML(body, finalURL, format)
		if extractErr == nil {
			for key, value := range document.Metadata {
				res.Metadata[key] = value
			}
			res.Output = document.Output
		} else {
			res.Metadata["extraction_error"] = extractErr.Error()
		}
	}
	return res, nil
}

// HTTPStatusError records a non-success response so auto mode can decide
// whether a browser retry is appropriate without parsing an error string.
type HTTPStatusError struct {
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("webfetch: unexpected status %d", e.StatusCode)
}

func canFallbackToBrowser(err error) bool {
	var statusErr *HTTPStatusError
	return errors.As(err, &statusErr)
}

func needsBrowser(result Result) bool {
	needed, ok := result.Metadata["needs_browser"].(bool)
	return ok && needed
}

func readBrowser(ctx context.Context, options Options, reason string) (Result, error) {
	reader := options.Browser
	if reader == nil {
		browser := NewChromeBrowser()
		browser.network = normalizedNetwork(options.network)
		reader = browser
	}
	result, err := reader.Read(ctx, options.URL)
	if err != nil {
		if reason != "" {
			return Result{}, fmt.Errorf("webfetch: browser fallback after %s: %w", reason, err)
		}
		return Result{}, err
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["mode"] = string(ModeBrowser)
	if reason != "" {
		result.Metadata["browser_fallback"] = reason
	}
	return result, nil
}

func newClient(client *http.Client, network networkDeps) (*http.Client, error) {
	if client == nil {
		client = &http.Client{}
	}
	transport, err := validatedTransport(client.Transport, network)
	if err != nil {
		return nil, err
	}
	copy := *client
	previousRedirect := client.CheckRedirect
	copy.Transport = transport
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateURL(req.Context(), req.URL, network.resolver); err != nil {
			return fmt.Errorf("webfetch: redirect to local or private host is not allowed: %w", err)
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	return &copy, nil
}

func normalizedNetwork(network networkDeps) networkDeps {
	if network.resolver == nil || network.dial == nil {
		return defaultNetwork
	}
	return network
}

func parseHTTPURL(urlStr string) (*url.URL, error) {
	if len([]rune(urlStr)) > maxURLRunes {
		return nil, fmt.Errorf("webfetch: URL exceeds %d characters", maxURLRunes)
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("webfetch: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("webfetch: unsupported scheme %q", u.Scheme)
	}
	return u, nil
}

func validatedTransport(roundTripper http.RoundTripper, network networkDeps) (*http.Transport, error) {
	base := http.DefaultTransport.(*http.Transport)
	useBaseDial := false
	if roundTripper != nil {
		var ok bool
		base, ok = roundTripper.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("webfetch: custom transports are not supported")
		}
		useBaseDial = true
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if base.TLSClientConfig != nil {
		transport.TLSClientConfig = base.TLSClientConfig.Clone()
		transport.TLSClientConfig.InsecureSkipVerify = false
	}
	dial := network.dial
	if useBaseDial && base.DialContext != nil {
		dial = base.DialContext
	}
	if dial == nil {
		return nil, fmt.Errorf("webfetch: dialer is not configured")
	}
	// Proxies and custom TLS dialers could tunnel an unvalidated destination.
	transport.Proxy = nil
	transport.DialTLSContext = nil
	transport.DialContext = func(ctx context.Context, networkName, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("webfetch: invalid dial address %q: %w", address, err)
		}
		ips, err := lookupPublicIPs(ctx, host, network.resolver)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dial(ctx, networkName, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("webfetch: dial approved addresses for %q: %w", host, lastErr)
	}
	return transport, nil
}

func validateURL(ctx context.Context, u *url.URL, r resolver) error {
	if u == nil {
		return fmt.Errorf("webfetch: missing URL")
	}
	if _, err := lookupPublicIPs(ctx, u.Hostname(), r); err != nil {
		return err
	}
	return nil
}

func lookupPublicIPs(ctx context.Context, host string, r resolver) ([]net.IPAddr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("webfetch: URL host is required")
	}
	if strings.Contains(host, "%") {
		return nil, ErrUnsafeDestination
	}
	host = strings.TrimSuffix(host, ".")
	if isPrivateName(host) {
		return nil, ErrUnsafeDestination
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return nil, ErrUnsafeDestination
		}
		return []net.IPAddr{{IP: ip}}, nil
	}
	if r == nil {
		return nil, fmt.Errorf("webfetch: resolver is not configured")
	}
	ips, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("webfetch: resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("webfetch: resolve %q: no addresses", host)
	}
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, ErrUnsafeDestination
		}
	}
	return ips, nil
}

func isPrivateName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal"
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}
