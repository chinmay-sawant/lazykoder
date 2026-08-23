package modelscache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestParseModelsDevReadsPricesAndVariants(t *testing.T) {
	raw := []byte(`{
  "opencode": {
    "models": {
      "deepseek-v4-flash-free": {
        "id": "deepseek-v4-flash-free",
        "limit": {"context": 200000},
        "cost": {"input": 0, "output": 0, "cache_read": 0},
        "reasoning_options": [{"type": "effort", "values": ["low", "high", "max"]}]
      },
      "future-zen-responses-model": {
        "id": "future-zen-responses-model",
        "api_format": "responses",
        "limit": {"context": 100000},
        "cost": {"input": 1, "output": 2}
      }
    }
  },
  "opencode-go": {
    "models": {
      "glm-5.3": {
        "id": "glm-5.3",
        "limit": {"context": 1000000},
        "cost": {"input": 1.4, "output": 4.4, "cache_read": 0.26},
        "reasoning_options": [{"type": "effort", "values": ["low", "high", "max"]}]
      },
      "gpt-5.6-luna": {
        "id": "gpt-5.6-luna",
        "limit": {"context": 1050000},
        "cost": {"input": 0.2, "output": 1.2, "cache_read": 0.02, "cache_write": 0.25},
        "reasoning_options": [{"type": "effort", "values": ["none", "low", "medium", "high", "xhigh", "max"]}]
      },
      "future-responses-model": {
        "id": "future-responses-model",
        "api_format": "responses",
        "limit": {"context": 100000},
        "cost": {"input": 1, "output": 2}
      }
    }
  }
}`)
	got, err := ParseModelsDev(raw)
	if err != nil {
		t.Fatalf("ParseModelsDev: %v", err)
	}
	glm := got["glm-5.3"]
	if glm.Context != 1000000 || glm.InputPerM != 1.4 || glm.CacheReadPerM != 0.26 || glm.CacheWritePerM != 0 {
		t.Fatalf("glm-5.3 = %+v", glm)
	}
	if strings.Join(glm.Variants, ",") != "low,high,max" {
		t.Fatalf("glm-5.3 variants = %v, want low high max", glm.Variants)
	}
	luna := got["gpt-5.6-luna"]
	if luna.CacheWritePerM != 0.25 || !containsID(luna.Variants, "max") || !containsID(luna.Variants, "xhigh") {
		t.Fatalf("gpt-5.6-luna = %+v", luna)
	}
	if luna.Endpoint != opencode.RouteForModel(opencode.DefaultBaseURL, luna.ID).Endpoint {
		t.Fatalf("gpt-5.6-luna endpoint = %q", luna.Endpoint)
	}
	free := got["deepseek-v4-flash-free"]
	if !free.Free || !containsID(free.Variants, "max") {
		t.Fatalf("free model = %+v", free)
	}
	if free.Endpoint != opencode.RouteForModel(opencode.DefaultBaseURL, free.ID).Endpoint {
		t.Fatalf("free endpoint = %q, want zen chat URL", free.Endpoint)
	}
	if glm.Endpoint != opencode.RouteForModel(opencode.DefaultBaseURL, glm.ID).Endpoint {
		t.Fatalf("glm endpoint = %q, want go chat URL", glm.Endpoint)
	}
	if free.Provider != ProviderOpenCodeZen || glm.Provider != ProviderOpenCodeGo {
		t.Fatalf("providers = %q / %q", free.Provider, glm.Provider)
	}
	future := got["future-responses-model"]
	if future.Endpoint != opencode.ResponsesURL(opencode.DefaultBaseURL) {
		t.Fatalf("future endpoint = %q", future.Endpoint)
	}
	zenFuture := got["future-zen-responses-model"]
	if zenFuture.Endpoint != "https://opencode.ai/zen/v1/responses" || zenFuture.Provider != ProviderOpenCodeZen {
		t.Fatalf("zen future = %+v", zenFuture)
	}
}

func TestMergeLiveFillsMissingOnly(t *testing.T) {
	live := map[string]Info{
		"glm-5.3": {Context: 1000000, InputPerM: 1.4, OutputPerM: 4.4, CacheReadPerM: 0.26, Variants: []string{"low", "high", "max"}},
	}
	got := MergeLive(Info{ID: "glm-5.3", InputPerM: 9}, live)
	if got.InputPerM != 9 {
		t.Fatalf("overwrote live API input: %+v", got)
	}
	if got.CacheReadPerM != 0.26 || strings.Join(got.Variants, ",") != "low,high,max" {
		t.Fatalf("merge = %+v", got)
	}
}

func TestMergeLiveFillsMissingEndpoint(t *testing.T) {
	live := map[string]Info{
		"deepseek-v4-flash-free": {Endpoint: opencode.RouteForModel(opencode.DefaultBaseURL, "deepseek-v4-flash-free").Endpoint},
	}
	got := MergeLive(Info{ID: "deepseek-v4-flash-free"}, live)
	if got.Endpoint != opencode.RouteForModel(opencode.DefaultBaseURL, "deepseek-v4-flash-free").Endpoint {
		t.Fatalf("endpoint = %q", got.Endpoint)
	}
	kept := MergeLive(Info{ID: "deepseek-v4-flash-free", Endpoint: "https://custom.example/v1/chat/completions"}, live)
	if kept.Endpoint != "https://custom.example/v1/chat/completions" {
		t.Fatalf("overwrote endpoint: %q", kept.Endpoint)
	}
}

func TestPreserveSpecializedEndpoints(t *testing.T) {
	got := PreserveSpecializedEndpoints(
		[]Info{{ID: "future-model", Provider: ProviderOpenCodeGo, Endpoint: "https://example.test/chat/completions"}},
		[]Info{{ID: "future-model", Provider: ProviderOpenCodeGo, Endpoint: "https://example.test/responses"}},
	)
	if got[0].Endpoint != "https://example.test/responses" {
		t.Fatalf("endpoint = %q, want preserved responses route", got[0].Endpoint)
	}

	explicit := PreserveSpecializedEndpoints(
		[]Info{{ID: "future-model", Provider: ProviderOpenCodeGo, Endpoint: "https://example.test/responses"}},
		[]Info{{ID: "future-model", Provider: ProviderOpenCodeGo, Endpoint: "https://example.test/chat/completions"}},
	)
	if explicit[0].Endpoint != "https://example.test/responses" {
		t.Fatalf("explicit endpoint = %q", explicit[0].Endpoint)
	}
}

func TestSaveWritesZeroCacheWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := Save(path, []Info{{ID: "minimax-m3", CacheReadPerM: 0.06}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"cache_write_per_million": 0`) {
		t.Fatalf("missing zero cache write:\n%s", body)
	}
	if !strings.Contains(body, `"cache_read_per_million": 0.06`) {
		t.Fatalf("missing cache read:\n%s", body)
	}
}
