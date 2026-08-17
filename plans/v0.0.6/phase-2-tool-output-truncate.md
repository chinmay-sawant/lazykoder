# v0.0.6 / Phase 2 - Truncate expanded tool output in the UI

> **Parent:** `plans/v0.0.6/README.md` - evidence: single bash up to 2,229
> lines / 124 KB; sessions with ~7k–10k tool-output lines if fully expanded
> **Status:** complete 2026-08-17
> **Estimated effort:** 1 day
> **Priority:** P0 (expand paint is unbounded)
> **Gate:** on the **main** transcript, expanded non-edit tool bodies never
> paint more than the configured line budget; truncation note is visible;
> collapse still one row. **Sub-agent log** keeps full stored tool bodies
> (no main-transcript line budget).

---

## Overview

`renderTool` already collapses non-edit tools to one header row. When open,
it paints the full `Output` string with no line cap. Write is preview-capped
at 400 runes; bash/read/grep are not. Combined with bulk `ctrl+e` (phase 1),
expanding **all** tools on a fat parent session without a UI budget dumps
thousands of lines into the viewport string.

This phase caps **display only on the parent/main transcript**. Agent
`maxToolOutput` (8000 runes to the model) and `@agent` mention caps stay
unless a later plan changes agent budgets. DB rows are never rewritten.

**Product choice (2026-08-17):** the full-screen **sub-agent log** is an
audit surface. Users open it to inspect the child end-to-end. Do **not**
apply the main-transcript line budget there. Performance for huge child
logs is handled by collapse defaults + phase 3 memo work, not by hiding
child tool output in the log view.

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

- Shared helper truncates multi-line tool output for **main transcript** paint.
- Head + tail when over budget; always show a mute omission line.
- Sub-agent log reuses `renderTool` only with an explicit "full body" mode
  (or a flag / separate call site) so it does **not** apply the line budget.
- Tests with synthetic 2k-line bash output on the main transcript.

## 2.1 Paint-time truncate helper (P0)

- [x] Add `truncateToolOutputForView(s string) (body string, omitted bool)`
      in `internal/ui/chat/transcript.go` (or a small `tool_view.go` if
      `transcript.go` is near the file-size limit)
- [x] Apply line cap first, then rune cap
- [x] Head+tail join with a clear middle marker line when omitted
- [x] Unit tests on pure helper: short string unchanged; 200 lines → 100 +
      note; rune-only oversize truncated - `TestTruncateToolOutputForView`,
      exit 0

## 2.2 Wire into main-transcript `renderTool` (P0)

- [x] Expanded `default` branch (bash and other non-write tools) on the
      **main** chat model: pass output through the helper before
      `toolOutputStyle.Render`
- [x] Write path keeps the 400-rune main-transcript preview; the explicit
      full-body mode is used by the audit log
      helper for consistency (pick one; document in test)
- [x] When `metadata` / stored JSON already has `"truncated": true`, still
      apply UI cap on the main transcript (double truncation is fine)
- [x] Collapsed path unchanged (header only)
- [x] `renderItemWithToolMode` and `renderToolMode` provide an explicit
      full-body paint mode
      so sub-agent log can call the same renderer with full bodies
- [x] Test: expanded bash with 500-line output on main transcript; body
      line count under budget and contains omission marker -
      `TestExpandedBashOutputCapped`, exit 0
- [x] Extend `TestBashCommandAndOutputRendered` so short outputs still show
      full body and `$ command`

## 2.3 Sub-agent log: full length (P0)

- [x] `renderSubagentLogContent` paints expanded tool bodies **without**
      `maxToolBodyLines` / `maxToolBodyRunes` (full stored `Output`)
- [x] Thinking / assistant text in the log stay as stored (no new cap)
- [x] Collapse defaults still apply (tools start collapsed) so open cost is
      opt-in via `ctrl+e` on that surface
- [x] Test: sub-agent log with a 500-line bash tool expanded contains the
      full body (or far more than the main budget), not the main omission
      marker - `TestSubagentLogToolOutputFull`, exit 0
- [x] Document in code comment: log is audit UI; main transcript is the
      performance-sensitive surface

## 2.4 Bulk expand safety with phase 1 (P0)

- [x] After `ctrl+e` opens all tools on the **main** transcript, total
      content size for a fixture of N tools × large output stays
      O(N × budget), not O(N × full)
- [x] Test: 20 tools × 2000-line outputs; after expand
      all on main transcript, joined content length below a fixed ceiling
      derived from constants - `TestBulkExpandRespectsUIBudget`, exit 0
- [x] Sub-agent log bulk `ctrl+e` may still open full bodies; acceptable
      for a single focused child. If a child log is extreme, phase 3 memo
      is the mitigation, not silent truncation

## 2.5 Docs (P1)

- [x] `docs/tui.md`: note that expanded tool output on the **main** chat is
      preview-capped (head/tail); **sub-agent log** shows full stored tool
      output when expanded; `ctrl+e` / `ctrl+p` apply on the focused surface
- [x] Do not claim the parent LLM receives the full child log (it does not;
      see README sub-agent table)

## Dependencies

- Works alone; pairs with phase 1 bulk expand (without this, phase 1
  `ctrl+e` can make slowness worse).
- Phase 3 should land after this so render benchmarks use capped bodies.

## Closure

- [x] `go test ./internal/ui/chat -count=1` exit 0 - 2026-08-17
- [x] `go test ./... -count=1` exit 0 - final gate below
- [x] Constants live in one place; no magic body limits are scattered in
      render
