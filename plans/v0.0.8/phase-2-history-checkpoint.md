# v0.0.8 / Phase 2 - History prune and checkpoint read

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** complete 2026-08-17
> **Estimated effort:** 1 day
> **Priority:** P0
> **Gate:** `buildHistory` can start at a compaction part and can
> placeholder old tool results without deleting SQLite rows

## Overview

Teach the agent loop to build a smaller request from stored data.

## Phase 2: History and checkpoint

### 2.1 Request-time prune

- [x] `buildHistory` replaces tool outputs older than the last 2 user
      turns / outside `keep.tokens` with a short placeholder in the
      **provider request only**.
- [x] SQLite tool rows stay intact. `Message.Visible` is not used.
- [x] Test: `TestBuildHistoryPrunesOldToolBodies` sends placeholders
      for the head and full bodies for the tail.

### 2.2 Checkpoint read

- [x] Latest `parts.type = "compaction"` is the start of model context.
- [x] That part is rendered as a historical user message that says the
      text is a checkpoint, not new instructions.
- [x] Messages after the checkpoint (and the keep-tail if encoded)
      follow as today.
- [x] Test: `TestBuildHistoryStartsAtCheckpoint` mixed-model session
      omits the prefix from the provider payload.

### 2.3 Token meter

- [x] After a compact event, `tokensUsed` may decrease to the estimate
      of summary + tail.
- [x] Live fill follows the latest request (`TestTokensFollowLatestRequestSize`).
      Empty usage blobs do not wipe the meter.
      `TestCompactEventResetsTokensUsed` and
      `TestReplayAfterCompactUsesCompactFill` cover the compact path.

### 2.4 Validation gate

- [x] `go test ./internal/agent ./internal/ui/chat -count=1` passes.
      exit 0
- [x] `go build ./...` passes. exit 0

## Dependencies

- Phase 1 policy helpers and `parts.type` string already used for
  `step-start` / `text` / `tool` / `step-finish`.
