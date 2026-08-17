# v0.0.7 / Phase 1 - Status drawer

> **Parent:** `docs/tui.md` and the existing `sessions.status_segments` state
> **Status:** complete 2026-08-17
> **Estimated effort:** 1-2 days
> **Priority:** P1
> **Gate:** the footer stays compact, `/status` opens an agent-style drawer,
> every status detail is independently visible or hidden, and visibility
> survives session reload with all fields enabled by default

## Overview

The composer footer currently tries to paint model, variant, token window,
cache hit and miss, cost, tokens/sec, sub-agent count, model count, scroll,
and prompt hints in one constrained row. Move those details behind a
discoverable `/status` drawer. The compact footer status control is the
source of truth for opening that drawer; the drawer owns the enabled state
and the current value for every detail.

## Phase 1: Status drawer

### 1.1 Status contract

- [x] Keep all status details enabled by default for new and existing sessions;
      `TestStatusDrawerOwnsDetailsAndArrowToggle` and the migration test pass.
- [x] Give model, variant, tokens, cache, cost, tps, sub-agents, models,
      scroll, and prompt their own persisted segment identifiers.
- [x] Migrate legacy segment rows so existing sessions retain their current
      visible information and gain the new default-on fields;
      `TestLegacyStatusSegmentsExpandOnMigration` passes.

### 1.2 Drawer interaction

- [x] Replace the one-line `/status` picker with an agent-style drawer above
      the prompt.
- [x] Show each segment's label, current value, and on/off state in the drawer.
- [x] Support `↑`/`↓` selection, `enter` toggle, `←`/`esc` close, and a
      clickable drawer row.
- [x] Keep the compact footer status control visible while the drawer is open.

### 1.3 Footer and discoverability

- [x] Hide detailed status metrics from the persistent footer and replace
      them with a compact clickable `status` control.
- [x] Keep busy/error/prompt behavior readable at 120x36 and 80x24; verified
      with real tmux captures at both sizes.
- [x] Update `/help`, `docs/tui.md`, and status tests for the drawer contract.

### 1.4 Validation gate

- [x] `GOCACHE=/tmp/lazykoder-go-cache go test ./internal/ui/chat -count=1`
      passes.
- [x] `GOCACHE=/tmp/lazykoder-go-cache go test ./... -count=1` and
      `GOCACHE=/tmp/lazykoder-go-cache make test` pass.
- [x] `GOCACHE=/tmp/lazykoder-go-cache go build ./...` passes.
- [x] `GOCACHE=/tmp/lazykoder-go-cache go vet ./...` passes.
- [x] `GOCACHE=/tmp/lazykoder-go-cache make lint` was run; it exits 2 on
      pre-existing repository findings, with no status-drawer findings.
- [x] Real tmux captures at 120x36 and 80x24 show the drawer and footer
      without clipping; arrow toggle and close behavior were exercised.

## Dependencies

- Existing SQLite `sessions.status_segments` persistence and migration
  framework.
- Existing model, variant, sub-agent, token, cache, cost, and tps formatters.
