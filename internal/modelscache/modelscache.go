// Package modelscache caches the model list in the workspace dir so the TUI
// does not hit the models endpoint on every start (e.g. under nodemon-style
// restarts). The cache is refreshed by the API when stale or via the manual
// refresh command.
package modelscache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTTL is how long a cached model list stays fresh.
const DefaultTTL = 15 * time.Minute

// cacheMode is the mode of the cache file; the file may contain API
// responses, so it is not world-readable.
const cacheMode = 0o600

// Info is one cached model: context window, USD per million tokens,
// selectable reasoning variants, and the chat-completions endpoint to call.
type Info struct {
	ID             string   `json:"id"`
	Endpoint       string   `json:"endpoint,omitempty"`
	Context        int      `json:"context,omitempty"`
	InputPerM      float64  `json:"input_per_million"`
	OutputPerM     float64  `json:"output_per_million"`
	CacheReadPerM  float64  `json:"cache_read_per_million"`
	CacheWritePerM float64  `json:"cache_write_per_million"`
	Variants       []string `json:"variants,omitempty"`
	Free           bool     `json:"free,omitempty"`
}

// file is the on-disk cache shape.
type file struct {
	FetchedAt int64  `json:"fetched_at"`
	Models    []Info `json:"models"`
}

// Path returns the cache file path inside the workspace dir.
func Path(dir string) string {
	return filepath.Join(dir, "models.json")
}

// IDs extracts model ids from a list of Info rows.
func IDs(infos []Info) []string {
	out := make([]string, 0, len(infos))
	for _, m := range infos {
		out = append(out, m.ID)
	}
	return out
}

// HasContext reports whether any cached model has a context window.
func HasContext(infos []Info) bool {
	for _, m := range infos {
		if m.Context > 0 {
			return true
		}
	}
	return false
}

// ContextOf returns the cached context window for id, or 0 if unknown.
func ContextOf(infos []Info, id string) int {
	if info, ok := InfoOf(infos, id); ok {
		return info.Context
	}
	return 0
}

// EndpointOf returns the stored chat-completions URL for id, or "".
func EndpointOf(infos []Info, id string) string {
	if info, ok := InfoOf(infos, id); ok {
		return info.Endpoint
	}
	return ""
}

// InfoOf returns the cached row for id.
func InfoOf(infos []Info, id string) (Info, bool) {
	for _, m := range infos {
		if m.ID == id {
			return markFree(m), true
		}
	}
	return Info{}, false
}

func markFree(info Info) Info {
	if isFreeID(info.ID) {
		info.Free = true
	}
	return info
}

// IsFree reports whether the model is a free OpenCode model.
func IsFree(info Info) bool {
	return info.Free || isFreeID(info.ID)
}

func isFreeID(id string) bool {
	return strings.HasSuffix(id, "-free") || id == "big-pickle"
}

// HasVariant reports whether id lists variant as a selectable option.
func HasVariant(infos []Info, id, variant string) bool {
	info, ok := InfoOf(infos, id)
	if !ok || variant == "" {
		return false
	}
	for _, name := range info.Variants {
		if name == variant {
			return true
		}
	}
	return false
}

// MergeByID appends extras whose ids are not already in base.
func MergeByID(base, extra []Info) []Info {
	seen := make(map[string]struct{}, len(base))
	for _, m := range base {
		seen[m.ID] = struct{}{}
	}
	out := append([]Info(nil), base...)
	for _, m := range extra {
		if _, ok := seen[m.ID]; ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

// CostUSD estimates USD for a step from token counts and list prices.
// Cache hits use cache_read_per_million when known, otherwise input price.
func (info Info) CostUSD(input, output, cacheRead, cacheWrite int64) float64 {
	miss := input
	if cacheRead > 0 && input > cacheRead {
		miss = input - cacheRead
	} else if cacheRead > 0 && input == 0 {
		miss = 0
	}
	readPrice := info.CacheReadPerM
	if readPrice <= 0 {
		readPrice = info.InputPerM
	}
	const perMillion = 1_000_000
	return (float64(miss)/perMillion)*info.InputPerM +
		(float64(cacheRead)/perMillion)*readPrice +
		(float64(cacheWrite)/perMillion)*info.CacheWritePerM +
		(float64(output)/perMillion)*info.OutputPerM
}

// Load returns the cached model list. fresh is true when the cache is younger
// than ttl. A missing file returns nil models and no error, so callers fall
// through to the API. An older cache that stored models as a string array
// still loads.
func Load(path string, now time.Time, ttl time.Duration) (models []Info, fresh bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var f file
	if err := unmarshalCache(raw, &f); err != nil {
		return nil, false, err
	}
	if len(f.Models) == 0 {
		return nil, false, nil
	}
	for i := range f.Models {
		f.Models[i] = markFree(f.Models[i])
	}
	return f.Models, now.Sub(time.UnixMilli(f.FetchedAt)) < ttl, nil
}

func unmarshalCache(raw []byte, f *file) error {
	var wire struct {
		FetchedAt int64           `json:"fetched_at"`
		Models    json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	f.FetchedAt = wire.FetchedAt
	if len(wire.Models) == 0 {
		return nil
	}
	var infos []Info
	if err := json.Unmarshal(wire.Models, &infos); err == nil && len(infos) > 0 && infos[0].ID != "" {
		f.Models = infos
		return nil
	}
	var ids []string
	if err := json.Unmarshal(wire.Models, &ids); err != nil {
		return err
	}
	f.Models = make([]Info, 0, len(ids))
	for _, id := range ids {
		f.Models = append(f.Models, markFree(Info{ID: id}))
	}
	return nil
}

// Save writes the model list and fetch time to the cache file.
func Save(path string, models []Info, now time.Time) error {
	saved := make([]Info, len(models))
	for i, m := range models {
		saved[i] = markFree(m)
	}
	raw, err := json.MarshalIndent(file{FetchedAt: now.UnixMilli(), Models: saved}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, cacheMode)
}
