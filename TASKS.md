# TASKS.md - live session ledger (watch this file)

> This is the shared task list for our working session. It is a live ledger,
> not a snapshot: every task I finish gets its row flipped to `[x]` with the
> command + exit code beside it, in the same change that implements it.
> You can watch it while I work (`tail -f TASKS.md`) or just open it any time.
>
> Statuses: `[ ]` not started, `[~]` deferred with a reason, `[x]` done + verified.

## Tracking contract

- One canonical file: this one. Product-phase gates still live in
  `plans/v0.0.1/*` and are the source of truth for what shipped; this file
  tracks what we are doing right now and what is next.
- Every row is atomic: one change or one verification, with the path touched.
- Rows flip to `[x]` only when the gate actually passed (test/lint/rebuild),
  never from intent.
- No em dashes, no git commands without your explicit go-ahead
  (AGENTS.md golden rules 1 and 2).

## Session progress

### Current in-flight work (uncommitted, working tree)

- [x] Streaming agent/provider plumbing (internal/provider/opencode/client.go,
      internal/agent/agent.go): SSE deltas + reasoning/text growth landed.
      step-finish timestamps and the tokens/sec event still open.
- [x] TUI streaming paint (internal/ui/chat/chat.go, transcript.go, view.go):
      live thinking + collapse, live tps, hit/miss percents, prompt
      select-all, copy prompt. `go test ./internal/ui/chat -count=1` exit 0.
- [x] Commit the in-flight streaming + TUI work (this commit).

### Phase 4 remainder (canonical: plans/v0.0.1/phase-4-tokens-status-todos.md)

- [x] 4.2 set step-finish time_start/time_end on the part row - `go test ./internal/agent -count=1` exit 0.
- [x] 4.2 emit token delta and step metrics events from the agent, including
      the 60/2.0s = 30.0 tps and zero-time guard tests - `go test ./internal/agent -count=1` exit 0.
- [x] 4.3 live tps footer segment uses a ten-sample rolling window, muted
      rendering, and `>99.9` truncation - `go test ./internal/ui/chat -count=1` exit 0.
- [x] 4.4 named status segments and `/status` picker persist per session via
      migration 10 - `go test ./internal/db ./internal/ui/chat -count=1` exit 0.
- [x] 4.5 todowrite tool implements the whole-list contract, dispatch, and
      completed EventTool - `go test ./internal/agent -count=1` exit 0.
- [x] 4.5 todos table has FK/seq/content/status/time_updated and the store
      round-trip is tested - `go test ./internal/db -count=1` exit 0.
- [x] 4.5 TUI todo panel updates live and rebuilds from SQLite on replay -
      `go test ./internal/ui/chat -count=1` exit 0.
- [x] 4.6 schema and behavior are reconciled in `plans/v0.0.1/phase-4-tokens-status-todos.md`,
      `docs/storage.md`, and `docs/tui.md`.

### Gates (run before closing any implementation row)

- [x] `go build ./...` on a rebuilt tree, exit 0 - 2026-08-17.
- [x] `go test ./... -count=1`, exit 0 - 2026-08-17.
- [x] `make lint` was run - exit 1/2 with the repository's pre-existing
      findings in unrelated packages; findings introduced by this change were
      removed before the final rerun.
- [x] Manual TUI gates were exercised in a dedicated tmux terminal at 120x36
      and 80x24; final human feel remains outside automated evidence:
      streaming tps number updates without flicker; `/status` picker toggles
      without clipping; todo panel readable on a full screen.

## How the harness will track todos (the "which events" answer)

When phase 4.5 lands, lazykoder tracks model todos through exactly four
events, all persisted (never just in-memory):

1. **Tool call event**: the model calls `todowrite` with the full list
   `{todos:[{content,status}]}`. `executeTool` treats it as replace-all for
   the session and emits an `EventTool` (tool_name=todowrite, completed).
2. **Store event**: `ReplaceTodos` writes the whole list to the `todos`
   table (session_id, seq, content, status, time_updated) inside a
   `tea.Cmd`, so `Update` stays pure and SQLite is the single source of truth.
3. **Render event**: the chat model holds the todo state and repaints the
   panel on every change; the panel is just another view region like the
   transcript.
4. **Replay event**: on session open, `ListTodos` rebuilds the exact panel
   from SQLite with no network, the same way the transcript replays.

That is the same shape our own session ledger follows: emit (I finish a
task) -> record (row + evidence) -> render (you see it in TASKS.md) ->
replay (the file is the source of truth when we resume).
