package chat

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLayoutSnapSettingsMatchesPaint(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.openSettings()
	m = m.ensureLayout()

	if !m.layout.settingsCloseOK {
		t.Fatal("layout snap missing settings close rect")
	}
	if m.layout.settingsPaint == "" {
		t.Fatal("layout snap missing settings paint")
	}
	if len(m.layout.settingsRowByY) == 0 {
		t.Fatal("layout snap missing settings row map")
	}

	x0, y, x1, ok := m.settingsCloseRect()
	if !ok || y != m.layout.settingsCloseY || x0 != m.layout.settingsCloseX0 || x1 != m.layout.settingsCloseX1 {
		t.Fatalf("close rect snap mismatch: ok=%v got (%d,%d,%d) snap (%d,%d,%d)",
			ok, x0, y, x1, m.layout.settingsCloseX0, m.layout.settingsCloseY, m.layout.settingsCloseX1)
	}

	// Second ensureLayout with same key must be a cache hit (same paint pointer values).
	before := m.layout.settingsPaint
	m2 := m.ensureLayout()
	if m2.layout.settingsPaint != before {
		t.Fatal("ensureLayout rebuilt settings paint with unchanged key")
	}
}

func TestLayoutSnapChatBands(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = m.ensureLayout()

	if m.layout.transcriptH < 1 {
		t.Fatalf("transcriptH=%d", m.layout.transcriptH)
	}
	if m.layout.jumpBarRow != m.layout.transcriptTop+m.layout.transcriptH {
		t.Fatalf("jumpBarRow=%d want %d", m.layout.jumpBarRow, m.layout.transcriptTop+m.layout.transcriptH)
	}
	if m.layout.composerTop < m.layout.jumpBarRow {
		t.Fatalf("composerTop %d < jumpBar %d", m.layout.composerTop, m.layout.jumpBarRow)
	}
}
