# v0.0.1 / Phase 4 - Live tokens/sec, status line customizations, and tracked todos

> **Parent:** `plans/v0.0.1/README.md` - status line, part types, and schema
> **Status:** planned 2026-08-16 (no rows landed; mark `[x]` only when the gate passes)
> **Estimated effort:** 2-3 days
> **Priority:** P1 (user requested; the three slices are independent except 4.2 feeds 4.3)
> **Gate:** status line shows a live tokens/sec value while the model answers; status line segments are user-configurable; model-driven todos render and update on screen and survive replay from SQLite

---

## Overview

Three user-requested features on top of the Phase 1-3 harness:

1. **Tokens/sec in the status line.** The bottom status line (currently `model X  •  m switch  •  / commands  •  enter to send  •  q to quit` in `internal/ui/chat/chat.go:statusLine`) must show how fast the model is generating tokens. Today the provider call is non-streaming (streaming is the deferred `[~]` row in phase-3 3.9), so a real-time speed needs streaming deltas from the API. Plan the streaming path first, with a usage-based fallback.
2. **Status line customizations.** Make the status line segments configurable (show/hide model, tps, token totals, cost, scroll hint, models count) through the TUI, not code edits.
3. **Model-driven todos.** Let the model call a `todowrite` tool so it can plan and update todo items; the TUI must track those todos somewhere on screen and keep them visible as they change.

Rules from the parent plan stay: `Update` pure, side effects in `tea.Cmd`, no new dependencies without user sign-off, run `go test ./...` before claiming anything, no em dashes in written output.

## Executive Summary

- Phase 3.9 deferred "Streaming token paint in the TUI" with `[~]`. This phase takes it: SSE deltas from the OpenCode chat endpoint, mapped onto the existing part types, then tokens/sec derived live from deltas (rolling window) and as a fallback from `usage` + elapsed time.
- `todowrite` becomes the seventh tool. Todos persist in a new `todos` table (migration v2) so replay from SQLite can rebuild the panel exactly like the transcript. The model advertises the tool via the existing `Tools []ToolSpec` array in `ChatRequest`.
- Status line becomes segment-based: a small set of named segments, each renderable or hidden. Toggling is a keybinding (`s` cycles or `/status` command); the choice is per-session and persisted in `sessions` (or a config row), not hardcoded.
- No new third-party dependencies are expected: SSE parsing is stdlib `bufio` over `http.Response.Body` with `application/x-ndjson` or `text/event-stream` handling; the existing Charm widgets cover the todo panel.

## 4.1 Provider streaming deltas (P1)

- [ ] `internal/provider/opencode` gains `ChatStream(ctx, req, onDelta func(Delta))` next to `Chat` - `Chat` stays for tests/fallback; `Delta` carries text, reasoning, tool-call deltas, and cumulative usage
- [ ] Parse `text/event-stream` (and `application/x-ndjson` if the endpoint emits that) with `bufio.Scanner`; `data: {...}` lines only, skip `[DONE]` and comments - stdlib only, no SSE dependency
- [ ] Reuse `wireResponse`/`wireChoice` shapes: each chunk has `choices[0].delta.{content,reasoning_content,tool_calls}` plus optional `usage` - verify against a recorded httptest fixture before writing the parser
- [ ] Usage arrives at the end of the stream (`usage` on the final chunk, or a trailing chunk); expose `TokensOutput` cumulative so the agent can write the `step-finish` part exactly as today - same fields as `wireUsage`
- [ ] Tool-call deltas accumulate across chunks (index + id + name + arguments fragments) until `finish_reason: "tool-calls"`; only then is a complete `ToolCall` emitted - mirror of the non-streaming unmarshal in `client.go`
- [ ] Timeout and cancellation: `ChatStream` respects `ctx` (client timeout from the agent call site), aborts mid-stream on ctx.Done, returns a readable error
- [ ] Test: httptest server streams 5 chunks; parser yields 5 deltas with accumulated text equal to the concatenation; usage on the final chunk lands - new `client_stream_test.go`, exit 0
- [ ] Test: garbage line mid-stream (non-JSON `data:`) is skipped, not fatal; server abort mid-stream returns an error and no panic - exit 0

## 4.2 Agent streaming turn + tokens/sec source (P1)

- [ ] `internal/agent` `Send` uses `ChatStream` when `Options.Streaming` is true (default true; non-streaming path retained for tests and the fallback)
- [ ] Deltas map to events: text delta updates the in-flight `text` part, reasoning delta updates the in-flight `reasoning` part, complete tool call emits `EventTool` exactly like today - `EventKind` may gain `EventStream`/`EventUsage` only if the chat model needs a distinct case; prefer reusing `EventPart` when possible
- [ ] `step-finish` part still written once with final usage and elapsed time (`time_start`/`time_end` on the parts rows already support this) - elapsed per step = `time.Since(stepStart)`
- [ ] New event carries tokens/sec data: `tokens_output` cumulative delta count + elapsed, or a computed rate when the agent closes the step - chat model renders, agent does not format
- [ ] Test: fake streaming provider drives `Send`; db rows match the non-streaming fixture test (part-type counts identical); `TestSendFixturePartTypes` still passes with streaming on - exit 0
- [ ] Test: tokens/sec math unit-tested in the agent or a small helper (delta 60 tokens over 2.0s = 30.0 tps; zero-time guard returns "-" not NaN/Inf) - exit 0

## 4.3 Status line: live tokens/sec (P1)

- [ ] `statusLine()` gains a tps segment rendered while `m.busy`: `tps: 42.3` from the rolling window; on completion show the final average `tps: 42.3` for a few seconds or fold into the summary - `internal/ui/chat/chat.go:1101`
- [ ] Rolling window: last N delta samples (e.g. 10) over their wall time; window resets per step (each tool step restarts the count) - deterministic, no time source beyond `time.Now()`
- [ ] Fallback when the provider or model returns no streaming (non-streaming client): compute `usage.TokensOutput / elapsed` at step end and render it once - never invent a live value when there are no deltas
- [ ] Status line stays readable: tps segment is muted style like the rest; long values truncate (`99.9` max, or `>99.9`), no wrapping - verify full-width render in chat tests
- [ ] Test: chat test drives a fake streaming provider, asserts `View()` status line contains `tps:` with the expected value within tolerance while busy and after done - exit 0

## 4.4 Status line customizations (P1)

- [ ] Define named segments: `model`, `tps`, `tokens` (totals from last step-finish), `cost`, `scroll`, `models`, `prompt` (key hints) - one render function per segment
- [ ] Segment visibility stored per session (new column `status_segments` in `sessions` as a JSON array, migration v2 alongside the todos table) or a tiny `config` table; default = all segments on - migration must be additive, existing dbs keep working
- [ ] `s` key (or `/status` command if `/commands` exist) cycles segment visibility: first press shows the segment list as a one-line picker on the status line, arrow keys + enter toggle, escape exits - the y/n confirm layout is NOT used here (not a destructive decision); this is a picker, not a modal
- [ ] Choice persists on `UpdateSession` so replay/restart restores the same layout - store write happens in a `tea.Cmd`, `Update` stays pure
- [ ] Test: chat test toggles `model` off, asserts `View()` no longer contains the model label but still shows the tps segment; persisted value round-trips through `db.UpdateSession` - exit 0

## 4.5 Model-driven todos (P1)

- [ ] New `internal/tools/todo` with `todowrite` tool spec advertised in the agent's `Tools` array - input: `{"todos": [{"content": "...", "status": "pending"|"in_progress"|"completed"|"cancelled"}]}`; the tool replaces the full list (idempotent, matches OpenCode's `todowrite` contract: the model sends the whole list every time)
- [ ] DB migration v2 adds `todos` table: `session_id` FK cascade, `seq`, `content`, `status`, `time_updated`; unique per (session_id, seq) - replay rebuilds the panel from SQLite like the transcript
- [ ] Agent dispatch: `executeTool` handles `todowrite` -> replace-all for the session, emit `EventTool` with status `completed`; unknown todo statuses map to `pending` with a note, never an error that kills the turn - pattern of `agent.execWrite`/`execRead` in `agent.go`
- [ ] Persist both the `parts` `tool` row and the `tool_calls` row (input_json = full todos JSON, output = "todos updated: N") so the export shape stays OpenCode-compatible - same invariants as Phase 3.6
- [ ] TUI tracks todos on screen: a todo panel rendered between transcript and status line (or a toggle `t` that swaps the panel), showing each todo as `[ ] pending` / `[>] in_progress` / `[x] completed` / `[-] cancelled`; updates apply in place on `EventTool`, no full repaint flicker - lipgloss styles match existing muted/focused palette
- [ ] Panel shows only the current session's todos; empty list renders nothing or a single muted `todos: none` line, never a blank hole - transcript height math must not clip the prompt
- [ ] Scroll interaction: when the panel is visible it owns its scroll keys; transcript scroll hints hide or stay consistent - verify full-screen render in chat tests, no clipped lines
- [ ] Test: agent test drives `todowrite` through the loop, asserts `todos` rows replace-all and the `tool` + `tool_calls` rows land - exit 0
- [ ] Test: chat test feeds two `todowrite` calls (second completes one item), asserts `View()` shows the status change on screen; replay test inserts the same rows and `View()` rebuilds the identical panel without network - exit 0

## 4.6 Schema migration (P1, part of 4.4/4.5)

- [ ] Migration v2: `todos` table + `sessions.status_segments` column (or config table) in one numbered migration - `schema_migrations` records version 2; existing v1 databases migrate in place, `go test ./internal/db` covers fresh + v1 -> v2 upgrade
- [ ] Indexes: `idx_todos_session ON todos(session_id, seq)` - mirror of `idx_parts_message_seq`
- [ ] Store API: `ReplaceTodos(ctx, sessionID, todos)`, `ListTodos(ctx, sessionID)`, and `UpdateSessionSegments` (or the config equivalent) - tested round-trip

## Dependencies

- Needs: Phase 1 store + parts schema (step-finish timestamps exist), Phase 2 bash policy (todo tool never shells out, no policy involvement), Phase 3 tool dispatch pattern (`agent.executeTool`)
- Blocks: nothing in 0.0.1; later versions append a new phase folder
- New dependencies: none expected (stdlib SSE parsing, existing Charm widgets). Any addition is a project-policy change and needs explicit user sign-off first per `AGENTS.md`.

## Closure gates

- [ ] `go test ./... -count=1` passes on a rebuilt tree (record exit code) - pending
- [ ] `go vet ./...` passes - pending
- [ ] Status line shows a live tokens/sec value while streaming and a usage-based value after a non-streaming turn - pending (chat test)
- [ ] Status line segments toggle via the picker and the layout survives a session restart (persisted) - pending
- [ ] A fixture session with `todowrite` calls replays into the same on-screen todo panel from SQLite with no network - pending
- [ ] Phase 3 deferred row "Streaming token paint in the TUI" flips from `[~]` to done reference (mark 3.9 row `[~]` -> update parent README table after the gate passes) - pending

Manual checks (need a live terminal): streaming looks smooth with a real key (no flicker, tps number updates, no clipped status line); todo panel readable on a full screen.
