# v0.0.12 / Phase 3 - Sub-agent goroutines and parent orchestration

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** planned; no implementation rows are closed
> **Estimated effort:** 3-5 working days
> **Priority:** P1

---

## Overview

Keep child agents as bounded goroutines behind `internal/subagent.Manager`.
Every child must have an owned cancellation function, a completion signal,
and a durable terminal state. Parent cancellation must reach all children and
must not create a second unmanaged lifecycle in the TUI or task tool.

## 3.1 Manager and runner contract

- [ ] Keep all child execution behind `internal/subagent.Manager` and
      `AgentRunner`; the TUI and task tool must not create unmanaged worker
      goroutines.
- [ ] Make each `Job` carry the cancellation and completion lifecycle through
      `Manager.execute` into `AgentRunner.Run` and `agent.Agent.Send`. A child
      provider request must stop when its job context is cancelled.
- [ ] Ensure `task_wait` distinguishes a completed summary, partial step-limit
      result, failed result, timed-out result, and cancelled result. It must
      not convert cancellation into a generic provider failure.

## 3.2 Parallel task calls

- [ ] Keep independent task calls in goroutines behind the existing bounded
      manager semaphore. Add a test that starts multiple children, confirms
      they overlap, and confirms the configured maximum is never exceeded.
- [ ] Define the failure policy for sibling tasks: one failed child must not
      strand the others, while parent cancellation must signal every sibling
      and wait for terminal cleanup through the manager-owned path.
- [ ] Preserve the depth-one rule, role tool allowlists, general-writer lock,
      durable child sessions, and crash recovery while adding cancellation
      state. No child may gain a second task-control plane as a side effect.

## 3.3 Task controls and recovery

- [ ] Make `task_cancel` return promptly after signalling one job or all jobs,
      then expose terminal status through `task_status` and `task_wait` after
      cleanup completes.
- [ ] Make `/agents` row cancellation use the same manager path as
      `task_cancel`; do not maintain a second UI-only cancellation flag.
- [ ] Confirm that shutdown cancels all live jobs, closes provider and tool
      work, waits for owned goroutines to exit, and prevents `Recover` from
      starting duplicate jobs.
- [ ] Add race and lifecycle tests for parent cancellation versus child
      completion, recovery versus cancellation, and manager replacement.

## Dependencies

- Phase 1 cancellation contract
- Existing `internal/subagent.Manager`, `AgentRunner`, `Host`, and task tools
- Existing SQLite `subagent_jobs` persistence
- Existing `/agents` drawer and parent turn event path

## Closure gate

- [ ] Multiple child tasks overlap within the configured semaphore limit.
- [ ] Parent cancellation, `task_cancel`, and shutdown stop all owned child
      provider requests and leave terminal durable statuses.
- [ ] `task_wait` and the `/agents` drawer distinguish completed, partial,
      failed, timed-out, and cancelled results.
- [ ] Race and lifecycle tests show no duplicate recovery or leaked goroutine.

## Out of scope

- Nested sub-agent trees beyond the existing depth-one rule
- A second task-control API owned by the UI
- Unbounded child concurrency or model-supplied wall-clock timeouts
