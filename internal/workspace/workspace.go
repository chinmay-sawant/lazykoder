package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

// Env holds the initialized workspace: the .lazykoder dir, the db path, and the open store.
type Env struct {
	Dir    string
	DBPath string
	DB     *db.Store
}

// Init creates <cwd>/.lazykoder (0755, exist-ok), opens and migrates the db, and appends ".lazykoder/" to <cwd>/.gitignore only if absent. Idempotent.
func Init(cwd string) (*Env, error) {
	dir := filepath.Join(cwd, ".lazykoder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	return &Env{Dir: dir, DBPath: dbPath, DB: store}, nil
}

func ensureGitignore(cwd string) error {
	path := filepath.Join(cwd, ".gitignore")
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(path, []byte(".lazykoder/\n"), 0o644)
	}
	if hasIgnoreLine(content) {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
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
