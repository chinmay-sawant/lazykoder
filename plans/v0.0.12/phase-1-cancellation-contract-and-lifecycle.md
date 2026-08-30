# v0.0.12 / Phase 1 - Cancellation contract and lifecycle

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** implementation rows validated; repository closure is blocked by an unrelated scripts package error
> **Estimated effort:** 3-5 working days
> **Priority:** P1

---

## Overview

Define one cancellation contract for the parent turn, child jobs, provider
requests, tools, browser work, and durable state. The local stop signal must
be immediate and idempotent. Cleanup must complete without blocking the
Bubble Tea update loop or leaving pending rows in SQLite.

## 1.1 Baseline and ownership map

- [x] Record a clean baseline for `make test`, `make lint`, `make vet`, and
      `go build ./...`; identify pre-existing failures before changing any
      status row. Store command output in the implementation change or CI
      record, not in a guessed checklist status.
- [x] Map the cancellation owners and handoffs in
      `internal/ui/chat/runtime.go`, `internal/ui/chat/keys.go`,
      `internal/agent/agent.go`, `internal/agent/tools_exec.go`,
      `internal/subagent/manager.go`, and each provider client. The map must
      name the context owner, cancel function, completion signal, and durable
      status writer for parent turns and child jobs.

## 1.2 Parent and child contexts

- [x] Make the parent turn own one `context.WithCancel(context.Background())`
      for each send, continue, or compact operation. Every provider call,
      tool call, and foreground child spawned by that operation must receive
      that context or a child derived from it.
- [x] Keep background child jobs independent of normal parent completion, but
      retain an explicit manager-owned `CancelFunc` for every queued and
      running job. A background job must stop through `task_cancel`, parent
      shutdown, or its configured timeout.
- [x] Keep `Manager.execute` responsible for releasing the concurrency
      semaphore and writer lock on every exit path, including cancellation
      while queued or waiting for the writer lock.
- [x] Make cancellation idempotent. Repeating `task_cancel`, pressing the UI
      stop action twice, or racing cancellation with normal completion must
      produce one terminal result and must not close a channel twice.

## 1.3 Immediate stop and durable state

- [x] Split cancellation signalling from cleanup waiting at the UI boundary.
      The Bubble Tea `Update` path must signal `turnCancel` and child cancel
      functions immediately, then use a `tea.Cmd` or existing event path to
      observe cleanup. It must not wait on a remote provider while holding the
      UI update loop.
- [x] Ensure a cancelled provider or tool call cannot leave its `tool_calls`
      row in `pending` or `running`. Persist a bounded `cancelled` result with
      a short independent persistence context when the request context is
      already done.
- [x] Preserve the job state contract: queued and running jobs become
      `cancelled` when explicitly stopped, `timed_out` only on deadline, and
      `failed` only for a non-cancellation error. Recovery must not resurrect
      a terminal cancelled job.
- [x] Keep cancellation visible in the transcript and `/agents` drawer while
      removing the live spinner and releasing the active count. A cancelled
      child summary must not be presented as a successful report.

## 1.4 Provider endpoint stop semantics

- [x] Add a provider-side cancellation seam only for protocols that document a
      request ID and cancellation endpoint. The seam must report whether a
      remote cancellation was requested, accepted, rejected, or unavailable.
- [x] For the current OpenCode, OpenAI-compatible, Codex, and Grok paths,
      guarantee local cancellation by passing the context through the request,
      stream reader, subscription runner, and retry wait. Do not invent a
      remote cancellation call when the provider does not expose one.
- [x] On cancellation, stop retries and close the active response body or
      stream. A late response, late delta, or late child result must not
      mutate the new turn's state.

## 1.5 Contract tests

- [x] Add a blocking provider fake and HTTP test server that prove cancelling
      a parent turn causes the request context to finish, prevents another
      retry, closes the stream, and returns a cancellation error promptly.
- [x] Add manager tests for queued cancellation, running cancellation,
      cancellation while waiting for the single-writer lock, `CancelAll`,
      repeated cancellation, timeout classification, slot release, and no
      goroutine leak after `Shutdown`.
- [x] Add agent/tool tests proving cancellation reaches `webfetch`, `bash`,
      and parallel task tool calls and that each persisted result is terminal.

## Dependencies

- `internal/ui/chat` turn ownership and event delivery
- `internal/agent` provider, tool, and persistence boundaries
- `internal/subagent.Manager` job handles, semaphore, writer lock, and store
- Provider request and stream implementations

## Ownership map

| Operation | Context owner | Cancel signal | Completion signal | Durable status writer |
| --- | --- | --- | --- | --- |
| Parent send, continue, or compact | `chat.Model.startTurn` | `Model.turnCancel` | `eventCh` close and `eventDoneMsg` | `Agent` writes messages, parts, and tool calls |
| Foreground child | `subagent.Manager.Spawn` from the parent context | The handle `CancelFunc` | The handle `done` channel | `Manager.finish` writes `subagent_jobs` |
| Background child | `subagent.Manager.Spawn` with `context.WithoutCancel` | The manager handle or `RequestCancel(All)` | The handle `done` channel | `Manager.finish` or conditional store cancellation |
| Provider request | The parent or child operation context | `http.Request` cancellation or subscription command context | HTTP response or stream return | The owning `Agent` step and tool writer |
| Tool and browser work | The `Agent` tool context | Tool process, browser process, or host cancellation | Tool return and `EventTool` | `CancelToolCall` writes bounded `cancelled` output |

The UI signals cancellation through `RequestCancel` or `RequestCancelAll` and
observes cleanup through the existing event and completion paths. It does not
wait for provider, tool, or child cleanup inside `Update`.

## Closure gate

- [~] Local cancellation stops the parent request and every owned child or
      tool operation, releases all slots, and persists terminal statuses.
      Phase 1 package tests pass. The repository-wide gate remains blocked by
      duplicate `main` functions in `scripts/spawn_hello.go` and
      `scripts/spawn_hello_retry.go`.
      Next gate: isolate or rename one helper entry point in that unrelated
      scripts scope, then rerun the repository-wide checks.
- [x] Provider-side cancellation is reported only when a documented provider
      cancellation endpoint accepts a request identifier. Otherwise the result
      states that local transport cancellation was performed.
- [~] Blocking provider, retry, stream, manager, tool, and persistence tests
      pass with timing and terminal-state evidence.
      The focused and race tests pass. The final repository gate cannot finish
      until the unrelated `scripts` package compiles.
      Next gate: rerun the full test, lint, vet, and build commands after the
      scripts package is repaired.

## Evidence

- Baseline: `GOCACHE=/tmp/lazykoder-phase1-go-cache make test`, `make lint`,
  `make vet`, and `go build ./...` all passed before the implementation edits.
- Focused contract tests: `go test ./internal/db ./internal/subagent
  ./internal/provider/opencode ./internal/agent ./internal/ui/chat` passed.
- Race coverage: `go test -race ./internal/agent ./internal/subagent
  ./internal/provider/opencode ./internal/ui/chat` passed.
- Final project checks: `GOCACHE=/tmp/lazykoder-phase1-go-cache make test`,
  `make lint`, `make vet`, and `go build ./...` all stop at the unrelated
  `scripts` package because `spawn_hello.go` and `spawn_hello_retry.go` both
  declare `main`. Those files were not changed in this phase.
- Targeted final checks: `go test ./internal/...`, `go vet ./internal/...`,
  `go build . ./internal/...`, and the focused race command above pass.
