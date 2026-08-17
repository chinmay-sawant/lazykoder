# v0.0.8 / Phase 3 - LLM compact turn

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** planned
> **Estimated effort:** 1-2 days
> **Priority:** P0
> **Gate:** when preflight says the live window is too small, the agent
> asks the chosen summarizer (tools off) with `compact.md`, persists a
> compaction part, and retries the step from the checkpoint

## Overview

This is the first provider call that is not a normal chat turn. It must
not advertise tools, must pick the summarizer via `PickSummarizer`, and
must leave the human transcript intact.

## Phase 3: LLM compact

### 3.1 Summarizer call

- [ ] Preflight in `runSteps` uses `NeedsCompact` against
      `ContextOf(live model)` (passed in from the TUI / options).
- [ ] Summarizer request: `compact.md` + serialized head + previous
      summary if any; tools disabled; ~4096 output cap if the client
      can express it.
- [ ] Persist `agent = "compaction"` plus `parts.type = "compaction"`
      with summary text and the JSON envelope (`tail_start_message_id`,
      `from_model`, `to_model`, `reason`).
- [ ] Emit TUI events (`EventCompacting`, `EventCompacted`).

### 3.2 Replay and overflow

- [ ] Auto path replays the last real user text (or a synthetic
      continue pointed at that request) so the next model turn is not
      aimed at the summary.
- [ ] Manual compact (wired in phase 4) stops after the checkpoint.
- [ ] Provider errors that mean overflow (`context_length_exceeded`,
      "maximum context", "prompt is too long") trigger **one**
      compact+retry. A second overflow is returned as an error.
- [ ] If the summarizer request cannot fit even after prune/chunking,
      do not send the overflowing user turn. Return a readable error.

### 3.3 Validation gate

- [ ] Agent tests with a fake client: under-budget send is unchanged;
      over-budget send issues a tools-off compact call then a normal
      call from the checkpoint; overflow error retries once.
- [ ] Shrink fixture: 400k estimate, incoming 256k, outgoing 1M uses
      outgoing for the compact call and incoming for the follow-up.
- [ ] `go test ./internal/agent -count=1` passes.
- [ ] `go build ./...` passes.

## Dependencies

- Phase 1 `PickSummarizer` / `NeedsCompact` / `compact.md`.
- Phase 2 checkpoint read in `buildHistory`.
