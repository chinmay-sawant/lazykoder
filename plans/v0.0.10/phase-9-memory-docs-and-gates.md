# v0.0.10 / Phase 9 - Memory documentation and closure gates

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** in progress (automated gates complete; live TTY check open)
> **Estimated effort:** 1 day
> **Priority:** P1
> **Gate:** code, docs, local knowledge-base guidance, and real runtime checks
> agree on the durable memory lifecycle.

## Overview

Close the extension only after a reader can understand what `memories.md`
contains, why recap evidence remains separate, and when the application reads
or writes each file. Keep the hidden worker out of the normal transcript and
avoid adding a second UI contract unless the implementation needs one for
failure visibility.

## Phase 9: Documentation, tests, and release checks

### 9.1 Synchronize committed documentation

- [x] Update `docs/architecture.md` with the memory aggregate, worker input,
      strict envelope, atomic write path, and first-request search order.
- [x] Update `docs/storage.md` with the memory update ledger, project-level
      ownership, source anchor, digest, and restart behavior.
- [x] Update `docs/tui.md` only where the existing recap settings or `/agents`
      failure state needs to explain memory updates. Do not promise a visible
      memory worker row.
- [x] Update `docs/plans.md` and the v0.0.10 parent plan when each phase gate
      passes. Keep unchecked rows open until the command or runtime evidence
      exists.

### 9.2 Synchronize the local knowledge base

- [x] Add a concise `knowledge-base/03-concepts/memory.md` page that links to
      `memories.md`, describes every section, and points to the source ledger.
- [x] Update `knowledge-base/03-concepts/recaps.md` to distinguish implemented
      recap artifacts from the new planned or shipped aggregate.
- [x] Add a real generated fixture only after the worker exists. Do not create
      an empty placeholder `knowledge-base/memories.md` during planning.
- [x] Run the unslop pass over all new prose and check that no em dashes or
      unsupported shipped claims remain.

### 9.3 Run automated and runtime gates

- [x] Run focused memory, database, agent, recap, settings, and chat tests.
- [x] Run `go build ./...`, `make test`, `make lint`, and `make vet`; record
      each exit code in the parent ledger.
- [x] Run the relevant race tests for the memory writer and first-request
      recall path.
- [ ] In a real TTY at 120x36 and 80x24, enable recaps, complete a parent
      request, inspect the recap row, then send a related request and verify
      that memory hints arrive before the first provider call. Confirm that
      tool follow-ups and `/continue` do not repeat the scan.
- [x] Inspect `memories.md` after repeated successful requests. Verify fixed
      headings, source IDs, stable ordering, bounded size, no secrets, no
      partial file, and no duplicate update after a replayed completion.

## Dependencies

- Phases 6 through 8
- Existing documentation and knowledge-base conventions
- A real terminal for the final interaction gate

## Closure gate

- [~] Every parent-plan memory row is either `[x]` with current evidence or
      `[~]` with an explicit deferred boundary. No memory feature claim is
      marked complete from plan intent alone.

Evidence: `make lint`, `make vet`, `make build`, and `make test` exited 0 on
2026-08-22. Focused and race tests exited 0, including
`go test -race ./internal/db ./internal/recap ./internal/ui/chat -count=1`.
The live 120x36 and 80x24 terminal interaction remains a manual follow-up;
the code has no new visible worker row, but the settings and first-request
behavior still need visual confirmation in a real terminal.
