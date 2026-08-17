# v0.0.8 / Phase 2 - History prune and checkpoint read

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** planned
> **Estimated effort:** 1 day
> **Priority:** P0
> **Gate:** `buildHistory` can start at a compaction part and can
> placeholder old tool results without deleting SQLite rows

## Overview

Teach the agent loop to build a smaller request from stored data. Still
no LLM compact turn. Writers of compaction parts land in phase 3; this
phase only reads them and applies request-time prune.

## Phase 2: History and checkpoint

### 2.1 Request-time prune

- [ ] `buildHistory` (or a helper it calls) replaces tool outputs older
      than the last 2 user turns / outside `keep.tokens` with a short
      placeholder in the **provider request only**.
- [ ] SQLite tool rows stay intact. `Message.Visible` is not used.
- [ ] Test: a long tool-heavy history sends placeholders for the head
      and full bodies for the tail.

### 2.2 Checkpoint read

- [ ] Latest `parts.type = "compaction"` is the start of model context.
- [ ] That part is rendered as a historical user message that says the
      text is a checkpoint, not new instructions.
- [ ] Messages after the checkpoint (and the keep-tail if encoded)
      follow as today.
- [ ] Test: mixed-model session with a compaction part in the middle
      omits the prefix from the provider payload.

### 2.3 Token meter

- [ ] After a compact event, `tokensUsed` may decrease to the estimate
      of summary + tail. Today's high-water mark must not pin the old
      peak.
- [ ] Test updates `TestTokensDoNotResetOnSmallerUsage` so the
      high-water rule still applies to ordinary step-finish usage, and
      a dedicated compact-reset test covers the new path.

### 2.4 Validation gate

- [ ] `go test ./internal/agent ./internal/ui/chat -count=1` passes.
- [ ] `go build ./...` passes.

## Dependencies

- Phase 1 policy helpers and `parts.type` string already used for
  `step-start` / `text` / `tool` / `step-finish`.
