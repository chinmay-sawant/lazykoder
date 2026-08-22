// Command lazykoder runs the OpenCode agent harness TUI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/envfile"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/ui/chat"
	"github.com/chinmay-sawant/lazykoder/internal/workspace"
)

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
	initial := ""
	if keyErr != nil {
		initial = keyErr.Error()
	}

	settingsPath := settings.Path(env.Dir)
	cfg, err := settings.LoadFile(settingsPath)
	if err != nil {
		// Non-fatal: fall back to defaults and surface the parse error.
		cfg = settings.Default()
		if initial == "" {
			initial = err.Error()
		}
	}
	retry := cfg.EffectiveRetry()
	client := opencode.NewClient(key, opencode.WithRetryPolicy(opencode.RetryPolicy{
		MaxRetries: retry.MaxRetries,
		Delay:      time.Duration(retry.DelaySeconds) * time.Second,
	}))

	// Always start fresh. Past runs stay in SQLite and load via /resume.
	p := tea.NewProgram(chat.New(chat.Options{
		Store:        env.DB,
		Client:       client,
		Workdir:      cwd,
		MaxSteps:     cfg.EffectiveMaxSteps(),
		InitialErr:   initial,
		CachePath:    filepath.Join(env.Dir, "models.json"),
		SettingsPath: settingsPath,
		Settings:     &cfg,
	}))
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", err)
		os.Exit(1)
	}
	// Print after alt-screen teardown so the banner lands on the normal console.
	if m, ok := final.(chat.Model); ok {
		fmt.Fprint(os.Stdout, chat.FormatQuitBanner(m.SessionID(), m.SessionTitle()))
	}
}
