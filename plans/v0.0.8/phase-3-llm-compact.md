# v0.0.8 / Phase 3 - LLM compact turn

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** complete 2026-08-17
> **Estimated effort:** 1-2 days
> **Priority:** P0
> **Gate:** when preflight says the live window is too small, the agent
> asks the chosen summarizer (tools off) with `compact.md`, persists a
> compaction part, and retries the step from the checkpoint

## Overview

This is the first provider call that is not a normal chat turn.

## Phase 3: LLM compact

### 3.1 Summarizer call

- [x] Preflight in `runSteps` / `maybeCompact` uses `NeedsCompact`
      against `ContextOf(live model)` (passed in from Options).
- [x] Summarizer request: `compact.md` + serialized head + previous
      summary if any; tools disabled; `MaxTokens` 4096.
- [x] Persist `agent = "compaction"` plus `parts.type = "compaction"`
      with summary text and the JSON envelope (`tail_start_message_id`,
      `from_model`, `to_model`, `reason`).
- [x] Emit TUI events (`EventCompacting`, `EventCompacted`).

### 3.2 Replay and overflow

- [x] Auto path keeps the last real user turn in the tail so the next
      model turn is not aimed at the summary.
- [x] Manual compact (`Agent.Compact`) stops after the checkpoint.
      `TestManualCompactStopsAfterCheckpoint`.
- [x] Provider overflow errors trigger **one** compact+retry.
      `TestOverflowRetriesOnce`. `CompactAuto: false` still retries.
- [x] If the summarizer request cannot fit even after prune/chunking,
      do not send the overflowing user turn. Return a readable error.

### 3.3 Validation gate

- [x] `TestSendUnderBudgetSkipsCompact`: under-budget send is unchanged.
- [x] `TestSendOverBudgetCompactsThenChats`: tools-off compact then
      normal call from the checkpoint.
- [x] `TestCompactShrinkUsesOutgoingModel`: 400k estimate, incoming
      256k, outgoing 1M uses outgoing for compact and incoming after.
- [x] `go test ./internal/agent -count=1` passes. exit 0
- [x] `go build ./...` passes. exit 0

## Dependencies

- Phase 1 `PickSummarizer` / `NeedsCompact` / `compact.md`.
- Phase 2 checkpoint read in `buildHistory`.
