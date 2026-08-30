package chat

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
)

func TestSlashMenuOpensAndDivides(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	if !strings.Contains(stripANSI(viewText(m)), "ask lazykoder") {
		t.Fatalf("prompt placeholder missing: %q", stripANSI(viewText(m)))
	}
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash mode not opened on /")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/new") || !strings.Contains(v, "/provider") || !strings.Contains(v, "/model") || !strings.Contains(v, "/variant") || !strings.Contains(v, "/help") || !strings.Contains(v, "/resume") || !strings.Contains(v, "/settings") || !strings.Contains(v, "/continue") {
		t.Errorf("slash menu missing commands: %q", v)
	}
	if strings.Contains(v, "/sessions") {
		t.Errorf("slash menu should list /resume, not /sessions: %q", v)
	}
	if !strings.Contains(v, "Session") || !strings.Contains(v, "Model") || !strings.Contains(v, "Project") || !strings.Contains(v, "Help") {
		t.Errorf("slash menu missing group headings: %q", v)
	}
	if !strings.Contains(v, "commands") {
		t.Errorf("slash menu missing header: %q", v)
	}
	if !strings.Contains(v, "start a new session") {
		t.Errorf("slash menu missing selected description: %q", v)
	}
}

func TestSlashProviderPickerSelectsParentProvider(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("XAI_API_KEY", "")
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for _, r := range "/provider" {
		m = typeRune(m, r)
	}
	if !m.slashMode || len(m.slashItems) != 1 || m.slashItems[0].name != "/provider" {
		t.Fatalf("provider slash items = %+v", m.slashItems)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.pickerMode || m.pickerKind != pickerKindProvider {
		t.Fatalf("provider picker state = mode=%v kind=%q", m.pickerMode, m.pickerKind)
	}
	v := stripANSI(m.pickerView())
	for _, label := range []string{"OpenCode", "OpenAI", "Grok", "Codex", "xAI"} {
		if !strings.Contains(v, label) {
			t.Fatalf("provider picker missing %q: %q", label, v)
		}
	}
	m, _ = m.selectPickerItem(1)
	if m.projectSettings.EffectiveProvider() != "openai" {
		t.Fatalf("selected provider = %q, want openai", m.projectSettings.EffectiveProvider())
	}
	if got := m.projectSettings.EffectiveModel(); got != "gpt-4.1-mini" {
		t.Fatalf("selected provider model = %q, want gpt-4.1-mini", got)
	}
	if !strings.Contains(m.err, "OPENAI_API_KEY is not configured") {
		t.Fatalf("missing-key selection error = %q", m.err)
	}
}

func TestProviderPickerDisplaysSelectionAndCredentialState(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("XAI_API_KEY", "")
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = m.openProviderPicker()
	v := stripANSI(m.pickerView())
	if strings.Contains(v, "available") {
		t.Fatalf("provider picker claims availability: %q", v)
	}
	if !strings.Contains(v, "selected • key missing") {
		t.Fatalf("selected provider state missing: %q", v)
	}
	if !strings.Contains(v, "not selected • key missing") {
		t.Fatalf("unselected provider state missing: %q", v)
	}
	if !strings.Contains(v, "not selected • checking sign-in") {
		t.Fatalf("subscription provider state missing: %q", v)
	}

	t.Setenv("OPENAI_API_KEY", "openai-test")
	m.providerAuthStatus[provider.IDCodex] = provider.AuthStatus{State: provider.AuthStateReady, Label: "signed in"}
	m.providerAuthStatus[provider.IDGrok] = provider.AuthStatus{State: provider.AuthStateRequired, Label: "sign in required"}
	m = m.openProviderPicker()
	v = stripANSI(m.pickerView())
	if !strings.Contains(v, "not selected • key set") {
		t.Fatalf("configured unselected provider state missing: %q", v)
	}
	if !strings.Contains(v, "not selected • signed in") || !strings.Contains(v, "not selected • sign in required") {
		t.Fatalf("subscription provider statuses missing: %q", v)
	}
}

func TestProviderPickerStartsGrokDeviceLogin(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.providerAuthStatus[provider.IDGrok] = provider.AuthStatus{State: provider.AuthStateRequired, Label: "sign in required"}
	var got string
	m.providerLogin = func(id string) (*exec.Cmd, error) {
		got = id
		return exec.Command("true"), nil
	}
	m = m.openProviderPicker()
	index := slices.Index(m.pickerItems, provider.IDGrok)
	if index < 0 {
		t.Fatal("Grok picker item missing")
	}
	next, command := m.selectPickerItem(index)
	if command == nil || got != provider.IDGrok || next.providerLoginTarget != provider.IDGrok {
		t.Fatalf("login state = command:%v provider:%q target:%q", command != nil, got, next.providerLoginTarget)
	}
}

func TestProviderLoginCompletionActivatesSubscriptionProvider(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	client := newClient(fake.srv)
	m := New(Options{
		Store: newTestStore(t), Client: client, Workdir: t.TempDir(),
		NewProviderClient: func(id string) (provider.Client, error) {
			if id != provider.IDCodex {
				return client, nil
			}
			return client, nil
		},
	})
	m = m.openProviderPicker()
	m.providerLoginTarget = provider.IDCodex
	next, command := m.Update(providerAuthMsg{
		id:     provider.IDCodex,
		status: provider.AuthStatus{State: provider.AuthStateReady, Label: "signed in"},
	})
	m = next.(Model)
	if command == nil || m.projectSettings.EffectiveProvider() != provider.IDCodex {
		t.Fatalf("provider activation = provider:%q command:%v", m.projectSettings.EffectiveProvider(), command != nil)
	}
	if m.pickerMode {
		t.Fatal("provider picker stayed open after sign-in")
	}
}

func TestSlashMenuAnchorsAbovePrompt(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')

	lines := strings.Split(stripANSI(viewText(m)), "\n")
	cmdRow := -1
	promptRow := -1
	for i, line := range lines {
		if cmdRow < 0 && strings.Contains(line, "/new") {
			cmdRow = i
			continue
		}
		if cmdRow >= 0 && promptRow < 0 && strings.Contains(line, "╭") {
			promptRow = i
		}
	}
	if cmdRow < 0 {
		t.Fatalf("slash commands missing: %q", lines)
	}
	if promptRow < 0 {
		t.Fatalf("prompt row missing: %q", lines)
	}
	if cmdRow >= promptRow {
		t.Errorf("slash list row %d is not above prompt row %d: %q", cmdRow, promptRow, lines)
	}
	if strings.Contains(stripANSI(m.slashView()), "╭") || strings.Contains(stripANSI(m.slashView()), "╰") {
		t.Errorf("slash list should not have a border: %q", stripANSI(m.slashView()))
	}
	if got := m.prompt.Value(); got != "/" {
		t.Errorf("prompt = %q, want /", got)
	}
	if len(lines) > m.height {
		t.Errorf("slash view has %d rows for a %d-row terminal", len(lines), m.height)
	}
}

func TestSlashResumeAcceptsSessionAlias(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for _, r := range "/session" {
		m = typeRune(m, r)
	}
	if !m.slashMode || len(m.slashItems) != 1 || m.slashItems[0].name != "/resume" {
		t.Fatalf("filtered items = %+v, want only /resume", m.slashItems)
	}
	if v := stripANSI(m.slashView()); strings.Contains(v, "/sessions") {
		t.Errorf("typed /session should display /resume, got: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.slashMode {
		t.Fatal("slash mode still open after enter")
	}
	if !m.sessionPickerMode {
		t.Fatal("enter on /session did not open the resume picker")
	}
}

func TestSlashResumeOpensPicker(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for _, r := range "/resume" {
		m = typeRune(m, r)
	}
	if !m.slashMode || len(m.slashItems) != 1 || m.slashItems[0].name != "/resume" {
		t.Fatalf("filtered items = %+v, want only /resume", m.slashItems)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.slashMode {
		t.Fatal("slash mode still open after enter")
	}
	if !m.sessionPickerMode {
		t.Fatal("enter on /resume did not open the resume picker")
	}
}

func TestSlashMenuFilterAndRunNew(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	m = typeRune(m, 'm')
	if len(m.slashItems) != 2 || m.slashItems[0].name != "/model" || m.slashItems[1].name != "/memory" {
		t.Fatalf("filtered items = %+v, want /model and /memory", m.slashItems)
	}
	if v := stripANSI(m.slashView()); strings.Contains(v, "Session") || strings.Contains(v, "Help") || !strings.Contains(v, "Project") {
		t.Errorf("filtered slash view has incorrect groups: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.slashMode {
		t.Fatal("slash mode still open after esc")
	}
	if got := m.prompt.Value(); got != "/" {
		t.Fatalf("prompt after esc = %q, want /", got)
	}

	m = typeRune(m, 'm')
	if !m.slashMode || len(m.slashItems) != 2 || m.slashItems[0].name != "/model" || m.slashItems[1].name != "/memory" {
		t.Fatalf("menu not reopened with /model and /memory filter: %+v", m.slashItems)
	}
	m.slashMode = false
	m.prompt.SetValue("")
	m, _ = m.runSlashArg("/model", "")
	if !m.settingsMode || m.pickerMode || m.settingsCurrentSection() != settingsSectionModel || m.settingsCursor != settingsRowModel {
		t.Fatal("/model did not open the model settings row")
	}
	if got := m.prompt.Value(); got != "" {
		t.Fatalf("prompt after /model enter = %q, want empty", got)
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m.prompt.SetValue("")
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "old line"})
	m = typeRune(m, '/')
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.slashMode {
		t.Fatal("slash mode still open after /new")
	}
	if len(m.items) != 0 {
		t.Errorf("transcript not cleared by /new: %d items", len(m.items))
	}
	if m.session != nil {
		t.Errorf("/new should drop the session for a fresh one")
	}
}

func TestSlashMenuFullWidthLeftAligned(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash mode not opened on /")
	}

	var modelLine, newLine, footer string
	var widest int
	lines := strings.Split(stripANSI(m.slashView()), "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
		if strings.Contains(line, "/model") && !strings.Contains(line, "commands") && modelLine == "" {
			modelLine = line
		}
		if strings.Contains(line, "/new") && !strings.Contains(line, "commands") && newLine == "" {
			newLine = line
		}
		if i == len(lines)-1 {
			footer = line
		}
	}
	if widest < m.width-4 {
		t.Errorf("slash card width = %d, want near %d", widest, m.width)
	}
	if modelLine == "" {
		t.Fatal("/model row missing")
	}
	if strings.Contains(modelLine, "open model settings") {
		t.Fatalf("compact /model row should be name-only: %q", modelLine)
	}
	if !strings.Contains(footer, "start a new session") {
		t.Errorf("compact footer missing selected description: %q", footer)
	}

	wide, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = wide.(Model)
	modelLine, newLine = "", ""
	for _, line := range strings.Split(stripANSI(m.slashView()), "\n") {
		if strings.Contains(line, "/model") && !strings.Contains(line, "commands") && modelLine == "" {
			modelLine = line
		}
		if strings.Contains(line, "/new") && !strings.Contains(line, "commands") && newLine == "" {
			newLine = line
		}
	}
	if modelLine == "" {
		t.Fatal("wide /model row missing")
	}
	nameAt := strings.Index(modelLine, "/model")
	descAt := strings.Index(modelLine, "open model settings")
	if nameAt < 0 || descAt < 0 || nameAt > descAt {
		t.Fatalf("/model should sit left of its description on the same line: %q", modelLine)
	}
	if !strings.Contains(newLine, "start a new session") {
		t.Errorf("/new line missing its description: %q", newLine)
	}
}

func TestSlashMenuEscapeLeavesSlash(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	m = typeRune(m, 'h')
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.slashMode {
		t.Fatal("slash mode still open")
	}
	if got := m.prompt.Value(); got != "/" {
		t.Errorf("prompt after esc = %q, want /", got)
	}
}

func TestSlashModelSearchFiltersDrawer(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.models = []string{"deepseek-v4-flash", "deepseek-v4-flash-free", "claude-4", "grok-4.5"}
	m.modelInfos = []modelscache.Info{
		{ID: "deepseek-v4-flash", Provider: modelscache.ProviderOpenCodeGo},
		{ID: "deepseek-v4-flash-free", Provider: modelscache.ProviderOpenCodeZen, Free: true},
		{ID: "claude-4", Provider: modelscache.ProviderOpenCodeGo},
		{ID: "grok-4.5", Provider: modelscache.ProviderOpenCodeGo},
	}

	for _, key := range "/model" {
		m = typeRune(m, key)
	}
	if m.slashMode || !m.pickerMode || m.pickerKind != pickerKindModel {
		t.Fatalf("typing /model state = slash=%v picker=%v kind=%q", m.slashMode, m.pickerMode, m.pickerKind)
	}
}

func TestSlashModelSearchBackspaceReturnsToMenu(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m, _ = m.runSlashArg("/model", "")
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.settingsMode {
		t.Fatal("escape did not close the model settings entry")
	}
}

func TestSlashModelUnmatchedQueryHidesDrawer(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m, _ = m.runSlashArg("/model", "anything after the command")
	if !m.settingsMode || m.pickerMode {
		t.Fatalf("/model arguments left an unexpected picker state: settings=%v picker=%v", m.settingsMode, m.pickerMode)
	}
}

func TestSlashVariantOpensPicker(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Variants: []string{"low", "medium", "high"}}}
	m, _ = m.runSlashArg("/variant", "")
	if !m.settingsMode || m.pickerMode || m.settingsCurrentSection() != settingsSectionModel || m.settingsCursor != settingsRowVariant {
		t.Fatalf("/variant state = settings=%v picker=%v section=%d row=%d", m.settingsMode, m.pickerMode, m.settingsCurrentSection(), m.settingsCursor)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.pickerMode || m.pickerKind != pickerKindVariant {
		t.Fatalf("variant row did not open the picker: mode=%v kind=%q", m.pickerMode, m.pickerKind)
	}
	v := stripANSI(m.pickerView())
	if !strings.Contains(v, "low") || !strings.Contains(v, "medium") || !strings.Contains(v, "high") {
		t.Fatalf("variant picker missing options: %q", v)
	}
	m.pickerCursor = 2
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pickerMode {
		t.Fatal("picker still open after select")
	}
	if m.variant != "high" {
		t.Fatalf("variant = %q, want high", m.variant)
	}
}
