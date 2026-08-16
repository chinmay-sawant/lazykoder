package chat

import (
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestReasoningStreamsThenCollapsesOnReply(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	thought := "planning the reply"
	m.applyPart(db.Part{ID: "prt_r1", Type: "reasoning", Text: &thought})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, thought) {
		t.Fatalf("live thinking missing: %q", v)
	}
	if !strings.Contains(v, streamCursor) {
		t.Fatalf("stream cursor missing while thinking: %q", v)
	}
	more := thought + " in more detail"
	m.applyPart(db.Part{ID: "prt_r1", Type: "reasoning", Text: &more})
	v = stripANSI(viewText(m))
	if !strings.Contains(v, more) {
		t.Fatalf("streamed thinking missing: %q", v)
	}
	reply := "here is the answer"
	m.applyPart(db.Part{ID: "prt_t1", Type: "text", Text: &reply})
	v = stripANSI(viewText(m))
	if strings.Contains(v, more) {
		t.Fatalf("thinking stayed open after reply: %q", v)
	}
	if !strings.Contains(v, thinkingLabel) {
		t.Fatalf("thinking header missing: %q", v)
	}
	if !strings.Contains(v, reply) {
		t.Fatalf("reply missing: %q", v)
	}
	if strings.Contains(v, streamCursor) {
		t.Fatalf("stream cursor leaked after collapse: %q", v)
	}
}

func TestFinishTurnCollapsesReasoning(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	thought := "only thinking"
	m.applyPart(db.Part{ID: "prt_r2", Type: "reasoning", Text: &thought})
	if !strings.Contains(stripANSI(viewText(m)), thought) {
		t.Fatalf("live thinking missing: %q", viewText(m))
	}
	m = m.finishTurn(nil)
	v := stripANSI(viewText(m))
	if strings.Contains(v, thought) {
		t.Fatalf("thinking stayed open after the turn ended: %q", v)
	}
	if !strings.Contains(v, thinkingLabel) {
		t.Fatalf("thinking header missing: %q", v)
	}
}

func TestReasoningCollapsesWhenToolStarts(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	thought := "need a command"
	m.applyPart(db.Part{ID: "prt_r3", Type: "reasoning", Text: &thought})
	m.applyTool(agent.Event{
		Tool: db.ToolCall{Tool: "bash", Status: "pending", InputJSON: `{"command":"ls"}`},
		Part: db.Part{Type: "tool"},
	})
	v := stripANSI(viewText(m))
	if strings.Contains(v, thought) {
		t.Fatalf("thinking stayed open after a tool started: %q", v)
	}
	if !strings.Contains(v, "bash") {
		t.Fatalf("tool card missing: %q", v)
	}
}

func TestReasoningWrapsAtPaneWidth(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 60
	m.busy = true
	thought := strings.Repeat("planning step ", 15) + "done"
	m.applyPart(db.Part{ID: "prt_r4", Type: "reasoning", Text: &thought})
	m.syncTranscript()
	m.transcript.SetWidth(60)
	view := stripANSI(viewText(m))
	lines := strings.Split(view, "\n")
	wrapped := false
	for _, line := range lines {
		if !strings.Contains(line, thought) && strings.Contains(line, "planning step") {
			wrapped = true
			break
		}
	}
	if !wrapped {
		t.Fatalf("long reasoning not wrapped at pane width: %q", view)
	}
}
