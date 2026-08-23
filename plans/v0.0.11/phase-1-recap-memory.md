# Phase 1 - Recap and memory surfaced into the loop

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** not started
> **Estimated effort:** 2-3 days

---

## Overview

Recall already exists (`internal/agent/recall`): recaps are grepped before
the first ordinary request of a turn as unpersisted hints. This phase adds
auto-loading of the bounded `knowledge-base/memories.md` aggregate,
model-based selection when grep misses, and a `/memory` view of what will be
injected. Smallest and independent phase; ships first.

## 1.1 Auto-load the memories aggregate

- [ ] Add a loader in `internal/recap/memory.go` that reads
      `knowledge-base/memories.md`, validates format_version, and returns a
      bounded context block; missing file yields an empty block, not an error.
- [ ] In `internal/agent`, inject the aggregate alongside recall hints before
      the first ordinary request of a user turn: unpersisted, untrusted,
      clearly sectioned, once per turn (no repeats from tool follow-ups,
      `/continue`, children, or compaction), mirroring existing recall rules.

## 1.2 Model-based selection when grep misses

- [ ] When recall grep returns zero hits for a turn, run one hidden selection
      call using the configured `recap.model` over recent recap titles plus
      summaries; selected lines enter the same unpersisted hint channel.
- [ ] Bound the pass: single call, no tools, capped input, failure falls back
      to grep-only behavior silently.

## 1.3 `/memory` view and toggle

- [ ] Add `/memory` slash command (`chat.go` + `slash.go`) rendering the
      exact context the next turn carries: memories sections and matched
      recap lines with source paths.
- [ ] Add a per-session injection toggle in the view; toggling off suppresses
      both aggregate and hint injection for the rest of the session only.

## 1.4 Docs and gate

- [ ] Update `docs/` and knowledge-base pages (recaps, memory concepts) in
      the change that ships the behavior.
- [ ] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      fresh session respects a stored preference and an avoid rule without
      being told, and `/memory` lists those exact lines. Record outcomes
      beside these rows when they run.
