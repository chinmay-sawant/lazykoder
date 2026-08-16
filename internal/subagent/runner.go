package subagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

// AgentRunner runs a child job via agent.Agent on a dedicated child session.
type AgentRunner struct {
	Store  *db.Store
	Client *opencode.Client
}

// Run creates a child session, runs one agent turn, and returns a summary.
func (r AgentRunner) Run(ctx context.Context, job Job) (Result, error) {
	res := Result{
		ID:   job.ID,
		Name: job.Name,
		Role: job.Role,
	}
	if r.Store == nil || r.Client == nil {
		res.Status = string(StatusFailed)
		res.Err = "subagent: runner missing store or client"
		return res, fmt.Errorf("%s", res.Err)
	}
	if strings.TrimSpace(job.Prompt) == "" {
		res.Status = string(StatusFailed)
		res.Err = "subagent: empty prompt"
		return res, fmt.Errorf("%s", res.Err)
	}
	workdir := job.Workdir
	var parentID *string
	if job.ParentSessionID != "" {
		parentID = &job.ParentSessionID
	}
	title := job.Name
	if title == "" {
		title = "subagent"
	}
	if len([]rune(title)) > 60 {
		title = string([]rune(title)[:60])
	}
	sess, err := r.Store.CreateSession(ctx, db.Session{
		Title:           title,
		Directory:       workdir,
		Model:           job.Model,
		Variant:         strPtr(job.Variant),
		ParentSessionID: parentID,
		Kind:            db.SessionKindSubagent,
	})
	if err != nil {
		res.Status = string(StatusFailed)
		res.Err = err.Error()
		return res, err
	}
	res.ChildSessionID = sess.ID
	// Publish session id before the turn so the drawer can merge live + store rows.
	if job.OnSession != nil {
		job.OnSession(sess.ID)
	}

	var ask func(q question.Question) (int, error)
	if job.Ask != nil {
		ask = func(q question.Question) (int, error) {
			return job.Ask(q.Question, q.Options)
		}
	}

	ag := agent.New(r.Store, r.Client, workdir, agent.Options{
		Session:          &sess,
		MaxSteps:         job.MaxSteps,
		Model:            job.Model,
		Endpoint:         job.Endpoint,
		Variant:          job.Variant,
		Confirm:          job.Confirm,
		Ask:              ask,
		Host:             nil, // depth 1: children cannot spawn
		ToolNames:        job.Tools,
		AgentName:        job.Name,
		DisableStreaming: true,
	})
	// Nudge the child to emit a final text answer inside its step budget.
	// Without this, multi-tool explores often burn every step on tools and
	// look like crashes to the parent (status failed / step limit).
	prompt := strings.TrimSpace(job.Prompt) + "\n\n" +
		"Finish with a concise written report as plain assistant text before your step budget ends. " +
		"Do not keep calling tools once you have enough evidence."
	if err := ag.Send(ctx, prompt, nil); err != nil {
		summary, _ := agent.LastAssistantText(ctx, r.Store, sess.ID)
		res.Summary = summary
		if ctx.Err() != nil {
			// Manager maps cancel/timeout status.
			res.Err = err.Error()
			return res, err
		}
		// Step budget exhausted after real work is a partial success, not a crash.
		// Parent models were treating "failed / step limit" as a hard agent crash.
		if isStepLimitErr(err) {
			res.Summary = withStepLimitNote(summary, err)
			res.Status = string(StatusCompleted)
			return res, nil
		}
		res.Status = string(StatusFailed)
		res.Err = err.Error()
		return res, err
	}
	summary, err := agent.LastAssistantText(ctx, r.Store, sess.ID)
	if err != nil {
		res.Status = string(StatusFailed)
		res.Err = err.Error()
		return res, err
	}
	res.Summary = summary
	res.Status = string(StatusCompleted)
	return res, nil
}

func isStepLimitErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "step limit reached")
}

func withStepLimitNote(summary string, err error) string {
	note := "[note: child step limit reached; results may be incomplete"
	if err != nil {
		note += " (" + err.Error() + ")"
	}
	note += "]"
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return note
	}
	return summary + "\n\n" + note
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
