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

- [ ] 4.2 set step-finish time_start/time_end on the part row.
- [ ] 4.2 emit a tokens/sec event from the agent (rate at step close), and a
      unit test: 60 tokens over 2.0s = 30.0 tps, zero-time guard = "-".
- [ ] 4.3 live tps segment in the status line (rolling window, muted style,
      `>99.9` truncation) + chat test asserting `tps:` in View().
- [ ] 4.4 status segments: named segments, `s` picker, per-session
      persistence (migration + sessions column), toggle test + round-trip.
- [ ] 4.5 todowrite tool: internal/tools/todo, whole-list replace contract,
      dispatch in executeTool, emit EventTool (tool_name=todowrite).
- [ ] 4.5 todos table (migration v2): session_id FK cascade, seq, content,
      status, time_updated; ReplaceTodos / ListTodos round-trip tested.
- [ ] 4.5 TUI todo panel: renders + updates live from model calls, and
      rebuilds identically on session replay from SQLite (no network).
- [ ] 4.6 remaining schema rows reconciled in plans + docs (storage.md,
      tui.md) when the above land.

### Gates (run before closing any implementation row)

- [ ] `go build ./...` on a rebuilt tree, exit 0.
- [ ] `go test ./... -count=1`, exit 0, recorded beside the row.
- [ ] `make lint` (golangci-lint) clean, findings fixed in the same change.
- [ ] Manual TUI gates (need a live terminal, so a human runs them):
      streaming tps number updates without flicker; `s` picker toggles
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
