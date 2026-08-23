package modelscache

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// modelsDevURL is the live OpenCode catalog used to fill prices, cache
// rates, context windows, and reasoning variants. models.json is the
// source of truth after a refresh; this fetch only supplies missing
// fields from the provider catalog.
const modelsDevURL = "https://models.dev/api.json"

// maxCatalogBytes caps the models.dev payload.
const maxCatalogBytes = 16 << 20

// Fetch loads live model metadata from models.dev. Tests should call
// ParseModelsDev with a fixture instead of hitting the network.
func Fetch(ctx context.Context, hc *http.Client) (map[string]Info, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return nil, fmt.Errorf("modelscache: build catalog request: %w", err)
	}
	req.Header.Set("User-Agent", "lazykoder")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modelscache: catalog request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("modelscache: catalog request failed: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	if err != nil {
		return nil, fmt.Errorf("modelscache: read catalog: %w", err)
	}
	return ParseModelsDev(raw)
}

type liveProvider struct {
	Models map[string]liveModel `json:"models"`
}

type liveModel struct {
	ID               string             `json:"id"`
	Limit            liveLimit          `json:"limit"`
	Cost             *liveCost          `json:"cost"`
	ReasoningOptions []liveReasonOption `json:"reasoning_options"`
}

type liveLimit struct {
	Context int `json:"context"`
}

type liveCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type liveReasonOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

// ParseModelsDev extracts OpenCode Go and Zen model rows from a models.dev
// api.json payload. Go rows win when both providers list the same id.
func ParseModelsDev(raw []byte) (map[string]Info, error) {
	var wire map[string]liveProvider
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("modelscache: decode catalog: %w", err)
	}
	out := make(map[string]Info)
	for _, key := range []string{"opencode", "opencode-go"} {
		p, ok := wire[key]
		if !ok {
			continue
		}
		for id, m := range p.Models {
			if id == "" {
				id = m.ID
			}
			if id == "" {
				continue
			}
			out[id] = liveInfo(id, m, key)
		}
	}
	return out, nil
}

func liveInfo(id string, m liveModel, provider string) Info {
	info := Info{ID: id, Context: m.Limit.Context, Variants: effortVariants(m.ReasoningOptions)}
	if route, ok := opencode.RouteForCatalogModel(opencode.DefaultBaseURL, provider, id); ok {
		info.Endpoint = route.Endpoint
		info.Provider = route.Provider
	}
	if m.Cost != nil {
		info.InputPerM = m.Cost.Input
		info.OutputPerM = m.Cost.Output
		info.CacheReadPerM = m.Cost.CacheRead
		info.CacheWritePerM = m.Cost.CacheWrite
		if m.Cost.Input == 0 && m.Cost.Output == 0 && m.Cost.CacheRead == 0 && m.Cost.CacheWrite == 0 {
			info.Free = true
		}
	}
	if isFreeID(id) {
		info.Free = true
	}
	return info
}

func effortVariants(opts []liveReasonOption) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, opt := range opts {
		if opt.Type != "effort" {
			continue
		}
		for _, name := range opt.Values {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

// MergeLive copies missing context, prices, variants, endpoint, and
// provider from the live catalog row. Existing non-zero API values are kept.
func MergeLive(info Info, live map[string]Info) Info {
	fb, ok := live[info.ID]
	if !ok {
		if isFreeID(info.ID) {
			info.Free = true
		}
		return info
	}
	if info.Context <= 0 {
		info.Context = fb.Context
	}
	if info.InputPerM <= 0 {
		info.InputPerM = fb.InputPerM
	}
	if info.OutputPerM <= 0 {
		info.OutputPerM = fb.OutputPerM
	}
	if info.CacheReadPerM <= 0 {
		info.CacheReadPerM = fb.CacheReadPerM
	}
	if info.CacheWritePerM <= 0 {
		info.CacheWritePerM = fb.CacheWritePerM
	}
	if len(info.Variants) == 0 && len(fb.Variants) > 0 {
		info.Variants = append([]string(nil), fb.Variants...)
	}
	if info.Endpoint == "" && fb.Endpoint != "" {
		info.Endpoint = fb.Endpoint
	}
	if info.Provider == "" && fb.Provider != "" {
		info.Provider = fb.Provider
	}
	if fb.Free || isFreeID(info.ID) {
		info.Free = true
	}
	return info
}

// ApplyLive merges live catalog rows into each cached model.
func ApplyLive(infos []Info, live map[string]Info) []Info {
	if len(live) == 0 {
		return infos
	}
	out := make([]Info, len(infos))
	for i, info := range infos {
		out[i] = MergeLive(info, live)
	}
	return out
}
