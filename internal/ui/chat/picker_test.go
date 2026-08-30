package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/provider/subscription"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

func TestModelSettingsOpenOnlyFromModelClick(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})

	m = upd(m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.pickerMode {
		t.Fatal("pressing m opened the model picker")
	}
	if got := m.prompt.Value(); got != "m" {
		t.Fatalf("prompt after m = %q, want %q", got, "m")
	}

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	left, top, right, _, ok := m.modelStatusRect()
	if !ok {
		t.Fatal("model status chip is not clickable")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (left + right) / 2, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.settingsMode || m.pickerMode || m.settingsCurrentSection() != settingsSectionModel || m.settingsCursor != settingsRowModel {
		t.Fatalf("model status click = settings=%v picker=%v section=%d row=%d", m.settingsMode, m.pickerMode, m.settingsCurrentSection(), m.settingsCursor)
	}
}

func TestCodexCatalogDefaultsToLunaLow(t *testing.T) {
	client := subscription.NewCodex("", subscription.WithCatalogLoader(func(context.Context) (subscription.ModelCatalog, error) {
		return subscription.ModelCatalog{
			Default:        "gpt-5.6-luna",
			DefaultVariant: "low",
			Models: []opencode.ModelInfo{{
				ID:       "gpt-5.6-luna",
				Provider: provider.IDCodex,
				Variants: []string{"low", "high"},
			}},
		}, nil
	}))
	infos, err := client.ModelInfos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Default()
	cfg.Provider.Active = provider.IDCodex
	cfg.Model.Default = ""
	cfg.Model.Variant = ""
	m := New(Options{Store: newTestStore(t), Client: client, Settings: &cfg, Workdir: t.TempDir()})
	next, _ := m.Update(modelsMsg{
		list:  modelscache.IDs(toCacheInfos(infos)),
		infos: toCacheInfos(infos),
		defaults: map[string]modelDefault{
			provider.IDCodex: {model: client.Model(), variant: client.DefaultVariant()},
		},
	})
	m = next.(Model)
	if m.model != "gpt-5.6-luna" || m.variant != "low" {
		t.Fatalf("selected model = %q variant = %q, want gpt-5.6-luna with low", m.model, m.variant)
	}
	if m.projectSettings.Model.Default != "gpt-5.6-luna" || m.projectSettings.Model.Variant != "low" {
		t.Fatalf("saved defaults = %+v", m.projectSettings.Model)
	}
}

func TestDefaultVariantUsesFirstSupportedVariant(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.modelInfos = []modelscache.Info{{
		ID:       "model-with-variants",
		Provider: provider.IDOpenCode,
		Variants: []string{"medium", "high"},
	}}
	if got := m.effectiveVariantFor("model-with-variants", provider.IDOpenCode, ""); got != "medium" {
		t.Fatalf("default variant = %q, want first supported variant medium", got)
	}
	if got := m.effectiveVariantFor("model-with-variants", provider.IDOpenCode, "high"); got != "high" {
		t.Fatalf("selected variant = %q, want high", got)
	}
}

func TestModelPickerGroupsAndRoutesCrossProviderSelection(t *testing.T) {
	openCodeClient := deadClient()
	codexClient := subscription.NewCodex("gpt-5.6-luna")
	cfg := settings.Default()
	m := New(Options{
		Store:    newTestStore(t),
		Client:   openCodeClient,
		Settings: &cfg,
		Workdir:  t.TempDir(),
		NewProviderClient: func(id string) (provider.Client, error) {
			switch id {
			case provider.IDCodex:
				return codexClient, nil
			case provider.IDOpenCode:
				return openCodeClient, nil
			default:
				return nil, nil
			}
		},
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.models = []string{"deepseek-v4-flash", "gpt-5.6-luna", "gpt-5.6-luna"}
	m.modelInfos = []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo},
		{ID: "gpt-5.6-luna", Provider: modelscache.ProviderOpenCodeGo},
		{ID: "gpt-5.6-luna", Provider: provider.IDCodex, Variants: []string{"low", "high"}},
	}
	m.modelDefaults = map[string]modelDefault{
		provider.IDCodex: {model: "gpt-5.6-luna", variant: "low"},
	}
	m = m.openKindPicker(pickerKindModel)
	content := stripANSI(m.pickerContent(m.pickerVp.Width()))
	if !strings.Contains(content, "OpenCode") || !strings.Contains(content, "Codex") {
		t.Fatalf("group headings missing: %q", content)
	}
	if got := strings.Count(content, "gpt-5.6-luna"); got != 2 {
		t.Fatalf("shared model rows = %d, want 2: %q", got, content)
	}
	headingY := m.pickerDrawerTop() + 1
	if _, ok := m.pickerIndexAtScreenY(headingY); ok {
		t.Fatalf("provider heading at y=%d must not be selectable", headingY)
	}
	if index, ok := m.pickerIndexAtScreenY(headingY + 1); !ok || index != 0 {
		t.Fatalf("first model row = index:%d ok:%t, want 0", index, ok)
	}

	m, _ = m.selectPickerItem(2)
	if m.projectSettings.EffectiveProvider() != provider.IDCodex {
		t.Fatalf("provider = %q, want codex", m.projectSettings.EffectiveProvider())
	}
	if m.client != codexClient {
		t.Fatalf("client was not switched to Codex: %T", m.client)
	}
	if m.model != "gpt-5.6-luna" || m.variant != "low" {
		t.Fatalf("selected model = %q variant = %q", m.model, m.variant)
	}

	m = m.openKindPicker(pickerKindModel)
	m, _ = m.selectPickerItem(1)
	if m.projectSettings.EffectiveProvider() != provider.IDOpenCode {
		t.Fatalf("provider after returning to OpenCode = %q", m.projectSettings.EffectiveProvider())
	}
	if m.client != openCodeClient {
		t.Fatalf("client was not switched back to OpenCode: %T", m.client)
	}
	if m.model != "gpt-5.6-luna" {
		t.Fatalf("OpenCode model = %q", m.model)
	}
}

func TestPromptModelPickerArrowMovesAcrossProviderGroups(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.models = []string{"gpt-5.6-luna", "gpt-5.6-luna"}
	m.modelInfos = []modelscache.Info{
		{ID: "gpt-5.6-luna", Provider: modelscache.ProviderOpenCodeGo},
		{ID: "gpt-5.6-luna", Provider: provider.IDCodex},
	}
	m = m.syncSlash("/model luna")
	if !m.pickerMode || !m.pickerFromPrompt {
		t.Fatalf("picker state = mode=%v fromPrompt=%v", m.pickerMode, m.pickerFromPrompt)
	}
	if m.pickerCursor != 0 {
		t.Fatalf("initial picker cursor = %d, want 0", m.pickerCursor)
	}

	next, _ := m.updatePickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if next.pickerCursor != 1 {
		t.Fatalf("down cursor = %d, want 1 for the Codex row", next.pickerCursor)
	}
}

func TestPromptModelPickerKeypadDownMovesAcrossProviderGroups(t *testing.T) {
	m := New(Options{Client: deadClient(), Workdir: t.TempDir()})
	m.models = []string{"gpt-5.6-luna", "gpt-5.6-luna"}
	m.modelInfos = []modelscache.Info{
		{ID: "gpt-5.6-luna", Provider: modelscache.ProviderOpenCodeGo},
		{ID: "gpt-5.6-luna", Provider: provider.IDCodex},
	}
	m = m.syncSlash("/model luna")

	next, _ := m.updatePickerKey(tea.KeyPressMsg{Code: tea.KeyKpDown})
	if next.pickerCursor != 1 {
		t.Fatalf("keypad down cursor = %d, want 1 for the Codex row", next.pickerCursor)
	}
}

func TestLoadSessionRestoresSessionProviderClient(t *testing.T) {
	openCodeClient := deadClient()
	codexClient := subscription.NewCodex("gpt-5.6-luna")
	m := New(Options{
		Client: openCodeClient,
		Settings: func() *settings.Settings {
			cfg := settings.Default()
			cfg.Provider.Active = provider.IDOpenCode
			return &cfg
		}(),
		NewProviderClient: func(id string) (provider.Client, error) {
			if id == provider.IDCodex {
				return codexClient, nil
			}
			return openCodeClient, nil
		},
		Workdir: t.TempDir(),
	})
	sess := db.Session{ID: "session-codex", Provider: provider.IDCodex, Model: "gpt-5.6-luna"}

	m = m.loadSession(&sess)
	if m.projectSettings.EffectiveProvider() != provider.IDCodex {
		t.Fatalf("provider after resume = %q, want codex", m.projectSettings.EffectiveProvider())
	}
	if m.client != codexClient {
		t.Fatalf("client after resume = %T, want Codex client", m.client)
	}
}

func TestSubagentProfilesUseTheConfiguredChildProvider(t *testing.T) {
	cfg := settings.Default()
	cfg.Orchestrator.Provider = provider.IDOpenCode
	m := New(Options{Client: deadClient(), Settings: &cfg, Workdir: t.TempDir()})
	m.modelInfos = []modelscache.Info{
		{ID: "gpt-5.6-luna", Provider: provider.IDCodex, Endpoint: "cli://codex/chat/completions"},
		{ID: "gpt-5.6-luna", Provider: modelscache.ProviderOpenCodeGo, Endpoint: "https://opencode.ai/zen/go/v1/chat/completions"},
	}

	profiles := m.subagentModelProfiles()
	if len(profiles) != 1 {
		t.Fatalf("profiles = %+v, want one OpenCode row", profiles)
	}
	if profiles[0].ID != "gpt-5.6-luna" || profiles[0].Endpoint != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("profile = %+v, want OpenCode Luna endpoint", profiles[0])
	}
}

func TestMergeKeepsSharedModelIDsAcrossProviders(t *testing.T) {
	infos := modelscache.MergeByID(
		[]modelscache.Info{{ID: "gpt-5.6-luna", Provider: provider.IDCodex}},
		[]modelscache.Info{{ID: "gpt-5.6-luna", Provider: modelscache.ProviderOpenCodeGo}},
	)
	if len(infos) != 2 {
		t.Fatalf("shared model rows = %d, want 2", len(infos))
	}
	if got := providerIDForModelInfo(infos[0]); got != provider.IDCodex {
		t.Fatalf("shared model provider = %q, want codex", got)
	}
	if got := providerIDForModelInfo(infos[1]); got != provider.IDOpenCode {
		t.Fatalf("shared model provider = %q, want opencode", got)
	}
}

func TestModelPickerGroupsGrokCatalogRows(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.models = []string{"grok-4.6", "grok-4.5"}
	m.modelInfos = []modelscache.Info{
		{ID: "grok-4.6", Provider: provider.IDGrok, Endpoint: "cli://grok/chat/completions"},
		{ID: "grok-4.5", Provider: provider.IDGrok, Endpoint: "cli://grok/chat/completions"},
	}
	m = m.openKindPicker(pickerKindModel)
	content := stripANSI(m.pickerContent(m.pickerVp.Width()))
	if !strings.Contains(content, "Grok") {
		t.Fatalf("Grok heading missing: %q", content)
	}
	if !strings.Contains(content, "grok-4.6") || !strings.Contains(content, "grok-4.5") {
		t.Fatalf("Grok rows missing: %q", content)
	}
	if got := providerIDForModelInfo(m.modelInfos[0]); got != provider.IDGrok {
		t.Fatalf("Grok provider = %q, want %q", got, provider.IDGrok)
	}
}

func TestGrokVariantPickerDisplaysReasoningEfforts(t *testing.T) {
	cfg := settings.Default()
	cfg.Provider.Active = provider.IDGrok
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Settings: &cfg, Workdir: t.TempDir()})
	m.model = "grok-4.6"
	m.modelInfos = []modelscache.Info{{
		ID:       "grok-4.6",
		Provider: provider.IDGrok,
		Endpoint: "cli://grok/chat/completions",
		Variants: []string{"low", "medium", "high", "xhigh"},
	}}
	m = m.openKindPicker(pickerKindVariant)
	content := stripANSI(m.pickerContent(m.pickerVp.Width()))
	for _, effort := range []string{"low", "medium", "high", "xhigh"} {
		if !strings.Contains(content, effort) {
			t.Fatalf("Grok variant picker missing %q: %q", effort, content)
		}
	}
}

func TestModelPickerSwitchAndPersist(t *testing.T) {
	tmp := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "s", Directory: tmp})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hello", "stop", nil))
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp, Session: &sess})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "deepseek-v4-flash") || !strings.Contains(v, "claude-4") {
		t.Fatalf("picker missing models: %q", v)
	}
	if !strings.Contains(v, "filter /") {
		t.Fatalf("picker missing filter prompt: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'j'})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.runStep(m, p.next())

	msgs, err := st.ListSessionsByDir(context.Background(), tmp)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Model != "claude-4" {
		t.Errorf("session model = %+v, want claude-4", msgs)
	}

	m = typeText(m, "hi there")
	m, cmd = updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	p.drainIdle(m)
	if got := fake.requestModel(0); got != "claude-4" {
		t.Errorf("wire model = %q, want claude-4", got)
	}
}

func TestModelPickerCancel(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "no models loaded") || !strings.Contains(v, "esc cancel") {
		t.Fatalf("picker not shown: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	v = stripANSI(viewText(m))
	if strings.Contains(v, "esc cancel") {
		t.Errorf("picker still shown after esc: %q", v)
	}
	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: 'q'})
	v = stripANSI(viewText(m))
	if strings.Contains(v, "esc cancel") {
		t.Errorf("picker still shown after q: %q", v)
	}
}

func TestModelPickerFilter(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: '/'})
	for _, r := range "claude" {
		m = upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	v := stripANSI(viewText(m))
	if len(m.pickerItems) != 1 || m.pickerItems[0] != "claude-4" {
		t.Errorf("filtered picker items = %v, want [claude-4]", m.pickerItems)
	}
	if !strings.Contains(v, "models  ·") || !strings.Contains(v, "claude-4") {
		t.Errorf("matching model missing: %q", v)
	}
	if !strings.Contains(v, "filter: claude") {
		t.Errorf("filter prompt missing query: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "claude-4") {
		t.Errorf("filter exit lost the list: %q", v)
	}
	if !strings.Contains(v, "filter: claude") {
		t.Errorf("active filter query not shown after exit: %q", v)
	}
}

func TestModelPickerFilterFree(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.models = []string{"deepseek-v4-flash", "deepseek-v4-flash-free", "big-pickle"}
	m.modelInfos = []modelscache.Info{
		{ID: "deepseek-v4-flash"},
		{ID: "deepseek-v4-flash-free", Free: true},
		{ID: "big-pickle", Free: true},
	}

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: '/'})
	for _, r := range "free" {
		m = upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !containsModel(m.pickerItems, "deepseek-v4-flash-free") || !containsModel(m.pickerItems, "big-pickle") {
		t.Fatalf("free filter items = %v", m.pickerItems)
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "big-pickle  free") {
		t.Fatalf("free label missing: %q", v)
	}
	if !strings.Contains(v, modelscache.ProviderOpenCodeZen) {
		t.Fatalf("zen provider missing on the right: %q", v)
	}
}

func TestModelPickerShowsProviderOnRight(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.models = []string{"deepseek-v4-flash", "deepseek-v4-flash-free"}
	m.modelInfos = []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo, Endpoint: "https://opencode.ai/zen/go/v1/chat/completions"},
		{ID: "deepseek-v4-flash-free", Provider: modelscache.ProviderOpenCodeZen, Endpoint: "https://opencode.ai/zen/v1/chat/completions", Free: true},
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	m = clickModelStatus(t, m)
	row := ""
	for _, line := range strings.Split(stripANSI(m.pickerView()), "\n") {
		if strings.Contains(line, "deepseek-v4-flash-free") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("free model row missing")
	}
	nameAt := strings.Index(row, "deepseek-v4-flash-free")
	provAt := strings.Index(row, modelscache.ProviderOpenCodeZen)
	if provAt < 0 || provAt < nameAt {
		t.Fatalf("provider should sit to the right of the model name: %q", row)
	}
}

func typeRune(m Model, r rune) Model {
	return upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

func TestPickerDrawerSitsAbovePrompt(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "models  ·") || !strings.Contains(v, "deepseek-v4-flash") {
		t.Fatalf("drawer labels missing: %q", v)
	}
	if !strings.Contains(v, "lazykoder") || !strings.Contains(v, "enter send") {
		t.Fatalf("chat chrome missing under the drawer: %q", v)
	}
	drawer := stripANSI(m.pickerView())
	if strings.Contains(drawer, "╭") {
		t.Fatalf("drawer should not use a centered card border: %q", drawer)
	}
	if got, want := lipgloss.Width(drawer), max(minPaneWidth, m.width-cardBorder); got != want {
		t.Errorf("drawer width = %d, want %d", got, want)
	}
	lines := strings.Split(v, "\n")
	drawerLine, promptLine := -1, -1
	for i, line := range lines {
		if drawerLine < 0 && strings.Contains(line, "models  ·") {
			drawerLine = i
		}
		if strings.Contains(line, "ask lazykoder") || strings.Contains(line, "enter send") {
			promptLine = i
		}
	}
	if drawerLine < 0 || promptLine < 0 || drawerLine >= promptLine {
		t.Fatalf("drawer should sit above the prompt: drawer=%d prompt=%d\n%s", drawerLine, promptLine, v)
	}
}

func TestPickerClosesWithEscAndQ(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.pickerMode {
		t.Fatal("esc did not close the picker drawer")
	}

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: 'x'})
	if m.pickerMode {
		t.Fatal("x did not close the picker drawer")
	}
}

func TestPickerArrowKeysRefreshSelectionAndScroll(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())
	m.models = make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		m.models = append(m.models, fmt.Sprintf("model-%02d", i))
	}

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.pickerCursor != 1 {
		t.Fatalf("down cursor = %d, want 1", m.pickerCursor)
	}
	v := stripANSI(m.pickerView())
	if !strings.Contains(v, "▸ model-01") || strings.Contains(v, "▸ model-00") {
		t.Fatalf("down did not refresh the visible selection: %q", v)
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.pickerCursor != 0 {
		t.Fatalf("up cursor = %d, want 0", m.pickerCursor)
	}
	v = stripANSI(m.pickerView())
	if !strings.Contains(v, "▸ model-00") || strings.Contains(v, "▸ model-01") {
		t.Fatalf("up did not refresh the visible selection: %q", v)
	}
	if p := m.pickerVp.ScrollPercent(); p != 0 {
		t.Errorf("scroll percent at cursor 0 = %v, want 0", p)
	}

	for i := 0; i < 12; i++ {
		m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.pickerCursor != 12 {
		t.Fatalf("scrolled cursor = %d, want 12", m.pickerCursor)
	}
	v = stripANSI(m.pickerView())
	if !strings.Contains(v, "▸ model-12") {
		t.Fatalf("scrolled view did not show the selected model: %q", v)
	}
}

func TestPickerCardFitsAndScrollbarDrags(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)

	for i := 0; i < 30; i++ {
		m.models = append(m.models, fmt.Sprintf("model-%02d", i))
	}
	m = clickModelStatus(t, m)

	v := stripANSI(viewText(m))
	drawer := stripANSI(m.pickerView())
	if strings.Contains(drawer, "╭") || strings.Contains(drawer, "╰") {
		t.Fatalf("drawer should not render a card border: %q", drawer)
	}
	if lipgloss.Height(v) > 30 {
		t.Errorf("screen height %d exceeds the 30-row window", lipgloss.Height(v))
	}
	if m.pickerVPHeight() < 13 {
		t.Errorf("drawer list height = %d, want at least 13 visible rows", m.pickerVPHeight())
	}

	top, _, col, ok := m.scrollbarRect(1)
	if !ok {
		t.Fatal("picker scrollbar rect not found")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.dragOn {
		t.Fatal("click on picker scrollbar did not start a drag")
	}
	if !m.pickerVp.AtTop() {
		t.Error("click at the top of the track should stay at top")
	}
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: top + 4, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.pickerVp.AtTop() {
		t.Error("drag did not scroll the picker")
	}
	mm, _ = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: col, Y: top + 4}))
	m = mm.(Model)
	if m.dragOn {
		t.Error("release did not end picker drag")
	}
}

func TestModelsLoadedFromCacheSkipsAPI(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo, Context: 128000},
		{ID: "gpt-cache", Provider: provider.IDCodex, Endpoint: "cli://codex/chat/completions"},
		{ID: "grok-cache", Provider: provider.IDGrok, Endpoint: "cli://grok/chat/completions", Variants: []string{"low", "medium", "high", "xhigh"}},
	}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	v := statusDrawerText(m)
	if !strings.Contains(v, "deepseek-v4-flash") {
		t.Errorf("status drawer missing cached label: %q", v)
	}
	if !m.modelsCached {
		t.Error("modelsCached = false, want true for fresh cache")
	}
}

func TestModelsCacheWithGrokWithoutVariantsRefreshesCatalog(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo, Context: 128000},
		{ID: "gpt-cache", Provider: provider.IDCodex, Endpoint: "cli://codex/chat/completions"},
		{ID: "grok-cache", Provider: provider.IDGrok, Endpoint: "cli://grok/chat/completions"},
	}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	grok := subscription.NewGrok("grok-4.6", subscription.WithCatalogLoader(func(context.Context) (subscription.ModelCatalog, error) {
		return subscription.ModelCatalog{Models: []opencode.ModelInfo{{
			ID:       "grok-4.6",
			Provider: provider.IDGrok,
			Endpoint: "cli://grok/chat/completions",
			Variants: []string{"low", "medium", "high", "xhigh"},
		}}}, nil
	}))
	m := New(Options{
		Store:     newTestStore(t),
		Client:    newClient(fake.srv),
		Workdir:   dir,
		CachePath: cachePath,
		NewProviderClient: func(id string) (provider.Client, error) {
			if id == provider.IDGrok {
				return grok, nil
			}
			return nil, nil
		},
	})
	msg, ok := m.fetchModels().(modelsMsg)
	if !ok {
		t.Fatalf("fetchModels returned %T", msg)
	}
	if msg.fromCache {
		t.Fatal("fetchModels used a Grok cache row without variants")
	}
	info, found := modelscache.InfoOf(msg.infos, "grok-4.6")
	if !found || strings.Join(info.Variants, ",") != "low,medium,high,xhigh" {
		t.Fatalf("refreshed Grok row = %+v, found=%t", info, found)
	}
}

func TestModelsCacheWithoutGrokRefreshesCatalog(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo, Context: 128000},
		{ID: "gpt-cache", Provider: provider.IDCodex, Endpoint: "cli://codex/chat/completions"},
	}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	grok := subscription.NewGrok("grok-4.6", subscription.WithCatalogLoader(func(context.Context) (subscription.ModelCatalog, error) {
		return subscription.ModelCatalog{Models: []opencode.ModelInfo{{
			ID:       "grok-4.6",
			Provider: provider.IDGrok,
			Endpoint: "cli://grok/chat/completions",
		}}}, nil
	}))
	m := New(Options{
		Store:     newTestStore(t),
		Client:    newClient(fake.srv),
		Workdir:   dir,
		CachePath: cachePath,
		NewProviderClient: func(id string) (provider.Client, error) {
			if id == provider.IDGrok {
				return grok, nil
			}
			return nil, nil
		},
	})
	msg, ok := m.fetchModels().(modelsMsg)
	if !ok {
		t.Fatalf("fetchModels returned %T", msg)
	}
	if msg.fromCache {
		t.Fatal("fetchModels used a cache that had no Grok rows")
	}
	if !containsModel(msg.list, "grok-4.6") {
		t.Fatalf("refreshed models missing Grok: %v", msg.list)
	}
}

func TestModelsCacheRefreshedWhenStale(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	stale := time.Now().Add(-modelscache.DefaultTTL - time.Minute)
	if err := modelscache.Save(cachePath, []modelscache.Info{{ID: "stale-model"}}, stale); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	if !containsModel(m.models, "deepseek-v4-flash") || !containsModel(m.models, "claude-4") {
		t.Errorf("refreshed models missing API ids: %v", m.models)
	}
	if m.modelsCached {
		t.Error("modelsCached = true, want false after live refresh")
	}
	models, fresh, err := modelscache.Load(cachePath, time.Now(), modelscache.DefaultTTL)
	if err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	if len(models) < 2 || models[0].ID == "stale-model" {
		t.Errorf("cache not rewritten: %v", models)
	}
	if !fresh {
		t.Error("cache still stale after refresh")
	}
}

func TestModelsRefreshKeyReloadsFromAPI(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo, Context: 128000},
		{ID: "gpt-cache", Provider: provider.IDCodex, Endpoint: "cli://codex/chat/completions"},
		{ID: "grok-cache", Provider: provider.IDGrok, Endpoint: "cli://grok/chat/completions", Variants: []string{"low", "medium", "high", "xhigh"}},
	}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	if !m.modelsCached {
		t.Fatal("precondition: models should come from cache")
	}
	m = clickModelStatus(t, m)
	if !m.pickerMode {
		t.Fatal("precondition: picker did not open")
	}
	mm, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	m = mm.(Model)
	if m.pickerMode {
		t.Error("picker still open after refresh key")
	}
	p.run(cmd)
	m = p.runStep(m, p.next())

	if !containsModel(m.models, "deepseek-v4-flash") || !containsModel(m.models, "claude-4") {
		t.Errorf("refreshed models missing API ids: %v", m.models)
	}
	if m.modelsCached {
		t.Error("modelsCached = true, want false after manual refresh")
	}
}

func containsModel(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
