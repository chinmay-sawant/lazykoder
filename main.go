// Command lazykoder runs the OpenCode agent harness TUI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent/toolplugin"
	"github.com/chinmay-sawant/lazykoder/internal/envfile"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/roles"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	toolcatalog "github.com/chinmay-sawant/lazykoder/internal/tools"
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
	if len(os.Args) > 1 && os.Args[1] == "init" {
		for _, path := range env.Created {
			fmt.Fprintln(os.Stdout, path)
		}
		return
	}

	if err := envfile.Load(filepath.Join(cwd, ".env")); err != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", err)
		os.Exit(1)
	}
	settingsPath := settings.Path(env.Dir)
	_, _ = provider.LoadProviders(cwd)
	_, _ = roles.Load(cwd)
	cfg, err := settings.LoadFile(settingsPath)
	initial := ""
	if err != nil {
		// Non-fatal: fall back to defaults and surface the parse error.
		cfg = settings.Default()
		if initial == "" {
			initial = err.Error()
		}
	}
	if cfg.EffectiveTools().AllowDiscovered {
		if discovered, loadErr := toolcatalog.Load(cwd, true, true, cfg.EffectiveTools().MaxDiscovered); loadErr == nil {
			snapshot := make(map[string]toolplugin.Tool, len(discovered.Tools))
			for _, descriptor := range discovered.Tools {
				snapshot[descriptor.Name] = descriptor.Plugin()
			}
			_ = toolplugin.ReplaceDiscovered(snapshot)
		}
	}
	client, keyErr := provider.NewClient(cfg.EffectiveProvider())
	if client == nil {
		// Active provider was deleted from providers.json - fallback to first available
		ids := provider.IDs()
		fallback := provider.IDOpenCode
		if len(ids) > 0 {
			fallback = ids[0]
		}
		if fallback != cfg.EffectiveProvider() {
			// Try fallback without surfacing as keyErr so TUI still starts
			if fb, err := provider.NewClient(fallback); err == nil && fb != nil {
				client = fb
				if keyErr == nil {
					keyErr = fmt.Errorf("provider %q not found, using %q", cfg.EffectiveProvider(), fallback)
				}
			}
		}
	}
	childProvider := cfg.EffectiveOrchestrator().Provider
	childClient := client
	if childProvider != cfg.EffectiveProvider() {
		if cc, err := provider.NewClient(childProvider); err == nil && cc != nil {
			childClient = cc
		}
	}
	if keyErr != nil {
		initial = keyErr.Error()
	}
	retry := cfg.EffectiveRetry()
	if client != nil {
		client.SetRetryPolicy(opencode.RetryPolicy{
			MaxRetries: retry.MaxRetries,
			Delay:      time.Duration(retry.DelaySeconds) * time.Second,
		})
	}
	if childClient != nil && childClient != client {
		childClient.SetRetryPolicy(opencode.RetryPolicy{
			MaxRetries: retry.MaxRetries,
			Delay:      time.Duration(retry.DelaySeconds) * time.Second,
		})
	}
	if client == nil {
		initial = "no provider available: " + initial
		ids := provider.IDs()
		if len(ids) == 0 {
			fmt.Fprintln(os.Stderr, "lazykoder: no providers configured; add one via .lazykoder/providers.json")
			os.Exit(1)
		}
		if c, err := provider.NewClient(ids[0]); err == nil && c != nil {
			client = c
		}
		if client == nil {
			fmt.Fprintln(os.Stderr, "lazykoder: no providers configured")
			os.Exit(1)
		}
		childClient = client
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
	final, runErr := p.Run()
	if m, ok := final.(chat.Model); ok {
		m.Shutdown()
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "lazykoder:", runErr)
		os.Exit(1)
	}
	// Print after alt-screen teardown so the banner lands on the normal console.
	if m, ok := final.(chat.Model); ok {
		fmt.Fprint(os.Stdout, chat.FormatQuitBanner(m.SessionID(), m.SessionTitle()))
	}
}
