# v0.0.8 / Phase 4 - Model-switch hook and /compact

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** complete 2026-08-17
> **Estimated effort:** 1 day
> **Priority:** P0
> **Gate:** shrinking the live model sets a pending-compact hint; the
> next send runs the phase 3 path; `/compact` works under budget;
> settings expose `compaction.auto` / `percent` / `keep_tokens`

## Overview

Wire the TUI and settings to the agent backend. Compact on the **next
send**, not on picker click.

## Phase 4: Model switch and slash

### 4.1 Picker shrink flag

- [x] `selectPickerItem` compares `tokensUsed` with
      `NeedsCompact(..., EffectiveCompaction().Percent)`.
- [x] Overflow sets `pendingCompactReason = "model-shrink"` and a
      composer hint `next send will compact (window X -> Y)`.
      `TestModelShrinkSetsCompactHint`.
- [x] Larger or unknown window clears the flag.
      `TestLargerWindowClearsCompactHint`,
      `TestUnknownWindowSkipsShrinkHint`.
- [x] `m.session.Model` is updated in memory (`syncSessionModel`).
- [x] A busy-turn switch stays cosmetic; the in-flight agent is not
      rebuilt. The flag is consumed on the next user turn (`submit`
      copies it into Options then clears the hint).

### 4.2 Slash and status

- [x] `/compact` runs Layer 0+1 now, even under budget. Trailing text
      is appended as compact instructions.
      `TestCompactSubmitParsesNotes`, `TestManualCompactStopsAfterCheckpoint`.
- [x] Help text lists `/compact`. `TestHelpListsCompact`.
- [x] While a compact call is in flight, `promptStatusValue` shows
      `compacting`. `TestPromptStatusShowsCompacting`.
- [x] After success, the transcript shows a divider / notice on the
      compaction part. Full human history remains painted.
      `TestCompactEventResetsTokensUsed`.

### 4.3 Settings

- [x] `.lazykoder/settings.json` gains `compaction.auto` / `percent` /
      `keep_tokens`. `/settings` edits auto and percent. `keep_tokens`
      is JSON-only.
- [x] Defaults apply when the block is missing.
- [x] `auto` gates same-model percent preflight only. `/compact`, shrink,
      and the single overflow retry remain available when `auto` is false.
- [x] Settings tests cover load/save/default.
      `TestCompactionLoadSaveAndMissingBlock`, `TestDefault`.

### 4.4 Validation gate

- [x] `go test ./internal/ui/chat ./internal/settings ./internal/agent -count=1`
      covers picker shrink hint, `/compact` slash, settings defaults.
      exit 0
- [x] `go build ./...` passes. exit 0

## Errata (as shipped)

There is no `buffer` key. Trigger is `used > window * percent / 100`.

## Dependencies

- Phase 3 compact turn and events.
- Existing picker persist path (`picker.go` `selectPickerItem` /
  `persistSelection`).
