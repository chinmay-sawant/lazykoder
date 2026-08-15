# v0.0.1 / Findings / Phase 1 - Stop fighting the user

> **Parent:** `plans/v0.0.1/findings/README.md` - evidence items 2, 3, 4, 9
> **Status:** planned 2026-08-16 (no rows landed; mark `[x]` only when the gate passes)
> **Estimated effort:** 1-2 days
> **Priority:** P0 (the app cannot be used as a chat surface until these land)
> **Gate:** `q` types a letter; latest project session replays on start; events paint during a turn; Esc cancels the in-flight turn; `/sessions` can open an older session

---

## Overview

Five defects make the current TUI unusable as a coding chat, independent of how it looks. Fix them before any chrome rewrite so later view work renders real data.

## Executive Summary

- `q` / `Q` in `internal/ui/chat/keys.go` quits before the prompt sees the key. `ctrl+c` already has a two-step quit (`quitConfirm`). Keep that as the exit path.
- Session rows store `directory = env.Dir` (`<cwd>/.lazykoder`). Startup queries `ListSessionsByDir(cwd)`. 35 existing rows are invisible. Store and look up the project root.
- `watchCmd` buffers every `agent.Event` until the channel closes, then applies them in one `eventBatchMsg`. The screen stays on `sending...` for the whole turn.
- There is no way to open any session except the latest lookup, and that lookup currently returns none.
- Esc is "maybe clear the prompt after two presses." It does not cancel work.

## 1.1 `q` is a letter when the prompt is focused (P0)

- [ ] `updateKey` in `internal/ui/chat/keys.go` no longer quits on `q` / `Q` when the prompt is focused or non-empty - `q` is forwarded to `textinput` like every other letter
- [ ] Quit bindings when the prompt is empty and not in slash/picker/confirm: `ctrl+c` (existing two-step) remains; optional `q` only in that empty-idle case. Document the chosen rule in `docs/tui.md`
- [ ] Status / help copy that says `q to quit` is updated if `q` is no longer a global quit - `internal/ui/chat/view.go` `statusLine`, `slash.go` `/help` text, `docs/tui.md`
- [ ] Test: type `q` into an empty-then-focused prompt; `View()` still running, prompt value contains `q`, no `tea.Quit` cmd - `internal/ui/chat` test, exit 0
- [ ] Test: type `hel` then `q`; prompt is `helq`; app does not quit - exit 0
- [ ] Test: existing two-step `ctrl+c` quit still works (`TestQuitKeys` or successor stays green)

## 1.2 Session directory is the project root, not `.lazykoder` (P0)

Evidence: every row in `.lazykoder/lazykoder.db` has `directory = /home/chinmay/ChinmayPersonalProjects/lazyKoder/.lazykoder`. `main.go` calls `ListSessionsByDir(ctx, cwd)`. `chat.New` gets `Workdir: env.Dir`. `agent.CreateSession` writes `Directory: a.workdir`.

- [ ] `main.go` passes the project cwd into `chat.Options.Workdir` (or a new `ProjectDir` field). `env.Dir` stays the `.lazykoder` folder for db path and `models.json` only
- [ ] `agent.New` / `CreateSession` persist the project cwd as `sessions.directory`. Tool `rootDir` for read/write/edit/bash default stays the project cwd, not `.lazykoder`
- [ ] `ListSessionsByDir` on startup uses that same project cwd so replay finds existing-and-future rows
- [ ] One-shot repair on migrate or startup: `UPDATE sessions SET directory = rtrim(directory, '/.lazykoder')` (or equivalent) when `directory` is exactly `<project>/.lazykoder`, so the 35 existing sessions become visible. Additive, idempotent. Covered by a db test
- [ ] Test: create a session with Workdir = project root; `ListSessionsByDir(project)` returns it; `ListSessionsByDir(project/.lazykoder)` does not need to - `internal/db` + `internal/agent`, exit 0
- [ ] Test: a fixture db with the old `.lazykoder` directory value is rewritten and then listed from the project root - exit 0
- [ ] Test: `TestReplayNoNetwork` still rebuilds the transcript with no provider call after the Workdir change - exit 0

## 1.3 Session picker (P0)

Without this, 1.2 only restores the latest session. The store already has 35 titled sessions.

- [ ] Slash command `/sessions` (and a documented key, e.g. `ctrl+s` when idle) opens a picker of sessions for the current project cwd, newest first - reuse the model-picker card pattern in `internal/ui/chat/picker.go` or a sibling `sessions.go`, do not invent a third overlay style
- [ ] Each row shows title (fallback: first user line or `untitled`), relative time or `time_updated`, and model. Selecting a row loads that session: `m.session`, `m.lines` rebuilt via `replay`, prompt cleared, picker closed
- [ ] `/new` stays: new empty session, no row selected until the first send creates one (existing `/new` behavior)
- [ ] Empty list (fresh project) shows `no sessions` and stays dismissible with `esc`
- [ ] Test: two fixture sessions; open picker; `View()` contains both titles; enter on the older one; `View()` contains that session's user text and not the newer one - exit 0
- [ ] Test: picker `esc` leaves the current session unchanged - exit 0

## 1.4 Paint events as they arrive (P0)

Evidence: `watchCmd` in `internal/ui/chat/keys.go` ranges the channel to completion, then returns one `eventBatchMsg`. `Update` sets `m.busy = false` only after that batch.

- [ ] Replace the batching watch with an incremental cmd: each `agent.Event` (or a small flush of events ready now) returns to `Update` and re-arms the watch until the channel closes
- [ ] `EventPart` and `EventTool` update `m.lines` / tool cards immediately. `pending` / `running` / `completed` tool statuses overwrite the same card (existing `lastTool` path)
- [ ] `m.busy` stays true until the terminal event (`EventDone` or the error/close path), not until the first event
- [ ] `Update` stays pure: the next recv is a `tea.Cmd`, not a goroutine that mutates `m`
- [ ] Test: fake provider emits a tool `pending` then `completed` then text; a mid-turn `View()` (or recorded event-apply) contains `bash: pending` or `bash: running` before the final text - exit 0
- [ ] Test: existing `TestBashCommandAndOutputRendered` still sees `bash: completed` and output after the turn ends - exit 0

## 1.5 Esc cancels the in-flight turn (P0)

- [ ] While `m.busy`, first `esc` cancels the turn context (`context.WithCancel` owned by `submit`). Agent / provider / tools see `ctx.Done` and stop
- [ ] Cancel does not confirm a pending `rm`. If confirm is open, existing `n` / `esc` deny path stays (confirm mode is handled first)
- [ ] After cancel: `m.busy = false`, a visible status or transcript note (`cancelled`), partial parts already persisted stay in the store and on screen
- [ ] Double-esc clear-prompt remains when not busy (existing `escapePending`)
- [ ] Test: start a turn against a slow fake provider; send `esc`; no `tea.Quit`; `busy` is false; later provider result is ignored or applied as cancelled, no panic - exit 0
- [ ] Test: confirm view still eats `esc` as deny and does not also cancel a turn that is waiting on confirm - exit 0

## Dependencies

- Needs: Phase 1-3 harness (store, agent events, picker card, slash menu)
- Blocks: findings phase 2 chrome (it will render live lines and a real session title)
- Blocks: `plans/v0.0.1/phase-4-tokens-status-todos.md` live tps (needs incremental events)
- New dependencies: none

## Closure gates

- [ ] `go test ./internal/ui/chat ./internal/agent ./internal/db ./internal/workspace -count=1` exit 0 - pending
- [ ] `go test ./... -count=1` exit 0 - pending
- [ ] `go vet ./...` exit 0 - pending
- [ ] tmux at 120x36: type `question`, app still up, prompt shows the word - pending
- [ ] tmux restart in the project root: latest session transcript is visible without sending a message - pending
