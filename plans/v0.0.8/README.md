# v0.0.8 - Auto-compaction and mid-session model switch

> **Parent:** token window already shown in footer/`/status`
> (`modelscache.ContextOf` + `tokensUsed`); agent history in
> `internal/agent.Agent.buildHistory`
> **Status:** complete 2026-08-17. All five phases implemented and gated.
> **Branch:** `feature/auto-compaction`
> **Estimated effort:** 4-6 days across five phases
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** a session that fills a large-window model can switch to a
> smaller-window model without sending an overflowing request; the next
> send prunes and, if needed, writes a durable checkpoint from
> `internal/prompts/compact.md`; the transcript still shows the full
> human history

This folder is the live ledger. Mark `[x]` only after the named gate
command passes. Do not claim TUI feel from headless `go run`.

`/compact` was out of scope in v0.0.5 because there was no backend. This
version is that backend.

---

## Overview

Shipped 2026-08-17. Fill is tracked against the **selected** model's
catalog window. `buildHistory` starts at the latest compaction
checkpoint plus the kept tail. A user can still accumulate 400k tokens
on a ~1M model and then pick a ~256k model; the next send (or
`/compact`) writes a checkpoint instead of overflowing.

Other harnesses (Codex, Claude Code, OpenCode) do not treat "model
changed" as a special product. They re-evaluate the **live** model's
window before the next call. A 1M session that is 40% full is fine on
DeepSeek and immediately overflowing on 256k. The same
`estimate > new_limit - buffer` check catches it, if we run it against
`ContextOf(newly selected id)`.

The hard part they mostly ignore: the summarizer request itself has to
fit in some model. OpenCode V2 fails when it does not. We must not copy
that for the shrink path. Compact with the largest window that can still
see the history (usually the outgoing model), then continue with the
newly selected model.

---

## Shipped code (facts)

- Window comes from `.lazykoder/models.json` (`GET /models` first,
  `models.dev` only fills zeros). Not hardcoded.
- Footer/`/status` show `tokensUsed / ContextOf(live model)`.
- `tokensUsed` is the latest request input (or `tokens_after` after
  compact), with a chars/4 floor. It is not a lifetime peak.
- `/model` or the footer chip updates `m.model` and `sessions.model`.
  Same session. Next `Send` uses the new id. If used exceeds
  `percent` of the new window, a shrink hint is set and the next send
  compacts first. Assistant rows store `messages.model_id`; older rows
  keep the old id.
- A switch during a busy turn is cosmetic until the next send. The
  in-flight `Agent` keeps the model it was built with.
- `Message.Visible` is TUI history-delete only. `buildHistory` ignores
  it. Compact does not flip it.
- `internal/agent/summary.go` is `LastAssistantText` for sub-agent
  handoff, not conversation compaction.
- `internal/prompts` embeds `compact.md` (`prompts.Must`). Checkpoints
  are `messages.agent = compaction` + `parts.type = compaction`.
- Settings: `compaction.auto` (default true), `percent` (80, 5-99),
  `keep_tokens` (15000, JSON-only). `/settings` rows are **auto-compact**
  and **compact at**. Old `buffer` keys are ignored.

---

## How other harnesses do this

Cheap local forgetting first, one structured LLM summary last.

| | Codex CLI | Claude Code | OpenCode |
| --- | --- | --- | --- |
| Layers | One LLM handoff | Tool-trim, cache-friendly trim, then 9-section summary | Prune (hide), then 5-heading summary |
| LLM required | Always | Only last resort | Only if prune is not enough |
| Tool results | Deleted | Placeholder | Timestamp-hidden, still in DB |
| User messages | Kept verbatim | Folded into the summary | Summary + replay last user turn |
| Trigger | Near current model limit | `effective_window - 13k` | `estimate > context - max(output, buffer)` |
| Overflow | Head-trim fallback | Compact + retry, pause after 3 fails | Compact + retry once |
| Summary model | Session model (or OpenAI remote) | Session model; SDK can use a cheaper one | Session model. No fallback. Fails if the summary request cannot fit |

OpenCode V2 is the closest template: 4 chars/token estimate, keep a
recent tail (`keep.tokens` default 15k), do not delete durable messages,
replay the last user intent after auto-compact, config
`compaction.auto` / `keep.tokens` / `buffer`.

---

## Detection

No new event bus. Hook the two places that already change the live model.

### Gate A: picker (UI only)

After `selectPickerItem` (and any other live-model write):

```
oldWindow = ContextOf(infos, previousID)
newWindow = ContextOf(infos, m.model)
need      = NeedsCompact(tokensUsed, newWindow, percent)
```

If overflowing: persist the new model, set `pendingCompactReason =
"model-shrink"`, show `next send will compact (window 1000k -> 256k)`.
Do not spend tokens on picker click.

If the new window is larger or unknown (`Context == 0`), clear the flag.
Unknown window: skip auto-compact rather than guess.

`m.session.Model` stays in sync with `m.model` in memory
(`syncSessionModel`).

### Gate B: agent preflight (the real check)

At the start of each `runSteps` iteration, before `Chat` / `ChatStream`:

```
estimate = max(tokensUsed, chars/4 of the request we are about to send)
window   = ContextOf(current model)
need     = estimate > window * percent / 100
```

Defaults:

- `auto = true`
- `percent = 80` (used > 80% of the live window)
- `keep_tokens = 15_000`
- `outputReserve = 4_096` for the summarizer

Classify provider overflow errors and run **one** compact+retry. A
second overflow is a user-visible error.

Same-model auto-compact and model-shrink compact are the same function.
Shrink just makes `limit` drop while `estimate` stays put.

---

## 1M to 256k (summarizer must fit)

```
summarizer = incoming model
if estimate > incoming.Context - summarizerReserve:
    if outgoing.Context > incoming.Context:
        summarizer = outgoing
    else:
        prune first; if still too big, chunked map-reduce
```

If prune drops 400k to ~180k, the new 256k model can summarize (or we
skip the LLM). If prune only reaches ~350k, summarize with the outgoing
1M model, store the checkpoint, then send the next real turn to 256k
with `summary + recent tail`.

If even the outgoing model cannot fit:

1. Prune more aggressively.
2. Chunked compact: summarize windows, then summarize the summaries.
3. If that still fails, do not send. Surface an error. Keep the session.

Do not silently create a new session.

---

## Algorithm

Stepped, matching OpenCode more than Claude. Prompt-cache prefix games
do not apply to our OpenCode Go client.

```
before each model call (and after a shrink flag):
  1. Estimate request size. Compare to percent of ContextOf(live model).
  2. If under budget: send.
  3. Layer 0, prune (no LLM):
       keep last 2 user turns + keep_tokens tail;
       older tool outputs become a short placeholder in the request only;
       do not delete SQLite rows; do not use Message.Visible.
  4. Re-estimate. Preflight still floors on tokensUsed, so a large last
     request still takes the LLM path even if prune would have fit.
  5. Layer 1, LLM checkpoint:
       pick summarizer (incoming, or outgoing if shrink will not fit);
       tools off, max ~4096 output;
       prompt from internal/prompts/compact.md + serialized head
       + previous summary if any;
       persist checkpoint; buildHistory from checkpoint + tail.
  6. Layer 2, replay:
       auto / shrink path: continue the current user turn;
       manual /compact: stop after the checkpoint.
  7. Provider still overflows: one compact+retry, then error.
```

`buildHistory` starts at the latest completed compaction part, rendered
as a historical user message ("this is a checkpoint, not new
instructions"), then every later message. Older rows stay in SQLite and
still paint in the transcript.

Sub-agents: children get `CompactAuto: true` but no catalog window, so
percent auto never fires. Overflow retry still can. A parent shrink does
not rewrite a running child.

---

## Prompt (to the model, not the human)

User-facing copy is a status line (`compacting context...`) and a
transcript notice (`context compacted (1000k -> 256k)`).

The summarizer instruction lives in the app:

```
internal/prompts/
  embed.go       // go:embed *.md + Must("compact.md")
  compact.md     // the only prompt in v1
```

Required sections in `compact.md`:

1. Primary request and intent
2. Key decisions and constraints (preserve "do not" rules verbatim)
3. Files and code that matter
4. Errors and how they were fixed
5. Pending work / TODOs
6. Current work
7. Next step in line with the last user request
8. All user messages, listed

Rules: quote paths and constraints; follow the user's language; handoff
to the next model in the same session; update a previous summary instead
of restarting; do not restate the retained tail.

Manual `/compact extra instructions` appends a "Compact instructions"
block.

---

## Storage

No new table. Reuse `parts.type`.

- Assistant (or user+assistant) row with `agent = "compaction"`.
- `parts.type = "compaction"` whose `text` is the summary plus a small
  JSON envelope: `tail_start_message_id`, `from_model`, `to_model`,
  `reason` (`auto` | `overflow` | `model-shrink` | `manual`).
- Avoid a migration in v1. Revisit if we need to query the envelope.

`ListMessages` stays complete. Only `buildHistory` starts at the latest
successful compaction part. Optionally paint a divider on that part so
the user can see that the model forgot the prefix.

`tokensUsed` after success = estimate of (summary + tail).

---

## Settings

Add to `.lazykoder/settings.json` (no new dependency):

```json
{
  "compaction": {
    "auto": true,
    "percent": 80,
    "keep_tokens": 15000
  }
}
```

`auto` gates same-model percent preflight only. Manual `/compact`,
mid-session shrink, and the single overflow retry stay available when
`auto` is false. `/settings` edits `auto` and `percent`. `keep_tokens`
is JSON-only. No separate compaction-model setting in v1. The shrink
rule is code, not config.

---

## TUI

- `/compact` slash: run Layer 0+1 now, even under budget. Optional
  trailing text becomes extra prompt instructions.
- Footer / status: while compacting, show `compacting` in the prompt
  segment.
- After a shrink that will compact on next send: composer hint, not a
  y/n confirm. Confirm only if we cannot compact and would otherwise
  send an overflowing request.

---

## Phase files (live ledgers)

| File | Priority | Goal |
| --- | --- | --- |
| [phase-1-policy-prompts.md](phase-1-policy-prompts.md) | P0 | embed `compact.md`; pure estimate / NeedsCompact / PickSummarizer / prune |
| [phase-2-history-checkpoint.md](phase-2-history-checkpoint.md) | P0 | request-time prune + read compaction parts in `buildHistory` |
| [phase-3-llm-compact.md](phase-3-llm-compact.md) | P0 | tools-off summarizer call, persist checkpoint, overflow retry |
| [phase-4-model-switch-slash.md](phase-4-model-switch-slash.md) | P0 | picker shrink flag, `/compact`, settings block |
| [phase-5-gates.md](phase-5-gates.md) | P1 | full test/build, docs (`architecture.md`, `tui.md`) |

```
phase-1-policy-prompts
        │
        v
phase-2-history-checkpoint
        │
        v
phase-3-llm-compact
        │
        v
phase-4-model-switch-slash
        │
        v
phase-5-gates
```

---

## Out of scope (v1)

- New session per compact.
- Reusing `Message.Visible` as the compact hide bit.
- A user-configured compaction model.
- Anthropic-style prompt-cache prefix surgery.
- Physical deletion of transcript rows.
- Compacting on picker click.
- Rewriting a running child's transcript when the parent shrinks.
- New dependencies.

---

## Decisions (shipped)

1. Compact after a shrink on the **next send**, not on picker click.
2. No y/n confirm. Hard error if the summarizer cannot fit; session kept.
3. Summarizer uses the outgoing larger model when the incoming window
   cannot hold the head.
4. `/compact` shipped in the same change as auto (shared backend).
