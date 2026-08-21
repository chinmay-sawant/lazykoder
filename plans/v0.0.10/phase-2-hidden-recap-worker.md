# v0.0.10 / Phase 2 - Hidden recap worker

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 2 days
> **Priority:** P0
> **Gate:** one reserved record turns a bounded persisted snapshot into one
> atomically written recap, with no normal sub-agent job, child session, or
> visible event.

## Overview

Build a dedicated `internal/recap` package. It is a single-shot internal
sub-agent: it reads a stable snapshot, calls the configured OpenCode model
with no tools, and lets application code write one known Markdown file. It is
not `subagent.Manager`, because that manager deliberately persists visible
task jobs and creates child sessions for the drawer.

## Executive summary

Do not pass the worker provider-shaped history. That history may start at a
compaction checkpoint and prunes old tool text. Build a recap-specific view
from durable main-session rows instead. Use source sequences for identity;
use timestamps only as metadata.

## Phase 2: Snapshot, worker, and file materialization

### 2.1 Select a stable recap snapshot

- [ ] Add a query or graph helper under `internal/db` that returns the latest
      five text-bearing `user` and `assistant` messages for one main session.
      Exclude `messages.agent = "compaction"`, child sessions, and incomplete
      streamed data.
- [ ] If the selected set has four messages, use four. If it has fewer than
      four, return no candidate. The snapshot preserves message ID, sequence,
      time, role, text parts, and bounded completed-tool facts.
- [ ] Derive the source end message ID from the selected final message. A
      successful reservation for that ID prevents duplicate work even when
      windows overlap across turns.
- [ ] Do not use `agent.buildHistory` for this feature. It is a provider
      request builder with compaction and tool-output pruning rules, not a
      durable transcript reader.
- [ ] Test five-entry selection, four-entry fallback, compaction exclusion,
      stable sequence order, and capped tool facts.

### 2.2 Run one hidden model call

- [ ] Add a recap manager with a bounded internal queue, one worker at a
      time, cancellable jobs, and startup recovery for queued or interrupted
      records. Recovery retries a record once; a second failure remains
      recorded and never blocks chat.
- [ ] Build the worker request from a versioned prompt owned by
      `internal/recap`. Ask for concrete decisions, files, constraints,
      completed work, failures, and next work. Require Markdown body only;
      application code owns the front matter.
- [ ] Call the OpenCode client directly with the explicit recap model and the
      endpoint resolved from `modelscache.Info`. Send no tool specifications,
      do not create a child session, and do not expose an `agent.Event`.
- [ ] A missing cache entry may still use the configured model ID with the
      provider default endpoint. A model-selection failure records `failed`;
      it must not fail or delay the completed parent turn.
- [ ] Test configured model and endpoint selection with the fake provider,
      no-tool requests, cancellation, one restart-safe retry, and failure
      isolation from the main turn.

### 2.3 Materialize an ordered local-memory file

- [ ] Create `knowledge-base/recaps/<session-id>/` under the resolved project
      workdir with `os.MkdirAll`. Reject paths outside that workdir before any
      write.
- [ ] Compute the immutable filename from zero-padded end sequence and end
      message ID. Do not use a timestamp in the filename.
- [ ] Write a temp file in the destination directory, sync and close it, then
      rename it into place. A completed record always points at a complete
      file, never a partial write.
- [ ] Generate YAML front matter with recap ID, session ID, source start/end
      sequences, source end message ID, source end Unix-millisecond time,
      `generated_at_utc`, model, and the source-content SHA-256. Append only
      the validated model body after that header.
- [ ] Mark the record completed only after the rename succeeds. Store the
      content hash and output path. On error, retain the error and lifecycle
      timestamps for recovery.
- [ ] Test exact path format, UTC metadata, natural ordering, atomic-write
      error handling, duplicate completion, and workspace containment.

## Dependencies

- Phase 1 recap settings and record ledger
- `db.LoadSessionGraph` or an equivalent focused query
- OpenCode client and cached model profiles

## Closure gate

- [ ] `go test ./internal/recap ./internal/db -count=1` exits 0.
