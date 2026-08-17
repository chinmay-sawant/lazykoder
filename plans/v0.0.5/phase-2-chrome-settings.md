# v0.0.5 / Phase 2 - Chrome and settings card

> **Parent:** `plans/v0.0.5/README.md` - evidence items 1, 2, 3, 7, 8, 9, 12, 13
> **Status:** implemented 2026-08-17 (gates green)
> **Estimated effort:** 2-3 days
> **Priority:** P1 (after phase 1; this is the daily-driver chrome)
> **Gate:** slash is a grouped palette; todos collapse to one row; footer
> model/variant look clickable; settings is a sectioned 80% card that
> exposes the remaining stored fields

---

## Overview

Make the standing chrome match `/resume` quality. Slash, todos, footer, and
the rest of Settings. Phase 1 already made Settings truthful for safety;
this phase makes it scannable and complete.

## Executive Summary

- Slash is faint autocomplete. Copy `/model` drawer chrome and group commands.
- Todos steal 4+ rows on every screen. Collapse by default.
- Footer is a mute soup. Model is clickable but invisible as a control.
- Settings is 9 unlabeled rows in a content-hugged island.
- Tips overwrite the transcript at 80 columns.

## 2.1 Slash command palette (P1)

- [x] `slashView` in `internal/ui/chat/slash.go` gets a header
      (`commands  ·  <selected name>`), mute descriptions (not `Faint(true)`),
      and a selected-row wash. No nested border
- [x] Group headings (not focusable): Session, Model, Project, Help
- [x] Session: `/new`, `/resume` (description mentions `ctrl+s` and
      `/session`), `/continue`
- [x] Model: `/model`, `/variant` (include `max`), `/refresh`
- [x] Project: `/agents` (mention `/subs`), `/settings`
      (`project defaults (model, steps, agents, safety)`)
- [x] Help: `/help` (mention `?`). Optional alias `/keys` that runs
      `runSlash("/help")`
- [x] Stop advertising `/slot` in copy. Keep the alias working
- [x] Do not add `/theme`, `/compact`, `/export`, `/init`, `/cost`,
      `/tips`, `/clear`, or conversation `/undo`
- [x] Under width ~100, descriptions move to a one-line footer (original
      phase-2 slash spec) so 9 rows do not eat the transcript
- [x] Test: `/` View contains `/settings` description with `agents` or
      `safety`; selected row is not faint -
      extend `TestSlashDescriptionNotOnNextCommand`, exit 0
- [x] Existing slash filter + `/new` + escape-leaves-slash tests stay green

## 2.2 Collapsible todo strip (P1)

- [x] `todoPanelView` in `internal/ui/chat/todos.go` defaults to one row:
      `todos  ·  d/t  ·  in progress` (or idle). Click or a dedicated key
      expands to at most `maxTodoPanelRows`
- [x] Expanded state is session-local (not a settings field unless it is
      cheap). Collapse again on the same toggle
- [x] Never steal more than 2 rows unless the user opens the list
- [x] Test: 3 todos; default View has the summary and not all 3 bodies;
      after toggle, bodies appear - `TestTodoPanelCollapsedByDefault`, exit 0
- [x] Existing todo render / replay tests stay green

## 2.3 Footer chips and tips (P1)

- [x] `composerFooter` / `fitFooterRight` in `internal/ui/chat/view.go`:
      model renders as a chip (`model ▾`). Click still opens `/model`
- [x] Variant is its own chip (`xhigh` / `default`). Click opens `/variant`
- [x] Token / cache / cost / tps stay mute. `subs:N` uses accent when
      live > 0
- [x] Rotating `Tip:` leaves the alert row. Cycle in the left footer, or
      only inside `/help`. At 80x24 a tip must not overwrite transcript
      text (evidence item 12)
- [x] `liveStatusView` gets one blank row above it (match `docs/tui.md`).
      While that strip is visible, `composerLeft` does not also say
      `working · esc cancel`
- [x] Test: idle View at 80 columns has no `Tip:` overlapping a transcript
      line; model chip still clickable -
      `TestTipDoesNotOverwriteTranscript`, exit 0
- [x] Test: existing footer / model-click tests stay green

## 2.4 Settings card: sections and remaining fields (P1)

- [x] `settingsCardView` uses a min height (same 80% family as
      `sessionCardHeightPct`) so a 48-row terminal is not a tiny island
- [x] Mute section headers, not focusable: `model`, `agent loop`,
      `sub-agents`, `safety`. Blank row after `SETTINGS`
- [x] Relabel `default model` / `default variant` to
      `new-session model` / `new-session variant`. Mute hint:
      `live /model and /variant do not change these defaults`
- [x] When step limit is off, dim `max steps` and do not show
      `89 (off)` as if it were the live budget
- [x] Add rows (backend already stores them):
      - `child model override` (`Agents.ModelOverride`, empty = inherit)
      - `explore model` (`Agents.ExploreModel`, empty = inherit)
      - `max queued` (`Agents.MaxQueued`, stepper, >= max concurrent)
      - `parallel writers` (`Agents.AllowParallelWriters`, toggle, last
        in Sub-agents)
- [x] `allowed executables` is a chip list or a trimmed count
      (`11 allowed`) plus enter-to-edit. Persist still uses
      `settings.Save`. Trim on each keystroke
- [x] Edit mode footer swaps to `enter save  ·  esc cancel`
- [x] `docs/tui.md` settings table lists every painted row. Clamp copy
      matches code (max steps 1-1000, child default 1000), not 1-128 / 32
- [x] Test: all new rows persist and reload -
      `TestSettingsExploreQueueOverridePersist`, exit 0
- [x] Test: card height at height 48 is not content-hugged to ~12 rows -
      `TestSettingsCardMinHeight`, exit 0

## 2.5 Agents drawer chrome (P1)

- [x] `subagentDrawerView` matches `/model`: header, list, footer. Status
      is a word on the left (`timed_out` / `completed`), not a JSON snippet
      on the right. Activity prefers `tool  title`
- [x] Auto-open on first `task` (`openSubagentDrawerIfNew`) no longer
      steals 8 transcript rows by default. Flash `subs:1/1` in the footer
      instead. `/agents` and click `subs:N` still open the drawer
- [x] Test: first spawn does not open the drawer; `subs:` chip is present -
      `TestSubagentDrawerDoesNotAutoOpen`, exit 0
- [x] Existing agents drawer / log tests stay green

## 2.6 Empty state and discoverability (P1)

- [x] Empty transcript copy uses one name (`new session`, not `new run`)
      and mentions `?` and `/settings`:
      `/ commands   @ files   ? help   /settings`
- [x] Center the empty state in the transcript pane
- [x] Add a tip for `/agents` in `internal/tips/tips.go` and
      `docs/tips.md` (keep the two in sync)
- [x] Test: extend `TestEmptyStateShown` for the new copy, exit 0

## Dependencies

- Needs: phase 1 settings rows and help rewrite
- Does not need: phase 3 resume search or compact-only rules
- New dependencies: none

## Closure gates

- [x] `go test ./internal/ui/chat ./internal/tips ./internal/settings -count=1` exit 0 - 2026-08-17
- [x] `go vet ./internal/ui/chat ./internal/tips ./internal/settings` exit 0 - 2026-08-17
- [x] tmux 167x48: slash grouped; settings sectioned at ~80% height;
      todos one row until opened; footer model/variant look like chips
      (`/tmp/lazykoder-ui-qa2/{slash,settings}.txt`)
- [x] `docs/tui.md` and `docs/tips.md` match the painted copy
