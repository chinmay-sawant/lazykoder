package modelscache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	now := time.Now()
	if err := Save(path, []string{"deepseek-v4-flash", "claude-4"}, now); err != nil {
		t.Fatalf("Save: %v", err)
	}
	models, fresh, err := Load(path, now.Add(time.Minute), DefaultTTL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(models) != 2 || models[0] != "deepseek-v4-flash" || models[1] != "claude-4" {
		t.Errorf("models = %v, want the saved list", models)
	}
	if !fresh {
		t.Error("fresh = false, want true within TTL")
	}
}

func TestLoadStaleBeyondTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	now := time.Now()
	if err := Save(path, []string{"deepseek-v4-flash"}, now); err != nil {
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
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := Load(path, time.Now(), DefaultTTL); err == nil {
		t.Error("Load corrupt: want error, got nil")
	}
}
