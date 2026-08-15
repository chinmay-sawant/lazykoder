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
)

func TestModelPickerOpensOnlyFromModelClick(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})

	m = upd(m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.pickerMode {
		t.Fatal("pressing m opened the model picker")
	}
	if got := m.prompt.Value(); got != "m" {
		t.Fatalf("prompt after m = %q, want %q", got, "m")
	}

	m = clickModelStatus(t, m)
	if !m.pickerMode {
		t.Fatal("clicking the model status did not open the picker")
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
	card := v[strings.Index(v, "╭"):]
	if len(m.pickerItems) != 1 || m.pickerItems[0] != "claude-4" {
		t.Errorf("filtered picker items = %v, want [claude-4]", m.pickerItems)
	}
	if !strings.Contains(card, "AVAILABLE MODELS") || !strings.Contains(card, "claude-4") {
		t.Errorf("matching model missing: %q", card)
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

func typeRune(m Model, r rune) Model {
	return upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

func TestPickerCardCenteredSettingsLayout(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	card := stripANSI(m.pickerView())
	if !strings.Contains(v, "SETTINGS  /  MODEL") || !strings.Contains(v, "AVAILABLE MODELS") {
		t.Fatalf("settings card labels missing: %q", v)
	}
	if got, want := lipgloss.Width(card), m.width*cardWidthPct/100; got != want {
		t.Errorf("card width = %d, want %d (%d%% of screen)", got, want, cardWidthPct)
	}
	cardLine := strings.Index(strings.Split(v, "\n")[0], "╭")
	if cardLine >= 0 {
		t.Fatalf("card unexpectedly starts on the first screen row: %q", v)
	}
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "╭") {
			wantLeft := (m.width - lipgloss.Width(card)) / 2
			if got := strings.Index(line, "╭"); got != wantLeft {
				t.Errorf("card left offset = %d, want %d: %q", got, wantLeft, line)
			}
			return
		}
	}
	t.Fatalf("card border missing: %q", v)
}

func TestPickerCloseButton(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	headerFound := false
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "SETTINGS  /  MODEL") {
			headerFound = true
			if !strings.HasSuffix(strings.TrimSpace(line), "X│") {
				t.Fatalf("settings card close button is not at the header edge: %q", line)
			}
		}
	}
	if !headerFound {
		t.Fatalf("settings card header missing: %q", v)
	}
	x, y, ok := m.pickerCloseRect()
	if !ok {
		t.Fatal("picker close button rectangle not found")
	}
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.pickerMode {
		t.Fatal("clicking the picker close button did not close the card")
	}

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: 'x'})
	if m.pickerMode {
		t.Fatal("x did not close the picker card")
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
	lines := strings.Split(v, "\n")
	bottomBorder := -1
	for i, line := range lines {
		if strings.Contains(line, "╰") {
			bottomBorder = i
		}
	}
	if bottomBorder < 0 {
		t.Fatalf("card bottom border missing: %q", v)
	}
	if bottomBorder >= 30 {
		t.Errorf("card bottom (%d) below the 30-row screen", bottomBorder)
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
	if err := modelscache.Save(cachePath, []modelscache.Info{{ID: "deepseek-v4-flash"}, {ID: "claude-4"}}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	v := stripANSI(viewText(m))
	if !strings.Contains(v, "deepseek-v4-flash") {
		t.Errorf("status missing cached label: %q", v)
	}
	if !m.modelsCached {
		t.Error("modelsCached = false, want true for fresh cache")
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

	if len(m.models) != 2 {
		t.Errorf("refreshed models = %d, want 2", len(m.models))
	}
	if m.modelsCached {
		t.Error("modelsCached = true, want false after live refresh")
	}
	models, fresh, err := modelscache.Load(cachePath, time.Now(), modelscache.DefaultTTL)
	if err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	if len(models) != 2 || models[0].ID == "stale-model" {
		t.Errorf("cache not rewritten: %v", models)
	}
	if !fresh {
		t.Error("cache still stale after refresh")
	}
}

func TestModelsRefreshKeyReloadsFromAPI(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []modelscache.Info{{ID: "deepseek-v4-flash"}}, time.Now()); err != nil {
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

	if len(m.models) != 2 {
		t.Errorf("refreshed models = %d, want 2", len(m.models))
	}
	if m.modelsCached {
		t.Error("modelsCached = true, want false after manual refresh")
	}
}
