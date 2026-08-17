## Summary

Ship the phase2 product surface on `feature/phase2`: in-process sub-agents with durable jobs, project settings and `/continue`, SQLite FK/index integrity (schema v6-8), tool and shell policy hardening, and the v0.0.5 TUI layout ledger with click-target and composer mouse fixes from live tmux walks.

## Motivation / context

- Plans: `plans/v0.0.2/` (sub-agent harness), `plans/v0.0.4/` (SQLite integrity), `plans/v0.0.5/` (TUI layout and settings UX)
- Live review: tmux `lazykoder-ui-qa` at 167x48 and 80x24 on 2026-08-17
- Branch: `feature/phase2` (29 commits from settings/`/continue` through click-target polish)

## Changes

### Project settings and step budget

- Configurable MaxSteps via `/settings`, persisted to `.lazykoder/settings.json`
- Full-screen project settings card (default model, variant, step budget, timeout, role, confirm, queue, explore model)
- New runs seed from project settings
- `/continue` resumes after a step-limit stop without a new user message
- Child agent timeout is settings-owned (`agents.default_timeout_sec`); models cannot kill children via invented `timeout_ms`

### In-process sub-agents (v0.0.2)

- Parent control plane: `task` / `task_list` / `task_status` / `task_wait` / `task_cancel`
- Role-based child agents (`explore` / `plan` / `general`), caps up to 20 concurrent
- Child sessions for audit; single SQLite connection so parallel sub-agents avoid `SQLITE_BUSY`
- Durable `subagent_jobs` table with crash recovery and resume on the existing child session
- Stable TUI drawer: status diamonds, activity lines, full-screen chat-style logs, open-on-spawn / compact summary strip
- `@` picker lists session sub-agents (status + context); `@agent:name` expands task/status/last reply into the model payload
- Session todos (`todowrite`), checklist under the header, expand when the model updates them

### SQLite integrity (v0.0.4, schema v6-8)

- Unique seq indexes, query-shaped session/job indexes
- `sessions.parent` CASCADE; `subagent_jobs` child/part `SET NULL` FKs
- Cascade delete tests; migration rebuild path for existing DBs
- Session `time_updated` bumps on message insert and visibility changes (resume order and age labels track real activity)

### Tools, policy, and busy UX

- Content search via `grep` (ripgrep with pure-Go fallback)
- Command allowlist; hardened shell policy and network boundaries
- Edit cards: correct line numbers, soft green/red washes, collapsible full-width unified diffs
- Busy turn: draft editing and enter-to-send while a turn is running
- Child max steps default raised to 32 with partial completion on limit
- Drag-copy strips work rail and user frame so paste is plain message content

### TUI layout and click targets (v0.0.5)

- Opaque confirm / ask / `@` cards; grouped slash palette; collapsed todos; model/variant footer chips
- `/help` lists `/settings`, `/new`, `/continue`, `/refresh`
- Even-spaced user-turn rail (right-edge ticks, label bubble, auto-hide after 10s)
- Composer mouse: accurate click-to-caret and drag-select via shared hard-wrap paint path
- Click hit-testing fixed for chips, picker rows, tool chevrons, and compact sub-agent expand

| Symptom | Cause | Fix |
| --- | --- | --- |
| Model / variant `▾` only opened if you clicked below or off to the side | Chip boxes overlapped; variant tested first | Paint-scan each label; nearer chip wins |
| Clicking a variant row did nothing | List Y assumed drawer under transcript | Map rows from painted `reasoning ·` / `models ·` header |
| `▸` thinking / tool headers needed a click one row low | Phantom `transcriptTop + 1` | Drop that `+ 1` |
| Enter left `│` / ticks / scrollbar junk on the left | User-nav overlay invented extra rows | Only paint ticks on rows that already exist |
| Todo expand moved the right-side dots | Rail respread on shorter transcript | Stable span as if todos stayed one line |
| `@` agent rows wrapped with orphan scrollbar lines | Scrollbar width math | Size list to contentW-1; truncate labels |
| Model chips dead while agents drawer open | Prompt/strip handled first | Handle chips before prompt and sub-agent strip |

### Docs and plans

- Docs updated: architecture, development, plans, safety, storage, tips, tools, tui
- Plan ledgers: `plans/v0.0.2/`, `plans/v0.0.4/`, `plans/v0.0.5/`, sub-agent audit report

## Commit log (oldest first)

```
11ee530 feature: slot settings drawer and /continue after step limit
7b89036 feature: full-screen project settings card with default model
e3e8c26 feature: in-process sub-agents with task tools and SQLite write safety
9a35ba1 feature: stable sub-agent drawer and full-screen chat-style logs
59790b6 feature: grep tool, busy send-now UX, and sub-agent step defaults
43cc62f fix: strip work rail and user frame from drag-copy clipboard
fe3c401 fix: bump session time_updated when messages are written
285d606 feature: durable sub-agent jobs in SQLite with crash recovery
8c49aa1 docs: add v0.0.4 plan for SQLite FK and index integrity
a8a7240 feature: SQLite FK and index integrity (schema v6-8)
f7214b3 Harden shell policy and network boundaries
f27bd64 fix: sub-agent drawer open-on-spawn and full-width edit diffs
c921e6a fix: edit card line numbers, soft tints, and collapsible panel
ac1ae64 fix: prompt select-all/newline keys and sub-agent drawer mouse
87f775e Add command allowlist and harden tool boundaries
eb88ca8 feature: @ picker lists session sub-agents with status and context
262eae9 feature: session todos tracker and composer with sub-agent drawer open
d110bf5 (exp) add even-spaced user-turn rail on the transcript
790ff66 (exp) auto-hide user-nav label after 10s and pad rail ticks
ed561f4 feature: TUI layout, settings card, and help rewrite
afe2d0e fix: keep user-nav dots still when the todo list opens
235bae6 fix: click targets for chips, pickers, and tool chevrons
af20580 fix: keep @ files & sub-agents picker rows from wrapping
003d017 fix: keep child agent timeout settings-owned
db75c37 fix: composer mouse edit and sub-agent/todo chrome
7502402 fix: restore model chip clicks and compact sub-agent expand
```

## Ratings (from `plans/v0.0.5/README.md`)

| Lens | Before | After |
| --- | --- | --- |
| Overall product | 6.0 / 10 | 8.0 / 10 |
| TUI / layout | 5.5 / 10 | 8.0 / 10 |
| Settings completeness | 4.0 / 10 | 8.5 / 10 |
| Discoverability | 5.0 / 10 | 7.5 / 10 |
| Compact 80x24 | 3.5 / 10 | 7.0 / 10 |

## Impact

| Area | Impact |
| --- | --- |
| **Performance** | Single SQLite writer serializes parallel sub-agent writes; avoids SQLITE_BUSY |
| **Memory** | Sub-agent jobs and child sessions persisted; caps concurrent children |
| **Behavior / correctness** | Sub-agents, durable jobs, FK cascades, mouse hit-testing, settings seed |
| **API / CLI** | New slash: `/settings`, `/continue`; task/grep/todo tools; settings JSON |
| **Dependencies** | Minimal (grep pure-Go fallback path; existing stack preferred) |
| **Binary size / build time** | Larger TUI and subagent packages; no new heavy deps expected |

## Breaking changes / migration

| Item | Migration |
| --- | --- |
| SQLite schema v6-8 | Automatic on open via existing migrate path; existing `.lazykoder/lazykoder.db` upgrades |
| Project settings | New optional `.lazykoder/settings.json`; defaults apply when absent |
| Task tool timeout | Public task tool no longer accepts model-supplied timeout fields |

## Test plan

- [x] `go test ./internal/ui/chat -count=1`
- [x] `go vet ./internal/ui/chat`
- [x] `go test ./... -count=1` (package gates on feature commits)
- [ ] `make run` in a real terminal: settings, sub-agent spawn/drawer, model/variant chips, tool chevrons, `@` picker, send a turn

### Commands

```sh
go test ./... -count=1
go vet ./...
make run
```

## Related issues

- Relates to `plans/v0.0.2/` (sub-agent harness)
- Relates to `plans/v0.0.4/` (SQLite FK and indexes)
- Relates to `plans/v0.0.5/` (TUI layout ledger)
- Relates to `plans/v0.0.1/findings/` (earlier chrome, already shipped)

## Follow-ups (out of scope)

- `@` still leads with sub-agents; files are below the fold but reachable
- Compact 80x24 footer still truncates the model id
- Nested sub-agents / worktree isolation not in v0.0.2
- Mermaid visualizer reserved for v0.0.3

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] Sub-agent manager, durable jobs, and SQLite migrations are coherent
- [ ] Settings and timeout ownership stay settings-backed
- [ ] Click targets match painted chrome
- [ ] Public API / CLI changes are documented in `docs/`
- [ ] PR has assignee and labels
- [ ] No secrets or generated artifacts committed
