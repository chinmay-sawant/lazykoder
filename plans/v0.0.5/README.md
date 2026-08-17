# v0.0.5 - TUI layout and settings UX

> **Parent:** `plans/v0.0.1/findings/` (2026-08-16 chrome) and
> `plans/v0.0.2/` (settings + agents surface)
> **Status:** implemented 2026-08-17 (automated gates green; tmux 167x48 and 80x24 checked)
> **Estimated effort:** 5-8 days across three phases
> **Priority:** P0 for phase 1 (broken overlays and missing safety controls);
> P1 for phase 2; P2 for phase 3
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Source:** live tmux inspection of `go run .` on 2026-08-17 in dedicated
> session `lazykoder-ui-qa` at 167x48 (user fullscreen) and 80x24, plus
> matching code in `internal/ui/chat`, `internal/settings`, `internal/tips`
> **Gate:** every menu and slash command is readable at 167x48 and 80x24;
> settings expose the safety and timeout fields the backend already stores;
> `/help` lists the real commands; `@` can still reach files when sub-agents exist

---

## Overview

v0.0.1 findings shipped a real chat surface. v0.0.2 shipped sub-agents and a
settings card. A 2026-08-17 fullscreen walk of every menu showed the chrome
is now fighting the transcript, several overlays have no fill, Settings hides
fields the backend already persists, and `/help` does not teach the product.

This folder is the live ledger for that review. It is not a second copy of
`plans/v0.0.1/findings/` (those rows stay `[x]`). Do not re-diagnose the
2026-08-16 defects from memory.

Inspected on a real pty (not a pipe). Headless `go run` cannot verify this TUI.

## Ratings

Ratings are product-feel scores, not checklist rows. They do not close a
phase. Recorded from the 2026-08-17 tmux walk (before) and the
implementation gates (after).

| Lens | Before (2026-08-17) | After (2026-08-17 ship) |
| --- | --- | --- |
| Overall product | 6.0 / 10 | 8.0 / 10 |
| TUI / layout | 5.5 / 10 | 8.0 / 10 |
| Settings completeness | 4.0 / 10 | 8.5 / 10 |
| Discoverability | 5.0 / 10 | 7.5 / 10 |
| Compact 80x24 | 3.5 / 10 | 7.0 / 10 |

Before notes: chat loop, resume card, model search, and edit diffs already
work. P0 holes (`@` burying files, transparent overlays, settings mouse
dead after max steps, missing timeout/role/confirm, `/help` omitting
`/settings`) keep it from being a daily driver. 2026-08-16 findings scored
the pre-chrome app 3.5 / 10 as a coding chat and 3 / 10 as a TUI; the
before column is the same app after that chrome shipped.

After notes: Settings is a real control panel (timeout, role, confirm,
queue, explore model). `/help` and slash teach the product. Todos collapse.
`@` has sections and a viewport. 80x24 keeps a composer and a one-line
todo strip. Remaining 2 points: `@` still leads with agents so files are
below the fold; compact footer still truncates the model id.

Phase files (live ledgers; mark `[x]` only after the gate passes):

| File | Priority | Goal |
| --- | --- | --- |
| [phase-1-trust-overlays.md](phase-1-trust-overlays.md) | P0 | Opaque cards, `@` viewport, settings mouse + timeout/role/confirm, `/help` rewrite |
| [phase-2-chrome-settings.md](phase-2-chrome-settings.md) | P1 | Slash palette, collapsible todos, footer chips, settings card redesign |
| [phase-3-polish-compact.md](phase-3-polish-compact.md) | P2 | Resume search, model metadata, tool cards, one overlay recipe, 80x24 |

## Evidence (2026-08-17, do not re-diagnose from memory)

Confirmed against the running binary on the last resumed session at 167x48
and again at 80x24. Did not send a turn, did not persist settings, did not
run `/new`, `/continue`, or `/refresh`. Confirm and question overlays were
reviewed from code only.

1. Header reads `lazykoder · <truncated first prompt> · lazykoder`. Brand and
   cwd basename are the same word.
2. Todo strip (`todos · 0/3 · in progress` plus 3 rows) is permanently open
   under the header. At 80x24 it leaves almost no transcript.
3. Slash menu (`/`) is 9 faint ungrouped rows, no title, no border.
   `/settings` still says `project settings (model, steps)`.
4. `/help` is a mute one-column card titled `keys`. It lists `/model`,
   `/variant`, `/agents` and omits `/settings`, `/new`, `/continue`, `/refresh`.
   At 80x24 it collides with the header brand.
5. `/resume` is the strongest screen (80% card, age groups). Titles are
   truncated first prompts; many rows look identical.
6. `/model` drawer works (header, list, search). Unselected rows are faint.
   `/variant` footer still advertises `r refresh`. Settings says `default`;
   the picker says `none`; the footer shows `xhigh`.
7. `/settings` is a short centered island in a 48-row terminal. Nine flat
   rows, no sections. `allowed executables` was blank. Mouse clicks after
   `max steps` only move the cursor. Footer still says `click`.
8. Backend already stores `default_timeout_sec` (600), `default_role`,
   `bash_confirm`, `explore_model`, `model_override`, `max_queued`,
   `allow_parallel_writers`. None of those appear on the card. This session's
   child log header was `layout-review · timed_out`.
9. `/agents` drawer has no border. Activity on the right is truncated JSON.
   Auto-open on first `task` steals ~8 transcript rows.
10. `@` opened as `@ files & sub-agents` and showed 12 agent rows (name plus
    status wrapping onto a second line). Files were `… 60 more`. Cursor can
    walk items that are not painted (`maxAtPickerVisible = 12`).
11. `@`, confirm, and question cards have no `Background(theme.ColorBg())`.
    Transcript shows through. Help / resume / settings already paint a fill.
12. At 80x24 the rotating tip overwrote the last assistant line
    (`▼Tip: /variant sets the reasoning effort`).
13. Footer is one mute soup:
    `gpt-5.6-luna  xhigh  22k/1050k  hit 283k 92%  miss 24k 8%  $0.0079  subs:12`.
    Model is clickable but does not look clickable. Variant has no click target.

## What already works (do not regress)

- Work rail on the live assistant turn
- User-prompt left curl (`frameUserPrompt`)
- `/resume` age groups (`just now` / `recently` / `older`)
- Edit-diff green/red washes
- Composer pinned to the bottom
- `/model` drawer search (`/model flash`)
- Deny-by-default y/n confirm copy

## Out of scope

- New providers, themes, or a `/theme` command (phase-3 polish: one palette)
- `/compact`, `/export`, `/init`, `/cost`, `/tips`, `/clear`, `/undo` as
  conversation rewind (no backend, or they would over-promise)
- Editable `agents.max_depth` until the manager actually honors nesting
- Re-opening any `[x]` row in `plans/v0.0.1/findings/`
- Mermaid visualizer (`plans/v0.0.3/`) and schema work (`plans/v0.0.4/`)

## Rules that stay

- `Update` stays deterministic. Side effects in `tea.Cmd`.
- No new third-party dependencies without explicit user sign-off (`AGENTS.md`).
- Prefer existing Charm widgets over hand-rolled chrome.
- `internal/ui/chat/chat.go` must not cross ~2,000 lines. Split by
  responsibility if a slice would push it.
- Never run the binary headless to claim a TUI gate. Use
  `go test ./internal/ui/chat` plus tmux capture (`skills/tmux-debug/SKILL.md`)
  at 167x48 (or 120x36) and 80x24.
- No em dashes in docs, UI copy, or commit messages.
- Mark `[x]` only when the gate passed. Record the command and exit code.
  `[~]` needs a reason and a pointer.
- Do not persist settings or create sessions while capturing.

## Dependencies

- Phase 1 unblocks phase 2 (opaque cards and a truthful settings/help
  surface are the base the chrome rewrite sits on).
- Phase 2 unblocks phase 3 (compact rules assume collapsed todos and a
  grouped slash list).
- Settings rows added in phase 1-2 must call the existing
  `settings.Save` / `persistSettings` path. No second settings file.

## Closure gates (whole v0.0.5 folder)

- [x] `go test ./internal/ui/chat ./internal/settings -count=1` exit 0 - 2026-08-17
- [x] `go test ./... -count=1` exit 0 - 2026-08-17
- [x] `go vet ./...` exit 0 - 2026-08-17
- [x] tmux 167x48: `@` lists files when sub-agents exist; settings timeout /
      role / confirm rows visible; `/help` lists `/settings` and `/continue`
      (`lazykoder-ui-qa`, `/tmp/lazykoder-ui-qa2/{help,settings,at2}.txt`)
- [x] tmux 80x24: todos collapsed to one row; tip does not overwrite
      transcript; help does not collide with the header brand
      (`/tmp/lazykoder-ui-qa2/{compact,compact-help}.txt`)
- [x] `docs/tui.md` matches the new settings rows, help keys, and slash copy
- [x] `docs/safety.md` no longer says there is no allow-list
