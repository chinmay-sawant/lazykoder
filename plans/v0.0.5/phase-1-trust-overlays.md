# v0.0.5 / Phase 1 - Trust and overlays

> **Parent:** `plans/v0.0.5/README.md` - evidence items 4, 7, 8, 10, 11
> **Status:** implemented 2026-08-17 (gates green)
> **Estimated effort:** 2-3 days
> **Priority:** P0 (broken pickers, missing safety controls, `/help` lies)
> **Gate:** `@` can reach files; confirm/ask/`@` cards are opaque; every
> settings row is clickable; timeout / role / bash-confirm are on the card;
> `/help` lists `/settings`, `/new`, `/continue`, `/refresh`

---

## Overview

Fix the surfaces that mislead or block the user before any chrome polish.
Opaque cards, a reachable `@` list, truthful Settings and Help. No new
features. Expose fields `internal/settings` already stores.

## Executive Summary

- `@` / confirm / question cards have no background. Transcript bleeds through.
- `@` paints 12 rows and can select hidden ones. Agents bury files.
- Settings mouse handler stops after `max steps`. Footer still says `click`.
- Child timeout (600s), default role, and bash confirm live only in JSON.
- `/help` omits first-class slash commands.

## 1.1 Opaque overlay cards (P0)

- [x] `filePickerOverlay` in `internal/ui/chat/files.go` sets
      `Background(theme.ColorBg())` (same as help / resume / settings)
- [x] `confirmOverlay` in `internal/ui/chat/view.go` sets
      `Background(theme.ColorBg())` and `BorderForeground(theme.ColorDanger())`
      instead of ANSI `"1"`
- [x] `askOverlay` in `internal/ui/chat/view.go` sets
      `Background(theme.ColorBg())` and `BorderForeground(theme.ColorBorder())`
      instead of ANSI `"8"`
- [x] Confirm subject wraps to `overlayWidth()`; a long `rm` path does not
      clip off the terminal
- [x] Ask overlay drops the dummy header `question` when `q.Header` is empty
- [x] Test: View of confirm / ask / `@` contains no transcript text inside
      the card bounds (or a dedicated fill assertion) -
      `TestOverlayCardsOpaque`, exit 0

## 1.2 `@` picker is a file picker again (P0)

- [x] `filePickerOverlay` paints two mute section headers: `sub-agents` then
      `files`. Agents no longer consume the whole card
- [x] Viewport + scrollbar (same pattern as `pickerView`). Cursor cannot
      land on a row that is not painted. Remove the
      "paint 12, walk N" split (`maxAtPickerVisible` either scrolls or
      clamps the cursor)
- [x] Default open still lists files. When sub-agents exist they sit in
      their own section above files, each on one row
      (`agent  name  ◆ status`), not two wrapped lines
- [x] Files remain reachable with 12+ sub-agents without typing a filter
- [x] Test: fixture with 15 agents + `hello.go`; `@` View contains
      `hello.go` (or a files section the cursor can reach);
      cursor max equals visible rows - `TestAtPickerFilesReachable`, exit 0
- [x] Existing `TestFilePickerInsertsPath` and `TestFilePickerEsc` stay green

## 1.3 Settings mouse hits every row (P0)

- [x] `settingsHit` in `internal/ui/chat/settings.go` handles
      `settingsRowAgentsEnabled`, `settingsRowAgentsConcurrent`,
      `settingsRowAgentsChildSteps`, `settingsRowAllowlistEnabled`,
      `settingsRowAllowlist` (toggle / stepper / open editor)
- [x] Click on `[on]`/`[off]` toggles. Click on `◂`/`▸` steps. Click on
      the allowlist value enters edit mode
- [x] Footer `click` is true for every painted row
- [x] Test: click each row; state changes or editor opens -
      `TestSettingsHitAllRows`, exit 0

## 1.4 Settings: timeout, role, bash confirm (P0)

Backend already has these on `settings.Agents`. Card does not.

- [x] New row `child timeout` bound to `Agents.DefaultTimeoutSec`.
      Stepper (0 = off from settings; show `10m` at 600). Group: Sub-agents.
      Path: `internal/ui/chat/settings.go` + `settings.Save`
- [x] New row `default role` cycles `explore` / `plan` / `general`.
      Group: Sub-agents. `rebuildSubMgr` after change
- [x] New row `child bash confirms` cycles `parent` (`ask parent`) /
      `deny`. Group: Safety
- [x] Relabel allowlist to `parent bash allowlist`. Mute hint under it:
      `children are not filtered by this list`
- [x] Do not add `max_depth` (stored, unused at runtime)
- [x] Test: change timeout / role / confirm; `settings.json` matches;
      reload sees the same values - `TestSettingsTimeoutRoleConfirmPersist`,
      exit 0
- [x] Test: existing settings tests stay green
      (`go test ./internal/ui/chat -run Settings -count=1` exit 0)

## 1.5 `/help` teaches the product (P0)

- [x] `helpOverlay` in `internal/ui/chat/view.go` lists `/settings`,
      `/new`, `/continue`, `/refresh` next to the existing `/model` /
      `/variant` / `/agents` rows
- [x] `esc` row says cancel turn, and that twice clears the idle prompt
- [x] `ctrl+c` row says copy when the prompt has text, two-step quit when empty
- [x] Add rows that already exist in code: `ctrl+z` undo prompt,
      `ctrl+e` expand last tool with a non-empty prompt, drag / `c` copy
- [x] Title row gets `[x]`. `Padding(1, 2)` so `keys` is not glued to
      `enter`. Key column uses text/accent; action column stays mute
- [x] Test: `?` View contains `/settings` and `/continue` and does not
      grow `m.items` - extend `TestHelpOverlayDoesNotGrowTranscript`, exit 0
- [x] Test: at width 80 the help card does not collide with the header
      brand (no `lazykoder╭` splice) - `TestHelpFitsAt80`, exit 0

## Dependencies

- Needs: shipped settings persist path (`persistSettings`) and `@` picker
- Does not need: phase 2 slash chrome or footer chips
- New dependencies: none

## Closure gates

- [x] `go test ./internal/ui/chat ./internal/settings -count=1` exit 0 - 2026-08-17
- [x] `go vet ./internal/ui/chat ./internal/settings` exit 0 - 2026-08-17
- [x] tmux 167x48: `@` shows a files section; settings has timeout / role /
      confirm; click toggles sub-agents; `/help` lists `/settings`
      (`TestSettingsHitAllRows`, `TestAtPickerFilesReachable`, tmux captures)
- [x] Manual: tmux pty at 167x48 and 80x24; help card readable with `[x]`
      (`/tmp/lazykoder-ui-qa2/help.txt`, `compact-help.txt`)
