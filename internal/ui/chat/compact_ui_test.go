package chat

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

func TestModelShrinkSetsCompactHint(t *testing.T) {
	tmp := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{
		Title: "s", Directory: tmp, Model: "deepseek-large",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: tmp, Session: &sess})
	m.model = "deepseek-large"
	m.tokensUsed = 400_000
	m.modelInfos = []modelscache.Info{
		{ID: "deepseek-large", Context: 1_000_000},
		{ID: "small-256k", Context: 256_000},
	}
	m.pickerItems = []string{"deepseek-large", "small-256k"}
	m.pickerBuilt = true
	m.pickerMode = true
	m, _ = m.selectPickerItem(1)
	if m.pendingCompactReason != agent.CompactReasonShrink {
		t.Fatalf("reason = %q", m.pendingCompactReason)
	}
	if m.session == nil || m.session.Model != "small-256k" {
		t.Fatalf("in-memory session model = %+v", m.session)
	}
	if !strings.Contains(m.compactHint, "next send will compact") {
		t.Fatalf("hint = %q", m.compactHint)
	}
	if !strings.Contains(m.compactHint, "1000k") || !strings.Contains(m.compactHint, "256k") {
		t.Fatalf("hint missing windows: %q", m.compactHint)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model)
	if !strings.Contains(stripANSI(viewText(m)), "next send will compact") {
		t.Fatalf("view missing hint: %q", stripANSI(viewText(m)))
	}
}

func TestLargerWindowClearsCompactHint(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "small-256k"
	m.tokensUsed = 80_000
	m.pendingCompactReason = agent.CompactReasonShrink
	m.compactHint = "stale"
	m.modelInfos = []modelscache.Info{
		{ID: "small-256k", Context: 256_000},
		{ID: "deepseek-large", Context: 1_000_000},
	}
	m = m.refreshCompactHint("small-256k", 256_000)
	if m.pendingCompactReason != "" || m.compactHint != "" {
		t.Fatalf("hint should clear on larger window: %q %q", m.pendingCompactReason, m.compactHint)
	}
}

func TestUnknownWindowSkipsShrinkHint(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "mystery"
	m.tokensUsed = 400_000
	m.modelInfos = []modelscache.Info{{ID: "mystery"}}
	m = m.refreshCompactHint("old", 1_000_000)
	if m.pendingCompactReason != "" {
		t.Fatalf("unknown window should skip: %q", m.pendingCompactReason)
	}
}

func TestPromptStatusShowsCompacting(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.compacting = true
	m.busy = true
	if got := m.promptStatusValue(); got != "compacting" {
		t.Fatalf("status = %q", got)
	}
}

func TestSlashListsCompact(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/compact") {
		t.Fatalf("slash menu missing /compact: %q", v)
	}
}

func TestHelpListsCompact(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.helpMode = true
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	if !strings.Contains(stripANSI(m.helpOverlay()), "/compact") {
		t.Fatal("help missing /compact")
	}
}

func TestCompactSubmitParsesNotes(t *testing.T) {
	name, extra, ok := parseCompactSubmit("/compact focus on tests")
	if !ok || name != "/compact" || extra != "focus on tests" {
		t.Fatalf("got %q %q %v", name, extra, ok)
	}
}

func TestAgentOptionsPassCompaction(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "small-256k"
	m.prevModel = "deepseek-large"
	m.prevWindow = 1_000_000
	m.tokensUsed = 400_000
	m.pendingCompactReason = agent.CompactReasonShrink
	m.modelInfos = []modelscache.Info{
		{ID: "small-256k", Context: 256_000},
		{ID: "deepseek-large", Context: 1_000_000},
	}
	m.projectSettings = settings.Default()
	opts := m.agentOptions()
	if !opts.CompactAuto || opts.ContextWindow != 256_000 {
		t.Fatalf("opts window/auto = %+v", opts)
	}
	if opts.OutgoingModel != "deepseek-large" || opts.CompactReason != agent.CompactReasonShrink {
		t.Fatalf("opts outgoing/reason = %+v", opts)
	}
}
