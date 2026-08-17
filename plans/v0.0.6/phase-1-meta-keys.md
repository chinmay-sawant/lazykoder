# v0.0.6 / Phase 1 - Meta keybindings (composer-safe)

> **Parent:** `plans/v0.0.6/README.md`
> **Status:** planned
> **Estimated effort:** 0.5-1 day
> **Priority:** P0 (empty-prompt `t` / `e` steal typing)
> **Gate:** with empty prompt, typing `test` inserts `test`; `ctrl+e` expands
> or collapses every tool card; `ctrl+p` expands or collapses every thinking
> block; click on a tool/thinking header does not toggle; `/help` and
> `docs/tui.md` match

---

## Overview

Today bare `t` / `e` only run when the composer is empty
(`keys.go` cases `'t'` / `'e'`). That means the first character of messages
like `test` or `edit this` never lands in the prompt. `ctrl+e` already
toggles the **last** tool only. Mouse click on a tool or thinking header
calls `toggleSelectedMeta`. This phase makes meta expand bulk-only and
frees letter keys for typing.

## Executive Summary

- Remove empty-prompt handlers for `t` and `e`.
- Remove click-to-toggle on `itemTool` / `itemReasoning` headers.
- `ctrl+e`: toggle **all** tool cards (open all or close all).
- `ctrl+p`: toggle **all** thinking blocks (open all or close all).
- Update help, footer hints if any, docs, and tests.

## Current code map (do not re-discover)

| Behavior | Location |
| --- | --- |
| Empty prompt `t` → last reasoning | `internal/ui/chat/keys.go` ~`case 't', 'T'` |
| Empty prompt `e` → last tool | `internal/ui/chat/keys.go` ~`case 'e', 'E'` |
| `ctrl+e` → `toggleLastTool` | `internal/ui/chat/keys.go` ctrl branch |
| Click header toggle | `internal/ui/chat/mouse.go` `itemTool` / `itemReasoning` |
| Toggle helpers | `internal/ui/chat/transcript.go` `toggleReasoning`, `toggleLastTool`, `toggleSelectedMeta` |
| Help rows | `internal/ui/chat/view.go` help table (`t / e`, `ctrl+e`) |
| Docs | `docs/tui.md` (mentions `t`, `e`, clicks) |
| Tests | `chat_test.go` (ctrl+e last tool), `mouse_test.go` (click expand) |

## 1.1 Remove bare `t` and `e` meta shortcuts (P0)

- [ ] Delete `case 't', 'T'` and `case 'e', 'E'` empty-prompt branches in
      `internal/ui/chat/keys.go` so those runes always reach the textarea
      (including when `prompt.Value() == ""`)
- [ ] Confirm no other path intercepts bare `t` / `e` for meta
      (grep `toggleReasoning` / `toggleLastTool` call sites)
- [ ] Test: model with empty prompt, key `t` then `e` then `s` → prompt
      value is `tes` (or full `test` sequence) -
      `TestBareTETypesIntoPrompt`, exit 0

## 1.2 `ctrl+e` toggles all tool cards (P0)

- [ ] Replace `toggleLastTool` usage on `ctrl+e` with a bulk helper, e.g.
      `toggleAllTools()` in `internal/ui/chat/transcript.go`
- [ ] Rule: if **any** `itemTool` has `collapsed == false`, set **all** tools
      to `collapsed = true`; else set **all** tools to `collapsed = false`
- [ ] Include edit cards in the same bulk set (they start open by default;
      bulk close must still collapse them)
- [ ] Call `syncTranscript` once after the bulk flip
- [ ] Keep `ctrl+e` working while the prompt has text (same as today)
- [ ] Sub-agent log focus: when `subagentLogMode`, `ctrl+e` applies to that
      log's tool items only (not the parent transcript). If log mode has no
      separate item list, document the chosen behavior in the test name
- [ ] Test: three tools mixed open/closed → `ctrl+e` closes all; again opens
      all - `TestCtrlETogglesAllTools`, exit 0
- [ ] Update or retire tests that expect last-tool-only `ctrl+e`
      (`chat_test.go` around edit collapse / re-open)

## 1.3 `ctrl+p` toggles all thinking blocks (P0)

- [ ] Add `ctrl+p` / `ctrl+P` in the ctrl branch of `updateKey` (before any
      path that would forward `p` to the prompt)
- [ ] Helper e.g. `toggleAllReasoning()`: if any `itemReasoning` is open,
      collapse all; else expand all
- [ ] Works with non-empty prompt (must not insert a character)
- [ ] Sub-agent log: same bulk rule on that surface when focused
- [ ] Test: two reasoning items collapsed → `ctrl+p` opens both; again
      closes both - `TestCtrlPTogglesAllThinking`, exit 0

## 1.4 Remove click-to-toggle on meta headers (P0)

- [ ] In `internal/ui/chat/mouse.go`, stop calling `toggleSelectedMeta` on
      left-click for `itemTool` / `itemReasoning`
- [ ] Click on those rows may still set selection / drag-select text like
      other transcript rows; it must **not** flip `collapsed`
- [ ] Remove or rewrite mouse tests that require click-expand
      (`mouse_test.go` tool/thinking click cases)
- [ ] Test: click tool header leaves `collapsed` unchanged -
      `TestClickToolHeaderNoToggle`, exit 0

## 1.5 Help, docs, and copy (P0)

- [ ] `/help` rows: remove `t / e`; set
      `ctrl+e` → `expand/collapse all tools`,
      `ctrl+p` → `expand/collapse all thinking`
- [ ] `docs/tui.md`: replace `t` / `e` / click-toggle language with
      `ctrl+e` / `ctrl+p` only
- [ ] Grep repo for `ctrl+e` / `` `e` expands`` / `` `t` expands`` in user-facing
      strings and fix leftovers (`docs/`, help, tips if any)
- [ ] Test: help View contains `ctrl+p` and `ctrl+e`, does not list bare
      `t / e` as meta shortcuts - extend existing help test or
      `TestHelpMetaKeys`, exit 0

## 1.6 Cleanup helpers (P1, same PR if small)

- [ ] Drop dead `toggleLastTool` / `toggleReasoning` / `toggleSelectedMeta`
      if no remaining callers; or keep thin wrappers only if tests still need
      them (prefer production bulk helpers only)
- [ ] `go test ./internal/ui/chat` exit 0
- [ ] `go test ./...` exit 0 before phase close

## Dependencies

None. Can land before or after phase 2.

## Closure

- [ ] Phase gate: `go test ./internal/ui/chat` and `go test ./...` exit 0
- [ ] Manual note (user TTY): empty prompt, type `test` and `edit` without
      accidental meta toggles; `ctrl+e` / `ctrl+p` bulk behavior
