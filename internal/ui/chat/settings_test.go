package chat

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestSettingsSlashOpensCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 48})
	m = mm.(Model)
	m = typeText(m, "/settings")
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(Model)
	if !m.settingsMode {
		t.Fatal("settings mode not opened")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "SETTINGS") || !strings.Contains(v, "[x]") {
		t.Fatalf("settings card missing header/x: %q", v)
	}
	for _, want := range []string{"categories", "appearance", "model", "recaps", "skills", "agent loop", "compaction", "sub-agents", "safety", "request retries", "filter settings", "theme"} {
		if !strings.Contains(v, want) {
			t.Fatalf("settings card missing %q: %q", want, v)
		}
	}
	for _, definition := range settingsRowDefinitions {
		m.settingsCursor = definition.id
		rowView := stripANSI(viewText(m))
		if !strings.Contains(rowView, definition.label) {
			t.Fatalf("settings category did not render row %q: %q", definition.label, rowView)
		}
		if description := settingsRowDescription(definition.id); !strings.Contains(rowView, description) {
			t.Fatalf("settings category did not render description for %q: %q", definition.label, rowView)
		}
	}
	// Full-screen card, not a drawer glued above the prompt. Composer may
	// still paint under Place only if not full replacement; settingsMode uses
	// a dedicated screen so the prompt should not be the focus frame.
	if !strings.Contains(v, "SETTINGS") {
		t.Fatal("missing SETTINGS title")
	}
}

func TestSettingsRowDefinitionsCoverSelectableRows(t *testing.T) {
	if len(settingsRowDefinitions) != settingsRowCount {
		t.Fatalf("settings definitions = %d, want %d", len(settingsRowDefinitions), settingsRowCount)
	}
	seen := make(map[int]bool, len(settingsRowDefinitions))
	for _, definition := range settingsRowDefinitions {
		if definition.id < 0 || definition.id >= settingsRowCount {
			t.Fatalf("invalid settings row definition = %+v", definition)
		}
		if definition.label == "" {
			t.Fatalf("empty label for row %d", definition.id)
		}
		if seen[definition.id] {
			t.Fatalf("duplicate settings row definition %d", definition.id)
		}
		seen[definition.id] = true
	}
	for row := 0; row < settingsRowCount; row++ {
		if !seen[row] {
			t.Fatalf("missing settings row definition %d", row)
		}
		if _, ok := settingsSectionForRow(row); !ok {
			t.Fatalf("row %d has no settings section", row)
		}
		if settingsRowDescription(row) == "" {
			t.Fatalf("row %d has no settings description", row)
		}
	}
}

func TestSettingsKVRowKeepsCompactStepperControls(t *testing.T) {
	row := settingsKVRow(
		false,
		false,
		"new-session model",
		"◂ muse-spark-1.2-contributor ▸",
		43,
	)
	if lipgloss.Width(row) > 43 {
		t.Fatalf("compact settings row exceeds width: %q", row)
	}
	if !strings.Contains(row, "◂") || !strings.Contains(row, "▸") {
		t.Fatalf("compact settings row lost a stepper control: %q", row)
	}
}

func TestSettingsFormValidationUsesSettingsBounds(t *testing.T) {
	for _, value := range []int{settings.MinCompactPercent, settings.MaxCompactPercent} {
		if err := validatePercentSetting(strconv.Itoa(value)); err != nil {
			t.Fatalf("compact percent %d rejected: %v", value, err)
		}
	}
	if err := validateAgentTimeoutSetting("0"); err != nil {
		t.Fatalf("zero timeout rejected: %v", err)
	}
}

func TestSettingsThemeCyclesAndPersists(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	}).openSettings()
	m.settingsCursor = settingsRowTheme
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.EffectiveTheme() != "light" {
		t.Fatalf("theme = %q, want light", m.projectSettings.EffectiveTheme())
	}
	if theme.CurrentMode() != theme.ModeLight {
		t.Fatalf("active palette = %q, want light", theme.CurrentMode())
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveTheme() != "light" {
		t.Fatalf("persisted theme = %q, want light", loaded.EffectiveTheme())
	}
	if !strings.Contains(stripANSI(viewText(m)), "◂ light ▸") {
		t.Fatal("settings card did not redraw the light theme row")
	}
	if !strings.Contains(viewText(m), "48;2;247;248;252") {
		t.Fatal("light theme did not repaint the application background")
	}
}

func TestThemeSwitchRerendersCachedTranscript(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })
	dir := t.TempDir()
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: filepath.Join(dir, "settings.json"),
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.items = []transcriptItem{{kind: itemAssistant, text: "cached assistant text"}}
	m.syncTranscript()
	if !strings.Contains(viewText(m), "48;2;16;40;50") {
		t.Fatal("dark assistant panel did not render before the theme switch")
	}

	m = m.setTheme("light")
	lightView := viewText(m)
	if !strings.Contains(lightView, "48;2;228;247;251") {
		t.Fatal("cached assistant panel retained dark colors after switching to light")
	}
	lightPlain := stripANSI(lightView)
	if !strings.Contains(lightPlain, "cached assistant text") || !strings.Contains(lightPlain, "enter send") {
		t.Fatalf("theme switch hid chat content or composer: %q", lightPlain)
	}
	if lines := strings.Split(strings.TrimRight(lightPlain, "\n"), "\n"); len(lines) > m.height {
		t.Fatalf("theme switch grew compact layout to %d rows, want at most %d", len(lines), m.height)
	}

	m = m.setTheme("dark")
	if !strings.Contains(viewText(m), "48;2;16;40;50") {
		t.Fatal("cached assistant panel did not repaint after switching back to dark")
	}
}

func TestDarkComposerFillsInputSurface(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })
	dir := t.TempDir()
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: filepath.Join(dir, "settings.json"),
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 48})
	m = mm.(Model)

	view := viewText(m)
	for _, ansi := range []string{"48;2;5;5;5", "48;2;16;16;16"} {
		if !strings.Contains(view, ansi) {
			t.Fatalf("dark view missing expected surface %q", ansi)
		}
	}
}

func TestSettingsAdjustAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	cfg := settings.Default()
	cfg.Slot.MaxSteps = 8
	if err := settings.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
		Settings:     &cfg,
	})
	m = m.openSettingsAt(settingsSectionAgentLoop, settingsRowSteps)
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Slot.MaxSteps != 9 {
		t.Fatalf("MaxSteps = %d, want 9", m.projectSettings.Slot.MaxSteps)
	}
	// Toggle the other agent-loop setting without leaving the category.
	m.settingsCursor = settingsRowLimit
	m = upd(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.projectSettings.Slot.LimitEnabled {
		t.Fatal("limit still enabled after toggle")
	}
	if m.maxSteps != m.projectSettings.EffectiveMaxSteps() {
		t.Fatalf("maxSteps = %d, want %d", m.maxSteps, m.projectSettings.EffectiveMaxSteps())
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slot.MaxSteps != 9 || loaded.Slot.LimitEnabled {
		t.Fatalf("persisted slot %+v", loaded.Slot)
	}
}

func TestSettingsDefaultModelCyclePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m.models = []string{"deepseek-v4-flash", "claude-4", "big-pickle"}
	m = m.openSettingsAt(settingsSectionAgentLoop, settingsRowSteps)
	m.settingsCursor = settingsRowModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Model.Default != "claude-4" {
		t.Fatalf("default model = %q, want claude-4", m.projectSettings.Model.Default)
	}
	if m.model != "claude-4" {
		t.Fatalf("live model = %q, want claude-4 on new session", m.model)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.Default != "claude-4" {
		t.Fatalf("persisted model = %q", loaded.Model.Default)
	}
}

func TestSettingsRecapToggleAndModelPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m.models = []string{"deepseek-v4-flash", "claude-4"}
	m = m.openSettingsAt(settingsSectionAgentLoop, settingsRowLimit)
	if m.projectSettings.EffectiveRecap().Enabled {
		t.Fatal("recaps enabled by default")
	}
	if got := m.projectSettings.EffectiveRecap().Model; got != settings.DefaultModelID {
		t.Fatalf("recap model = %q, want %q", got, settings.DefaultModelID)
	}
	if got := m.projectSettings.EffectiveRecap().AfterChats; got != settings.DefaultRecapAfterChats {
		t.Fatalf("recap after chats = %d, want %d", got, settings.DefaultRecapAfterChats)
	}
	m.settingsCursor = settingsRowRecapEnabled
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.projectSettings.Recap.Enabled {
		t.Fatal("right arrow did not enable recaps")
	}
	m.settingsCursor = settingsRowRecapModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.projectSettings.Recap.Model; got != "claude-4" {
		t.Fatalf("recap model = %q, want claude-4", got)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Recap.Enabled || loaded.Recap.Model != "claude-4" || loaded.Recap.AfterChats != settings.DefaultRecapAfterChats {
		t.Fatalf("persisted recap settings = %+v", loaded.Recap)
	}
}

func TestSettingsRecapAfterChatsAdjustsAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	}).openSettings()
	m.settingsCursor = settingsRowRecapAfterChats
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.projectSettings.EffectiveRecap().AfterChats; got != settings.DefaultRecapAfterChats+1 {
		t.Fatalf("after chats = %d, want %d", got, settings.DefaultRecapAfterChats+1)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := m.projectSettings.EffectiveRecap().AfterChats; got != settings.DefaultRecapAfterChats {
		t.Fatalf("after chats after decrement = %d, want %d", got, settings.DefaultRecapAfterChats)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveRecap().AfterChats != settings.DefaultRecapAfterChats {
		t.Fatalf("persisted after chats = %d, want %d", loaded.EffectiveRecap().AfterChats, settings.DefaultRecapAfterChats)
	}
}

func TestSettingsRetryAdjustsAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	}).openSettings()

	m.settingsCursor = settingsRowRetryMaxRetries
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := m.projectSettings.EffectiveRetry().MaxRetries; got != settings.DefaultRetryMaxRetries-1 {
		t.Fatalf("max retries after left = %d, want %d", got, settings.DefaultRetryMaxRetries-1)
	}
	m.settingsCursor = settingsRowRetryDelay
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.projectSettings.EffectiveRetry().DelaySeconds; got != settings.DefaultRetryDelaySeconds+1 {
		t.Fatalf("retry delay after right = %d, want %d", got, settings.DefaultRetryDelaySeconds+1)
	}
	policy := m.client.RetryPolicy()
	if policy.MaxRetries != settings.DefaultRetryMaxRetries-1 || policy.Delay != 11*time.Second {
		t.Fatalf("live client retry policy = %+v", policy)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.EffectiveRetry(); got.MaxRetries != settings.DefaultRetryMaxRetries-1 || got.DelaySeconds != settings.DefaultRetryDelaySeconds+1 {
		t.Fatalf("persisted retry = %+v", got)
	}
}

func TestSettingsRecapModelOpensSharedModelDrawer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	}).openSettings()
	m.models = []string{"deepseek-v4-flash", "claude-4"}
	m.settingsCursor = settingsRowRecapModel

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.pickerMode || m.pickerKind != pickerKindModel || m.settingsPickerTarget != settingsPickerRecapModel {
		t.Fatalf("picker state = mode=%v kind=%q target=%d, want shared recap model drawer", m.pickerMode, m.pickerKind, m.settingsPickerTarget)
	}
	if m.pickerCursor != 0 {
		t.Fatalf("picker cursor = %d, want current recap model at index 0", m.pickerCursor)
	}
	m.pickerCursor = 1
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pickerMode || !m.settingsMode {
		t.Fatalf("after selection picker=%v settings=%v, want settings reopened", m.pickerMode, m.settingsMode)
	}
	if got := m.projectSettings.EffectiveRecap().Model; got != "claude-4" {
		t.Fatalf("recap model = %q, want claude-4", got)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveRecap().Model != "claude-4" {
		t.Fatalf("persisted recap model = %q, want claude-4", loaded.EffectiveRecap().Model)
	}
}

func TestSettingsRecapModelMouseOpensSharedModelDrawer(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()}).openSettings()
	m.models = []string{"deepseek-v4-flash", "claude-4"}
	m.settingsCursor = settingsRowRecapModel
	line := settingsPaintedRow(m, "recap model")
	x0, x1, ok := displaySpan(line, "recap model")
	if !ok {
		t.Fatalf("recap model label missing from painted row: %q", line)
	}
	next, _, hit := m.settingsHit((x0+x1)/2, settingsPaintedRowY(m, "recap model"), tea.MouseLeft)
	if !hit || !next.pickerMode || next.pickerKind != pickerKindModel || next.settingsPickerTarget != settingsPickerRecapModel {
		t.Fatalf("mouse result = hit=%v picker=%v kind=%q target=%d, want shared recap model drawer", hit, next.pickerMode, next.pickerKind, next.settingsPickerTarget)
	}
}

func TestSettingsChildVariantUsesSharedVariantDrawer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	}).openSettings()
	m.models = []string{"child-model"}
	m.modelInfos = []modelscache.Info{{
		ID:       "child-model",
		Provider: modelscache.ProviderOpenCodeGo,
		Variants: []string{"low", "high"},
	}}
	m.projectSettings.Agents.ModelOverride = "child-model"
	m.settingsCursor = settingsRowChildVariant

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.pickerMode || m.pickerKind != pickerKindVariant || m.settingsPickerTarget != settingsPickerChildVariant {
		t.Fatalf("picker state = mode=%v kind=%q target=%d, want shared child variant drawer", m.pickerMode, m.pickerKind, m.settingsPickerTarget)
	}
	if len(m.pickerItems) != 2 || m.pickerItems[0] != "low" || m.pickerItems[1] != "high" {
		t.Fatalf("child variant choices = %v, want [low high]", m.pickerItems)
	}

	m.pickerCursor = 1
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pickerMode || !m.settingsMode {
		t.Fatalf("after selection picker=%v settings=%v, want settings reopened", m.pickerMode, m.settingsMode)
	}
	if got := m.projectSettings.Agents.ModelVariant; got != "high" {
		t.Fatalf("child model variant = %q, want high", got)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agents.ModelVariant != "high" {
		t.Fatalf("persisted child model variant = %q, want high", loaded.Agents.ModelVariant)
	}
}

func TestSettingsChildVariantMouseOpensSharedVariantDrawer(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()}).openSettings()
	m.models = []string{"child-model"}
	m.modelInfos = []modelscache.Info{{
		ID:       "child-model",
		Provider: modelscache.ProviderOpenCodeGo,
		Variants: []string{"low", "high"},
	}}
	m.projectSettings.Agents.ModelOverride = "child-model"
	m.settingsCursor = settingsRowChildVariant
	line := settingsPaintedRow(m, "child model variant")
	x0, x1, ok := displaySpan(line, "child model variant")
	if !ok {
		t.Fatalf("child variant label missing from painted row: %q", line)
	}

	next, _, hit := m.settingsHit((x0+x1)/2, settingsPaintedRowY(m, "child model variant"), tea.MouseLeft)
	if !hit || !next.pickerMode || next.pickerKind != pickerKindVariant || next.settingsPickerTarget != settingsPickerChildVariant {
		t.Fatalf("mouse result = hit=%v picker=%v kind=%q target=%d, want shared child variant drawer", hit, next.pickerMode, next.pickerKind, next.settingsPickerTarget)
	}
}

func TestSettingsChildAndExploreModelMouseOpenSharedModelDrawer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		row      int
		label    string
		target   settingsPickerTarget
		getValue func(settings.Settings) string
	}{
		{
			name:   "child model override",
			row:    settingsRowChildModel,
			label:  "child model override",
			target: settingsPickerChildModel,
			getValue: func(s settings.Settings) string {
				return s.Agents.ModelOverride
			},
		},
		{
			name:   "explore model",
			row:    settingsRowExploreModel,
			label:  "explore model",
			target: settingsPickerExploreModel,
			getValue: func(s settings.Settings) string {
				return s.Agents.ExploreModel
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
			mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 48})
			m = mm.(Model).openSettings()
			m.models = []string{"deepseek-v4-flash", "claude-4"}
			m.settingsCursor = tc.row
			line := settingsPaintedRow(m, tc.label)
			x0, x1, ok := displaySpan(line, tc.label)
			if !ok {
				t.Fatalf("model label missing from painted row: %q", line)
			}

			mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (x0 + x1) / 2, Y: settingsPaintedRowY(m, tc.label), Button: tea.MouseLeft}))
			next := mm.(Model)
			if !next.pickerMode || next.pickerKind != pickerKindModel {
				t.Fatalf("mouse result = picker=%v kind=%q, want shared model drawer", next.pickerMode, next.pickerKind)
			}
			if next.settingsPickerTarget != tc.target {
				t.Fatalf("picker target = %d, want %d", next.settingsPickerTarget, tc.target)
			}
			if next.pickerSelectedValue() != "" || next.pickerCursor != 0 {
				t.Fatalf("inherit selection = value %q cursor %d, want empty value at index 0", next.pickerSelectedValue(), next.pickerCursor)
			}

			next.pickerCursor = 1
			next, _ = next.updatePickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !next.settingsMode || next.pickerMode {
				t.Fatalf("after selection settings=%v picker=%v, want settings reopened", next.settingsMode, next.pickerMode)
			}
			if got := tc.getValue(next.projectSettings); got != "deepseek-v4-flash" {
				t.Fatalf("selected model = %q, want deepseek-v4-flash", got)
			}
		})
	}
}

func TestSettingsCloseClick(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()
	x0, y, x1, ok := m.settingsCloseRect()
	if !ok {
		t.Fatal("close rect missing")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (x0 + x1) / 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.settingsMode {
		t.Fatal("click [x] did not close settings")
	}
}

func TestSettingsMouseAdjustSteps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettingsAt(settingsSectionAgentLoop, settingsRowSteps)
	before := m.projectSettings.Slot.MaxSteps
	rowTop := settingsPaintedRowY(m, "parent max steps")
	if rowTop < 0 {
		t.Fatal("parent max steps row missing")
	}
	line := settingsPaintedRow(m, "parent max steps")
	if line == "" {
		t.Fatal("parent max steps row missing")
	}
	dec0, dec1, ok := displaySpan(line, "◂")
	if !ok {
		t.Fatalf("decrease chevron missing in %q", line)
	}
	_, _, ok = displaySpanLast(line, "▸")
	if !ok {
		t.Fatalf("increase chevron missing in %q", line)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (dec0 + dec1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before-1 {
		t.Fatalf("click ◂: MaxSteps = %d, want %d", m.projectSettings.Slot.MaxSteps, before-1)
	}
	// Re-read after re-render.
	line = settingsPaintedRow(m, "parent max steps")
	rowTop = settingsPaintedRowY(m, "parent max steps")
	if rowTop < 0 {
		t.Fatal("parent max steps row missing after decrease")
	}
	inc0, inc1, ok := displaySpanLast(line, "▸")
	if !ok {
		t.Fatal("▸ missing after decrease")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (inc0 + inc1) / 2, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.MaxSteps != before {
		t.Fatalf("click ▸: MaxSteps = %d, want %d", m.projectSettings.Slot.MaxSteps, before)
	}
}

func TestSettingsMouseToggleOnGlyph(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettingsAt(settingsSectionAgentLoop, settingsRowLimit)
	if !m.projectSettings.Slot.LimitEnabled {
		t.Fatal("want limit on by default")
	}
	rowTop := settingsPaintedRowY(m, "step limit")
	if rowTop < 0 {
		t.Fatal("step limit row missing")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 10, Y: rowTop, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Slot.LimitEnabled {
		t.Fatal("click step limit row did not turn limit off")
	}
}

func TestNewSeedsModelFromSettings(t *testing.T) {
	cfg := settings.Default()
	cfg.Model.Default = "claude-4"
	cfg.Model.Variant = "high"
	m := New(Options{
		Store:    newTestStore(t),
		Client:   deadClient(),
		Workdir:  t.TempDir(),
		Settings: &cfg,
	})
	if m.model != "claude-4" {
		t.Fatalf("model = %q, want claude-4", m.model)
	}
	if m.variant != "high" {
		t.Fatalf("variant = %q, want high", m.variant)
	}
}

func TestNewAppliesRetrySettingsToClient(t *testing.T) {
	cfg := settings.Default()
	cfg.Retry.MaxRetries = 2
	cfg.Retry.DelaySeconds = 4
	client := deadClient()
	_ = New(Options{
		Store:    newTestStore(t),
		Client:   client,
		Workdir:  t.TempDir(),
		Settings: &cfg,
	})
	policy := client.RetryPolicy()
	if policy.MaxRetries != 2 || policy.Delay != 4*time.Second {
		t.Fatalf("retry policy = %+v, want 2 retries and 4s", policy)
	}
}

func TestSlashMenuListsSettingsAndContinue(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/settings") || !strings.Contains(v, "/continue") {
		t.Fatalf("slash menu missing settings/continue: %q", v)
	}
}

func TestContinueAfterStepLimitResumes(t *testing.T) {
	tc := fakeToolCall{ID: "call_1", Name: "bash", Args: `{"command":"echo x"}`}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("all done", "stop", nil),
	)
	st := newTestStore(t)
	m := New(Options{
		Store:    st,
		Client:   newClient(fake.srv),
		Workdir:  t.TempDir(),
		MaxSteps: 2,
	})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "work")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if !m.stepLimitHit {
		t.Fatalf("want stepLimitHit after limit, err=%q", m.err)
	}
	if !strings.Contains(m.err, "/continue") {
		t.Fatalf("err missing /continue hint: %q", m.err)
	}
	usersBefore := countSessionUsers(t, st, m.session)

	m, cmd = m.runContinue()
	if cmd == nil {
		t.Fatal("continue nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if m.stepLimitHit {
		t.Fatal("stepLimitHit still set after successful continue")
	}
	if m.err != "" {
		t.Fatalf("continue err: %q", m.err)
	}
	usersAfter := countSessionUsers(t, st, m.session)
	if usersAfter != usersBefore {
		t.Fatalf("continue wrote user message: %d -> %d", usersBefore, usersAfter)
	}
}

func TestContinueWithoutLimitSendsMessage(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("ok", "stop", nil))
	m := New(Options{
		Store:   newTestStore(t),
		Client:  newClient(fake.srv),
		Workdir: t.TempDir(),
	})
	p := newPump(t)
	p.run(m.Init())
	m, cmd := m.runContinue()
	if cmd == nil {
		t.Fatal("continue nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if m.err != "" {
		t.Fatalf("err: %q", m.err)
	}
	found := false
	for _, it := range m.items {
		if it.kind == itemUser && it.text == "continue" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected user continue message in items: %+v", m.items)
	}
}

func TestSettingsHitAllRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 48})
	m = mm.(Model)
	m.models = []string{"deepseek-v4-flash", "claude-4", "big-pickle"}
	m = m.openSettings()

	for row := 0; row < settingsRowCount; row++ {
		label := settingsRowLabel(row)
		section, ok := settingsSectionForRow(row)
		if !ok {
			t.Fatalf("row %d has no section", row)
		}
		base := m.openSettingsAt(section, row)
		y := settingsPaintedRowY(base, label)
		if y < 0 {
			t.Fatalf("row %d %q not painted:\n%s", row, label, stripANSI(viewText(base)))
		}
		line := settingsPaintedRow(base, label)
		x := settingsHitX(line, row)
		next, _, hit := base.settingsHit(x, y, tea.MouseLeft)
		if !hit {
			t.Fatalf("settingsHit missed %q at (%d,%d) line %q", label, x, y, line)
		}
		if !settingsHitChanged(base, next, row) {
			t.Fatalf("settingsHit on %q did not change state or open editor (x=%d y=%d line=%q)", label, x, y, line)
		}
	}
}

func TestSettingsKeyboardNavigationFollowsSelectedCategory(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model).openSettingsAt(settingsSectionModel, settingsRowModel)
	for index, want := range []int{settingsRowModel, settingsRowVariant} {
		if m.settingsCursor != want {
			t.Fatalf("model cursor at step %d = %d, want %d", index, m.settingsCursor, want)
		}
		if index == 0 {
			m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
		}
	}
	m = upd(m, tea.KeyPressMsg{Code: ']'})
	if m.settingsCurrentSection() != settingsSectionRecaps || m.settingsCursor != settingsRowRecapEnabled {
		t.Fatalf("next category = section %d row %d", m.settingsCurrentSection(), m.settingsCursor)
	}
}

func TestSettingsFilterAndCategoryRailKeepFocusVisible(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model).openSettings()
	m = upd(m, tea.KeyPressMsg{Code: '/'})
	if !m.settingsFilterActive() {
		t.Fatal("settings filter did not activate")
	}
	for _, key := range "retry" {
		m = upd(m, tea.KeyPressMsg{Code: key, Text: string(key)})
	}
	filtered := stripANSI(viewText(m))
	if !strings.Contains(filtered, "api retries") || strings.Contains(filtered, "new-session model") {
		t.Fatalf("unexpected filter results:\n%s", filtered)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.settingsFilterActive() || m.settingsCurrentSection() != settingsSectionRequestRetries {
		t.Fatalf("filter did not restore focus: filter=%v section=%d", m.settingsFilterActive(), m.settingsCurrentSection())
	}

	m = m.openSettings().ensureLayout()
	modelY := -1
	for y, section := range m.layout.settingsSectionByY {
		if section == settingsSectionModel {
			modelY = y
			break
		}
	}
	if modelY < 0 {
		t.Fatal("model category missing from settings rail")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: m.layout.settingsRailX0 + 1, Y: modelY, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.settingsCurrentSection() != settingsSectionModel || m.settingsCursor != settingsRowModel {
		t.Fatalf("rail click selected section=%d row=%d", m.settingsCurrentSection(), m.settingsCursor)
	}
}

func TestSettingsPickerCancelReturnsToSameSectionAndRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  int
	}{
		{name: "model", row: settingsRowModel},
		{name: "variant", row: settingsRowVariant},
		{name: "recap model", row: settingsRowRecapModel},
		{name: "child model", row: settingsRowChildModel},
		{name: "child variant", row: settingsRowChildVariant},
		{name: "explore model", row: settingsRowExploreModel},
		{name: "role", row: settingsRowAgentsRole},
		{name: "tools", row: settingsRowTools},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
			m.models = []string{"deepseek-v4-flash"}
			m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Variants: []string{"low", "high"}}}
			section, ok := settingsSectionForRow(tc.row)
			if !ok {
				t.Fatalf("row %d has no section", tc.row)
			}
			m = m.openSettingsAt(section, tc.row)
			m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if !m.pickerMode {
				t.Fatalf("row %d did not open its shared picker", tc.row)
			}
			m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			if !m.settingsMode || m.pickerMode || m.settingsCurrentSection() != section || m.settingsCursor != tc.row {
				t.Fatalf("picker return = settings=%v picker=%v section=%d row=%d", m.settingsMode, m.pickerMode, m.settingsCurrentSection(), m.settingsCursor)
			}
		})
	}
}

func TestSettingsChildModelMouseChevronsWorkThroughUpdate(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 48})
	m = mm.(Model)
	m.models = []string{"child-one", "child-two", "child-three"}
	m = m.openSettings()
	m.settingsCursor = settingsRowChildModel
	line := settingsPaintedRow(m, "child model override")
	y := settingsPaintedRowY(m, "child model override")
	dec0, dec1, ok := displaySpan(line, "◂")
	if !ok {
		t.Fatalf("child model row has no decrease control: %q", line)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (dec0 + dec1) / 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Agents.ModelOverride != "child-three" {
		t.Fatalf("left child-model click = %q, want child-three", m.projectSettings.Agents.ModelOverride)
	}

	line = settingsPaintedRow(m, "child model override")
	y = settingsPaintedRowY(m, "child model override")
	inc0, inc1, ok := displaySpanLast(line, "▸")
	if !ok {
		t.Fatalf("child model row has no increase control: %q", line)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: (inc0 + inc1) / 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.projectSettings.Agents.ModelOverride != "" {
		t.Fatalf("right child-model click = %q, want inherit", m.projectSettings.Agents.ModelOverride)
	}
}

func TestSettingsCardFitsCompactTerminal(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model).openSettings()

	view := stripANSI(viewText(m))
	if got := len(strings.Split(view, "\n")); got != 24 {
		t.Fatalf("compact settings height = %d, want 24:\n%s", got, view)
	}
	for _, want := range []string{"theme", "j/k move", "╰"} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact settings missing %q:\n%s", want, view)
		}
	}

	m.settingsCursor = settingsRowAllowlist
	view = stripANSI(viewText(m))
	for _, want := range []string{"allowed executables", "j/k move", "╰"} {
		if !strings.Contains(view, want) {
			t.Fatalf("scrolled settings missing %q:\n%s", want, view)
		}
	}
}

func TestSettingsRecapRowsFitRequestedTerminals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "wide", width: 120, height: 36},
		{name: "compact", width: 80, height: 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
			mm, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			m = mm.(Model).openSettings()
			if got := len(strings.Split(m.settingsScreen(), "\n")); got != tc.height {
				t.Fatalf("settings height = %d, want %d", got, tc.height)
			}
			for _, row := range []struct {
				id    int
				label string
			}{
				{id: settingsRowRecapEnabled, label: "recaps enabled"},
				{id: settingsRowRecapModel, label: "recap model"},
				{id: settingsRowRecapAfterChats, label: "recap after chats"},
			} {
				m.settingsCursor = row.id
				if y := settingsPaintedRowY(m, row.label); y < 0 {
					t.Fatalf("row %q is not visible at %dx%d:\n%s", row.label, tc.width, tc.height, stripANSI(viewText(m)))
				}
			}
		})
	}
}

func TestSettingsCompactPercentAdjustAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m = m.openSettings()
	if m.projectSettings.Compaction.Percent != agent.DefaultCompactPercent {
		t.Fatalf("default percent = %d", m.projectSettings.Compaction.Percent)
	}
	m.settingsCursor = settingsRowCompactPercent
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.projectSettings.Compaction.Percent != agent.DefaultCompactPercent-settingsCompactPercentStep {
		t.Fatalf("percent after left = %d", m.projectSettings.Compaction.Percent)
	}
	m.settingsCursor = settingsRowCompactAuto
	m = upd(m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.projectSettings.Compaction.Auto {
		t.Fatal("auto-compact should toggle off")
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Compaction.Auto {
		t.Fatal("persisted auto still on")
	}
	if loaded.Compaction.Percent != agent.DefaultCompactPercent-settingsCompactPercentStep {
		t.Fatalf("persisted percent = %d", loaded.Compaction.Percent)
	}
}

func TestSettingsTimeoutRoleConfirmPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m = m.openSettings()
	m.settingsCursor = settingsRowAgentsTimeout
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.DefaultTimeoutSec != settings.DefaultAgentsTimeoutSec+settingsTimeoutStepSec {
		t.Fatalf("timeout = %d, want %d", m.projectSettings.Agents.DefaultTimeoutSec, settings.DefaultAgentsTimeoutSec+settingsTimeoutStepSec)
	}
	m.settingsCursor = settingsRowAgentsRole
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.DefaultRole != "plan" {
		t.Fatalf("role = %q, want plan", m.projectSettings.Agents.DefaultRole)
	}
	m.settingsCursor = settingsRowBashConfirm
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.BashConfirm != "deny" {
		t.Fatalf("bash confirm = %q, want deny", m.projectSettings.Agents.BashConfirm)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agents.DefaultTimeoutSec != settings.DefaultAgentsTimeoutSec+settingsTimeoutStepSec {
		t.Fatalf("persisted timeout = %d", loaded.Agents.DefaultTimeoutSec)
	}
	if loaded.Agents.DefaultRole != "plan" {
		t.Fatalf("persisted role = %q", loaded.Agents.DefaultRole)
	}
	if loaded.Agents.BashConfirm != "deny" {
		t.Fatalf("persisted confirm = %q", loaded.Agents.BashConfirm)
	}
}

func TestSettingsExploreQueueOverridePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	m := New(Options{
		Store:        newTestStore(t),
		Client:       deadClient(),
		Workdir:      dir,
		SettingsPath: path,
	})
	m.models = []string{"deepseek-v4-flash", "claude-4", "big-pickle"}
	m = m.openSettings()
	m.settingsCursor = settingsRowExploreModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.ExploreModel != "deepseek-v4-flash" {
		t.Fatalf("explore model = %q, want deepseek-v4-flash", m.projectSettings.Agents.ExploreModel)
	}
	m.settingsCursor = settingsRowChildModel
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.ModelOverride != "deepseek-v4-flash" {
		t.Fatalf("override = %q, want deepseek-v4-flash", m.projectSettings.Agents.ModelOverride)
	}
	beforeQ := m.projectSettings.Agents.MaxQueued
	m.settingsCursor = settingsRowAgentsQueued
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.projectSettings.Agents.MaxQueued != beforeQ+1 {
		t.Fatalf("queued = %d, want %d", m.projectSettings.Agents.MaxQueued, beforeQ+1)
	}
	loaded, err := settings.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agents.ExploreModel != "deepseek-v4-flash" {
		t.Fatalf("persisted explore = %q", loaded.Agents.ExploreModel)
	}
	if loaded.Agents.ModelOverride != "deepseek-v4-flash" {
		t.Fatalf("persisted override = %q", loaded.Agents.ModelOverride)
	}
	if loaded.Agents.MaxQueued != beforeQ+1 {
		t.Fatalf("persisted queued = %d", loaded.Agents.MaxQueued)
	}
}

func TestSettingsCardMinHeight(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 48})
	m = mm.(Model)
	m = m.openSettings()
	cardLines := strings.Split(m.settingsCardView(), "\n")
	if len(cardLines) <= 20 {
		t.Fatalf("settings card height %d looks content-hugged:\n%s", len(cardLines), stripANSI(m.settingsCardView()))
	}
}

func settingsPaintedRowY(m Model, label string) int {
	for i, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if settingsLineHasLabel(line, label) {
			return i
		}
	}
	return -1
}

func settingsPaintedRow(m Model, label string) string {
	for _, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if settingsLineHasLabel(line, label) {
			return line
		}
	}
	return ""
}

func settingsHitX(line string, row int) int {
	switch row {
	case settingsRowLimit, settingsRowCompactAuto, settingsRowAgentsEnabled, settingsRowAgentsWriters, settingsRowAllowlistEnabled, settingsRowRecapEnabled, settingsRowSkillsEnabled, settingsRowSkillsAutoDetect, settingsRowSkillsLocal, settingsRowSkillsGlobal, settingsRowSkillsRemember:
		if x0, x1, ok := displaySpan(line, "[on]"); ok {
			return (x0 + x1) / 2
		}
		if x0, x1, ok := displaySpan(line, "[off]"); ok {
			return (x0 + x1) / 2
		}
		if hitToggle(line, 10) {
			return 10
		}
	case settingsRowAllowlist:
		if x0, x1, ok := displaySpan(line, "allowed"); ok {
			return (x0 + x1) / 2
		}
	case settingsRowAgentsChildSteps:
		if x0, x1, ok := displaySpan(line, "◂"); ok {
			return (x0 + x1) / 2
		}
	default:
		if x0, x1, ok := displaySpanLast(line, "▸"); ok {
			return (x0 + x1) / 2
		}
		if x0, x1, ok := displaySpan(line, "◂"); ok {
			return (x0 + x1) / 2
		}
	}
	return max(1, lipgloss.Width(line)-2)
}

func settingsHitChanged(before, after Model, row int) bool {
	if after.settingsEdit && !before.settingsEdit {
		return true
	}
	if after.pickerMode && !before.pickerMode {
		return true
	}
	if !after.settingsMode && before.settingsMode {
		return true
	}
	ba, aa := before.projectSettings.Agents, after.projectSettings.Agents
	switch row {
	case settingsRowTheme:
		return after.projectSettings.EffectiveTheme() != before.projectSettings.EffectiveTheme()
	case settingsRowModel:
		return after.projectSettings.Model.Default != before.projectSettings.Model.Default
	case settingsRowVariant:
		return after.projectSettings.Model.Variant != before.projectSettings.Model.Variant
	case settingsRowChildModel:
		return aa.ModelOverride != ba.ModelOverride
	case settingsRowChildVariant:
		return aa.ModelVariant != ba.ModelVariant
	case settingsRowExploreModel:
		return aa.ExploreModel != ba.ExploreModel
	case settingsRowRecapEnabled:
		return after.projectSettings.Recap.Enabled != before.projectSettings.Recap.Enabled
	case settingsRowRecapModel:
		return after.projectSettings.Recap.Model != before.projectSettings.Recap.Model
	case settingsRowRecapAfterChats:
		return after.projectSettings.EffectiveRecap().AfterChats != before.projectSettings.EffectiveRecap().AfterChats
	case settingsRowRetryMaxRetries:
		return after.projectSettings.EffectiveRetry().MaxRetries != before.projectSettings.EffectiveRetry().MaxRetries
	case settingsRowRetryDelay:
		return after.projectSettings.EffectiveRetry().DelaySeconds != before.projectSettings.EffectiveRetry().DelaySeconds
	case settingsRowSkillsEnabled:
		return after.projectSettings.EffectiveSkills().Enabled != before.projectSettings.EffectiveSkills().Enabled
	case settingsRowSkillsAutoDetect:
		return after.projectSettings.EffectiveSkills().AutoDetect != before.projectSettings.EffectiveSkills().AutoDetect
	case settingsRowSkillsLocal:
		return after.projectSettings.EffectiveSkills().IncludeLocal != before.projectSettings.EffectiveSkills().IncludeLocal
	case settingsRowSkillsGlobal:
		return after.projectSettings.EffectiveSkills().IncludeGlobal != before.projectSettings.EffectiveSkills().IncludeGlobal
	case settingsRowSkillsRemember:
		return after.projectSettings.EffectiveSkills().Remember != before.projectSettings.EffectiveSkills().Remember
	case settingsRowSkillsMaxMatches:
		return after.projectSettings.EffectiveSkills().MaxAutoMatches != before.projectSettings.EffectiveSkills().MaxAutoMatches
	case settingsRowLimit:
		return after.projectSettings.Slot.LimitEnabled != before.projectSettings.Slot.LimitEnabled
	case settingsRowSteps:
		return after.projectSettings.Slot.MaxSteps != before.projectSettings.Slot.MaxSteps
	case settingsRowCompactAuto:
		return after.projectSettings.Compaction.Auto != before.projectSettings.Compaction.Auto
	case settingsRowCompactPercent:
		return after.projectSettings.Compaction.Percent != before.projectSettings.Compaction.Percent
	case settingsRowAgentsEnabled:
		return aa.Enabled != ba.Enabled
	case settingsRowAgentsRole:
		return aa.DefaultRole != ba.DefaultRole
	case settingsRowAgentsConcurrent:
		return aa.MaxConcurrent != ba.MaxConcurrent
	case settingsRowAgentsQueued:
		return aa.MaxQueued != ba.MaxQueued
	case settingsRowAgentsChildSteps:
		return aa.ChildMaxSteps != ba.ChildMaxSteps
	case settingsRowAgentsTimeout:
		return aa.DefaultTimeoutSec != ba.DefaultTimeoutSec
	case settingsRowAgentsWriters:
		return aa.AllowParallelWriters != ba.AllowParallelWriters
	case settingsRowBashConfirm:
		return aa.BashConfirm != ba.BashConfirm
	case settingsRowAllowlistEnabled:
		return aa.BashAllowlistEnabled != ba.BashAllowlistEnabled
	case settingsRowAllowlist:
		return after.settingsEdit
	default:
		return false
	}
}

func countSessionUsers(t *testing.T, st *db.Store, sess *db.Session) int {
	t.Helper()
	if sess == nil {
		return 0
	}
	msgs, err := st.ListMessages(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, msg := range msgs {
		if msg.Role == "user" {
			n++
		}
	}
	return n
}

func TestSettingsMouseHover(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model).openSettings()
	m.settingsCursor = settingsRowSteps

	if m.settingsHover != -1 {
		t.Fatalf("settingsHover on init = %d, want -1", m.settingsHover)
	}

	// Move mouse over the visible parent max steps row.
	rowY := -1
	for y := 0; y < 30; y++ {
		if r, ok := m.settingsRowAtScreenY(y); ok && r == settingsRowSteps {
			rowY = y
			break
		}
	}
	if rowY < 0 {
		t.Fatal("could not locate settingsRowSteps on screen")
	}

	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: 30, Y: rowY}))
	m = mm.(Model)

	if m.settingsHover != settingsRowSteps {
		t.Fatalf("settingsHover = %d, want %d (settingsRowSteps)", m.settingsHover, settingsRowSteps)
	}
}

func TestSettingsLeftRightArrowBooleans(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = m.openSettings()

	// Navigate to Limit
	m.settingsCursor = settingsRowLimit
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.projectSettings.Slot.LimitEnabled {
		t.Fatal("Left arrow should turn LimitEnabled off")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.projectSettings.Slot.LimitEnabled {
		t.Fatal("Right arrow should turn LimitEnabled on")
	}

	// Navigate to AgentsEnabled
	m.settingsCursor = settingsRowAgentsEnabled
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.projectSettings.Agents.Enabled {
		t.Fatal("Left arrow should turn Agents.Enabled off")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.projectSettings.Agents.Enabled {
		t.Fatal("Right arrow should turn Agents.Enabled on")
	}
}
