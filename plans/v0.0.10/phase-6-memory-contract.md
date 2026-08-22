# v0.0.10 / Phase 6 - Durable memory contract

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1-2 days
> **Priority:** P0
> **Gate:** the application can parse, validate, and render one bounded
> `knowledge-base/memories.md` document without accepting model-authored file
> structure or secret-like content.

## Overview

Add a durable aggregate beside the existing recap evidence files. The
aggregate is the fast project-memory entry point. It is not a transcript and
it does not replace the session-scoped recap folders.

## Executive summary

`memories.md` is one application-owned file at the project knowledge-base
root. Its front matter records the format version, update time, and last
source anchor. Its body has fixed sections for user preferences, decisions and
constraints, things to avoid, open questions, recent context, and a source
ledger. The model supplies typed facts only. Code validates and renders the
file.

## Phase 6: Schema, identity, and limits

### 6.1 Define the file format

- [ ] Add a versioned `MemoryDocument` type in `internal/recap` or a focused
      memory package. Include `format_version`, `updated_at_utc`,
      `last_session_id`, `last_message_id`, `last_message_seq`, and the six
      fixed sections.
- [ ] Define one entry type with a stable category, text, evidence/reason,
      source message IDs, first-seen UTC, last-seen UTC, and an active or
      superseded state. Keep source IDs required for every generated fact.
- [ ] Render the document with application-owned Markdown headings and YAML
      front matter. Reject arbitrary headings, raw front matter, and model
      supplied paths. Keep the output deterministic so equivalent updates
      produce the same bytes.
- [ ] Document the exact layout with a checked-in fixture. Keep the file
      readable by a human and easy to search with the existing grep tool.

### 6.2 Validate model input at the boundary

- [ ] Define a strict `MemoryEnvelope` containing typed arrays for
      preferences, decisions, avoid rules, questions, and recent context.
      Require evidence and one or more known source message IDs for each item.
- [ ] Reuse the recap envelope checks for JSON decoding, literal control
      repair, unknown-field rejection, citation validation, secret detection,
      and unsupported failure claims where they apply.
- [ ] Normalize whitespace and stable keys in code. Deduplicate equivalent
      entries without allowing the model to delete an entry silently.
- [ ] Test malformed JSON, unknown fields, missing citations, duplicate
      entries, raw Markdown control characters, secret-like text, and an
      unsupported source message.

### 6.3 Add durable update identity

- [ ] Add migration 13 for a `memory_updates` ledger keyed by project/workdir,
      source session, and source end message. Track queued, running,
      completed, and failed states, attempts, error, and the resulting file
      digest.
- [ ] Add narrow Store methods for reserve, claim, complete, fail, and list
      open memory updates. A repeated completion event must return the existing
      completed result without rewriting the document.
- [ ] Keep the ledger separate from `recap_records`: recap artifacts are
      session evidence, while `memories.md` is a project-level aggregate.

### 6.4 Enforce bounded, safe persistence

- [ ] Cap the rendered file at 64 KiB and cap each section's entry count and
      entry length. Prune only superseded or oldest low-value entries after
      preserving their source IDs in the source ledger.
- [ ] Reject paths outside the project workdir and refuse symlinked or
      unexpected destinations before writing.
- [ ] Test size limits, stable ordering, source-ledger retention, workspace
      containment, and backward-compatible handling of a missing file.

## Dependencies

- Existing `internal/recap` envelope and artifact validation
- Existing SQLite migration and Store patterns in `internal/db`
- Existing knowledge-base path containment checks

## Closure gate

- [ ] `go test ./internal/recap ./internal/db -count=1` exits 0, with the
      command and exit code recorded here.
