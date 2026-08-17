# v0.0.4 - SQLite schema integrity (FKs + indexes)

> **Parent:** `plans/v0.0.2/` (sub-agent sessions + durable jobs) and
> shipped foundation in `plans/v0.0.1/`
> **Status:** implemented (migrations 6-8 + tests; 2026-08-16)
> **Scope:** foreign keys, uniqueness, indexes, cascade delete correctness
> **Note:** `plans/v0.0.3/` is reserved for the Mermaid visualizer. Schema
> work lives here so the two tracks stay separate.

## Goal

Make the project SQLite schema (`<cwd>/.lazykoder/lazykoder.db`) honest and
robust:

1. Every logical parent/child link is a real foreign key (or an explicit,
   documented exception).
2. Hot queries use covering indexes that match the store API predicates and
   order-by clauses.
3. Delete/cascade behavior is correct for parent sessions, child sessions,
   and durable sub-agent jobs.
4. Migrations stay numbered and idempotent; existing databases upgrade
   without data loss.

This is a storage hardening track, not a new product feature.

## Why this track

Audit of `internal/db` (2026-08-16) and a live project DB showed:

- **FKs are not absent.** The core chain already has them:
  `messages.session_id -> sessions`, `parts.message_id -> messages`,
  `tool_calls.part_id -> parts`, all `ON DELETE CASCADE`, with
  `PRAGMA foreign_keys = ON` at open.
- **Several links are still plain TEXT** (no FK):
  `sessions.parent_session_id`, `subagent_jobs.child_session_id`,
  `subagent_jobs.parent_part_id`.
- **Indexes exist**, but some hot paths miss the right shape (resume list by
  `directory`, child list by parent, open-job recover by status).
- **Uniqueness gaps:** no unique `(session_id, seq)` / `(message_id, seq)`.
- **Delete path** now relies on `sessions.parent_session_id` CASCADE
  (migration 7); `DeleteSession` is a single `DELETE` by id.

Phase files (live ledgers; mark `[x]` only after the gate passes):

| File | Goal |
| --- | --- |
| [phase-1-schema-fk-indexes.md](phase-1-schema-fk-indexes.md) | Audit + migrations 6-8 implementation |

## Out of scope

- Mermaid visualizer (`plans/v0.0.3/`)
- New product tables (todos, etc.) unless required for integrity
- Switching away from SQLite or from `modernc.org/sqlite`
- Multi-connection pool (keep `MaxOpenConns(1)`)

## Closure gates (track-level)

- [x] Migration applies on empty DBs (`Migrate` idempotent) - `go test ./internal/db -run TestMigrateIdempotent` exit 0
- [x] `PRAGMA foreign_key_check` clean on test fixtures - `TestForeignKeysEnabled` exit 0
- [x] `go test ./internal/db/ -count=1` exit 0
- [x] `go test ./internal/subagent/ ./internal/agent/ ./internal/ui/chat/ -count=1` exit 0
- [x] Docs (`docs/storage.md`) match the real schema and indexes
- [x] No silent orphan child sessions after `DeleteSession` - `TestChildSessionsCascadeOnParentDelete` exit 0
- [ ] Optional: run Migrate on a copy of a large live project DB and confirm resume list
