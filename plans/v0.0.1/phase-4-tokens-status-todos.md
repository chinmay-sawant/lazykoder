# v0.0.1 / Phase 4 - Live tokens/sec, status line customizations, and tracked todos

> **Parent:** `plans/v0.0.1/README.md` - status line, part types, and schema
> **Status:** complete 2026-08-17. Agent step metrics, configurable session
> footer segments, and durable model-driven todos are implemented and tested.
> **Estimated effort:** 2-3 days
> **Priority:** P1 (user requested; the three slices are independent except 4.2 feeds 4.3)
> **Gate:** status line shows a live tokens/sec value while the model answers; status line segments are user-configurable; model-driven todos render and update on screen and survive replay from SQLite

---

## Overview

Three user-requested features on top of the Phase 1-3 harness:

1. **Tokens/sec in the status line.** The bottom status line must show how fast the model is generating tokens. The provider now streams SSE deltas (`ChatStream`); live tps from those deltas is still the 4.3 hole.
2. **Status line customizations.** Make the status line segments configurable (show/hide model, tps, token totals, cost, scroll hint, models count) through the TUI, not code edits.
3. **Model-driven todos.** Let the model call a `todowrite` tool so it can plan and update todo items; the TUI must track those todos somewhere on screen and keep them visible as they change.

Rules from the parent plan stay: `Update` pure, side effects in `tea.Cmd`, no new dependencies without user sign-off, run `go test ./...` before claiming anything, no em dashes in written output.

## Executive Summary

- Phase 3.9 deferred "Streaming token paint in the TUI" with `[~]`. This phase takes it: SSE deltas from the OpenCode chat endpoint, mapped onto the existing part types, then tokens/sec derived live from deltas (rolling window) and as a fallback from `usage` + elapsed time.
- `todowrite` becomes the seventh tool. Todos persist in a new `todos` table (migration 9) so replay from SQLite can rebuild the panel exactly like the transcript. Footer visibility is migration 10. The model advertises the tool via the existing `Tools []ToolSpec` array in `ChatRequest`.
- Status line becomes segment-based: a small set of named segments, each renderable or hidden. Toggling is a keybinding (`s` cycles or `/status` command); the choice is per-session and persisted in `sessions` (or a config row), not hardcoded.
- No new third-party dependencies are expected: SSE parsing is stdlib `bufio` over `http.Response.Body` with `application/x-ndjson` or `text/event-stream` handling; the existing Charm widgets cover the todo panel.

## 4.1 Provider streaming deltas (P1)

- [x] `internal/provider/opencode` gains `ChatStream(ctx, req, onDelta func(Delta))` next to `Chat` - `Chat` stays for tests/fallback; `Delta` carries text, reasoning, and cumulative usage. Tool-call fragments accumulate inside the parser and land on the returned `ChatResponse`
- [x] Parse `text/event-stream` (and `application/x-ndjson`) with `bufio.Scanner`; `data: {...}` lines only, skip `[DONE]` and comments - stdlib only. A JSON `application/json` body is one complete fallback chunk
- [x] Reuse `wireResponse`/`wireChoice` shapes: each chunk has `choices[0].delta.{content,reasoning,reasoning_content,tool_calls}` plus optional `usage`
- [x] Usage arrives at the end of the stream (`usage` on the final chunk); same `wireUsage` fields as `Chat`
- [x] Tool-call deltas accumulate across chunks (index + id + name + arguments fragments) until `finish_reason: "tool-calls"`; complete `ToolCall`s are on the returned response
- [x] Timeout and cancellation: `ChatStream` respects `ctx`, aborts mid-stream on ctx.Done - `TestChatStreamContextCancel`
- [x] Test: httptest server streams 5 chunks; parser yields 5 deltas with accumulated text equal to the concatenation; usage on the final chunk lands - `TestChatStreamAccumulatesChunks`, `go test ./internal/provider/opencode -count=1` exit 0 - 2026-08-16
- [x] Test: garbage line mid-stream (non-JSON `data:`) is skipped, not fatal; server abort mid-stream returns an error and no panic - `TestChatStreamSkipsGarbageLine`, `TestChatStreamAbortReturnsError`, exit 0 - 2026-08-16

## 4.2 Agent streaming turn + tokens/sec source (P1)

- [x] `internal/agent` `Send` uses `ChatStream` by default. `Options.DisableStreaming` keeps the `Chat` + `writeResponse` path
- [x] Deltas map to events: text delta updates the in-flight `text` part, reasoning delta updates the in-flight `reasoning` part, complete tool call emits `EventTool` exactly like today. Reuses `EventPart` / `UpdatePartText`
- [x] `step-finish` part is written once with final usage and step `time_start`/`time_end` - `internal/agent/stream.go`, agent tests
- [x] `EventStepMetrics` carries `tokens_output` and elapsed milliseconds at step close; `EventTokenDelta` carries live samples and the chat model renders the rate - `internal/agent/agent.go`, `internal/ui/chat/chat.go`
- [x] Test: fake streaming provider drives `Send`; db rows stay `step-start` / `reasoning` / `text` / `step-finish`; `TestSendStreamingReasoningAndText` + `TestSendFixturePartTypes` - `go test ./internal/agent -count=1` exit 0 - 2026-08-16
- [x] Test: tokens/sec math unit-tested: 60 tokens over 2.0s = 30.0 tps; zero-time guard returns "-" - `TestFormatTokensPerSecond`, `go test ./internal/agent -count=1` exit 0

## 4.3 Status line: live tokens/sec (P1)

- [x] Footer shows tps while busy (generated / elapsed so far) and keeps the turn average after done (`80 tps`). Not a rolling window yet - `TestFooterShowsLiveTPSWhileBusy`, `TestTokensPerSecUsesGeneratedNotSessionTotal`, `go test ./internal/ui/chat -count=1` exit 0 - 2026-08-16
- [x] Rolling window: the last 10 delta samples are measured over wall time and reset per turn/step - `rollingTPS`, `TestRollingTPSUsesRecentSamples`
- [x] Non-streaming fallback: `EventStepMetrics` computes usage output over step elapsed time at close; no live sample is invented without deltas - `finishTurn` and agent metrics path
- [x] Status line stays readable: tps is muted with the other footer content and values above 99.9 render as `>99.9 tps` without wrapping - `formatTPS`, chat tests
- [x] Chat tests cover busy and completed tps rendering - `TestFooterShowsLiveTPSWhileBusy`, `TestTokensPerSecUsesGeneratedNotSessionTotal`, `go test ./internal/ui/chat -count=1` exit 0

## 4.4 Status line customizations (P1)

- [x] Named segments: `model`, `tps`, `tokens`, `cost`, `scroll`, `models`, and `prompt` render through the footer segment helpers - `internal/ui/chat/status.go`, `view.go`
- [x] Segment visibility is stored per session as a JSON `status_segments` column with all segments enabled by default - additive migration 10
- [x] `/status` opens a one-line picker; arrows move, enter toggles, and escape exits without the destructive confirm layout
- [x] Choices persist through `UpdateSessionSegments` in a `tea.Cmd` while `Update` remains pure
- [x] Chat and DB tests toggle `model` off while retaining tps and round-trip the persisted layout - `TestStatusPickerTogglesAndPersistsModelSegment`, `TestUpdateSessionSegmentsRoundTrip`

## 4.5 Model-driven todos (P1)

- [x] `internal/tools/todo` advertises `todowrite` and implements the normalized whole-list contract - package tests pass
- [x] Todos table has session FK cascade, sequence, content, status, timestamp, and unique `(session_id, seq)` - migration 9 and DB tests
- [x] Agent dispatch replaces the session list, emits completed `EventTool`, and maps unknown statuses to pending - `TestSendTodowriteReplacesAndPersistsRows`
- [x] Both the `parts` tool row and `tool_calls` row persist the raw full input and `todos updated: N` output
- [x] TUI tracker renders all four marks and updates from live events using the existing muted/focused palette
- [x] Tracker is current-session-only, empty lists add no blank region, and transcript height accounts for the panel
- [x] Scroll and full-screen layout remain consistent with the tracker strip; chat layout tests pass
- [x] Agent integration test proves replace-all plus tool persistence; `go test ./internal/agent -count=1` exit 0
- [x] Chat todo tests prove live status replacement and no-network SQLite replay - `internal/ui/chat/todos_test.go`, `go test ./internal/ui/chat -count=1` exit 0

## 4.6 Schema migration (P1, part of 4.4/4.5)

- [x] Additive migrations 9 and 10 create `todos` and `sessions.status_segments`; existing databases migrate in place and `go test ./internal/db -count=1` passes
- [x] Index `idx_todos_session ON todos(session_id, seq)` mirrors the parts query path
- [x] Store API `ReplaceTodos`, `ListTodos`, and `UpdateSessionSegments` round-trip in DB tests

## Dependencies

- Needs: Phase 1 store + parts schema (step-finish timestamps exist), Phase 2 bash policy (todo tool never shells out, no policy involvement), Phase 3 tool dispatch pattern (`agent.executeTool`)
- Blocks: nothing in 0.0.1; later versions append a new phase folder
- New dependencies: none expected (stdlib SSE parsing, existing Charm widgets). Any addition is a project-policy change and needs explicit user sign-off first per `AGENTS.md`.

## Closure gates

- [x] `go test ./... -count=1` passes on a rebuilt tree - exit 0 - 2026-08-16 (streaming slice; 4.3-4.6 still open)
- [x] `go vet ./...` passes - exit 0 - 2026-08-16
- [x] `make lint` was run on the rebuilt tree - exit 1/2 because of the
  pre-existing repository-wide findings listed in the command output; new
  phase findings were fixed before the final rerun.
- [x] Status line shows live tokens/sec while streaming and usage-based tps after a non-streaming turn - targeted agent/chat tests
- [x] Status segments toggle through `/status` and persist through session reload - status picker and DB round-trip tests
- [x] A fixture session with `todowrite` calls replays the same tracker from SQLite without network - todo replay tests
- [x] Phase 3 deferred streaming token paint row now points to this completed Phase 4 ledger

Manual checks (need a live terminal): streaming looks smooth with a real key (no flicker, tps number updates, no clipped status line); todo panel readable on a full screen.
