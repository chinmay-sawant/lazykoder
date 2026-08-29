# v0.0.12 / Phase 1 - Cancellation contract and lifecycle

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** planned; no implementation rows are closed
> **Estimated effort:** 3-5 working days
> **Priority:** P1

---

## Overview

Define one cancellation contract for the parent turn, child jobs, provider
requests, tools, browser work, and durable state. The local stop signal must
be immediate and idempotent. Cleanup must complete without blocking the
Bubble Tea update loop or leaving pending rows in SQLite.

## 1.1 Baseline and ownership map

- [ ] Record a clean baseline for `make test`, `make lint`, `make vet`, and
      `go build ./...`; identify pre-existing failures before changing any
      status row. Store command output in the implementation change or CI
      record, not in a guessed checklist status.
- [ ] Map the cancellation owners and handoffs in
      `internal/ui/chat/runtime.go`, `internal/ui/chat/keys.go`,
      `internal/agent/agent.go`, `internal/agent/tools_exec.go`,
      `internal/subagent/manager.go`, and each provider client. The map must
      name the context owner, cancel function, completion signal, and durable
      status writer for parent turns and child jobs.

## 1.2 Parent and child contexts

- [ ] Make the parent turn own one `context.WithCancel(context.Background())`
      for each send, continue, or compact operation. Every provider call,
      tool call, and foreground child spawned by that operation must receive
      that context or a child derived from it.
- [ ] Keep background child jobs independent of normal parent completion, but
      retain an explicit manager-owned `CancelFunc` for every queued and
      running job. A background job must stop through `task_cancel`, parent
      shutdown, or its configured timeout.
- [ ] Keep `Manager.execute` responsible for releasing the concurrency
      semaphore and writer lock on every exit path, including cancellation
      while queued or waiting for the writer lock.
- [ ] Make cancellation idempotent. Repeating `task_cancel`, pressing the UI
      stop action twice, or racing cancellation with normal completion must
      produce one terminal result and must not close a channel twice.

## 1.3 Immediate stop and durable state

- [ ] Split cancellation signalling from cleanup waiting at the UI boundary.
      The Bubble Tea `Update` path must signal `turnCancel` and child cancel
      functions immediately, then use a `tea.Cmd` or existing event path to
      observe cleanup. It must not wait on a remote provider while holding the
      UI update loop.
- [ ] Ensure a cancelled provider or tool call cannot leave its `tool_calls`
      row in `pending` or `running`. Persist a bounded `cancelled` result with
      a short independent persistence context when the request context is
      already done.
- [ ] Preserve the job state contract: queued and running jobs become
      `cancelled` when explicitly stopped, `timed_out` only on deadline, and
      `failed` only for a non-cancellation error. Recovery must not resurrect
      a terminal cancelled job.
- [ ] Keep cancellation visible in the transcript and `/agents` drawer while
      removing the live spinner and releasing the active count. A cancelled
      child summary must not be presented as a successful report.

## 1.4 Provider endpoint stop semantics

- [ ] Add a provider-side cancellation seam only for protocols that document a
      request ID and cancellation endpoint. The seam must report whether a
      remote cancellation was requested, accepted, rejected, or unavailable.
- [ ] For the current OpenCode, OpenAI-compatible, Codex, and Grok paths,
      guarantee local cancellation by passing the context through the request,
      stream reader, subscription runner, and retry wait. Do not invent a
      remote cancellation call when the provider does not expose one.
- [ ] On cancellation, stop retries and close the active response body or
      stream. A late response, late delta, or late child result must not
      mutate the new turn's state.

## 1.5 Contract tests

- [ ] Add a blocking provider fake and HTTP test server that prove cancelling
      a parent turn causes the request context to finish, prevents another
      retry, closes the stream, and returns a cancellation error promptly.
- [ ] Add manager tests for queued cancellation, running cancellation,
      cancellation while waiting for the single-writer lock, `CancelAll`,
      repeated cancellation, timeout classification, slot release, and no
      goroutine leak after `Shutdown`.
- [ ] Add agent/tool tests proving cancellation reaches `webfetch`, `bash`,
      and parallel task tool calls and that each persisted result is terminal.

## Dependencies

- `internal/ui/chat` turn ownership and event delivery
- `internal/agent` provider, tool, and persistence boundaries
- `internal/subagent.Manager` job handles, semaphore, writer lock, and store
- Provider request and stream implementations

## Closure gate

- [ ] Local cancellation stops the parent request and every owned child or
      tool operation, releases all slots, and persists terminal statuses.
- [ ] Provider-side cancellation is reported only when a documented provider
      cancellation endpoint accepts a request identifier. Otherwise the result
      states that local transport cancellation was performed.
- [ ] Blocking provider, retry, stream, manager, tool, and persistence tests
      pass with timing and terminal-state evidence.
