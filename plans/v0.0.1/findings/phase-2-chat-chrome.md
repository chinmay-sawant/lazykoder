# v0.0.1 / Findings / Phase 2 - Chat chrome

> **Parent:** `plans/v0.0.1/findings/README.md` - evidence items 1, 5, 6, 8
> **Status:** planned 2026-08-16 (no rows landed; mark `[x]` only when the gate passes)
> **Estimated effort:** 2-3 days
> **Priority:** P1 (after findings phase 1; this is the visual product)
> **Gate:** the chat screen has a header, a compact status row, turn-styled transcript, collapsible tool cards, a multiline prompt, a small slash popover, and confirm as an overlay

---

## Overview

The current view is a data dump of the store: `user:` / `assistant:` / `reasoning:` prefixes, a hint novel, a one-line `textinput`, an 80% slash card, and a full-screen confirm wipe. Replace those surfaces with a designed chat layout. Do not add features from phase 3 (palette sweep, `@` picker, help overlay) here except as needed to keep the screen readable.

## Executive Summary

- Header carries session title, model, and project cwd. The static `lazykoder` word does no work (`TestTitleStatic` will change).
- Status is one row of facts, not key-binding prose. Hints move to `?` or `/help` (help *overlay* is findings phase 3; this phase only stops dumping hints into the status).
- Transcript is turns, not prefixes. Reasoning starts collapsed. Tool cards start collapsed.
- Prompt is a `textarea` (Charm bubbles), `enter` sends, `shift+enter` newline.
- Slash is a small popover above the prompt, one command column, no nested borders.
- Confirm stays y/n deny-by-default but renders as an overlay on the chat. The `question` tool gets a real option list instead of `Delete <question>?`.

This evolves the parent plan's "full view switch" confirm spec. The safety copy and key bindings for `rm` stay.

## 2.1 Header bar (P1)

- [ ] Replace `titleLine()` (`internal/ui/chat/view.go`) with a one-row (or two-row on narrow) header: session title (or `new session`), current model, and the project cwd basename or shortened path
- [ ] Header uses the same width as the terminal, no wrapping into the transcript at 80 columns. Truncate the title with an ellipsis before wrapping the bar
- [ ] `transcriptRenderHeight` / `titleBlockRows` / `chromeLines` updated so the prompt never clips
- [ ] Test: `View()` contains the session title after replay, not only the word `lazykoder` - update `TestTitleStatic`
- [ ] Test: at width 80 the header is at most 2 rows and the prompt is still visible - chat view test, exit 0

## 2.2 Compact status (P1)

- [ ] Idle status is one row: model id (clickable, existing `modelStatusRect`), optional token/cost placeholder, nothing else. Remove `click model to switch`, `/ commands`, `enter to send`, `q to quit`, `models: N available` from the default idle line
- [ ] Busy status is one row: `sending` until findings 1.4 lands, then the current tool name or `thinking`. Do not invent a live tps number here; leave a segment hole for [../phase-4-tokens-status-todos.md](../phase-4-tokens-status-todos.md) 4.3
- [ ] Error status stays red and still one wrapped block, not mixed with hints
- [ ] Scroll / history hints render only while that mode is active (existing history and overflow branches), still not as a permanent novel
- [ ] Test: idle `View()` at 80 columns has a single status row that does not contain `enter to send` or `q to quit` - exit 0
- [ ] Test: click-model still opens the picker (`TestModelPickerOpensOnlyFromModelClick` or successor)

## 2.3 Turn layout (P1)

- [ ] Stop prefixing lines with `user:` / `assistant:` / `reasoning:` (`internal/ui/chat/transcript.go` `renderUserLine`, `renderAssistantLine`, `renderReasoningLine`)
- [ ] User turn: muted role label or right/left treatment plus the text. Assistant turn: role label plus markdown body (existing `internal/ui/markdown`)
- [ ] Reasoning is collapsed by default (`▸ thinking` or similar). A documented key (e.g. `t` on a selected turn, or click) expands the stored reasoning text. Replay restores collapsed
- [ ] Step-start / step-finish stay out of the transcript (already omitted). Do not start rendering them
- [ ] Test: fixture user + assistant + reasoning; `View()` has the texts and does not contain the raw `user:` / `assistant:` / `reasoning:` prefixes - exit 0
- [ ] Test: reasoning body is absent while collapsed and present after the expand key - exit 0

## 2.4 Collapsible tool cards (P1)

Evidence: current `renderTool` always prints header + `$ command` + full `output`. Real sessions already store 55 bash calls, including heredoc `cat > main.go` titles.

- [ ] Collapsed card is one header row: `tool  status  title-or-command` (truncate the command). No output body
- [ ] New and in-flight cards start collapsed. Completed cards stay collapsed until expanded (key or click on that card)
- [ ] Expanded card shows command and output using the existing inner box, still width-clamped
- [ ] Replay builds collapsed cards from the store
- [ ] Test: completed bash fixture; default `View()` contains `bash` and `completed` and does not contain the full output body; after expand, output is present - exit 0
- [ ] Test: `TestBashCommandAndOutputRendered` updated to expand (or to assert the collapsed header plus an explicit expand step)

## 2.5 Multiline prompt (P1)

- [ ] Replace `textinput` with Charm `textarea` (already in the bubbles module; no new dependency). `enter` sends, `shift+enter` inserts a newline. Empty / whitespace-only still ignored
- [ ] Prompt height grows with content up to a small cap (e.g. 6 rows) and then scrolls. `transcriptRenderHeight` shrinks with the prompt
- [ ] Placeholder is one short line (`ask lazykoder` or similar). Drop `(type / for commands)` from the placeholder; slash still opens on `/`
- [ ] Existing prompt undo, paste, history up/down, and double-esc clear still work on the textarea
- [ ] Test: `shift+enter` (or the v2 equivalent) grows the value with a newline and does not submit - exit 0
- [ ] Test: `enter` on `hello` still submits (`TestEmptyEnterIgnored` and submit tests stay green)

## 2.6 Compact slash popover (P1)

Evidence: 80% card, nested input box, description wrap colliding with `/model` at 80x24.

- [ ] Slash menu is a small card anchored above the prompt, width min(60, terminal-4) or similar, not `overlayWidth()` 80%
- [ ] One column of command names. Selected description is a single footer line under the list, not a right pane that wraps into the next row
- [ ] No inner bordered input box. The chat prompt remains the query; the card only lists matches
- [ ] Filter, `↑`/`↓`, `enter`, `esc` behavior stays. `esc` still leaves `/` in the prompt (documented)
- [ ] Test: `View()` in slash mode at 80 columns has no description text sitting on the `/model` row - exit 0
- [ ] Test: existing filter + `/new` + escape-leaves-slash tests stay green (`slash_test.go`)

## 2.7 Confirm overlay and question list (P1)

This changes `plans/v0.0.1/README.md` "full view switch" and `docs/tui.md`. Safety rules do not change.

- [ ] `rm` confirm renders as a centered overlay on top of the dimmed chat, not a full-view replace. Copy stays `Delete <subject> (<qualifier>)?` / `y confirm  •  n cancel`. Keys still isolated (`TestConfirmModeKeyIsolation`)
- [ ] Default remains deny. `n` / `esc` / `q` (only in this overlay) cancel. `ctrl+c` quits without executing
- [ ] `question` tool no longer uses `confirm.New(question, header)` as a yes/no `Delete`. New option list: highlight the question, list options, `j`/`k` or numbers, `enter` selects. Fewer than two options still errors as today
- [ ] Parent README + `docs/tui.md` + `docs/safety.md` updated to say overlay, not full-view wipe. Question flow documented separately from `rm`
- [ ] Test: confirm allow/deny tests still pass; `View()` during confirm still contains the chat transcript underneath or a dimmed frame, plus the overlay copy - exit 0
- [ ] Test: `TestAskQuestion` drives a two-option question via the list, not y/n-as-index-0/1 - exit 0

## 2.8 Picker wrap and scrollbar (P1, small)

Evidence: left copy wraps `for` alone at 120x36; thumb near bottom while cursor is on the first model.

- [ ] Left rail description uses a width that does not orphan short words. If it cannot fit, drop the paragraph and keep `MODEL` / `Selected` / current id
- [ ] Scrollbar thumb matches `pickerVp.ScrollPercent()` against the visible cursor. After `openPicker` with cursor on the current model at index 0, thumb is at the top
- [ ] Test: picker `View()` at 120 width does not contain a lone `for` line - exit 0
- [ ] Test: existing `TestPickerArrowKeysRefreshSelectionAndScroll` still green; add an assertion that at cursor 0 the thumb row is the first track row

## Dependencies

- Needs: findings phase 1 (real session title, live tool statuses, `q` not stolen from the prompt)
- Blocks: findings phase 3 (diffs, palette, help overlay land on these components)
- Must not: re-plan phase 4 tps/todos. Status leaves a hole
- New dependencies: none (use `charm.land/bubbles/v2/textarea`)

## Closure gates

- [ ] `go test ./internal/ui/chat ./internal/ui/confirm -count=1` exit 0 - pending
- [ ] `go test ./... -count=1` exit 0 - pending
- [ ] `go vet ./...` exit 0 - pending
- [ ] tmux 120x36 and 80x24: header + one status row + prompt visible; slash description does not collide; confirm overlay does not wipe the chat - pending
- [ ] `docs/tui.md` and `docs/safety.md` match overlay confirm + textarea keys - pending
