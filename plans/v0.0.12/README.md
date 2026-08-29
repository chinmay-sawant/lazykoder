# v0.0.12 - Browser access, cancellable sub-agents, and TUI refresh

> **Parent:** `plans/v0.0.11/README.md` - follow-on work for browser access and orchestration
> **Status:** proposed; planning only, no implementation rows are closed
> **Estimated effort:** 10-15 working days, excluding screenshot review and provider availability
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`

---

## Overview

This is the v0.0.12 index and shared contract. The implementation ledger is
split into one file per responsibility so each phase can be reviewed and
closed without opening an unrelated section.

The plan carries the unfinished browser work out of v0.0.10 Phase 11,
hardens the existing goroutine-based sub-agent manager, and reserves a
screenshot-driven pass for the TUI.

## Executive summary

- Public internet access stays behind the existing `webfetch` tool. HTTP mode
  remains deterministic. Auto mode may use an isolated Chrome or Chromium
  process for JavaScript-rendered pages. The tool reads one public page at a
  time and never crawls, sends email, reuses a browser profile, or reaches a
  private destination.
- Sub-agents remain bounded child `agent.Agent` runs managed by
  `internal/subagent.Manager`. Each job gets a cancellable context and a
  terminal `done` signal. Foreground jobs follow the parent turn. Background
  jobs remain alive until explicit cancellation, timeout, shutdown, or normal
  completion.
- Cancelling a parent turn must signal the parent provider request and every
  child job immediately. Cleanup and persistence must finish without blocking
  the Bubble Tea `Update` loop or leaving a pending tool or job row forever.
- UI scope stays provisional until the user supplies screenshots. No visual
  row may be marked complete from source inspection alone.

## Current implementation boundary

The current code already has these pieces:

- `internal/tools/webfetch` validates public HTTP(S) destinations, supports
  `auto`, `http`, and `browser` modes, extracts bounded HTML metadata, and
  launches an isolated browser through a local validating proxy.
- `internal/subagent.Manager` creates per-job contexts, runs jobs in
  goroutines, limits concurrency, serializes general-role writers by default,
  persists job handles, and exposes `Cancel`, `CancelAll`, and `Shutdown`.
- `internal/ui/chat` owns the parent `context.WithCancel`, forwards parent
  cancellation to child jobs, and renders live child rows in the `/agents`
  drawer.
- Provider HTTP and stream calls already receive a `context.Context` through
  `http.NewRequestWithContext`; the new work must prove the full stop path and
  close the remaining lifecycle gaps.

These facts are starting points, not closure evidence for this plan.

## Phase index

| Phase | Ledger | Responsibility | Status |
| --- | --- | --- | --- |
| 1 | [`phase-1-cancellation-contract-and-lifecycle.md`](phase-1-cancellation-contract-and-lifecycle.md) | Parent and child cancellation, provider stop semantics, durable terminal state | `[ ]` planned |
| 2 | [`phase-2-public-internet-and-browser-reading.md`](phase-2-public-internet-and-browser-reading.md) | Public HTTP access, isolated browser reads, egress safety, fixtures | `[ ]` planned |
| 3 | [`phase-3-subagent-goroutines-and-parent-orchestration.md`](phase-3-subagent-goroutines-and-parent-orchestration.md) | Bounded goroutines, task controls, recovery, and sibling behavior | `[ ]` planned |
| 4 | [`phase-4-screenshot-driven-tui-enhancements.md`](phase-4-screenshot-driven-tui-enhancements.md) | Screenshot-led cancellation, browser status, and responsive TUI work | `[~]` awaiting screenshots |
| 5 | [`phase-5-documentation-and-closure-gates.md`](phase-5-documentation-and-closure-gates.md) | Documentation, automated gates, manual proof, and release closure | `[ ]` planned |

## Dependency order

Phase 1 defines cancellation ownership and the local versus provider-side
stop boundary. Phase 2 uses that contract for HTTP and browser operations.
Phase 3 applies it to child goroutines and task controls. Phase 4 starts only
after the screenshots turn UI requests into concrete acceptance rows. Phase 5
closes documentation and verification after the implementation phases.

## Shared rules

- `[ ]` means not started or not proven.
- `[x]` means implemented and validated with current evidence.
- `[~]` means intentionally partial or deferred, with a reason and next gate.
- A provider may expose a request-cancellation endpoint, but the shared
  OpenAI-compatible contract does not provide one. lazykoder can guarantee
  local context cancellation, response or stream closure, browser termination,
  worker-slot release, and durable terminal state. It must not claim remote
  inference stopped unless a documented provider endpoint accepted a request
  identifier.
- No new browser or provider dependency may be added without an explicit
  capability decision and user approval.
- No phase row is closed from plan prose. The matching source, test,
  benchmark, or terminal evidence must exist first.

## Shared dependencies

- Existing `internal/tools/webfetch` HTTP, extraction, proxy, and process seams
- Existing provider context propagation in `internal/provider/opencode`,
  `internal/provider/openai`, and `internal/provider/subscription`
- Existing `internal/agent` tool lifecycle and event channels
- Existing `internal/subagent.Manager`, SQLite job persistence, and `/agents`
  drawer
- User screenshots before any final UI layout or styling decision
- An installed Google Chrome or Chromium binary for the opt-in browser check
