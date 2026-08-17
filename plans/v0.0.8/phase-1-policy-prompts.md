# v0.0.8 / Phase 1 - Policy and prompts

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** complete 2026-08-17
> **Estimated effort:** 1 day
> **Priority:** P0
> **Gate:** `internal/prompts` embeds `compact.md`; compact policy
> functions are pure and tested with no provider calls

## Overview

Land the summarizer prompt as an embedded file and the decision helpers
the later phases call. No Chat request, no SQLite write, no TUI.

## Phase 1: Policy and prompts

### 1.1 Prompt package

- [x] Add `internal/prompts` with `go:embed *.md`.
- [x] Add `compact.md` with the eight required headings from the README
      (intent, decisions, files, errors, pending, current, next step,
      all user messages) plus the handoff / language / no-invent-files
      rules.
- [x] `prompts.Must("compact.md")` returns the file text; missing name
      fails at init.
- [x] Test that the embedded file contains each required heading.
      `TestMustCompactContainsHeadings` and `TestMustUnknownPanics`.

### 1.2 Policy helpers

- [x] Add `internal/agent/compact.go` with:
      `EstimateTokens` (chars/4, same unit as the TUI),
      `NeedsCompact(estimate, window, buffer)`,
      `PickSummarizer(outgoing, incoming, estimate, reserve)`,
      prune of a message slice (keep last 2 user turns + `keep.tokens`
      tail; replace older tool bodies with a short placeholder).
- [x] Unknown window (`0`) means `NeedsCompact` is false.
      `TestNeedsCompactUnknownWindowSkips`.
- [x] `PickSummarizer` returns the outgoing model when the incoming
      window cannot hold `estimate + reserve` and outgoing is larger.
      `TestPickSummarizer1MTo256kUsesOutgoing`.

### 1.3 Validation gate

- [x] `go test ./internal/prompts ./internal/agent -count=1` covers:
      1M to 256k picks outgoing,
      256k to 1M does not need compact,
      unknown window skips,
      prune-enough-to-skip-LLM,
      `compact.md` headings present.
      exit 0
- [x] `go build ./...` passes. exit 0
- [x] No provider or SQLite in `compact_test.go` / `prompts_test.go`.

## Dependencies

- Catalog windows already on `modelscache.Info.Context`.
- Existing chars/4 estimate in `internal/ui/chat` (`estimateCharsPerToken`).
