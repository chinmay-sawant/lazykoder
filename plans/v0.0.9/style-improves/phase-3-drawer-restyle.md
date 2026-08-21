# v0.0.11 Phase 3 - Unified drawer restyle

> **Parent:** `plans/v0.0.11/README.md` - reference `docs/tui.md`, `internal/ui/theme/theme.go`
> **Status:** planned
> **Estimated effort:** M (2 to 3 sessions: 1 shared-chrome extraction, 1 per-drawer pass, 1 gates)

---

## Overview

The lower chrome in `internal/ui/chat/view.go` stacks up to four drawers above
the composer: the slash palette (`slashView`), the model picker
(`pickerView` in `picker.go`), the sub-agent drawer (`subagentDrawerView` in
`subagents.go`), and the status drawer (`statusDrawerView` in `status.go`).
Settings (`settingsCardView`) renders as an overlay card.

Each drawer grew its own chrome: different header treatments, hint rows,
cursor glyphs (`▸`), selection highlights (bold text on Border background),
and padding. They share `pickerDrawerWidth()` but not a common frame. The
result reads as four separate widgets instead of one system.

This phase extracts one shared drawer frame and applies it everywhere. It is
a pure restyle plus small layout fixes: every feature, key binding, row, and
mouse target survives. Nothing is removed.

## Executive Summary

Add a single `drawerChrome` helper (new file `internal/ui/chat/drawer.go`)
that renders title, count/status line, body rows, and a hint bar with one
lipgloss style set derived from `internal/ui/theme`: Accent `#d4a0c7` for
focus and titles, Text `#eceae6` for selected rows, Mute `#8a8680` for hints,
Border `#2a2a2a` for rules and card edges on `Bg #000000`. Convert the four
drawers to it one at a time, keeping golden-style View tests green by updating
expectations in the same commit as each conversion. Mouse hit-testing rects
(`statusIndexAtScreenY`, `subagentIndexAtScreenY`, `pickerDrawerTop`,
`statusDrawerTop`) must stay in sync with any row-height changes, so each
conversion includes a mouse-map check.

## Phase 1: Shared chrome

### 1.1 Extract the frame

- [x] Create `internal/ui/chat/drawer.go` with `drawerChrome`: params are title, right-aligned meta, body lines, hint string, width; output is one bordered block with consistent 1-cell horizontal padding and a top rule. Path: new file. Proof: unit test renders a sample at 80 cols; borders align, no clipped runes.
- [x] Define cursor and selection styles once: unfocused row = Mute, focused row = Text bold with Accent left rail (replaces per-drawer bold-on-Border where it clashes), hint bar = Mute with `·` separators matching today's `↑/↓ select  ·  enter toggle  ·  ←/esc close` pattern. Path: `drawer.go`. Proof: golden test strings committed alongside.
- [x] Keep `pickerDrawerWidth()` as the single width source so all drawers still align. Path: `internal/ui/chat/picker.go`. Proof: test asserts equal rendered widths across all four drawers at several terminal sizes.

### 1.2 Layout safety

- [x] Preserve the stacking order and spacing in `view.go` (`slashView`, `pickerView`, `subagentDrawerView`, `statusDrawerView`, error row, composer) and the pad-to-bottom logic. Path: `internal/ui/chat/view.go`. Proof: existing `layout_test.go` and `layout_v005_test.go` pass unchanged.
- [x] For each drawer, recompute its `*Top()` / `*IndexAtScreenY()` helpers from the new frame heights in the same change that alters height. Path: `status.go`, `subagents.go`, `view.go`. Proof: mouse tests in `mouse_test.go` pass after each conversion, not just at the end.

## Phase 2: Per-drawer conversion

### 2.1 Status drawer

- [x] Rebuild `statusDrawerView` on `drawerChrome`: header becomes title "status" + enabled count as right meta; rows keep label left, value + on/off right; hint bar keeps `enter toggle` semantics. Path: `internal/ui/chat/status.go`. Proof: `go test ./internal/ui/chat -run TestStatus -count=1` green after expectation update.
- [x] Verify segment toggling and persistence unchanged. Path: `status.go` (`toggleStatusSegment`). Proof: state round-trip test.

### 2.2 Sub-agent drawer

- [x] Rebuild `subagentDrawerView` header (counts line) and rows on the shared frame; keep diamonds, live activity lines, and right-side usage exactly as data, only restyled containers. Path: `internal/ui/chat/subagents.go`. Proof: `subagentDrawerCounts` output unchanged; row content tests green.
- [x] Keep the log screen (`subagentLogScreen`) out of the frame conversion except its title bar and jump bar, which adopt the same styles. Path: `subagents.go`. Proof: log interaction tests pass untouched.
- [x] File-size guard: `subagents.go` is already 1,398 lines; if the conversion pushes it toward 2,000, split module-wise first (for example `subagents_view.go` for render-only funcs). Path: `subagents.go`. Proof: `wc -l` recorded before and after.

### 2.3 Model picker and slash palette

- [x] Apply the frame to `pickerView` (title from picker kind, filter line as meta) and `slashView` (title "commands", description as meta). Path: `internal/ui/chat/picker.go`, slash view file. Proof: picker filter tests and slash tests green.
- [x] Settings overlay card adopts the same border/title/hint styles while keeping its paint-line layout. Path: `internal/ui/chat/settings.go`. Proof: settings tests green; visual check via View strings.

## Phase 3: Gates

- [x] `go vet ./...` clean; `go test ./... -count=1` green. Proof: exit codes captured in session log.
- [x] Full-screen render check at 80x24, 100x30, 120x40 using View-output tests: no clipped lines, drawers aligned to one width, readable contrast. Proof: test output or captured frames referenced here.
- [x] User runs `make run` and confirms the look; agent does not iterate on aesthetics without this check. Proof: user confirmation noted in plan.
- [x] Update `knowledge-base/02-architecture/component-map.md` and `04-workflows/adding-a-view.md` to mention `drawer.go` as the required frame for new drawers, in the same session as the merge.

## Dependencies

- Independent of phases 1 and 2 code-wise; can land first, but sequencing after them avoids restyling twice if huh forms change drawer contents.
- No new dependencies; lipgloss only.

## Risks

- Row-height changes silently breaking mouse hit maps; mitigated by converting top/index helpers in the same change.
- Golden-test churn: expectations update in the same commit as each conversion, never batched blind.
- Aesthetics loops: one style proposal, one user review, then done.

## Out of scope

- Removing any drawer, row, key binding, or mouse target.
- Changing what data drawers show (that is phases 1 and 2).
