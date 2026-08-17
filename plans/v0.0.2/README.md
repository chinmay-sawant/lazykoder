# v0.0.2 - Sub-agent harness

> **Parent:** `plans/v0.0.1/` - foundation and tools already shipped
> **Status:** implemented (automated gates green; manual TUI open)
> **Estimated effort:** multi-session
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **App version:** 0.0.2 (feature work; no release tag yet)

---

## Overview

Add in-process sub-agents to lazykoder: the parent agent can spawn up to 20
concurrent children via `task` tools, with role-based tool allowlists,
settings-backed caps, child sessions for audit, and minimal TUI surface.

Phase files (live ledgers; mark `[x]` only after the gate passes):

| File | Goal |
| --- | --- |
| [phase-1-registry-settings.md](phase-1-registry-settings.md) | Tool registry, advertise full tool set, `agents` settings |
| [phase-2-manager-tools.md](phase-2-manager-tools.md) | `internal/subagent`, `tools/task`, Host seam |
| [phase-3-runner-storage.md](phase-3-runner-storage.md) | AgentRunner, child sessions, hybrid storage |
| [phase-4-tui-parallel.md](phase-4-tui-parallel.md) | Parallel task tools, confirm queue, TUI + docs |

---

## Design summary

- Deep module: `internal/subagent.Manager` (Spawn / List / Wait / Cancel)
- Thin schemas: `internal/tools/task`
- Parent tools: `task`, `task_list`, `task_status`, `task_wait`, `task_cancel`
- Child roles: `explore` / `plan` (read-only), `general` (single writer)
- Max concurrent: hard cap 20, default 4; depth 1 (no nested spawn)
- Handoff: final summary to parent model; progress via parent tool cards
- Isolation v1: same workspace; no worktrees yet

---

## Closure gates

- [x] `go build ./...` / `go build -o bin/lk .` exit 0
- [x] `go test ./... -count=1` exit 0
- [~] `make lint` clean (pre-existing mnd/unused findings in chat view/theme; not introduced by this work)
- [ ] Manual TUI: spawn explore task, see card, status `subs:N/M`, cancel confirm
