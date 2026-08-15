// Command lazykoder runs the OpenCode agent harness TUI.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/envfile"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/ui/chat"
	"github.com/chinmay-sawant/lazykoder/internal/workspace"
)

// defaultMaxSteps is the fallback when the user does not configure a step limit.
const defaultMaxSteps = 8

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", err)
		os.Exit(1)
	}
	env, err := workspace.Init(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", err)
		os.Exit(1)
	}
	defer func() { _ = env.DB.Close() }()

	if err := envfile.Load(filepath.Join(cwd, ".env")); err != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", err)
		os.Exit(1)
	}
	key, keyErr := opencode.APIKeyFromEnv()
	client := opencode.NewClient(key)
	initial := ""
	if keyErr != nil {
		initial = keyErr.Error()
	}

	var sess *db.Session
	sessions, err := env.DB.ListSessionsByDir(context.Background(), cwd)
	if err == nil && len(sessions) > 0 {
		sess = &sessions[0]
	}

	p := tea.NewProgram(chat.New(chat.Options{
		Store:      env.DB,
		Client:     client,
		Workdir:    env.Dir,
		Session:    sess,
		MaxSteps:   defaultMaxSteps,
		InitialErr: initial,
		CachePath:  filepath.Join(env.Dir, "models.json"),
	}))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", err)
		os.Exit(1)
	}
}
