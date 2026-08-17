# v0.0.5 / Phase 3 - Polish and compact terminals

> **Parent:** `plans/v0.0.5/README.md` - evidence items 1, 5, 6, 9, 12
> **Status:** implemented 2026-08-17 (gates green)
> **Estimated effort:** 1-2 days
> **Priority:** P2 (after phase 2; daily driver already usable)
> **Gate:** resume rows are distinguishable; model rows show window/price;
> tool cards have a header/body split; one overlay recipe; 80x24 is readable

---

## Overview

Finish the surfaces that already work so they feel like one product.
Compact geometry is a first-class gate, not an afterthought.

## Executive Summary

- Resume titles are truncated first prompts. Duplicates look identical.
- Model rows hide context window and price until after you pick.
- Non-edit tools look like leaked stdout.
- Overlay families disagree on padding, fill, `[x]`, and ANSI vs theme.
- 80x24 is unusable until todos, slash, tips, and help share compact rules.

## 3.1 Resume list (P2)

- [x] One blank row after `RUNS` in `sessionPickerView`
      (`internal/ui/chat/sessions.go`)
- [x] Selected row uses the same filled wash as hover
- [x] Empty list keeps the 80% frame and a centered
      `no sessions yet  ·  esc back` (not a content-hugged island)
- [x] Duplicate titles are distinguishable: add message count, or a
      one-line preview, or a stronger time column
- [x] Optional `/` filter inside the card (same interaction as `/model`)
      once there are more than ~20 sessions
- [x] Current session marked; `[x]` close matches settings
- [x] Test: empty picker is still 80% tall -
      `TestResumeEmptyKeepsFrame`, exit 0
- [x] Existing session picker tests stay green

## 3.2 Model and variant drawers (P2)

- [x] Unselected model rows use `theme.Mute`, not `Faint(true)`.
      Selected row is a full-width wash. `free` is a small good/accent tag
- [x] Mute metadata on the right or under the id: context window and a
      short price (`1050k · $x/M`) from `models.json` / `modelscache`
- [x] Variant footer drops `r refresh`. Header hint:
      `sent as reasoning_effort`
- [x] One word for provider default across settings, picker, and footer
      (`default` or `none`, not both)
- [x] Test: variant View does not contain `r refresh` -
      `TestVariantFooterNoRefresh`, exit 0
- [x] Existing picker tests stay green

## 3.3 Tool cards and transcript (P2)

- [x] Non-edit collapsed tools stay one row. Expanded body is indented
      or separated by a blank line plus a mute `output` rule. Not raw
      stdout under `◆ bash` (`internal/ui/chat/transcript.go`)
- [x] User-nav tooltip dwell drops from ~10s to ~2s
      (`userNavTipDuration` in `internal/ui/chat/usernav.go`)
- [x] Role labels (`you` / `assistant`) are stronger than the clock
- [x] Header: drop cwd when it equals the brand. Long titles wrap to a
      second header row instead of eating the middle
      (`headerView` in `internal/ui/chat/view.go`)
- [x] Sub-agent log: one blank row under
      `SUB-AGENT  ·  name  ·  status` so it does not sit on `you`
- [x] Test: expanded bash View has a header/body split, not only a
      `$` dump - extend `TestBashCommandAndOutputRendered`, exit 0

## 3.4 One overlay recipe (P2)

Shared rule for every card and drawer (document in `docs/tui.md`):

- [x] Opaque `theme.Bg` fill
- [x] `theme.Border` (danger only for confirm)
- [x] `Padding(1, 2)`
- [x] Title + `[x]` on modal cards
- [x] Mute footer
- [x] No raw ANSI `"1"` / `"8"` / `"15"` on confirm, ask, slash, or
      picker selection. Use `theme.*`
- [x] Drawers (slash, model, variant, agents) stay above the prompt.
      Modals (`/help`, `/settings`, `/resume`, sub-agent log) stay
      centered or full-screen. Do not mix both styles for the same job
- [x] Grep `lipgloss.Color("1")` / `Color("8")` / `Color("15")` in
      `internal/ui/chat` is empty after the sweep

## 3.5 Compact terminal 80x24 (P2)

Depends on phase 2 collapsed todos and compact slash.

- [x] At 80x24: todos one row, slash descriptions in the footer,
      rotating tips hidden or in the left footer only
- [x] Help is two columns or grouped so it does not splice into
      `lazykoder╭── keys`
- [x] Settings footer does not clip to `esc/…`
- [x] Footer may drop tps / cache before it drops model + tokens
- [x] Test: View at 80x24 contains the composer and at least 6
      transcript rows when todos exist -
      `TestCompact80x24KeepsTranscript`, exit 0
- [x] tmux 80x24: help readable, `@` lists files, settings footer intact,
      tip does not overwrite a transcript line

## 3.6 Docs lockstep (P2)

- [x] `docs/tui.md`: help is whatever the code paints (not "two columns"
      unless phase 3.5 shipped two columns). Busy line blank-row copy
      matches `liveStatusView`. Settings table matches the card
- [x] `docs/safety.md` describes the parent allowlist and child
      `bash_confirm`. It does not say there is no allow-list
- [x] `docs/tips.md` stays in sync with `internal/tips/tips.go`
- [x] `docs/plans.md` already points at this folder (parent README)

## Dependencies

- Needs: phase 2 collapsed todos, grouped slash, settings sections
- Does not need: new tools or providers
- New dependencies: none

## Closure gates

- [x] `go test ./internal/ui/chat -count=1` exit 0 - 2026-08-17
- [x] `go test ./... -count=1` exit 0 - 2026-08-17
- [x] `go vet ./...` exit 0 - 2026-08-17
- [x] tmux 167x48 and 80x24: resume / model / help / settings / `@`
      share padding, fill, and close controls
      (`TestResumeEmptyKeepsFrame`, `TestHelpFitsAt80`, tmux captures)
- [x] `docs/tui.md` and `docs/safety.md` match the running keys and rows
- [x] Manual: tmux full-screen walk of help, settings, slash, `@`, compact
      (`lazykoder-ui-qa` killed after captures)
