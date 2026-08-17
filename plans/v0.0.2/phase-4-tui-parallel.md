# Phase 4 - Parallel tools, TUI, docs

> **Parent:** `plans/v0.0.2/README.md`
> **Status:** done (manual TUI open)

## 4.1 Parallel and safety

- [x] Parallel execute for concurrent `task` tool calls in one step
- [x] Confirm/ask channel queue (buffer 32, block instead of silent deny)
- [x] Parent turn cancel cancels children

## 4.2 TUI

- [x] Wire Manager + Host on submit/continue
- [x] Settings rows: agents enabled, max concurrent, child max steps
- [x] Status segment when subs active (`subs:N/M`)
- [~] User cancel running sub-agent uses confirm (`sub-agent` qualifier) - deferred: parent cancel + `task_cancel` cover model/turn cancel; dedicated list + `d` key is a follow-up

## 4.3 Docs

- [x] Update `docs/tools.md`, `docs/architecture.md`, `docs/storage.md`, `docs/plans.md`, `docs/tui.md`, `docs/development.md`
