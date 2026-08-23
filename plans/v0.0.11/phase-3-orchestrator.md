# Phase 3 - Orchestrator layer over internal/subagent

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** not started
> **Estimated effort:** 5-7 days

---

## Overview

Planner/orchestrator layer over `internal/subagent`: the parent emits a
structured plan, assigns children by model strength, reviews summaries, and
may re-spawn failures. Depth stays capped at 1. Prefers Phase 2 landed
(cross-provider fan-out) but works OpenCode-only.

## 3.1 Plan emission

- [ ] Add orchestrator prompt path in `internal/agent`: when sub-agents are
      enabled and the task looks decomposable, the first request asks for a
      structured plan (subtasks, role, suggested model class); persist the
      plan as a message part so `/resume` restores it.
- [ ] Fallback: if the plan call fails or returns malformed structure, run
      the turn as today with no orchestration.

## 3.2 Strength table and assignment

- [ ] Add settings for role-to-model-class defaults (`orchestrator.*`),
      shipping built-in defaults (flash tier for explore, pro coder tier for
      general); users override in settings UI; validate against the cached
      model catalog at load time.
- [ ] Extend Host dispatch to accept per-subtask model class from the plan,
      resolving through the same override chain used today
      (`ConfigFromSettings`).

## 3.3 Review and re-spawn

- [ ] After children finish, the parent reviews summaries against the plan;
      failed or incomplete subtasks may be re-spawned once, still respecting
      MaxDepth=1, budget caps, and wall-clock timeouts.
- [ ] Drawer and transcript render the plan and per-child status without
      breaking existing layout rules (single-line rows, truncation to width).

## 3.4 Docs and gate

- [ ] Update `docs/`, knowledge-base sub-agent concept page, and glossary in
      the same change.
- [ ] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      "audit these N packages" produces one plan message, N concurrent
      children on distinct models, ordered summaries, final answer citing
      each child. Record outcomes beside these rows when they run.
