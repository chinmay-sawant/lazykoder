package chat

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestStatusPickerTogglesAndPersistsModelSegment(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Directory: t.TempDir(), Model: "model-a"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: sess.Directory, Session: &sess})
	m.tokensPerSec = 80
	m = m.runSlashStatus()
	if !strings.Contains(stripANSI(viewText(m)), "status") {
		t.Fatalf("status picker missing: %q", stripANSI(viewText(m)))
	}
	var cmd tea.Cmd
	m, cmd = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("status toggle returned no persistence command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("status persistence returned %v", msg)
	}
	m, _ = m.updateStatusKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	v := stripANSI(viewText(m))
	if strings.Contains(v, "model-a") {
		t.Fatalf("model segment remained visible: %q", v)
	}
	if !strings.Contains(v, "80 tps") {
		t.Fatalf("tps segment disappeared with model: %q", v)
	}
	got, err := st.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.StatusSegments[0] != "tps" {
		t.Fatalf("persisted segments = %v, want tps first", got.StatusSegments)
	}
}

func (m Model) runSlashStatus() Model {
	m, _ = m.runSlash("/status")
	return m
}
