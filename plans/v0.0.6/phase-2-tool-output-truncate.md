# v0.0.6 / Phase 2 - Truncate expanded tool output in the UI

> **Parent:** `plans/v0.0.6/README.md` - evidence: single bash up to 2,229
> lines / 124 KB; sessions with ~7k–10k tool-output lines if fully expanded
> **Status:** planned
> **Estimated effort:** 1 day
> **Priority:** P0 (expand paint is unbounded)
> **Gate:** expanded non-edit tool body never paints more than the configured
> line budget; truncation note is visible; collapse still one row; sub-agent
> log uses the same helper

---

## Overview

`renderTool` already collapses non-edit tools to one header row. When open,
it paints the full `Output` string with no line cap. Write is preview-capped
at 400 runes; bash/read/grep are not. Combined with bulk `ctrl+e` (phase 1),
expanding **all** tools without a UI budget would intentionally dump
thousands of lines into the viewport string.

This phase caps **display only**. Agent `maxToolOutput` (8000 runes to the
model) stays unless a later plan changes agent budgets. DB rows may still
hold large historical output; the TUI must not paint them raw.

## Evidence driving the caps

| Fact | Value |
| --- | --- |
| Worst bash lines (this DB) | 2,229 |
| Worst bash bytes | 124,752 |
| Worst session tool lines if all open | ~9,874 |
| Typical agent model cap | 8,000 runes (`maxToolOutput`) |
| Write UI preview today | 400 runes |

Suggested starting constants (tune in one place, e.g. `transcript.go`):

| Constant | Start | Role |
| --- | ---: | --- |
| `maxToolBodyLines` | 100 | Max painted lines for expanded non-edit output |
| `maxToolBodyRunes` | 6,000 | Hard rune budget (belt and suspenders) |
| Head / tail split | 70 / 30 lines | Prefer head; keep tail for exit errors |
| Truncation footer | one mute line | e.g. `… 2129 lines omitted · showing first 70 + last 30` |

Edit diffs keep their own path (`renderEditTool` / existing diff caps). Do
not fold edit into the bash-style head/tail unless a test proves edit dumps
are also huge.

## Executive Summary

- Shared helper truncates multi-line tool output for paint.
- Head + tail when over budget; always show a mute omission line.
- Main transcript and sub-agent log share the helper.
- Tests with synthetic 2k-line bash output.

## 2.1 Paint-time truncate helper (P0)

- [ ] Add something like `truncateToolOutputForView(s string) (body string, omitted bool)`
      in `internal/ui/chat/transcript.go` (or a small `tool_view.go` if
      `transcript.go` is near the file-size limit)
- [ ] Apply line cap first, then rune cap
- [ ] Head+tail join with a clear middle marker line when omitted
- [ ] Unit tests on pure helper: short string unchanged; 200 lines → 100 +
      note; rune-only oversize truncated - `TestTruncateToolOutputForView`,
      exit 0

## 2.2 Wire into `renderTool` (P0)

- [ ] Expanded `default` branch (bash and other non-write tools): pass
      output through the helper before `toolOutputStyle.Render`
- [ ] Write path: either keep 400-rune preview or route through the same
      helper for consistency (pick one; document in test)
- [ ] When `metadata` / stored JSON already has `"truncated": true`, still
      apply UI cap (double truncation is fine; note can say `truncated`)
- [ ] Collapsed path unchanged (header only)
- [ ] Test: expanded bash with 500-line output; View/rendered body line
      count under budget and contains omission marker -
      `TestExpandedBashOutputCapped`, exit 0
- [ ] Extend `TestBashCommandAndOutputRendered` so short outputs still show
      full body and `$ command`

## 2.3 Sub-agent log parity (P0)

- [ ] `renderSubagentLogContent` / shared `renderTool` path must not bypass
      the cap (prefer one code path)
- [ ] Test: sub-agent log model with huge bash tool; rendered content
      respects the same line budget -
      `TestSubagentLogToolOutputCapped`, exit 0

## 2.4 Bulk expand safety with phase 1 (P0)

- [ ] After `ctrl+e` opens all tools, total transcript string size for a
      fixture of N tools × large output stays O(N × budget), not O(N × full)
- [ ] Optional lightweight test: 20 tools × 2000-line outputs; after expand
      all, joined content length below a fixed ceiling derived from constants
      - `TestBulkExpandRespectsUIBudget`, exit 0

## 2.5 Docs (P1)

- [ ] `docs/tui.md`: note that expanded tool output is preview-capped
      (head/tail) and that `ctrl+e` expands all cards under that cap
- [ ] Do not claim the full DB blob is always visible in the TUI

## Dependencies

- Works alone; pairs with phase 1 bulk expand (without this, phase 1
  `ctrl+e` can make slowness worse).
- Phase 3 should land after this so render benchmarks use capped bodies.

## Closure

- [ ] `go test ./internal/ui/chat` exit 0
- [ ] `go test ./...` exit 0
- [ ] Constants live in one place; no magic numbers scattered in render
