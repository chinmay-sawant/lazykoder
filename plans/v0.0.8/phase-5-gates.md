# v0.0.8 / Phase 5 - Closure gates and docs

> **Parent:** `plans/v0.0.8/README.md`
> **Status:** complete 2026-08-17
> **Estimated effort:** half day
> **Priority:** P1
> **Gate:** targeted then full tests pass; architecture and TUI docs
> describe compaction; no headless TUI claim

## Overview

One full verification pass after phases 1-4.

## Phase 5: Gates

### 5.1 Tests and build

- [x] `go test ./internal/prompts ./internal/agent ./internal/settings ./internal/ui/chat -count=1`
      passes. exit 0
- [x] `go test ./... -count=1` passes once at the end of the version.
      exit 0
- [x] `go build ./...` passes. exit 0
- [x] `go vet ./...` passes. exit 0
- [x] `make lint` exit 0. Findings in `compact_run.go` (mnd / ineffassign)
      were fixed in the same change. No unrelated lint churn.

### 5.2 Docs

- [x] `docs/architecture.md` agent loop mentions preflight compact and
      checkpointed `buildHistory`. Compaction is a separate assistant
      message, not a field on the chat step. Stale "key `m` opens the
      picker" line now says `/model` or the footer chip.
- [x] `docs/tui.md` documents `tokensUsed / window`, the shrink hint,
      `/compact`, `/settings` compact rows, and the compact notice.
- [x] `docs/storage.md` documents `parts.type = compaction`.
- [x] `docs/plans.md` lists this folder.

### 5.3 Manual TUI (human)

- [x] Live-terminal feel check is for the user. Automated stand-in
      (repo TUI gate is View output, not headless `go run`):
      `go test ./internal/ui/chat -count=1 -run 'TestModelShrinkSetsCompactHint|TestPromptStatusShowsCompacting|TestCompactEventResetsTokensUsed|TestSlashListsCompact|TestHelpListsCompact'`
      exit 0. Hint, compacting status, compact notice, and `/compact`
      help/slash all render.

## Dependencies

- Phases 1-4 complete with their own targeted tests already green.
