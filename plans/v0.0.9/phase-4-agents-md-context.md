# v0.0.9 / Phase 4 - Workdir AGENTS.md in model context

> **Parent:** `plans/v0.0.9/README.md`
> **Status:** complete on branch `feature/v0.0.9-resume-session`
> **Estimated effort:** 0.5-1 day
> **Priority:** P1
> **Gate:** with `AGENTS.md` in the workdir, every chat `callModel` request
> prepends a `system` message containing that file; missing file is a no-op;
> TUI shows a short loaded notice; `go test ./internal/agent ./internal/ui/chat`
> exit 0

---

## Overview

Coding agents that edit this repo read `AGENTS.md` automatically. lazykoder's
in-app agent now does too: it loads `AGENTS.md` (fallback `agents.md`) from
the session workdir and injects it as a system message on the provider wire,
plus a small TUI notice when present.

## Locked decisions

- Workdir `AGENTS.md` only (then `agents.md`)
- System role on the wire; not stored in SQLite
- TUI notice when loaded
- Soft size cap (200000 bytes) with truncate note

## 4.1 Loader

- [x] `LoadProjectInstructions` in `internal/agent/project_instructions.go`
- [x] Prefer `AGENTS.md`, else `agents.md`
- [x] Truncate oversized files with an explicit trailing note
- [x] Unit tests: missing / found / fallback / truncate / prefer AGENTS

## 4.2 Wire injection

- [x] Cache instructions on `Agent`
- [x] Prepend `system` message in `callModel` (stream + non-stream)
- [x] Compaction uses `callSummarizer`, not `callModel` (no AGENTS inject)
- [x] `opencode.Message` role comment allows `system`
- [x] `TestSendInjectsProjectInstructions` + `TestSendWithoutProjectInstructions`

## 4.3 TUI notice

- [x] `chat.New` sets `projectInstructionsNotice` when present
- [x] Alert row + empty-state line: `project instructions: AGENTS.md`
- [x] Cleared on submit / compact / continue
- [x] `TestNewSurfacesProjectInstructionsNotice`

## 4.4 Docs and KB

- [x] `docs/architecture.md`, `docs/tui.md`
- [x] `knowledge-base/` overview + data-flow
- [x] This phase file + README table

## Gates

- [x] `go test ./internal/agent ./internal/ui/chat` exit 0
- [x] `go build -o bin/lk .` exit 0
- [ ] Manual: notice visible in this repo; send still works
