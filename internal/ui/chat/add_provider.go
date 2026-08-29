package chat

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

type addProviderData struct {
	template string
	id       string
	label    string
	baseURL  string
	envKey   string
	model    string
}

type providerTemplate struct {
	id       string
	label    string
	auth     string
	baseURL  string
	envKey   string
	model    string
}

var providerTemplates = []providerTemplate{
	{id: "opencode", label: "OpenCode", auth: "api_key", baseURL: "", envKey: "OPENCODE_API_KEY", model: "deepseek-v4-flash"},
	{id: "openai", label: "OpenAI", auth: "api_key", baseURL: "https://api.openai.com/v1", envKey: "OPENAI_API_KEY", model: "gpt-4.1-mini"},
	{id: "gemini", label: "Gemini", auth: "api_key", baseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", envKey: "GEMINI_API_KEY", model: "gemini-3-flash-preview"},
	{id: "grok", label: "Grok", auth: "grok", baseURL: "", envKey: "XAI_API_KEY", model: "grok-4.6"},
	{id: "codex", label: "Codex", auth: "codex", baseURL: "", envKey: "", model: ""},
	{id: "xai", label: "xAI", auth: "api_key", baseURL: "https://api.x.ai/v1", envKey: "XAI_API_KEY", model: "grok-4.6"},
	{id: "custom", label: "Custom", auth: "api_key", baseURL: "https://example.com/v1", envKey: "CUSTOM_API_KEY", model: ""},
}

func providerTemplateOptions() []huh.Option[string] {
	out := make([]huh.Option[string], 0, len(providerTemplates))
	for _, t := range providerTemplates {
		out = append(out, huh.NewOption(t.label, t.id))
	}
	return out
}

func findProviderTemplate(id string) providerTemplate {
	for _, t := range providerTemplates {
		if t.id == id {
			return t
		}
	}
	return providerTemplates[0]
}

func nextOpenCodePlaceholder() string {
	count := 0
	for _, id := range provider.IDs() {
		if strings.HasPrefix(strings.ToLower(id), "opencode") {
			count++
		}
	}
	if count == 0 {
		return "opencode"
	}
	return fmt.Sprintf("opencode-%d", count+1)
}

func randomProviderPlaceholder(templateID string) string {
	if templateID == "opencode" {
		return nextOpenCodePlaceholder()
	}
	if templateID == "custom" {
		return fmt.Sprintf("custom-provider-%d", rand.Intn(900)+100)
	}
	if templateID == "gemini" {
		return "gemini"
	}
	tmpl := findProviderTemplate(templateID)
	if tmpl.id != "" && tmpl.id != "custom" {
		exists := false
		for _, id := range provider.IDs() {
			if id == tmpl.id {
				exists = true
				break
			}
		}
		if !exists {
			return tmpl.id
		}
		return fmt.Sprintf("%s-%d", tmpl.id, rand.Intn(90)+10)
	}
	return fmt.Sprintf("provider-%d", rand.Intn(900)+100)
}

func nextOpenCodeLabelPlaceholder() string {
	count := 0
	for _, id := range provider.IDs() {
		if strings.HasPrefix(strings.ToLower(id), "opencode") {
			count++
		}
	}
	if count == 0 {
		return "OpenCode"
	}
	return fmt.Sprintf("OpenCode %d", count+1)
}

func (m Model) openAddProviderDialog() (Model, tea.Cmd) {
	templateID := "opencode"
	placeholderID := nextOpenCodePlaceholder()
	placeholderLabel := nextOpenCodeLabelPlaceholder()
	tmpl := findProviderTemplate(templateID)
	data := &addProviderData{
		template: templateID,
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("template").
				Title("Template").
				Description("Choose a provider template").
				Options(providerTemplateOptions()...).
				Value(&data.template),
			huh.NewInput().
				Key("id").
				Title("Provider ID").
				Description("Unique id used in settings and picker").
				Placeholder(placeholderID).
				Value(&data.id).
				Validate(func(s string) error {
					id := strings.TrimSpace(s)
					if id == "" {
						id = placeholderID
					}
					if strings.Contains(id, " ") {
						return fmt.Errorf("id must not contain spaces")
					}
					return nil
				}),
			huh.NewInput().
				Key("label").
				Title("Label").
				Description("Display name in the picker").
				Placeholder(placeholderLabel).
				Value(&data.label),
			huh.NewInput().
				Key("base_url").
				Title("Base URL").
				Description("API base URL, empty for default").
				Placeholder(tmpl.baseURL).
				Value(&data.baseURL),
			huh.NewInput().
				Key("env_key").
				Title("Env Key").
				Description("Env var holding the credential").
				Placeholder(tmpl.envKey).
				Value(&data.envKey),
			huh.NewInput().
				Key("model").
				Title("Default Model").
				Description("Default model for this provider").
				Placeholder(tmpl.model).
				Value(&data.model),
		),
	).WithTheme(formTheme()).WithWidth(min(formOverlayMaxWidth, max(minPaneWidth, m.width-cardBorder)))

	host := &formHost{
		form:  form,
		title: "Add New Provider",
		kind:  "add-provider",
		width: min(formOverlayMaxWidth, max(minPaneWidth, m.width-cardBorder)),
		onDone: func(mod Model) (Model, tea.Cmd) {
			mod = mod.clearFocus(focusForm)
			id := strings.TrimSpace(data.id)
			if id == "" {
				id = randomProviderPlaceholder(strings.TrimSpace(data.template))
				if strings.TrimSpace(data.template) == "opencode" {
					id = nextOpenCodePlaceholder()
				}
			}
			label := strings.TrimSpace(data.label)
			if label == "" {
				if strings.TrimSpace(data.template) == "opencode" {
					label = nextOpenCodeLabelPlaceholder()
				} else {
					t := findProviderTemplate(strings.TrimSpace(data.template))
					label = t.label
					if label == "" {
						label = id
					}
				}
			}
			tmplSel := findProviderTemplate(strings.TrimSpace(data.template))
			baseURL := strings.TrimSpace(data.baseURL)
			if baseURL == "" {
				baseURL = tmplSel.baseURL
			}
			envKey := strings.TrimSpace(data.envKey)
			if envKey == "" {
				envKey = tmplSel.envKey
			}
			model := strings.TrimSpace(data.model)
			if model == "" {
				model = tmplSel.model
			}
			if err := mod.saveProviderFromDialog(id, label, tmplSel.auth, baseURL, envKey, model); err != nil {
				mod.err = err.Error()
				return mod, nil
			}
			_, _ = provider.LoadProviders(mod.workdir)
			mod.pickerProviderIDs = nil
			for _, d := range provider.Descriptors() {
				mod.providerAuthStatus[d.ID] = provider.InitialAuthStatus(d.ID)
			}
			if mod.pickerMode && mod.pickerKind == pickerKindProvider {
				mod.applyFilter()
			}
			mod.copyNotice = "provider added: " + id
			return mod, tea.Batch(clearCopyNotice(), mod.checkProviderAuth(id))
		},
		onCancel: func(mod Model) (Model, tea.Cmd) {
			return mod.clearFocus(focusForm), nil
		},
	}
	m = m.setFocus(focusForm)
	m.prompt.SetValue("")
	m.promptUndo = nil
	m.slashMode = false
	m.formHost = host
	cmd := form.Init()
	host.form = form
	return m, cmd
}

func (m Model) saveProviderFromDialog(id, label, auth, baseURL, envKey, model string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	if _, exists := provider.DescriptorFor(id); exists {
		return fmt.Errorf("provider %q already exists", id)
	}
	path := filepath.Join(m.workdir, ".lazykoder", "providers.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read providers.json: %w", err)
	}
	var list []map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("providers.json is invalid JSON: %w", err)
		}
	}
	if list == nil {
		list = []map[string]any{}
	}
	for _, item := range list {
		if v, ok := item["id"].(string); ok && strings.ToLower(strings.TrimSpace(v)) == id {
			return fmt.Errorf("provider %q already exists in file", id)
		}
	}
	entry := map[string]any{
		"id":           id,
		"label":        label,
		"auth_method":  auth,
		"supported":    true,
		"display_order": len(list)*10 + 60,
	}
	if baseURL != "" {
		entry["base_url"] = baseURL
	}
	if envKey != "" {
		if auth == "api_key" && strings.Contains(envKey, ",") {
			parts := strings.Split(envKey, ",")
			var keys []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					keys = append(keys, p)
				}
			}
			entry["env_keys"] = keys
		} else {
			entry["env_key"] = envKey
		}
	}
	if model != "" {
		entry["model"] = model
	}
	if auth == "grok" {
		entry["cli"] = "grok"
	}
	if auth == "codex" {
		entry["cli"] = "codex"
	}
	list = append(list, entry)
	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

func (m Model) addProviderOverlayView() string {
	if m.formHost == nil || m.formHost.form == nil {
		return ""
	}
	cardW := max(minPaneWidth, min(formOverlayMaxWidth, m.overlayWidth()))
	innerW := max(1, cardW-cardBorder-cardBorderPad)
	m.formHost.form.WithWidth(innerW)
	headTitle := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(m.formHost.title)
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	gap := max(1, innerW-lipgloss.Width(headTitle)-lipgloss.Width(closeBtn))
	header := headTitle + strings.Repeat(" ", gap) + closeBtn
	body := m.formHost.form.View()
	hint := hintStyle.Render("tab/shift-tab navigate  •  enter submit  •  esc cancel  •  click x to close")
	layout := strings.TrimSpace(providerAddProviderPreview(m))
	preview := ""
	if layout != "" {
		preview = lipgloss.NewStyle().Foreground(theme.ColorMute()).Width(innerW).Render(layout) + "\n"
	}
	cardContent := lipgloss.JoinVertical(lipgloss.Left, header, "", preview, body, "", hint)
	cardContent = keepBackground(cardContent, theme.ColorSurface())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		BorderBackground(theme.ColorSurface()).
		Background(theme.ColorSurface()).
		Padding(1, cardHorzPad).
		Width(cardW).
		Render(cardContent)
}

func providerAddProviderPreview(m Model) string {
	if m.formHost == nil {
		return ""
	}
	// Show the first template as preview when the form is pristine.
	// The live template choice is stored in the form data pointer, but the
	// preview is intentionally static to avoid huh internal state coupling.
	t := findProviderTemplate("opencode")
	preview := map[string]any{
		"id":          "example: " + randomProviderPlaceholder(t.id),
		"label":       t.label,
		"auth_method": t.auth,
		"base_url":    t.baseURL,
		"env_key":     t.envKey,
		"model":       t.model,
	}
	b, _ := json.MarshalIndent(preview, "", "  ")
	return "Template JSON preview:\n" + string(b)
}

func (m Model) addProviderCloseRect() (x0, y, x1 int, ok bool) {
	if m.formHost == nil || m.formHost.kind != "add-provider" {
		return 0, 0, 0, false
	}
	if !m.formMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.frame(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "Add New Provider") || !strings.Contains(plain, "[x]") {
			continue
		}
		start, end, found := displaySpan(plain, "[x]")
		if !found {
			continue
		}
		return max(0, start-1), i, end + 1, true
	}
	return 0, 0, 0, false
}
