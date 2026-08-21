# v0.0.10 / Phase 4 - Documentation and gates

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1 day
> **Priority:** P1
> **Gate:** the written contract matches the shipped worker, all automated
> checks pass, and a real terminal confirms the settings card stays usable.

## Overview

Finish only after the feature has a truthful local narrative. The durable
record is internal, but users need to know where recap files live, what they
contain, how to turn the feature off, and that the agent does not yet retrieve
them automatically.

## Executive summary

Documentation must separate current recap creation from the later recall
policy. Automated tests prove persistence and idempotence. A real terminal
proves two more settings rows did not make the card unusable.

## Phase 4: Documentation and release checks

### 4.1 Synchronize documentation and local knowledge base

- [ ] Update `docs/storage.md` with `recap_records`, the non-cascading recap
      files, status values, and sequence-based ordering.
- [ ] Update `docs/tui.md` with the `recaps enabled` and `recap model` rows,
      defaults, and silent behavior.
- [ ] Update `docs/architecture.md` with the dedicated recap worker boundary
      and its relationship to the chat runtime, database, model cache, and
      knowledge base.
- [ ] Add `knowledge-base/03-concepts/recaps.md` with the same local contract:
      inputs, path, ordering, model selection, recovery, and the fact that
      recall-before-tool-call remains unshipped.
- [ ] Do not create placeholder recap files. `knowledge-base/recaps/` is
      created by the worker when the first real recap succeeds.

### 4.2 Validate the delivered behavior

- [ ] Run focused unit and integration tests without a live API:
      `go test ./internal/settings ./internal/db ./internal/recap ./internal/ui/chat -count=1`.
- [ ] Run `go build ./...`, then `make test`, then `make lint`. Record each
      exit code in this plan before marking its row complete.
- [ ] In a real TTY, inspect `/settings` at 120x36 and 80x24. Toggle recaps,
      choose a model, save, reopen, and confirm the value remains readable.
- [ ] With a fake provider, complete enough main-chat turns to make a recap.
      Confirm one ordered Markdown file, correct front matter, no duplicate
      after replaying completion, and no drawer or transcript entry.
- [ ] Restart with a queued and with an interrupted record. Confirm one retry,
      then a terminal record, without delaying a new parent turn.

## Parked follow-up

- [~] Retrieval before later tool calls is deferred. A future plan must define
      relevance, query text, result limits, conflict handling, empty-directory
      behavior, prompt placement, and an evaluation set before it adds a
      `grep knowledge-base/recaps` instruction or tool-loop hook.

## Dependencies

- Phases 1 through 3

## Closure gate

- [ ] All parent-plan closure rows are complete with current command output.
