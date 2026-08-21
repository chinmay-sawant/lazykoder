# v0.0.10 / Phase 2 - Hidden recap worker

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 2 days
> **Priority:** P0
> **Gate:** one reserved record turns a bounded persisted snapshot into recap,
> question, and avoid artifacts, with no normal sub-agent job, child session,
> or visible event.

## Overview

Build a dedicated `internal/recap` package. It is a single-shot internal
sub-agent: it reads a stable snapshot, calls the configured OpenCode model
with no tools, and lets application code write related Markdown artifacts. It
is not `subagent.Manager`, because that manager deliberately persists visible
task jobs and creates child sessions for the drawer.

## Executive summary

Do not pass the worker provider-shaped history. That history may start at a
compaction checkpoint and prunes old tool text. Build a recap-specific view
from durable main-session rows instead. The snapshot is newest first, starts
with one hour of history, and expands to two hours only when necessary. Use
source sequences for identity; use timestamps only as metadata.

## Phase 2: Snapshot, worker, and file materialization

### 2.1 Select a stable recap snapshot

- [ ] Add a query or graph helper under `internal/db` that returns
      text-bearing `user` and `assistant` messages for one main session.
      Exclude `messages.agent = "compaction"`, child sessions, and incomplete
      streamed data.
- [ ] Sort by `time_created DESC, seq DESC`. Select the prior hour first. If
      it has fewer than four entries, extend to two hours. Keep five at most,
      allow four, and return no candidate below four. Preserve message ID,
      sequence, time, role, text parts, and bounded completed, denied, and
      failed tool facts.
- [ ] Derive the source end message ID from the selected final message. A
      successful reservation for that ID prevents duplicate work even when
      windows overlap across turns.
- [ ] Do not use `agent.buildHistory` for this feature. It is a provider
      request builder with compaction and tool-output pruning rules, not a
      durable transcript reader.
- [ ] Test one-hour selection, two-hour fallback, newest-first tie breaking,
      four-entry minimum, five-entry cap, compaction exclusion, and capped
      failed-tool facts.

### 2.2 Generate questions and avoid rules

- [ ] Before the model call, run a best-effort internal `grep.Run` only under
      `knowledge-base/recaps/things-to-avoid/` with safely quoted snapshot
      terms. Include capped matching rules so the worker can update or confirm
      them instead of recording duplicates.
- [ ] Require a strict JSON envelope with `recap_markdown`, `questions`,
      and `things_to_avoid` fields. Every question needs its reason and cited
      source message IDs. Every avoid rule needs a concrete rule, its reason,
      and cited source message IDs.
- [ ] Reject malformed data, empty recap text, oversized fields, uncited
      questions or rules, invented failures, and requests for secrets. Record
      failure instead of writing a partial artifact set.
- [ ] Test related-avoid grep input, envelope validation, empty question or
      avoid lists, duplicate rules, and malformed model output.

### 2.3 Run one hidden model call

- [ ] Add a recap manager with a bounded internal queue, one worker at a
      time, cancellable jobs, and startup recovery for queued or interrupted
      records. Recovery retries a record once; a second failure remains
      recorded and never blocks chat.
- [ ] Build the worker request from a versioned prompt owned by
      `internal/recap`. Ask for concrete decisions, files, constraints,
      completed work, failures, unresolved questions, and source-backed things
      to avoid. Application code owns every front matter field.
- [ ] Call the OpenCode client directly with the explicit recap model and the
      endpoint resolved from `modelscache.Info`. Send no tool specifications,
      do not create a child session, and do not expose an `agent.Event`.
- [ ] A missing cache entry may still use the configured model ID with the
      provider default endpoint. A model-selection failure records `failed`;
      it must not fail or delay the completed parent turn.
- [ ] Test configured model and endpoint selection with the fake provider,
      no-tool requests, cancellation, one restart-safe retry, and failure
      isolation from the main turn.

### 2.4 Materialize ordered local-memory artifacts

- [ ] Create `sessions`, `questions`, and `things-to-avoid` folders under
      `knowledge-base/recaps/` in the resolved project workdir. Reject paths
      outside that workdir before any write.
- [ ] Compute the immutable filename from zero-padded end sequence and end
      message ID. Do not use a timestamp in the filename.
- [ ] Always write the recap file. Write question and avoid files only for
      nonempty validated lists. Use the same source stem in all three folders.
- [ ] Write each file to a temp path in its destination directory, sync and
      close it, then rename it into place. A completed record points only to
      complete files.
- [ ] Generate YAML front matter with recap ID, session ID, source start/end
      sequences, source end message ID, source end Unix-millisecond time,
      `generated_at_utc`, model, and the source-content SHA-256. Append only
      the validated model body after that header.
- [ ] Mark the record completed only after every required rename succeeds.
      Store each path and SHA-256 in the artifact manifest. On error, retain
      the error and lifecycle timestamps for recovery.
- [ ] Test exact path format, UTC metadata, natural ordering, atomic-write
      error handling, duplicate completion, and workspace containment.

## Dependencies

- Phase 1 recap settings and record ledger
- `db.LoadSessionGraph` or an equivalent focused query
- OpenCode client and cached model profiles

## Closure gate

- [ ] `go test ./internal/recap ./internal/db -count=1` exits 0.
