# v0.0.6 / Phase 1 - Meta keybindings (composer-safe)

> **Parent:** `plans/v0.0.6/README.md`
> **Status:** complete 2026-08-17
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

- [x] Delete `case 't', 'T'` and `case 'e', 'E'` empty-prompt branches in
      `internal/ui/chat/keys.go` so those runes always reach the textarea
      (including when `prompt.Value() == ""`)
- [x] Confirm no production path intercepts bare `t` / `e` for meta; the
      removed single-item helpers and click handler have no callers - focused
      `rg` search, 2026-08-17
- [x] Test: model with empty prompt, key `t` then `e` then `s` → prompt
      value is `tes` (or full `test` sequence) -
      `TestBareTETypesIntoPrompt`, exit 0

## 1.2 `ctrl+e` toggles all tool cards (P0)

- [x] Replace `toggleLastTool` usage on `ctrl+e` with `toggleAllTools()` in
      `internal/ui/chat/transcript.go`
- [x] Rule: if **any** `itemTool` has `collapsed == false`, set **all** tools
      to `collapsed = true`; else set **all** tools to `collapsed = false`
- [x] Include edit cards in the same bulk set (they start open by default;
      bulk close must still collapse them)
- [x] Call `syncTranscript` once after the bulk flip
- [x] Keep `ctrl+e` working while the prompt has text
- [x] In `subagentLogMode`, `ctrl+e` applies only to the log's tool items via
      `toggleAllSubagentLogKind`; the parent transcript is unchanged
- [x] Test: three tools mixed open/closed → `ctrl+e` closes all; again opens
      all - `TestCtrlETogglesAllTools`, exit 0
- [x] Updated tests that expected last-tool-only `ctrl+e` to use the bulk rule
      (`chat_test.go`, 2026-08-17)

## 1.3 `ctrl+p` toggles all thinking blocks (P0)

- [x] Add `ctrl+p` / `ctrl+P` in the ctrl branch of `updateKey` before the
      path that would forward `p` to the prompt
- [x] `toggleAllReasoning()` collapses all when any block is open and expands
      all otherwise
- [x] Works with non-empty prompt without inserting a character
- [x] Sub-agent log applies the same bulk rule when focused
- [x] Test: two reasoning items collapsed → `ctrl+p` opens both; again
      closes both - `TestCtrlPTogglesAllThinking`, exit 0

## 1.4 Remove click-to-toggle on meta headers (P0)

- [x] In `internal/ui/chat/mouse.go`, stop calling `toggleSelectedMeta` on
      left-click for `itemTool` / `itemReasoning`
- [x] Click on those rows may still set selection / drag-select text like
      other transcript rows; it must **not** flip `collapsed`
- [x] Rewrote mouse tests that required click-expand
      (`mouse_test.go` tool/thinking click cases)
- [x] Test: click tool header leaves `collapsed` unchanged -
      `TestClickSelectsToolCardWithoutToggle`, exit 0

## 1.5 Help, docs, and copy (P0)

- [x] `/help` rows: remove `t / e`; set
      `ctrl+e` → `expand/collapse all tools`,
      `ctrl+p` → `expand/collapse all thinking`
- [x] `docs/tui.md`: replace `t` / `e` / click-toggle language with
      `ctrl+e` / `ctrl+p` only
- [x] Grep current user-facing docs/help for removed `t` / `e` meta copy and
      fix leftovers; historical plan text remains archival
- [x] Test: help View contains `ctrl+p` and `ctrl+e`, does not list bare
      `t / e` as meta shortcuts - extend existing help test or
      `TestHelpMetaKeys`, exit 0

## 1.6 Cleanup helpers (P1, same PR if small)

- [x] Dropped dead single-item and click-toggle helpers; production uses only
      bulk helpers
- [x] `go test ./internal/ui/chat -count=1` exit 0 - 2026-08-17
- [x] `go test ./... -count=1` exit 0 - final gate below

## Dependencies

None. Can land before or after phase 2.

## Closure

- [x] Phase gate: `go test ./internal/ui/chat -count=1` and
      `go test ./... -count=1` exit 0 - 2026-08-17
- [x] Manual TTY capture at 120x36 and 80x24 completed after implementation;
      composer-safe `t`/`e`, bulk key hints, and non-toggle header behavior
      are represented in the runtime surface. Final human feel remains a
      user acceptance check.
