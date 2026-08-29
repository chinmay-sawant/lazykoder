package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	spawnFormSelectHeight = 4
	spawnFormMaxWidth     = 72
	settingFormMaxWidth   = 60
	formOverlayMaxWidth   = 106
)

type formHost struct {
	form     *huh.Form
	title    string
	kind     string
	width    int
	onDone   func(Model) (Model, tea.Cmd)
	onCancel func(Model) (Model, tea.Cmd)
}

func formTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(theme.CurrentMode() == theme.ModeDark)

		t.Focused.Base = lipgloss.NewStyle()
		t.Focused.Title = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
		t.Focused.Description = lipgloss.NewStyle().Foreground(theme.ColorMute())
		t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(theme.ColorAccent())
		t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(theme.ColorAccent())
		t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(theme.ColorAccent()).SetString("▸ ")
		t.Focused.Option = lipgloss.NewStyle().Foreground(theme.ColorText())
		t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Background(theme.ColorBorder())
		t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Background(theme.ColorBorder()).Padding(0, 1)
		t.Focused.BlurredButton = lipgloss.NewStyle().Foreground(theme.ColorMute()).Padding(0, 1)
		t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(theme.ColorDanger())
		t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(theme.ColorDanger())

		t.Blurred.Base = lipgloss.NewStyle()
		t.Blurred.Title = lipgloss.NewStyle().Foreground(theme.ColorMute())
		t.Blurred.Description = lipgloss.NewStyle().Foreground(theme.ColorMute())
		t.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(theme.ColorMute())
		t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(theme.ColorMute())
		t.Blurred.Option = lipgloss.NewStyle().Foreground(theme.ColorMute())
		t.Blurred.FocusedButton = lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Background(theme.ColorBorder()).Padding(0, 1)
		t.Blurred.BlurredButton = lipgloss.NewStyle().Foreground(theme.ColorMute()).Padding(0, 1)

		return t
	})
}

type spawnFormData struct {
	description string
	model       string
	steps       string
	background  bool
}

func (m Model) openSubagentSpawnForm() (Model, tea.Cmd) {
	data := &spawnFormData{
		steps:      strconv.Itoa(m.projectSettings.Agents.ChildMaxSteps),
		background: true,
	}
	if data.steps == "0" {
		data.steps = "30"
	}

	var modelOptions []huh.Option[string]
	modelOptions = append(modelOptions, huh.NewOption("default (session model)", ""))
	for _, mod := range m.models {
		modelOptions = append(modelOptions, huh.NewOption(mod, mod))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("description").
				Title("Prompt / Task Description").
				Description("What should the sub-agent do?").
				Placeholder("e.g. run test suite and report summary").
				Validate(huh.ValidateNotEmpty()).
				Value(&data.description),
			huh.NewSelect[string]().
				Key("model").
				Title("Model").
				Options(modelOptions...).
				Height(spawnFormSelectHeight).
				Value(&data.model),
			huh.NewInput().
				Key("steps").
				Title("Max Steps").
				Placeholder("30").
				Validate(func(s string) error {
					v, err := strconv.Atoi(strings.TrimSpace(s))
					if err != nil || v < 1 {
						return fmt.Errorf("steps must be a positive integer")
					}
					return nil
				}).
				Value(&data.steps),
			huh.NewConfirm().
				Key("background").
				Title("Run in background").
				Value(&data.background),
		),
	).WithTheme(formTheme()).WithWidth(min(spawnFormMaxWidth, max(minPaneWidth, m.width-cardBorder)))

	host := &formHost{
		form:  form,
		title: "spawn sub-agent",
		kind:  "spawn",
		width: min(spawnFormMaxWidth, max(minPaneWidth, m.width-cardBorder)),
		onDone: func(mod Model) (Model, tea.Cmd) {
			mod = mod.clearFocus(focusForm)
			steps, _ := strconv.Atoi(strings.TrimSpace(data.steps))
			if steps <= 0 {
				steps = mod.projectSettings.Agents.ChildMaxSteps
			}
			spec := subagent.Spec{
				Prompt:      strings.TrimSpace(data.description),
				Description: strings.TrimSpace(data.description),
				Model:       data.model,
				MaxSteps:    steps,
				Background:  data.background,
			}
			return mod.spawnSubagentFromForm(spec)
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

func (m Model) spawnSubagentFromForm(spec subagent.Spec) (Model, tea.Cmd) {
	if m.store == nil || m.client == nil {
		m.err = "cannot spawn: store or client unavailable"
		return m, nil
	}
	m = m.ensureSession(spec.Prompt)
	if m.session == nil {
		m.err = "cannot spawn: failed to initialize session"
		return m, nil
	}
	if !m.projectSettings.Agents.Enabled {
		m = m.setAgentsEnabled(true)
	}
	if m.subMgr == nil {
		m = m.rebuildSubMgr()
	}
	m.wireSubMgrRuntime()

	parentID := m.session.ID
	snap, err := m.subMgr.Spawn(context.Background(), parentID, "", spec)
	if err != nil {
		m.err = "spawn failed: " + err.Error()
		return m, nil
	}

	m = m.openSubagentPicker()
	m = m.reloadSubagentRows()
	m.pulseOn = true
	m.activity = "spawned " + snap.Name
	return m, pulseTick()
}

func (m Model) openSettingInputForm(title, desc, current string, validator func(string) error, onSave func(Model, string) (Model, tea.Cmd)) (Model, tea.Cmd) {
	val := current
	inp := huh.NewInput().
		Title(title).
		Description(desc).
		Value(&val)
	if validator != nil {
		inp.Validate(validator)
	}

	form := huh.NewForm(
		huh.NewGroup(inp),
	).WithTheme(formTheme()).WithWidth(min(settingFormMaxWidth, max(minPaneWidth, m.width-cardBorder)))

	host := &formHost{
		form:  form,
		title: title,
		kind:  "setting",
		width: min(settingFormMaxWidth, max(minPaneWidth, m.width-cardBorder)),
		onDone: func(mod Model) (Model, tea.Cmd) {
			mod = mod.clearFocus(focusForm)
			return onSave(mod, strings.TrimSpace(val))
		},
		onCancel: func(mod Model) (Model, tea.Cmd) {
			mod = mod.clearFocus(focusForm)
			return mod.openSettings(), nil
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

func (m Model) updateFormKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.formHost == nil || m.formHost.form == nil {
		return m.clearFocus(focusForm), nil
	}

	if key.Code == tea.KeyEscape {
		if m.formHost.onCancel != nil {
			return m.formHost.onCancel(m)
		}
		return m.clearFocus(focusForm), nil
	}
	return m.updateFormMsg(key)
}

func (m Model) updateFormMsg(msg tea.Msg) (Model, tea.Cmd) {
	if m.formHost == nil || m.formHost.form == nil {
		return m.clearFocus(focusForm), nil
	}

	updatedForm, cmd := m.formHost.form.Update(msg)
	if f, ok := updatedForm.(*huh.Form); ok {
		m.formHost.form = f
	}

	if m.formHost.form.State == huh.StateCompleted {
		if m.formHost.onDone != nil {
			return m.formHost.onDone(m)
		}
		return m.clearFocus(focusForm), nil
	}
	if m.formHost.form.State == huh.StateAborted {
		if m.formHost.onCancel != nil {
			return m.formHost.onCancel(m)
		}
		return m.clearFocus(focusForm), nil
	}

	return m, cmd
}

func (m Model) formOverlay() string {
	if m.formHost == nil || m.formHost.form == nil {
		return ""
	}
	if m.formHost.kind == "add-provider" {
		return m.addProviderOverlayView()
	}
	cardW := max(minPaneWidth, min(formOverlayMaxWidth, m.overlayWidth()))
	innerW := max(1, cardW-cardBorder-cardBorderPad)

	m.formHost.form.WithWidth(innerW)

	head := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(m.formHost.title)
	body := m.formHost.form.View()
	hint := hintStyle.Render("tab/shift-tab navigate  •  enter submit  •  esc cancel")

	cardContent := lipgloss.JoinVertical(lipgloss.Left, head, "", body, "", hint)
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
