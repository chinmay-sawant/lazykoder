package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestCommitPushButtonUsesInjectedWorktreeStatus(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.height = 24
	m.pushPromptUntil = time.Now().Add(time.Minute)
	if !m.commitPushVisible() {
		t.Fatal("commit action should be visible during its window")
	}
	if !strings.Contains(stripANSI(m.commitPushRow()), "commit and push") {
		t.Fatal("commit action row missing label")
	}

	m.worktreeDirty = func(context.Context, string) (bool, error) { return true, nil }
	msg := m.checkWorktree()()
	status, ok := msg.(worktreeStatusMsg)
	if !ok || !status.dirty {
		t.Fatalf("worktree status = %#v", msg)
	}
}

func TestCommitPushActivationKeepsPromptWireOnly(t *testing.T) {
	store := newTestStore(t)
	session, err := store.CreateSession(context.Background(), db.Session{Title: "action", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: store, Client: deadClient(), Session: &session, Workdir: session.Directory})
	m.pushPromptUntil = time.Now().Add(time.Minute)
	next, cmd := m.activateCommitPush()
	if cmd == nil || !next.busy || !next.pushPromptBusy {
		t.Fatalf("activation state busy=%v pushBusy=%v cmd=%v", next.busy, next.pushPromptBusy, cmd != nil)
	}
}
