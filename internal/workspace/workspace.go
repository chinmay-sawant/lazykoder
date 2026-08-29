package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

const (
	// dirMode is the mode of the .lazykoder workspace directory.
	dirMode = 0o755
	// gitignoreMode is the mode used when creating or appending the
	// project .gitignore.
	gitignoreMode = 0o600
	// catalogFileMode is the on-disk mode for project catalog JSON files.
	catalogFileMode = 0o600
)

// Env holds the initialized workspace directory and open store.
type Env struct {
	Dir     string
	DB      *db.Store
	Created []string
}

// Init creates <cwd>/.lazykoder (0755, exist-ok), opens and migrates the db, and appends ".lazykoder/" to <cwd>/.gitignore only if absent. Idempotent.
func Init(cwd string) (*Env, error) {
	dir := filepath.Join(cwd, ".lazykoder")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("workspace: mkdir %s: %w", dir, err)
	}
	dbPath := filepath.Join(dir, "lazykoder.db")
	store, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("workspace: open %s: %w", dbPath, err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		return nil, fmt.Errorf("workspace: migrate %s: %w", dbPath, err)
	}
	if err := ensureGitignore(cwd); err != nil {
		store.Close()
		return nil, fmt.Errorf("workspace: gitignore: %w", err)
	}
	created, err := ensureCatalogFiles(dir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("workspace: catalog bootstrap: %w", err)
	}
	return &Env{Dir: dir, DB: store, Created: created}, nil
}

const (
	providersFile = "providers.json"
	toolsFile     = "tools.json"
	rolesFile     = "roles.json"
)

// ensureCatalogFiles creates the project settings and catalog files without
// touching files that already exist. The returned paths are the files created
// by this call, which the explicit init command prints for the user.
func ensureCatalogFiles(dir string) ([]string, error) {
	files := []string{
		filepath.Join(dir, settings.FileName),
		filepath.Join(dir, providersFile),
		filepath.Join(dir, toolsFile),
		filepath.Join(dir, rolesFile),
	}
	created := make([]string, 0, len(files))
	for _, path := range files {
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if filepath.Base(path) == settings.FileName {
			if err := settings.Save(path, settings.Default()); err != nil {
				return nil, err
			}
		} else if filepath.Base(path) == providersFile {
			if err := os.WriteFile(path, defaultProvidersJSON(), catalogFileMode); err != nil {
				return nil, err
			}
		} else if err := os.WriteFile(path, []byte("[]\n"), catalogFileMode); err != nil {
			return nil, err
		}
		created = append(created, path)
	}
	return created, nil
}

func defaultProvidersJSON() []byte {
	return []byte(`[
  {
    "id": "opencode",
    "label": "OpenCode",
    "auth_method": "api_key",
    "env_keys": ["OPENCODE_API_KEY", "OPENCODE_ZEN_API_KEY"],
    "model": "deepseek-v4-flash",
    "supported": true,
    "display_order": 10,
    "aliases": ["opencode-go", "opencode-zen"]
  },
  {
    "id": "opencode-team",
    "label": "OpenCode Team",
    "auth_method": "api_key",
    "env_keys": ["OPENCODE_TEAM_API_KEY"],
    "base_url": "https://opencode.ai/zen/go/v1",
    "model": "deepseek-v4-flash",
    "supported": true,
    "display_order": 11
  },
  {
    "id": "openai",
    "label": "OpenAI",
    "auth_method": "api_key",
    "env_key": "OPENAI_API_KEY",
    "base_url": "https://api.openai.com/v1",
    "model": "gpt-4.1-mini",
    "supported": true,
    "display_order": 20,
    "aliases": ["openei", "open-ai"]
  },
  {
    "id": "grok",
    "label": "Grok",
    "auth_method": "grok",
    "cli": "grok",
    "env_keys": ["XAI_API_KEY"],
    "model": "grok-4.6",
    "supported": true,
    "display_order": 30
  },
  {
    "id": "codex",
    "label": "Codex",
    "auth_method": "codex",
    "cli": "codex",
    "env_keys": ["OPENAI_API_KEY"],
    "supported": true,
    "display_order": 40
  },
  {
    "id": "xai",
    "label": "xAI",
    "auth_method": "api_key",
    "env_key": "XAI_API_KEY",
    "base_url": "https://api.x.ai/v1",
    "model": "grok-4.6",
    "supported": true,
    "display_order": 50,
    "aliases": ["x-ai"]
  }
]
`)
}

func ensureGitignore(cwd string) error {
	path := filepath.Join(cwd, ".gitignore")
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(path, []byte(".lazykoder/\n"), gitignoreMode)
	}
	if hasIgnoreLine(content) {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, gitignoreMode)
	if err != nil {
		return err
	}
	defer f.Close()
	if !strings.HasSuffix(string(content), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(".lazykoder/\n")
	return err
}

func hasIgnoreLine(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == ".lazykoder/" {
			return true
		}
	}
	return false
}
