# Phase 5 - Memory update speed and history list churn

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** not started
> **Estimated effort:** 2-3 days

---

## Overview

Two observed problems, both diagnosed against the current code:

1. **Memory updates feel slow.** Every successful parent turn schedules a
   full memory pipeline: snapshot selection, aggregate read, grep over recap
   evidence, one provider call with the whole prompt, a possible repair call,
   re-read of the document under a global per-workdir lock, merge, atomic
   write, ledger completion. The provider call dominates wall-clock time and
   nothing is incremental.
2. **History view churns many rows.** The history/picker list sorts by
   `sessions.time_updated DESC` (`internal/db/queries.go`,
   `ListSessionsByDirectory`). `time_updated` is bumped by every message
   insert (`InsertMessage`, `touchSessionTx`), every part insert
   (`InsertPart`), visibility changes, model/variant/segment updates, and
   explicitly by sub-agent completion (`internal/subagent/manager.go:524`
   touches the parent). So while any background work runs (memory worker,
   recaps, sub-agents), every touched session jumps to the top and other
   rows visibly reorder. Only the session that actually received new user
   content should move; ordering should not change just because a background
   worker or a child finished.

## 5.1 Diagnose and instrument memory latency

- [ ] Add timing spans (start/end logged to the existing status/error rows,
      not stdout) around each `RunMemoryUpdate` stage: claim, snapshot read,
      `RelatedRecapEvidence`, first provider call, repair call, merge/write.
      Record stage durations on the `memory_updates` row so slowness is
      measurable, not guessed.
- [ ] Confirm from real data which stage dominates (expected: the provider
      call with the full aggregate plus recap evidence in one prompt).

## 5.2 Make memory updates cheaper

- [ ] Skip no-op turns before any provider call: if the snapshot contains no
      new user-authored signals and no assistant content beyond what the last
      completed update already covered, mark the run complete immediately.
- [ ] Cap and trim the prompt: send only entries changed since the last
      digest plus the bounded new window instead of the whole aggregate when
      it exceeds a size threshold; keep strict citation validation unchanged.
- [ ] Run the grep for related recap evidence concurrently with reading the
      aggregate (independent operations today, sequential in
      `memory_run.go`).
- [ ] Consider making the repair path cheaper: only issue the single repair
      call when the failure is exactly the repairable recent-context case
      (already true in `memory_worker.go`; verify with tests).
- [ ] Gate: measured p50 end-to-end memory update time drops versus the 5.1
      baseline on the same workload.

## 5.3 Fix history ordering churn

- [ ] Split "activity timestamp" from "conversation timestamp": keep
      `time_updated` for resume-list freshness, but add a column (e.g.
      `time_active`) bumped only when user-visible conversation content lands
      in that session (user message, assistant reply, tool call the user
      triggered).
- [ ] Background workers (memory, recap) and sub-agent completion must stop
      touching the parent's ordering key: `manager.go` parent-touch switches
      to the activity column only where a drawer-visible state actually
      changed, and never bumps conversation time.
- [ ] History/picker views sort by the conversation timestamp; the drawer
      keeps its own most-recent-activity ordering for child jobs.
- [ ] Migration: backfill the new column from `time_updated` (one-time,
      idempotent, follows the existing migration patterns in
      `internal/db/migrate_rebuild.go` / `status_segments_migration.go`).
- [ ] Gate: with a memory update and a sub-agent job running in the
      background, opening history shows exactly one reordered row (the
      active chat); all other rows hold their positions.

## 5.4 Docs and gate

- [ ] Update `docs/`, knowledge-base `memory.md` lifecycle section, sessions
      concept page, and this roadmap page in the same change.
- [ ] Gate: `make lint` PASS, `make test` PASS, live TTY checks from 5.2 and
      5.3 recorded beside those rows.
