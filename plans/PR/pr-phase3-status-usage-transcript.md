## Summary

Ship the post-phase2 product surface on `feature/phase-3`: OpenCode usage dashboard, live status segments and status drawer, session todos polish, v0.0.6 transcript performance (composer-safe meta keys, tool paint caps, render memoization), and sub-agent log navigation.

## Motivation / context

- Plans: `plans/v0.0.6/` (transcript performance and meta keys - complete), `plans/v0.0.7/phase-1-status-drawer.md` (complete), `plans/v0.0.7/phase-2-opencode-accounts.md` (planned only), `plans/PR/pr-usage-feature.md`
- Branch: `feature/phase-3` (8 commits on top of master after phase2 merge)
- Live ledger: status visibility first; multi-account OpenCode profiles remain follow-up

## Changes

### OpenCode usage dashboard

- Authenticated `GET /zen/go/v1/usage` on the OpenCode client
- `/usage` centered modal: rolling / weekly / monthly percentages, rate-limit status, reset times, refresh
- Latest usage values under OpenCode usage in `/settings`
- API keys stay environment-only; UI labels the rolling window as `rolling` to match the API

### Live metrics, status segments, and status drawer (v0.0.7 phase 1)

- Persist status segment visibility on the session (`sessions.status_segments` migration; all details default on)
- Compact footer status control; `/status` opens an agent-style drawer above the prompt
- Drawer shows label, current value, and on/off per segment (model, variant, tokens, cache, cost, tps, sub-agents, models, scroll, prompt)
- Arrow select, enter toggle, left/esc close, clickable rows; footer control stays visible while open

### Session todos and dialog polish

- Stronger todo list UX under the header (expand when the model updates todos)
- Dialog and confirm chrome polish for status, agents, and todos surfaces

### Transcript performance and meta keys (v0.0.6)

- Composer-safe bulk meta: plain `t` / `e` always type; only `ctrl+e` / `ctrl+p` toggle meta blocks
- Paint-only caps on expanded bash/read/grep bodies in the main transcript; sub-agent audit log stays full length
- Cached content digests and per-item render memoization so long sessions do not rebuild the full transcript string on every stream delta
- Benchmark and phase evidence under `plans/v0.0.6/`

### Sub-agent navigation

- Arrow navigation through sub-agent logs in the drawer
- Sub-agent drawer and compact strip polish

### Docs and plans

- `docs/tui.md`, `docs/storage.md`, `docs/plans.md`, `TASKS.md` updates
- v0.0.6 and v0.0.7 plan ledgers; phase2 PR body archived under `plans/v0.0.5/PR/`

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Transcript render path memoized; expanded tool paint bounded on main transcript |
| **Memory** | Lower peak string rebuild cost on fat sessions; tool bodies capped for paint only |
| **Behavior / correctness** | Bare `t`/`e` no longer steal keys when prompt empty; status visibility persists per session |
| **API / CLI** | New slash commands `/usage`, expanded `/status` drawer; no CLI flag changes |
| **Dependencies** | None added |
| **Binary size / build time** | Negligible (UI + client methods only) |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| Status segments schema | Automatic DB migration expands legacy segment rows; new fields default visible |
| Meta keybindings | `t`/`e` no longer toggle thinking/tools; use `ctrl+e` / `ctrl+p` (documented in plan and TUI docs) |

## Test plan

- [x] `go test ./... -count=1` (all packages pass; `internal/ui/chat` ~7s)
- [x] `go vet ./...`
- [ ] `make run` in a real terminal (visual: status drawer, usage modal, todos, sub-agent arrows)
- [ ] Confirm plain `t` / `e` type into empty composer; `ctrl+e` / `ctrl+p` toggle meta
- [ ] Confirm `/status` drawer toggles persist across session reload
- [ ] Confirm `/usage` with a valid OpenCode key shows rolling/weekly/monthly windows

### Commands

```sh
go test ./... -count=1
go vet ./...
make run
```

## Screenshots / sample output

```
go test ./... -count=1  # all ok (2026-08-17)
go vet ./...            # clean
```

Visual QA needs a real TTY via `make run` (headless `go run` cannot open `/dev/tty`).

## Related issues

- Relates to phase ledgers `plans/v0.0.6/` and `plans/v0.0.7/phase-1-status-drawer.md`
- Follow-up (out of scope): `plans/v0.0.7/phase-2-opencode-accounts.md` multi-account profiles

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [x] Related issues / plans filled
- [x] Filled body under `plans/PR/pr-phase3-status-usage-transcript.md`

## Follow-ups (out of scope)

- OpenCode multi-account / plan profiles (v0.0.7 phase 2)
- Repo-wide lint cleanup for pre-existing findings outside this branch

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public slash-command / settings changes documented in `docs/tui.md`
- [ ] New status/usage/render paths have package tests
- [ ] PR has assignee and labels
- [ ] Related issues use correct Closes/Relates keywords
- [ ] No secrets or generated artifacts committed
