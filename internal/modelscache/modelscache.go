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

// file is the on-disk cache shape.
type file struct {
	FetchedAt int64    `json:"fetched_at"`
	Models    []string `json:"models"`
}

// Path returns the cache file path inside the workspace dir.
func Path(dir string) string {
	return filepath.Join(dir, "models.json")
}

// Load returns the cached model list. fresh is true when the cache is younger
// than ttl. A missing file returns nil models and no error, so callers fall
// through to the API.
func Load(path string, now time.Time, ttl time.Duration) (models []string, fresh bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, false, err
	}
	if len(f.Models) == 0 {
		return nil, false, nil
	}
	return f.Models, now.Sub(time.UnixMilli(f.FetchedAt)) < ttl, nil
}

// Save writes the model list and fetch time to the cache file.
func Save(path string, models []string, now time.Time) error {
	raw, err := json.Marshal(file{FetchedAt: now.UnixMilli(), Models: models})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, cacheMode)
}
