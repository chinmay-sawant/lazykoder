## Summary

Ship auto-compaction so long sessions and mid-session model shrinks do not overflow the live model's window. The token, cache, and cost meters now show current model-facing usage (and child-agent spend) instead of a lifetime peak priced on the live picker model.

## Motivation / context

- Plans: `plans/v0.0.8/` (all five phases complete)
- Branch: `feature/auto-compaction` (on top of master)
- Docs: `docs/architecture.md` (Compaction), `docs/tui.md`, `docs/storage.md`

Without this, `buildHistory` sent the full SQLite transcript on every step. A 1M-window session switched to a 256k model would overflow. Status tokens stayed at the high-water mark after `/compact`, and cost used the currently selected model's prices for every historical step.

## Changes

### Auto-compaction

- Preflight compact when `used > window * percent / 100` (default 80, range 5-99).
- Request-time prune of old tool bodies (placeholder only; SQLite rows stay).
- Tools-off summarizer using embedded `internal/prompts/compact.md` (8-section handoff).
- Durable checkpoint: `parts.type = compaction` with summary, tail start, models, windows, reason, and `tokens_after`.
- Later requests start at that checkpoint plus the kept tail (`keep_tokens`, default 15,000).
- `/compact` runs even under budget; trailing notes append compact instructions.
- Mid-session shrink (`/model` or footer chip) sets `next send will compact (window X -> Y)` and can summarize with the outgoing larger model.
- One provider overflow retry even when auto is off.

### Settings

- `.lazykoder/settings.json` `compaction.auto`, `compaction.percent`, `compaction.keep_tokens`.
- `/settings` rows: **auto-compact** and **compact at** (5% steps).

### Live meters

- Token fill is the latest request input, or `tokens_after` after compact. Not a session peak.
- Parent cache hit/miss reset at compact, then grow from new turns.
- Cost is priced with `messages.model_id` and `models.json` list prices (input, output, cache read/write). API-stored step cost still wins when present.

### Sub-agent cost and cache

- Child session `step-finish` usage is rolled up (already stored; no new table).
- `/agents` rows and the child log header show cost and cache hit/miss.
- Status cost is parent + children (`$0.60  ·  subs $0.18`). Status cache is parent-since-compact plus every child.

### Docs and ledger

- `docs/architecture.md`, `docs/tui.md`, `docs/tips.md`, rotating tips.
- `plans/v0.0.8/` marked complete with gate evidence.

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Smaller provider payloads after compact; one extra tools-off call when compacting |
| **Memory** | Full transcript still in SQLite and the TUI; only the model request shrinks |
| **Behavior / correctness** | Long sessions and 1M-to-256k switches no longer send overflowing history; meters match live context; child spend is visible |
| **API / CLI** | New slash `/compact`; `/settings` compaction rows; no CLI flag changes |
| **Dependencies** | None added (`go:embed` only) |
| **Binary size / build time** | Small (prompt file + compact path) |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| Compaction settings | Missing `compaction` block defaults to auto on, 80%, keep 15k. Old `buffer` key is ignored. |
| Token meter | Shows current fill, not lifetime peak. After compact, expect ~summary+tail, not the old 145k-style number. |
| Cache hit/miss | Reset at compact; then parent-since-compact plus children. Sums can still exceed the window. |
| Cost | Historical steps reprice by the model that ran them when catalog prices load. |

## Test plan

- [x] `go test ./internal/prompts ./internal/agent ./internal/settings ./internal/ui/chat -count=1`
- [x] `go test ./... -count=1`
- [x] `go build ./...`
- [x] `go vet ./...`
- [x] `make lint` (exit 0)
- [ ] `make run` in a real terminal: shrink a fat session, confirm the hint, send, see `compacting` then ~20k fill
- [ ] `/settings` toggle auto-compact and compact-at; `/compact` under budget
- [ ] `/agents` shows child cost/cache; status cost includes `subs`

### Commands

```sh
go test ./... -count=1
go vet ./...
make lint
```

## Screenshots / sample output

Default 80% trigger:

| Model window | Compact when used exceeds |
| --- | ---: |
| 1,000,000 | 800,000 |
| 256,000 | 204,800 |
| 200,000 | 160,000 |

Worked example (`ses_ba60cff7d375e6d4`, `/compact` manual): last request 47,485 tokens; after compact ~21k (1.3k summary + ~20k tail). Peak 144,635 is no longer the status number.

## Related issues

- None (no open ticket for this work)

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [x] Related issues filled (none exist)
- [x] Filled body under `plans/PR/pr-auto-compaction.md`

## Follow-ups (out of scope)

- Persist rolled-up child cost on `subagent_jobs` (today it is computed from child `step-finish` rows)
- Separate compaction-model setting (shrink uses the outgoing larger model in code)
- Human TUI walk on a 1M-to-256k switch in a real terminal

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in the diff
- [ ] Public slash/settings changes documented
- [ ] PR has assignee and labels
- [ ] No secrets or generated artifacts committed
