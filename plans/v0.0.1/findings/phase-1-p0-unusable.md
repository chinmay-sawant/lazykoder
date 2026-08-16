# v0.0.1 / Findings / Phase 1 - Stop fighting the user

> **Parent:** `plans/v0.0.1/findings/README.md` - evidence items 2, 3, 4, 9
> **Status:** implemented 2026-08-16 (gates green)
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

- [x] `updateKey` in `internal/ui/chat/keys.go` no longer quits on `q` / `Q` when the prompt is focused or non-empty - `q` is forwarded to the prompt like every other letter
- [x] Quit bindings when the prompt is empty and not in slash/picker/confirm: `ctrl+c` (existing two-step) remains; `q` is always a letter because the prompt is focused. Documented in `docs/tui.md`
- [x] Status / help copy that said `q to quit` updated - idle status is the model id; help overlay says `ctrl+c quit`; `docs/tui.md`
- [x] Test: type `q` into an empty-then-focused prompt; `View()` still running, prompt value contains `q`, no `tea.Quit` cmd - `TestQTypesInPrompt`, `TestQuitKeys`, exit 0
- [x] Test: type `hel` then `q`; prompt is `helq`; app does not quit - `TestQTypesInPrompt`, exit 0
- [x] Test: existing two-step `ctrl+c` quit still works (`TestQuitKeys` stays green)

## 1.2 Session directory is the project root, not `.lazykoder` (P0)

- [x] `main.go` passes the project cwd into `chat.Options.Workdir`. `env.Dir` stays the `.lazykoder` folder for db path and `models.json` only
- [x] `agent.New` / `CreateSession` persist the project cwd as `sessions.directory`. Tool `rootDir` for read/write/edit/bash default is the project cwd
- [x] `ListSessionsByDir` on startup uses that same project cwd so replay finds existing-and-future rows
- [x] One-shot repair: migration v3 + `Store.RepairSessionDirectories` strips a trailing `/.lazykoder`. Additive, idempotent
- [x] Test: create a session with Workdir = project root; `ListSessionsByDir(project)` returns it - `TestCreateSessionListedByProjectRoot`, exit 0
- [x] Test: a fixture db with the old `.lazykoder` directory value is rewritten and then listed from the project root - `TestRepairSessionDirectories`, exit 0
- [x] Test: `TestReplayNoNetwork` still rebuilds the transcript with no provider call - exit 0

## 1.3 Session picker (P0)

- [x] Slash command `/sessions` and `ctrl+s` when idle open a picker of sessions for the current project cwd, newest first - `internal/ui/chat/sessions.go`
- [x] Each row shows title (fallback `untitled`), relative time, and model. Selecting a row loads that session via `loadSession` + `replay`
- [x] `/new` stays: new empty session via `loadSession(nil)`
- [x] Empty list (fresh project) shows `no sessions` and stays dismissible with `esc`
- [x] Test: two fixture sessions; open picker; `View()` contains both titles; enter on the older one; `View()` contains that session's user text and not the newer one - `TestSessionPickerSelectsOlderSession`, exit 0
- [x] Test: picker `esc` leaves the current session unchanged - `TestSessionPickerEscKeepsCurrent`, exit 0

## 1.4 Paint events as they arrive (P0)

- [x] Incremental `watchEvents`: each `agent.Event` returns to `Update` as `eventMsg` and re-arms until `eventDoneMsg`
- [x] `EventPart` and `EventTool` update items / tool cards immediately. `pending` / `completed` overwrite the same card (`lastTool`)
- [x] `m.busy` stays true until `eventDoneMsg` (or `cancelTurn`)
- [x] `Update` stays pure: the next recv is a `tea.Cmd`
- [x] Test: fake provider emits a tool `pending` then `completed` then text; mid-turn `View()` contains `pending` while still busy - `TestLiveToolCardBeforeTurnEnds`, exit 0
- [x] Test: `TestBashCommandAndOutputRendered` still sees completed bash after the turn ends (expand for output) - exit 0

## 1.5 Esc cancels the in-flight turn (P0)

- [x] While `m.busy`, first `esc` cancels the turn context. Agent / provider / tools see `ctx.Done`
- [x] Cancel does not confirm a pending `rm`. Confirm mode is handled first
- [x] After cancel: `m.busy = false`, transcript note `cancelled`, late events ignored via `turnSeq`
- [x] Double-esc clear-prompt remains when not busy
- [x] Test: slow fake provider; `esc`; no `tea.Quit`; `busy` is false; late result ignored - `TestEscCancelsInFlightTurn`, exit 0
- [x] Test: confirm view still eats `esc` as deny - `TestConfirmEscDoesNotCancelTurn`, exit 0

## Dependencies

- Needs: Phase 1-3 harness (store, agent events, picker card, slash menu)
- Blocks: findings phase 2 chrome (it will render live lines and a real session title)
- Blocks: `plans/v0.0.1/phase-4-tokens-status-todos.md` live tps (needs incremental events)
- New dependencies: none

## Closure gates

- [x] `go test ./internal/ui/chat ./internal/agent ./internal/db ./internal/workspace -count=1` exit 0 - 2026-08-16
- [x] `go test ./... -count=1` exit 0 - 2026-08-16
- [x] `go vet ./...` exit 0 - 2026-08-16
- [x] tmux at 120x36: type `question`, app still up, prompt shows the word - 2026-08-16
- [x] tmux restart in the project root: latest session transcript is visible without sending a message - 2026-08-16 (session "say hello world in the golang")
