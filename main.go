// Command lazykoder runs the OpenCode agent harness TUI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/envfile"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
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
	settingsPath := settings.Path(env.Dir)
	cfg, err := settings.LoadFile(settingsPath)
	initial := ""
	if err != nil {
		// Non-fatal: fall back to defaults and surface the parse error.
		cfg = settings.Default()
		if initial == "" {
			initial = err.Error()
		}
	}
	client, keyErr := provider.NewClient(cfg.EffectiveProvider())
	childProvider := cfg.EffectiveOrchestrator().Provider
	childClient := client
	if childProvider != cfg.EffectiveProvider() {
		childClient, _ = provider.NewClient(childProvider)
	}
	if keyErr != nil {
		initial = keyErr.Error()
	}
	retry := cfg.EffectiveRetry()
	client.SetRetryPolicy(opencode.RetryPolicy{
		MaxRetries: retry.MaxRetries,
		Delay:      time.Duration(retry.DelaySeconds) * time.Second,
	})
	if childClient != client {
		childClient.SetRetryPolicy(opencode.RetryPolicy{
			MaxRetries: retry.MaxRetries,
			Delay:      time.Duration(retry.DelaySeconds) * time.Second,
		})
	}

	// Always start fresh. Past runs stay in SQLite and load via /resume.
	p := tea.NewProgram(chat.New(chat.Options{
		Store:             env.DB,
		Client:            client,
		ChildClient:       childClient,
		NewProviderClient: provider.NewClient,
		Workdir:           cwd,
		MaxSteps:          cfg.EffectiveMaxSteps(),
		InitialErr:        initial,
		CachePath:         filepath.Join(env.Dir, "models.json"),
		SettingsPath:      settingsPath,
		Settings:          &cfg,
		WorktreeDirty:     chat.DefaultWorktreeDirty,
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
