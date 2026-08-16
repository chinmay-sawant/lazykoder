package modelscache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	now := time.Now()
	if err := Save(path, []Info{
		{ID: "deepseek-v4-flash", Endpoint: "https://opencode.ai/zen/go/v1/chat/completions", Context: 128000},
		{ID: "claude-4", Context: 200000},
	}, now); err != nil {
		t.Fatalf("Save: %v", err)
	}
	models, fresh, err := Load(path, now.Add(time.Minute), DefaultTTL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(models) != 2 || models[0].ID != "deepseek-v4-flash" || models[1].ID != "claude-4" {
		t.Errorf("models = %v, want the saved list", models)
	}
	if models[0].Context != 128000 {
		t.Errorf("context = %d, want 128000", models[0].Context)
	}
	if models[0].Endpoint != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Errorf("endpoint = %q", models[0].Endpoint)
	}
	if EndpointOf(models, "deepseek-v4-flash") == "" || EndpointOf(models, "missing") != "" {
		t.Errorf("EndpointOf = %q / %q", EndpointOf(models, "deepseek-v4-flash"), EndpointOf(models, "missing"))
	}
	if !fresh {
		t.Error("fresh = false, want true within TTL")
	}
}

func TestProviderFromEndpoint(t *testing.T) {
	if got := ProviderFromEndpoint("https://opencode.ai/zen/go/v1/chat/completions", "deepseek-v4-flash"); got != ProviderOpenCodeGo {
		t.Fatalf("go = %q", got)
	}
	if got := ProviderFromEndpoint("https://opencode.ai/zen/v1/chat/completions", "deepseek-v4-flash-free"); got != ProviderOpenCodeZen {
		t.Fatalf("zen = %q", got)
	}
	if got := ProviderOf(nil, "deepseek-v4-flash-free"); got != ProviderOpenCodeZen {
		t.Fatalf("free fallback = %q", got)
	}
}

func TestLoadStaleBeyondTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	now := time.Now()
	if err := Save(path, []Info{{ID: "deepseek-v4-flash"}}, now); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, fresh, err := Load(path, now.Add(DefaultTTL+time.Second), DefaultTTL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fresh {
		t.Error("fresh = true, want false beyond TTL")
	}
}

func TestLoadMissingFile(t *testing.T) {
	models, fresh, err := Load(filepath.Join(t.TempDir(), "nope.json"), time.Now(), DefaultTTL)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if models != nil || fresh {
		t.Errorf("missing file: models=%v fresh=%v, want nil/false and no error", models, fresh)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := Load(path, time.Now(), DefaultTTL); err == nil {
		t.Error("Load corrupt: want error, got nil")
	}
}

func TestHasContext(t *testing.T) {
	if HasContext([]Info{{ID: "a"}}) {
		t.Fatal("HasContext true without windows")
	}
	if !HasContext([]Info{{ID: "a", Context: 1000}}) {
		t.Fatal("HasContext false with a window")
	}
}

func TestLoadLegacyStringArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	raw := `{"fetched_at":1,"models":["deepseek-v4-flash","claude-4"]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	models, _, err := Load(path, time.UnixMilli(1), DefaultTTL)
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if len(models) != 2 || models[0].ID != "deepseek-v4-flash" || models[1].ID != "claude-4" {
		t.Errorf("legacy models = %+v", models)
	}
	if models[0].ID != "deepseek-v4-flash" || models[0].Context != 0 {
		t.Errorf("legacy models = %+v, want ids only", models[0])
	}
}

func TestSaveKeepsProvidedPrices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := Save(path, []Info{{ID: "deepseek-v4-flash", Context: 1000000, InputPerM: 0.14, OutputPerM: 0.28, CacheReadPerM: 0.0028, CacheWritePerM: 0}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{`"context": 1000000`, `"input_per_million": 0.14`, `"output_per_million": 0.28`, `"cache_read_per_million": 0.0028`, `"cache_write_per_million": 0`} {
		if !strings.Contains(body, want) {
			t.Errorf("cache missing %s:\n%s", want, body)
		}
	}
}

func TestCostUSDUsesCacheReadPrice(t *testing.T) {
	info := Info{InputPerM: 0.14, OutputPerM: 0.28, CacheReadPerM: 0.0028}
	got := info.CostUSD(1_000_000, 0, 900_000, 0)
	// 100k uncached at 0.14 + 900k cache read at 0.0028 = 0.014 + 0.00252
	want := 0.01652
	if got < want-0.00001 || got > want+0.00001 {
		t.Fatalf("CostUSD = %v, want %v", got, want)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
