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
	if err := Save(path, []Info{{ID: "deepseek-v4-flash", Context: 128000}, {ID: "claude-4", Context: 200000}}, now); err != nil {
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
	if !fresh {
		t.Error("fresh = false, want true within TTL")
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
	if models[0].Context != 1000000 || models[0].InputPerM != 0.14 || models[0].OutputPerM != 0.28 {
		t.Errorf("legacy enrich = %+v, want catalog context and prices", models[0])
	}
}

func TestEnrichFillsCatalog(t *testing.T) {
	got := Enrich(Info{ID: "deepseek-v4-flash"})
	if got.Context != 1000000 || got.InputPerM != 0.14 || got.OutputPerM != 0.28 {
		t.Fatalf("Enrich = %+v, want catalog row", got)
	}
	kept := Enrich(Info{ID: "deepseek-v4-flash", Context: 128000, InputPerM: 1, OutputPerM: 2})
	if kept.Context != 128000 || kept.InputPerM != 1 || kept.OutputPerM != 2 {
		t.Fatalf("Enrich overwrote live values: %+v", kept)
	}
}

func TestSaveWritesCatalogPrices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := Save(path, []Info{{ID: "deepseek-v4-flash"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{`"context": 1000000`, `"input_per_million": 0.14`, `"output_per_million": 0.28`} {
		if !strings.Contains(body, want) {
			t.Errorf("cache missing %s:\n%s", want, body)
		}
	}
}
