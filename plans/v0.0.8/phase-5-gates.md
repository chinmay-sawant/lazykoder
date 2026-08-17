# v0.0.8 / Phase 5 - Closure gates and docs

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** planned
> **Estimated effort:** half day
> **Priority:** P1
> **Gate:** targeted then full tests pass; architecture and TUI docs
> describe compaction; no headless TUI claim

## Overview

One full verification pass after phases 1-4. Do not mark this file
complete from intent.

## Phase 5: Gates

### 5.1 Tests and build

- [ ] `go test ./internal/prompts ./internal/agent ./internal/settings ./internal/ui/chat -count=1`
      passes. Record the command and exit code.
- [ ] `go test ./... -count=1` passes once at the end of the version.
      Record the command and exit code.
- [ ] `go build ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `make lint` is run. Record exit code. Do not churn unrelated
      pre-existing findings.

### 5.2 Docs

- [ ] `docs/architecture.md` agent loop mentions preflight compact and
      checkpointed `buildHistory`. Fix the stale "key `m` opens the
      picker" line (picker is `/model` or the footer chip).
- [ ] `docs/tui.md` documents `tokensUsed / window`, the shrink hint,
      and `/compact`.
- [ ] `docs/plans.md` already lists this folder (done when the ledger
      was created).

### 5.3 Manual TUI (human)

- [ ] Human: in a real terminal, fill a session, switch to a smaller
      window model, confirm the composer hint, send, and see
      `compacting` then a usable follow-up. Do not mark from a
      headless `go run`.

## Dependencies

- Phases 1-4 complete with their own targeted tests already green.
