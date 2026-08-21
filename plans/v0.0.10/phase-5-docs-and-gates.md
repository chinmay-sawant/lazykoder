# v0.0.10 / Phase 5 - Documentation and gates

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1 day
> **Priority:** P1
> **Gate:** docs match the worker and recall path, automated checks pass, and a
> real terminal confirms the settings card stays usable.

## Overview

Finish only after the local narrative explains both halves. Recaps write local
artifacts after a turn. The next parent user turn searches them before its
first ordinary request. The parent never searches again merely because the
model made a tool call.

## Executive summary

Documentation must state that recap text is historical and untrusted. Tests
prove time selection, artifact integrity, first-request lookup, and the lack
of hidden-worker UI. A real terminal proves two more settings rows did not
make the card unusable.

## Phase 5: Documentation and release checks

### 5.1 Synchronize documentation and local knowledge base

- [ ] Update `docs/storage.md` with `recap_records`, artifact manifests,
      preserved files after session deletion, statuses, and sequence ordering.
- [ ] Update `docs/tui.md` with recap settings, defaults, silent artifact
      behavior, and first-request recall with no visible tool row.
- [ ] Update `docs/architecture.md` with recap worker, first-request lookup,
      agent injection order, and `grep.Run` as the confined search path.
- [ ] Update `knowledge-base/03-concepts/recaps.md` with folder layout,
      one-to-two-hour selection, generated questions, things to avoid, safe
      recall, and the no-repeat tool-follow-up boundary.
- [ ] Do not create placeholder artifacts. The three recap subfolders appear
      only when a real artifact needs them.

### 5.2 Validate the delivered behavior

- [ ] Run focused checks without a live API:
      `go test ./internal/settings ./internal/db ./internal/recap ./internal/agent ./internal/ui/chat -count=1`.
- [ ] Run `go build ./...`, then `make test`, then `make lint`. Record
      each exit code in this plan before marking its row complete.
- [ ] In a real TTY, inspect `/settings` at 120x36 and 80x24. Toggle recaps,
      choose a model, save, reopen, and confirm the value remains readable.
- [ ] With a fake provider, complete enough parent messages for a recap. Check
      recap, questions, and avoid outputs; correct front matter; no duplicate
      after replaying completion; and no drawer or transcript entry.
- [ ] Send a related next user request. Check exactly one internal grep scan
      before the first normal provider request, bounded safe prompt injection,
      no scan after the model's tool call, and no scan for `/continue`.

## Dependencies

- Phases 1 through 4

## Closure gate

- [ ] All parent-plan closure rows are complete with current command output.
