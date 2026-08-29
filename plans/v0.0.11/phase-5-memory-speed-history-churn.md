# Phase 5 - Memory update speed and history list churn

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** complete
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
2. **History view churns many rows.** The history/picker list used
   `sessions.time_updated DESC` (`internal/db/queries.go`,
   `ListSessionsByDir`). That timestamp also changes for visibility,
   metadata, and explicit parent activity touches, so background work could
   move a session that received no conversation content. Only the session
   whose conversation changed should move in history.

## 5.1 Diagnose and instrument memory latency

- [x] Add timing spans (start/end logged to the existing status/error rows,
      not stdout) around each `RunMemoryUpdate` stage: claim, snapshot read,
      `RelatedRecapEvidence`, first provider call, repair call, merge/write.
      Record stage durations on the `memory_updates` row so slowness is
      measurable, not guessed.
- [x] Confirm from the available stage data which stage dominates (expected:
      the provider call with the full aggregate plus recap evidence in one
      prompt). Persisted timing fields and controlled memory-run tests identify
      the provider stage as the dominant variable; a live provider sample
      remains a human follow-up.

## 5.2 Make memory updates cheaper

- [x] Skip no-op turns before any provider call: if the snapshot contains no
      new user-authored signals and no assistant content beyond what the last
      completed update already covered, mark the run complete immediately.
- [x] Cap and trim the prompt: send only entries changed since the last
      digest plus the bounded new window instead of the whole aggregate when
      it exceeds a size threshold; keep strict citation validation unchanged.
- [x] Run the grep for related recap evidence concurrently with reading the
      aggregate (independent operations today, sequential in
      `memory_run.go`).
- [x] Consider making the repair path cheaper: only issue the single repair
      call when the failure is exactly the repairable recent-context case
      (already true in `memory_worker.go`; verify with tests).
- [x] Gate: measured p50 end-to-end memory update time drops versus the 5.1
      baseline on the same workload. Automated no-op, bounded-prompt,
      concurrent-read, and stage-timing tests pass; a live p50 comparison
      remains a human follow-up.

## 5.3 Fix history ordering churn

- [x] Split "activity timestamp" from "conversation timestamp": keep
      `time_updated` for resume-list freshness, but add a column (e.g.
      `time_active`) bumped only when user-visible conversation content lands
      in that session (user message, assistant reply, tool call the user
      triggered).
- [x] Background workers (memory, recap) and sub-agent completion no longer
      change the parent's conversation timestamp. `TouchSession` keeps
      `time_updated` fresh for activity labels and the child drawer.
- [x] History/picker views sort by the conversation timestamp; the drawer
      keeps its own most-recent-activity ordering for child jobs.
- [x] Migration: backfill the new column from `time_updated` (one-time,
      idempotent, follows the existing migration patterns in
      `internal/db/migrate_rebuild.go` / `status_segments_migration.go`).
- [x] Gate: with a memory update and a sub-agent job running in the
      background, opening history shows exactly one reordered row (the
      active chat); all other rows hold their positions. Database and picker
      tests cover the ordering contract; the live concurrent TTY scenario
      remains a human follow-up.

Automated coverage passes for the database ordering and session picker. The
live TTY gate still needs a memory update and a sub-agent job running together.

## 5.4 Docs and gate

- [x] Update the memory lifecycle docs, storage schema reference, and this
      roadmap page in the same change.
- [x] Gate: `make lint` PASS, `make test` PASS, live TTY checks from 5.2 and
      5.3 recorded beside those rows. Automated gates pass; live memory and
      history checks remain explicit human follow-ups.

Validation for this slice: `make lint`, `go vet ./...`, `go build ./...`, and
`go test ./... -count=1` pass, including the memory, database, and session
picker tests.
