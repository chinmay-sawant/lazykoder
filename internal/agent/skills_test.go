package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/skills"
)

func TestSendInjectsSkillsAfterRecallAndDoesNotPersist(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		Recall: func(context.Context, string, string) (string, error) {
			return "old memory hint", nil
		},
		Skills: func(context.Context, string, string) ([]skills.Context, error) {
			return []skills.Context{{
				Name: "review", Scope: skills.ScopeLocal, Path: "skills/review/SKILL.md",
				Body: "Check tests before changing code.",
			}}, nil
		},
	})

	events, err := sendAndCollect(t, a, "review this change")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	started, finished := -1, -1
	for index, event := range events {
		switch event.Kind {
		case EventSkillsStarted:
			started = index
		case EventSkillsFinished:
			finished = index
			if len(event.Skills) != 1 || event.Skills[0].Name != "review" {
				t.Fatalf("skills event = %+v", event.Skills)
			}
		}
	}
	if started < 0 || finished <= started {
		t.Fatalf("skill events = %d, %d", started, finished)
	}
	messages := fake.requestMessages(0)
	if len(messages) != 3 {
		t.Fatalf("request messages = %+v", messages)
	}
	if !strings.Contains(messages[0].Content, "untrusted historical hints") {
		t.Fatalf("recall message = %+v", messages[0])
	}
	if !strings.Contains(messages[1].Content, "Relevant local skills") || !strings.Contains(messages[1].Content, "Check tests") {
		t.Fatalf("skill message = %+v", messages[1])
	}
	if messages[2].Role != "user" {
		t.Fatalf("user message = %+v", messages[2])
	}
	persisted, err := st.ListMessages(context.Background(), a.sessionID())
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range persisted {
		if message.Role == "system" {
			t.Fatalf("skill system message persisted: %+v", message)
		}
	}
}

func TestSkillsRunOnceAndContinueDoesNotRescan(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{{ID: "unknown", Name: "not-a-tool", Args: `{}`}}, nil),
		respBody("continued", "", "stop", nil, nil),
	)
	st, _ := newTestEnv(t)
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		MaxSteps:         1,
		Skills: func(context.Context, string, string) ([]skills.Context, error) {
			calls++
			return []skills.Context{{Name: "review", Scope: skills.ScopeGlobal, Body: "hint"}}, nil
		},
	})
	if _, err := sendAndCollect(t, a, "work"); err == nil {
		t.Fatal("Send unexpectedly succeeded")
	}
	if _, err := continueAndCollect(t, a); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if calls != 1 {
		t.Fatalf("skill calls = %d, want 1", calls)
	}
	if strings.Contains(fake.requestMessages(1)[0].Content, "Relevant local skills") {
		t.Fatal("Continue injected skill context")
	}
}

func TestChildAgentDoesNotPrepareSkills(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		AgentName:        "worker",
		Skills: func(context.Context, string, string) ([]skills.Context, error) {
			calls++
			return []skills.Context{{Name: "review", Body: "hint"}}, nil
		},
	})
	if _, err := sendAndCollect(t, a, "work"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if calls != 0 {
		t.Fatalf("skill calls = %d, want 0", calls)
	}
}

func TestChildAgentUsesExplicitSkillContextWithoutScanning(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	calls := 0
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		DisableStreaming: true,
		AgentName:        "worker",
		SkillContext: []skills.Context{{
			Name: "review", Scope: skills.ScopeLocal, Path: "skills/review/SKILL.md",
			Body: "Use the focused review checklist.",
		}},
		Skills: func(context.Context, string, string) ([]skills.Context, error) {
			calls++
			return []skills.Context{{Name: "unexpected", Body: "should not scan"}}, nil
		},
	})
	if _, err := sendAndCollect(t, a, "work"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if calls != 0 {
		t.Fatalf("skill calls = %d, want 0", calls)
	}
	messages := fake.requestMessages(0)
	if len(messages) != 2 || !strings.Contains(messages[0].Content, "Use the focused review checklist") {
		t.Fatalf("child request messages = %+v", messages)
	}
}
