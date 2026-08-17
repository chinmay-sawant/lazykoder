# v0.0.8 / Phase 4 - Model-switch hook and /compact

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** planned
> **Estimated effort:** 1 day
> **Priority:** P0
> **Gate:** shrinking the live model sets a pending-compact hint; the
> next send runs the phase 3 path; `/compact` works under budget;
> settings expose `compaction.auto` / `buffer` / `keep_tokens`

## Overview

Wire the TUI and settings to the agent backend. Compact on the **next
send**, not on picker click.

## Phase 4: Model switch and slash

### 4.1 Picker shrink flag

- [ ] `selectPickerItem` compares `tokensUsed` to
      `ContextOf(new model) - buffer`.
- [ ] Overflow sets `pendingCompactReason = "model-shrink"` and a
      composer hint `next send will compact (window X -> Y)`.
- [ ] Larger or unknown window clears the flag.
- [ ] `m.session.Model` is updated in memory when the picker persists
      (today only SQLite is written).
- [ ] A busy-turn switch stays cosmetic; the in-flight agent is not
      rebuilt. The flag is consumed on the next user turn.

### 4.2 Slash and status

- [ ] `/compact` runs Layer 0+1 now, even under budget. Trailing text
      is appended as compact instructions.
- [ ] Help text lists `/compact`.
- [ ] While a compact call is in flight, `promptStatusValue` shows
      `compacting`.
- [ ] After success, the transcript can show a divider / notice on the
      compaction part. Full human history remains painted.

### 4.3 Settings

- [ ] `.lazykoder/settings.json` gains:

      ```json
      "compaction": { "auto": true, "buffer": 20000, "keep_tokens": 15000 }
      ```

- [ ] Defaults apply when the block is missing.
- [ ] `auto` gates preflight only. `/compact` and the single overflow
      retry remain available when `auto` is false.
- [ ] Settings tests cover load/save/default.

### 4.4 Validation gate

- [ ] `go test ./internal/ui/chat ./internal/settings ./internal/agent -count=1`
      covers picker shrink hint, `/compact` slash, settings defaults.
- [ ] `go build ./...` passes.

## Dependencies

- Phase 3 compact turn and events.
- Existing picker persist path (`picker.go` `selectPickerItem` /
  `persistSelection`).
