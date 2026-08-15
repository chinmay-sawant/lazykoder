package webfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result holds the fetched body and response metadata.
type Result struct {
	Output   string
	Metadata map[string]any
}

// Run GETs url (http/https only) and returns the body, capped at 5MB.
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
	if format == "markdown" {
		q := u.Query()
		q.Set("format", format)
		u.RawQuery = q.Encode()
	}
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
	res := Result{
		Output: string(body),
		Metadata: map[string]any{
			"content_type": resp.Header.Get("Content-Type"),
		},
	}
	if len(body) > cap {
		res.Metadata["truncated"] = true
		res.Output = string(body[:cap])
	}
	return res, nil
}
