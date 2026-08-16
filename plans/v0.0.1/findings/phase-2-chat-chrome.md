# v0.0.1 / Findings / Phase 2 - Chat chrome

> **Parent:** `plans/v0.0.1/findings/README.md` - evidence items 1, 5, 6, 8
> **Status:** implemented 2026-08-16 (gates green)
> **Estimated effort:** 2-3 days
> **Priority:** P1 (after findings phase 1; this is the visual product)
> **Gate:** the chat screen has a header, a compact status row, turn-styled transcript, collapsible tool cards, a multiline prompt, a small slash popover, and confirm as an overlay

---

## Overview

The current view is a data dump of the store: `user:` / `assistant:` / `reasoning:` prefixes, a hint novel, a one-line `textinput`, an 80% slash card, and a full-screen confirm wipe. Replace those surfaces with a designed chat layout.

## Executive Summary

- Header carries session title, model, and project cwd.
- Status is one row of facts, not key-binding prose.
- Transcript is turns, not prefixes. Reasoning starts collapsed. Tool cards start collapsed.
- Prompt is a `textarea`. `enter` sends, `shift+enter` newline.
- Slash is a small popover above the prompt, one command column, no nested borders.
- Confirm stays y/n deny-by-default but renders as an overlay. The `question` tool is a real option list.

## 2.1 Header bar (P1)

- [x] Replace `titleLine()` with `headerView()`: session title (or `new session`), current model, and project cwd basename
- [x] Header uses the same width as the terminal. Truncate the title; at 80 columns at most 2 rows
- [x] `transcriptRenderHeight` / chrome height updated so the prompt never clips
- [x] Test: `View()` contains the session title after replay - `TestTitleStatic`, exit 0
- [x] Test: at width 80 the header is at most 2 rows and the prompt is still visible - `TestHeaderFitsAt80`, exit 0

## 2.2 Compact status (P1)

- [x] Idle status is one row: model id (clickable). Removed `click model to switch`, `/ commands`, `enter to send`, `q to quit`, `models: N available`
- [x] Busy status is one row: `thinking` or the current tool name. No live tps number (hole for phase 4)
- [x] Error status stays red
- [x] Scroll / history hints render only while that mode is active
- [x] Test: idle `View()` at 80 columns does not contain `enter to send` or `q to quit` - `TestIdleStatusIsOneFactRow`, exit 0
- [x] Test: click-model still opens the picker - `TestModelPickerOpensOnlyFromModelClick`, exit 0

## 2.3 Turn layout (P1)

- [x] Stopped prefixing lines with `user:` / `assistant:` / `reasoning:` - structured `transcriptItem`s
- [x] User turn: `you` + text. Assistant turn: `assistant` + markdown
- [x] Reasoning collapsed by default (`▸ thinking`). `t` (empty prompt) expands. Replay restores collapsed
- [x] Step-start / step-finish stay out of the transcript
- [x] Test: fixture user + assistant + reasoning; `View()` has the texts and does not contain raw prefixes - `TestReplayNoNetwork`, exit 0
- [x] Test: reasoning body is absent while collapsed and present after expand - `TestReasoningToggle`, exit 0

## 2.4 Collapsible tool cards (P1)

- [x] Collapsed card is one header row: `tool  status  title-or-command`
- [x] New and in-flight cards start collapsed. `e` expands the last tool
- [x] Expanded card shows command and output
- [x] Replay builds collapsed cards from the store
- [x] Test: completed bash fixture; default `View()` has bash/completed and no output body; after expand, output is present - `TestBashCommandAndOutputRendered`, exit 0

## 2.5 Multiline prompt (P1)

- [x] Replaced `textinput` with Charm `textarea`. `enter` sends, `shift+enter` inserts a newline
- [x] Prompt height grows with content up to 6 rows. Transcript shrinks with the prompt
- [x] Placeholder is `ask lazyKoder`
- [x] Prompt undo, paste, history up/down, and double-esc clear still work
- [x] Test: `shift+enter` grows the value with a newline and does not submit - `TestShiftEnterDoesNotSubmit`, exit 0
- [x] Test: `enter` on `hello` still submits - `TestEmptyEnterIgnored` and submit tests green

## 2.6 Compact slash popover (P1)

- [x] Slash menu is a small card (max 60 cols) anchored above the prompt
- [x] One column of command names. Selected description is a footer line
- [x] No inner bordered input box
- [x] Filter, `↑`/`↓`, `enter`, `esc` behavior stays. `esc` still leaves `/`
- [x] Test: `View()` in slash mode at 80 columns has no description on the `/model` row - `TestSlashDescriptionNotOnNextCommand`, exit 0
- [x] Test: existing filter + `/new` + escape-leaves-slash tests stay green (`slash_test.go`)

## 2.7 Confirm overlay and question list (P1)

- [x] `rm` confirm renders as a centered overlay on the dimmed chat. Copy stays `Delete <subject> (<qualifier>)?` / `y confirm  •  n cancel`
- [x] Default remains deny. `n` / `esc` / `q` (overlay only) cancel. `ctrl+c` quits without executing
- [x] `question` tool uses an option list: highlight, `j`/`k` or numbers, `enter` selects
- [x] Parent README + `docs/tui.md` + `docs/safety.md` updated to overlay + separate question flow
- [x] Test: confirm allow/deny tests still pass; ask overlay keeps the chat underneath - `TestConfirmDeny`, `TestAskQuestion`, exit 0
- [x] Test: `TestAskQuestion` drives a two-option question via the list, not y/n-as-index-0/1 - exit 0

## 2.8 Picker wrap and scrollbar (P1, small)

- [x] Left rail keeps `MODEL` / `Selected` / current id. Dropped the wrapping paragraph
- [x] After `openPicker` with cursor 0, `GotoTop()` so the thumb is at the top (`ScrollPercent` 0)
- [x] Test: picker `View()` at 120 width does not contain a lone `for` line - `TestPickerHasNoOrphanFor`, exit 0
- [x] Test: `TestPickerArrowKeysRefreshSelectionAndScroll` asserts scroll percent 0 at cursor 0 - exit 0

## Dependencies

- Needs: findings phase 1
- Blocks: findings phase 3
- Must not: re-plan phase 4 tps/todos. Status leaves a hole
- New dependencies: none (`charm.land/bubbles/v2/textarea`)

## Closure gates

- [x] `go test ./internal/ui/chat ./internal/ui/confirm -count=1` exit 0 - 2026-08-16
- [x] `go test ./... -count=1` exit 0 - 2026-08-16
- [x] `go vet ./...` exit 0 - 2026-08-16
- [x] tmux 120x36 and 80x24: header + compact status + prompt visible; slash description is a footer; confirm overlay covered by tests - 2026-08-16
- [x] `docs/tui.md` and `docs/safety.md` match overlay confirm + textarea keys
