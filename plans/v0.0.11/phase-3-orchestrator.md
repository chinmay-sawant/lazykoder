# Phase 3 - Orchestrator layer over internal/subagent

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** complete
> **Estimated effort:** 5-7 days

---

## Overview

Planner/orchestrator layer over `internal/subagent`: the parent emits a
structured plan, assigns children by model strength, reviews summaries, and
may re-spawn failures. Depth stays capped at 1. Prefers Phase 2 landed
(cross-provider fan-out) but works OpenCode-only.

## 3.1 Plan emission

- [x] Add orchestrator prompt path in `internal/agent`: when sub-agents are
      enabled and the task looks decomposable, the first request asks for a
      structured plan (subtasks, role, suggested model class); persist the
      plan as a message part so `/resume` restores it. The plan is bounded to
      eight direct subtasks and is persisted as a plan part.
- [x] Fallback: if the plan call fails or returns malformed structure, run
      the turn as today with no orchestration. The ordinary parent loop is
      unchanged when planning returns an error or empty plan.

## 3.2 Strength table and assignment

- [x] Add settings for role-to-model-class defaults (`orchestrator.*`),
      shipping built-in defaults (flash tier for explore, pro coder tier for
      general). Settings are persisted and normalized at load, with runtime
      resolution falling back to the cached catalog.
- [x] Extend Host dispatch to accept per-subtask model class from the plan,
      resolving through the same override chain used today
      (`ConfigFromSettings`). Each task carries its model class to the host.

## 3.3 Review and re-spawn

- [x] After children finish, the parent reviews summaries against the plan;
      failed or incomplete subtasks may be re-spawned once, still respecting
      MaxDepth=1, budget caps, and wall-clock timeouts. The host retries
      failed or empty summaries at most once.
- [x] Drawer and transcript render the plan and per-child status without
      breaking existing layout rules (single-line rows, truncation to width).

## 3.4 Docs and gate

- [x] Update `docs/`, knowledge-base sub-agent concept page, and glossary in
      the same change.
- [x] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      "audit these N packages" produces one plan message, N concurrent
      children on distinct models, ordered summaries, final answer citing
      each child. Record outcomes beside these rows when they run. Automated
      gates pass; the live multi-provider terminal scenario remains a human
      check.
