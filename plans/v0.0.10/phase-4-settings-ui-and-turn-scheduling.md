# v0.0.10 / Phase 4 - Settings UI and turn scheduling

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1-2 days
> **Priority:** P1
> **Gate:** users can configure recaps in `/settings`, and a successful parent
> turn queues one silent worker without changing normal chat behavior.

## Overview

Wire the persisted setting to the settings card and attach recap worker and
lookup services to the main chat runtime. Scheduling must run after the turn
has persisted. It cannot reuse the turn context that `finishTurn` cancels.

## Executive summary

The card gets two rows in a `recaps` section. The setting applies to future
worker jobs and first-request lookups. No recap activity appears in the normal
sub-agent drawer, task listings, transcript, status line, or usage totals.

## Phase 4: User configuration and background scheduling

### 4.1 Add the recap section to `/settings`

- [ ] Add non-focusable `recaps` plus focusable `recaps enabled` and
      `recap model` rows in `internal/ui/chat/settings.go`.
- [ ] Wire both rows through painted rows, label lookup, keyboard selection,
      left/right adjustment, mouse hit testing, scrolling, and
      `persistSettings`. Update row-count constants and tests in the same
      change.
- [ ] Toggle `recaps enabled` with the standard boolean control. Cycle
      `recap model` through cached IDs with the existing model-choice helper.
      An empty catalog still displays and preserves `deepseek-v4-flash`.
- [ ] Test keyboard and mouse behavior, reload persistence, row-hit coverage,
      and card geometry at 120x36 and 80x24.

### 4.2 Schedule safely after a parent turn

- [ ] Attach recap manager and lookup implementation with the chat Store,
      client, workdir, current settings, and catalog profiles. Refresh them on
      recap-setting or catalog changes without rebuilding normal sub-agents.
- [ ] At successful `chat.finishTurn`, use a fresh background context to
      reserve and enqueue one recap candidate. Do not schedule from streamed
      parts, event handlers, or a context that `finishTurn` cancels.
- [ ] Disabled recaps, a missing session, fewer than four source entries, a
      duplicate reservation, and failed or cancelled parent turns are quiet
      no-ops.
- [ ] A record receives the recap model at reservation time. A later
      `/model` switch does not affect it. A later recap setting applies to
      later jobs and the next parent `Send` lookup.
- [ ] Disabling recaps cancels queued jobs and prevents later lookup. A
      finished artifact set remains local memory. No action changes ordinary
      sub-agent jobs or settings.
- [ ] Add an integration test that completes a main turn, then proves one
      background recap is reserved and no normal sub-agent job, child session,
      drawer row, transcript item, usage value, or visible error was added.

## Dependencies

- Phases 1 through 3
- Existing `chat.finishTurn` parent-turn lifecycle

## Closure gate

- [ ] `go test ./internal/ui/chat ./internal/settings ./internal/recap -count=1`
      exits 0.
