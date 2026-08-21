package webfetch

import (
	"context"
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

const requestTimeout = 30 * time.Second

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

func run(ctx context.Context, urlStr, format string, client *http.Client, network networkDeps) (Result, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return Result{}, fmt.Errorf("webfetch: unsupported scheme %q", u.Scheme)
	}
	if err := validateURL(ctx, u, network.resolver); err != nil {
		return Result{}, err
	}
	if format == "markdown" {
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
		return Result{}, fmt.Errorf("webfetch: unexpected status %d", resp.StatusCode)
	}
	const cap = 5 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, cap+1))
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	res := Result{Output: string(body), Metadata: map[string]any{"content_type": resp.Header.Get("Content-Type")}}
	if len(body) > cap {
		res.Metadata["truncated"] = true
		res.Output = string(body[:cap])
	}
	return res, nil
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
	if isPrivateName(host) {
		return nil, fmt.Errorf("webfetch: local or private host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("webfetch: local or private host is not allowed")
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
			return nil, fmt.Errorf("webfetch: local or private host is not allowed")
		}
	}
	return ips, nil
}

func isPrivateName(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal"
}

func isPrivateIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}
