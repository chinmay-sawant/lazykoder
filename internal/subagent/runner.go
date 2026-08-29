package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/skills"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

// maxTitleRunes caps the rune length of a child session title.
const maxTitleRunes = 60

// AgentRunner runs a child job via agent.Agent on a dedicated child session.
type AgentRunner struct {
	Store    *db.Store
	Client   provider.Client
	Provider string
}

// Run creates (or resumes) a child session, runs one agent turn, and returns a summary.
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
	if strings.TrimSpace(job.Prompt) == "" && !job.Resume {
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
	if len([]rune(title)) > maxTitleRunes {
		title = string([]rune(title)[:maxTitleRunes])
	}

	var sess db.Session
	var err error
	if job.ChildSessionID != "" {
		sess, err = r.Store.GetSession(ctx, job.ChildSessionID)
		if err != nil {
			// Stale id: fall through to a new session.
			job.ChildSessionID = ""
		}
	}
	if job.ChildSessionID == "" {
		sess, err = r.Store.CreateSession(ctx, db.Session{
			Title:           title,
			Directory:       workdir,
			Model:           job.Model,
			Provider:        r.Provider,
			Variant:         strPtr(job.Variant),
			ParentSessionID: parentID,
			Kind:            db.SessionKindSubagent,
		})
		if err != nil {
			res.Status = string(StatusFailed)
			res.Err = err.Error()
			return res, err
		}
	}
	res.ChildSessionID = sess.ID
	// Publish session id before the turn so the drawer can merge live + store rows.
	if job.OnSession != nil {
		job.OnSession(sess.ID)
	}

	// Crash recovery: if the child already finished with a text answer and
	// has no pending tools, adopt that summary without another model turn.
	if job.Resume {
		if summary, ok := r.finishedSummary(ctx, sess.ID); ok {
			res.Summary = summary
			res.Status = string(StatusCompleted)
			return res, nil
		}
	}

	var ask func(q question.Question) (int, error)
	if job.Ask != nil {
		ask = func(q question.Question) (int, error) {
			return job.Ask(q.Question, q.Options)
		}
	}

	// The selected child profile supplies context metadata when the cache knows it.
	ag := agent.New(r.Store, r.Client, workdir, agent.Options{
		Session:          &sess,
		MaxSteps:         job.MaxSteps,
		Provider:         r.Provider,
		Model:            job.Model,
		Endpoint:         job.Endpoint,
		Variant:          job.Variant,
		ContextWindow:    job.ContextWindow,
		Confirm:          job.Confirm,
		Ask:              ask,
		ToolNames:        job.Tools,
		AgentName:        job.Name,
		SkillContext:     append([]skills.Context{}, job.Skills...),
		DisableStreaming: true,
		CompactAuto:      job.ContextWindow > 0,
	})
	// Nudge the child to emit a final text answer inside its step budget.
	// Without this, multi-tool explores often burn every step on tools and
	// look like crashes to the parent (status failed / step limit).
	prompt := strings.TrimSpace(job.Prompt) + "\n\n" +
		"Finish with a concise written report as plain assistant text before your step budget ends. " +
		"Do not keep calling tools once you have enough evidence."
	if job.Resume {
		prompt = "You were interrupted mid-task (process restart). Continue from the transcript above. " +
			"Finish with a concise written report as plain assistant text. " +
			"Do not redo completed work; do not keep calling tools once you have enough evidence."
		if strings.TrimSpace(job.Prompt) != "" {
			prompt = "Original task:\n" + strings.TrimSpace(job.Prompt) + "\n\n" + prompt
		}
	}
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
		if errors.Is(err, agent.ErrStepLimit) {
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

// finishedSummary reports whether the child session already has a usable
// terminal answer (assistant text and no pending tool calls).
func (r AgentRunner) finishedSummary(ctx context.Context, sessionID string) (string, bool) {
	summary, err := agent.LastAssistantText(ctx, r.Store, sessionID)
	if err != nil || strings.TrimSpace(summary) == "" {
		return "", false
	}
	tcs, err := r.Store.ListToolCalls(ctx, sessionID)
	if err != nil {
		return "", false
	}
	for _, tc := range tcs {
		switch strings.ToLower(tc.Status) {
		case "pending", "running", "":
			return "", false
		}
	}
	return summary, true
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
