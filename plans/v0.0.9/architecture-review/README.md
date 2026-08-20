# v0.0.9 architecture review - deepen hot modules

> **Parent:** `plans/v0.0.9/` (fresh launch / quit banner track is separate)
> **Status:** review complete; implementation not started
> **Branch:** plan only (pick a `chore/` or `refactor/` branch when executing)
> **Estimated effort:** 5-8 days across six phases
> **Priority:** P2 (navigability and test locality; no user-facing feature)
> **Skill:** `skills/phase-wise-checklist/SKILLS.md` + improve-codebase-architecture
> **Gate:** each phase's checklist rows and named `go test` / `make test` lines pass;
> HTML report candidates stay aligned with code; knowledge-base page updated

This folder is the live ledger for architecture deepening after the 2026-08-20
scan (5 explore agents over `ui/chat`, `agent`, `subagent`, `db`/provider, and
cross-cutting seams).

**Artifacts**

| File | Role |
| --- | --- |
| `architecture-review.html` | Visual candidate report (open in a browser) |
| `phase-wise-checklist.md` | Canonical execution ledger |
| this README | Scope, order, constraints |

Mark `[x]` only after the named gate command passes. Do not claim TUI feel
from headless `go run`.

---

## Overview

Recent work piled into `internal/ui/chat` (transcript, subagents, status,
settings, compact UI) and `internal/agent` (compaction). File splits follow
AGENTS.md module-wise rules, but several concepts still have two homes:
Compaction thresholds, Session graph assembly, turn wiring, and Subagent
lifecycle projection.

The report proposes deepening opportunities. This checklist turns the strong
ones into ordered phases. Speculative items stay deferred.

**Top recommendation:** one home for Compaction and fill policy (phase 1).

---

## Constraints (do not re-litigate)

From `knowledge-base/02-architecture/decisions.md` and AGENTS.md:

- OpenCode Go is the only provider. Do not invent a one-adapter provider
  interface "for purity."
- Bubble Tea: `Update` stays pure; side effects in `tea.Cmd`.
- Split module-wise, not by line count. No rename sweeps during a split.
- Subagent nesting depth 1 (`Host: nil` on children) is product policy until
  nesting ships.
- SQLite Session store stays the source of truth.
- No new dependencies without explicit sign-off.

---

## Phase map

Rating is priority for deepening now (leverage × proven friction × fit with
existing decisions), not a grade of today's code quality.

| Phase | Focus | Strength | Rating |
| --- | --- | --- | --- |
| 1 | Compaction / fill ownership (C1) | Strong | 9/10 |
| 2 | Session graph + transcript projection (C3) | Strong | 8/10 |
| 3 | Exclusive TUI mode ownership (C4) | Strong | 8/10 |
| 4 | Turn-runtime + human-gate adapter (C2) | Strong | 9/10 |
| 5 | Agent tool dispatch + wire/retire `tools/task` (C5, C7) | Strong | 8/10, 7/10 |
| 6 | Subagent drawer projection + MaxDepth honesty (C6) | Worth exploring | 7/10 |
| Deferred | Layout snapshot (C8); Event without `db.Part` (C9) | Worth exploring / Speculative | 6/10, 4/10 |

---

## How to open the report

```bash
xdg-open plans/v0.0.9/architecture-review/architecture-review.html
```

Absolute path on this machine:

`/home/chinmay/ChinmayPersonalProjects/lazykoder/plans/v0.0.9/architecture-review/architecture-review.html`
