# v0.0.10 / Phase 3 - Settings UI and turn scheduling

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1-2 days
> **Priority:** P1
> **Gate:** users can configure recaps in `/settings`, and a completed main
> turn queues one hidden worker without changing normal chat behavior.

## Overview

Wire the persisted settings to the existing settings card and attach the
recap manager to the main chat runtime. The scheduling point must run after
the turn has fully persisted. It cannot reuse the cancelled turn context.

## Executive summary

The card gets two rows under a `recaps` section. The setting applies to
future jobs immediately. No job appears in the normal sub-agent drawer, in
task listings, in the transcript, or in usage totals.

## Phase 3: User configuration and background scheduling

### 3.1 Add the recap section to `/settings`

- [ ] Add a non-focusable `recaps` section and focusable `recaps enabled` and
      `recap model` rows in `internal/ui/chat/settings.go`.
- [ ] Wire both rows through the existing painted-row table, label lookup,
      keyboard selection, left/right adjustment, mouse hit testing, scrolling,
      and `persistSettings` paths. Update any row-count constants and tests.
- [ ] Toggle `recaps enabled` with the standard boolean control. Cycle
      `recap model` through cached model IDs using the existing settings model
      choice helper. An empty catalog must still display and preserve the
      normalized `deepseek-v4-flash` default.
- [ ] Update `/settings` help text and `docs/tui.md` during implementation so
      every painted row has user-facing documentation.
- [ ] Test keyboard and mouse behavior, reload persistence, row hit coverage,
      and card geometry at 120x36 and 80x24.

### 3.2 Schedule after a completed parent turn

- [ ] Construct and attach the recap manager with the chat's Store, client,
      workdir, and catalog profiles. Rebuild it when recap settings or model
      profiles change, without disturbing the normal sub-agent manager.
- [ ] At the successful end of `chat.finishTurn`, use a fresh background
      context to ask the recap manager for one candidate. Do not start from
      streamed parts, apply-event handlers, or a context that `finishTurn`
      has cancelled.
- [ ] Reserve the durable record before enqueueing. A duplicate reservation,
      fewer than four source messages, disabled recaps, a missing session, or
      a failed/cancelled parent turn is a quiet no-op.
- [ ] Give a job the snapshot's configured model at reservation time. A later
      `/model` change affects neither that job nor its endpoint. A later
      recap-setting change applies to jobs reserved after the change.
- [ ] Disabling recaps cancels queued work and prevents future scheduling.
      A job that already finished remains as local memory. No toggle changes
      ordinary sub-agent settings or jobs.
- [ ] Keep recap cost, events, activity strings, and errors out of the normal
      transcript and sub-agent drawer. The durable record is the diagnostic
      trail for this version.
- [ ] Add an integration test that completes a main turn, then proves one
      background recap is reserved and no normal sub-agent job, child session,
      drawer row, or transcript item was added.

## Dependencies

- Phase 1 settings contract
- Phase 2 recap manager and writer
- Existing `chat.finishTurn` parent-turn lifecycle

## Closure gate

- [ ] `go test ./internal/ui/chat ./internal/settings ./internal/recap -count=1`
      exits 0.
