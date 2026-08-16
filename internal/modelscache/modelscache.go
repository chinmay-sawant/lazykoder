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
	"time"
)

// DefaultTTL is how long a cached model list stays fresh.
const DefaultTTL = 15 * time.Minute

// cacheMode is the mode of the cache file; the file may contain API
// responses, so it is not world-readable.
const cacheMode = 0o600

// Info is one cached model: context window and USD per million tokens.
type Info struct {
	ID         string  `json:"id"`
	Context    int     `json:"context,omitempty"`
	InputPerM  float64 `json:"input_per_million,omitempty"`
	OutputPerM float64 `json:"output_per_million,omitempty"`
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
	return Lookup(id).Context
}

// InfoOf returns the cached row for id.
func InfoOf(infos []Info, id string) (Info, bool) {
	for _, m := range infos {
		if m.ID == id {
			return Enrich(m), true
		}
	}
	fb := Lookup(id)
	if fb.Context > 0 || fb.InputPerM > 0 {
		fb.ID = id
		return fb, true
	}
	return Info{}, false
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
		f.Models[i] = Enrich(f.Models[i])
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
		f.Models = append(f.Models, Enrich(Info{ID: id}))
	}
	return nil
}

// Save writes the model list and fetch time to the cache file.
func Save(path string, models []Info, now time.Time) error {
	enriched := make([]Info, len(models))
	for i, m := range models {
		enriched[i] = Enrich(m)
	}
	raw, err := json.MarshalIndent(file{FetchedAt: now.UnixMilli(), Models: enriched}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, cacheMode)
}
