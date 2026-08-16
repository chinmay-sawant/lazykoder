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

func isPrivateHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") || strings.EqualFold(host, "metadata.google.internal") {
		return true
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}

// Run GETs an HTTP(S) URL, rejects private destinations for the default client,
// and caps the response body at 5MB. A custom client is intended for tests or
// callers with an explicit egress policy; redirects must still be validated by
// that caller.
func Run(ctx context.Context, urlStr, format string, client *http.Client) (Result, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return Result{}, fmt.Errorf("webfetch: unsupported scheme %q", u.Scheme)
	}
	if client == nil {
		client = http.DefaultClient
	}
	if isPrivateHost(u.Hostname()) && client == http.DefaultClient {
		return Result{}, fmt.Errorf("webfetch: local or private host is not allowed")
	}
	if format == "markdown" {
		q := u.Query()
		q.Set("format", format)
		u.RawQuery = q.Encode()
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
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
