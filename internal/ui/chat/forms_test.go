package chat

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/subagent"
)

func TestFormHostFocusIntegration(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)

	if m.formMode {
		t.Fatal("formMode should be false on init")
	}

	m, _ = m.openSubagentSpawnForm()
	if !m.formMode {
		t.Fatal("openSubagentSpawnForm did not set formMode")
	}
	if m.currentFocus() != focusForm {
		t.Fatalf("currentFocus = %v, want focusForm", m.currentFocus())
	}
	if m.promptEditing() {
		t.Fatal("promptEditing should be false while form is open")
	}

	// Escape closes the form and restores focus
	m, _ = m.updateFormKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.formMode {
		t.Fatal("escape did not close formMode")
	}
	if m.currentFocus() == focusForm {
		t.Fatal("currentFocus still focusForm after escape")
	}
}

func TestSubagentSpawnFormRender(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = mm.(Model).openSubagentSpawnForm()

	v := ansi.Strip(viewText(m))
	for _, want := range []string{"spawn sub-agent", "Prompt / Task Description", "Model", "Max Steps", "Run in background"} {
		if !strings.Contains(v, want) {
			t.Errorf("form view missing %q:\n%s", want, v)
		}
	}
}

func pumpTestCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- nil
			}
		}()
		ch <- cmd()
	}()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(30 * time.Millisecond):
		return
	}
	if msg == nil {
		return
	}
	switch mmsg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range mmsg {
			pumpTestCmds(m, c)
		}
	case cursor.BlinkMsg, pulseMsg, tipsTickMsg:
		// ignore background timers in test runner
	default:
		mm, nextCmd := m.Update(mmsg)
		*m = mm.(Model)
		pumpTestCmds(m, nextCmd)
	}
}

func TestSettingInputFormSave(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	savedVal := ""
	var cmd tea.Cmd
	m, cmd = m.openSettingInputForm("Max Steps", "turn steps", "30", nil, func(mod Model, val string) (Model, tea.Cmd) {
		savedVal = val
		return mod, nil
	})
	pumpTestCmds(&m, cmd)

	if !m.formMode {
		t.Fatal("setting form not open")
	}

	// Press Enter to submit the current value
	mm, next := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(Model)
	pumpTestCmds(&m, next)

	if savedVal != "30" {
		t.Fatalf("savedVal = %q, want 30", savedVal)
	}
	if m.formMode {
		t.Fatal("formMode should be closed after submit")
	}
}

func TestSubagentSpawnFormVariousSizes(t *testing.T) {
	sizes := []struct {
		w, h int
	}{
		{60, 20},
		{80, 24},
		{100, 30},
		{140, 45},
	}
	for _, sz := range sizes {
		m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
		mm, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m, _ = mm.(Model).openSubagentSpawnForm()

		v := ansi.Strip(viewText(m))
		if !strings.Contains(v, "spawn sub-agent") {
			t.Fatalf("size %dx%d missing title in view:\n%s", sz.w, sz.h, v)
		}
		if !strings.Contains(v, "Prompt / Task Description") {
			t.Fatalf("size %dx%d missing prompt description in view:\n%s", sz.w, sz.h, v)
		}
	}
}

func TestSpawnSubagentFromForm(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)

	m, _ = m.spawnSubagentFromForm(subagent.Spec{
		Prompt:      "run unit tests",
		Description: "test run",
		MaxSteps:    10,
		Background:  true,
	})

	if m.err != "" {
		t.Fatalf("spawn error: %s", m.err)
	}
	if !m.subagentPickerMode {
		t.Fatal("subagent picker drawer should be open after spawn")
	}
	if len(m.subagentItems) == 0 {
		t.Fatal("subagent items should contain newly spawned job")
	}
	if m.subagentItems[0].Name != "test run" {
		t.Fatalf("unexpected subagent item: %+v", m.subagentItems[0])
	}
}

func TestSubagentSpawnFormInteractiveInput(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{
		Store:   st,
		Client:  deadClient(),
		Workdir: t.TempDir(),
	})
	m.models = []string{"anthropic/claude-3-7-sonnet", "openai/gpt-4o"}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	var cmd tea.Cmd
	m, cmd = mm.(Model).openSubagentSpawnForm()
	pumpTestCmds(&m, cmd)

	if !m.formMode {
		t.Fatal("formMode should be true")
	}

	step := func(msg tea.Msg) {
		mm, cmd := m.Update(msg)
		m = mm.(Model)
		pumpTestCmds(&m, cmd)
	}

	// 1. Type prompt "build search indexing"
	for _, ch := range "build search indexing" {
		step(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}

	// 2. Press Enter to advance to Model select
	step(tea.KeyPressMsg{Code: tea.KeyEnter})

	// 3. Press Down to select first model (anthropic/claude-3-7-sonnet)
	step(tea.KeyPressMsg{Code: tea.KeyDown})

	// 4. Press Enter to advance to Max Steps
	step(tea.KeyPressMsg{Code: tea.KeyEnter})

	// 5. Clear / Type steps "45"
	// Backspace existing "30"
	step(tea.KeyPressMsg{Code: tea.KeyBackspace})
	step(tea.KeyPressMsg{Code: tea.KeyBackspace})
	for _, ch := range "45" {
		step(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}

	// 6. Press Enter to advance to Background toggle
	step(tea.KeyPressMsg{Code: tea.KeyEnter})

	// 7. Press Enter to submit form
	step(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.formMode {
		t.Fatal("formMode should be closed after submission")
	}
	if !m.subagentPickerMode {
		t.Fatal("subagent picker drawer should open upon submission")
	}
	if len(m.subagentItems) == 0 {
		t.Fatal("subagent items should have the spawned subagent")
	}
	item := m.subagentItems[0]
	if item.Name != "build search indexing" {
		t.Fatalf("item.Name = %q, want 'build search indexing'", item.Name)
	}
	if item.Model != "anthropic/claude-3-7-sonnet" {
		t.Fatalf("item.Model = %q, want 'anthropic/claude-3-7-sonnet'", item.Model)
	}
}
