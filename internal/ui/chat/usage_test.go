package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func TestUsageSlashCommandAndModal(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	if !strings.Contains(stripANSI(viewText(m)), "/usage") {
		t.Fatalf("slash menu missing /usage: %q", stripANSI(viewText(m)))
	}
	m.usage = opencode.BillingUsage{
		Rolling: opencode.BillingWindow{Percent: 26, Status: "ok", ResetsAt: time.Now().Add(time.Hour)},
		Weekly:  opencode.BillingWindow{Percent: 10, Status: "ok", ResetsAt: time.Now().Add(24 * time.Hour)},
		Monthly: opencode.BillingWindow{Percent: 63, Status: "ok", ResetsAt: time.Now().Add(7 * 24 * time.Hour)},
	}
	m.usageLoaded = true
	m, _ = m.runSlashArg("/usage", "")
	v := stripANSI(viewText(m))
	for _, want := range []string{"USAGE", "rolling", "weekly", "monthly", "26%", "10%", "63%"} {
		if !strings.Contains(v, want) {
			t.Errorf("usage view missing %q: %q", want, v)
		}
	}
}

func TestSettingsDisplaysOpenCodeUsage(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 48})
	m = mm.(Model)
	m.usage = opencode.BillingUsage{
		Rolling: opencode.BillingWindow{Percent: 26, Status: "ok"},
		Weekly:  opencode.BillingWindow{Percent: 10, Status: "ok"},
		Monthly: opencode.BillingWindow{Percent: 63, Status: "ok"},
	}
	m.usageLoaded = true
	m = m.openSettings()
	v := stripANSI(viewText(m))
	for _, want := range []string{"opencode usage", "rolling", "weekly", "monthly", "26%", "10%", "63%"} {
		if !strings.Contains(v, want) {
			t.Errorf("settings view missing %q: %q", want, v)
		}
	}
}

func TestUsageModalRefreshKeyStartsFetch(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.usageMode = true
	m.usageLoaded = true
	m, cmd := m.updateUsageKey(keyPress("r"))
	if !m.usageLoading {
		t.Fatal("refresh did not mark usage loading")
	}
	if cmd == nil {
		t.Fatal("refresh did not return usage command")
	}
}

func keyPress(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: text, Code: rune(text[0])}
}
