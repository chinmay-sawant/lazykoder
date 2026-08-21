# v0.0.10 / Phase 1 - Settings and recap records

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1 day
> **Priority:** P0
> **Gate:** a fresh or legacy settings file has a stable recap configuration,
> and the database can reserve one recoverable artifact record for a source message.

## Overview

Give recaps their own persisted contract before any background work begins.
The configuration must not inherit normal child-agent enablement or model
overrides. The database must be able to reserve a job before a worker starts,
then show exactly what happened after a restart.

## Executive summary

The settings change is backward compatible and defaults to off. The database
change adds a small `recap_records` ledger that is the source of truth for
deduplication, worker status, source times, and the written artifact manifest.
SQLite remains the single writer.

## Phase 1: Settings and durable state

### 1.1 Recap settings contract

- [ ] Add `settings.Recap` in `internal/settings/settings.go` with
      `Enabled bool` (`json:"enabled"`) and `Model string` (`json:"model"`).
      Keep it separate from `settings.Agents`.
- [ ] Add `Recap` to `settings.Settings` as `json:"recap"`; `Default()` must
      set `enabled: false` and `model: "deepseek-v4-flash"`.
- [ ] Normalize whitespace and an empty recap model back to
      `deepseek-v4-flash`. Do not validate against a stale model cache during
      file load.
- [ ] Extend `NormalizeAfterLoad` so settings files with no `recap` object
      become recap-disabled with the DeepSeek model. A legacy missing key must
      never silently enable model calls.
- [ ] Add `EffectiveRecap() Recap` beside the existing effective-setting
      helpers. It is the only runtime read of the recap setting.
- [ ] Test defaults, a missing legacy block, invalid/empty model cleanup, and
      save-load round trips in `internal/settings/settings_test.go`.

### 1.2 Durable recap ledger

- [ ] Add migration 12 under `internal/db` for `recap_records`. Define:

      ```text
      id                     TEXT PRIMARY KEY       // rec_<16 hex>
      session_id             TEXT NOT NULL FK sessions ON DELETE CASCADE
      source_start_seq       INTEGER NOT NULL
      source_end_seq         INTEGER NOT NULL
      source_start_time      INTEGER NOT NULL        // Unix milliseconds
      source_end_time        INTEGER NOT NULL        // Unix milliseconds
      source_end_message_id  TEXT NOT NULL
      model                  TEXT NOT NULL
      artifacts_json         TEXT NOT NULL           // path and SHA-256 by artifact type
      status                 TEXT NOT NULL          // queued|running|completed|failed|cancelled
      attempts               INTEGER NOT NULL
      error                  TEXT nullable
      time_created           INTEGER NOT NULL
      time_started           INTEGER nullable
      time_finished          INTEGER nullable
      ```

- [ ] Add a unique index on `(session_id, source_end_message_id)` and indexes
      for open records and per-session source order. Model duplicate inserts
      as a harmless already-reserved result.
- [ ] Add `db.RecapRecord`, `db.RecapArtifacts`, and narrow Store methods
      for reserve, claim, complete, fail, cancel, list open, and list records
      after a session sequence. Validate the artifact JSON at the database
      boundary and keep status transitions in one package.
- [ ] Preserve recap files if a user deletes a session. Deleting the session
      cascades the SQLite ledger only. The knowledge base is project memory,
      not a session-owned cache.
- [ ] Test migration from the current schema, uniqueness, artifact-manifest
      validation, status transitions, and record ordering in `internal/db`
      tests.

## Dependencies

- Current settings save/load compatibility behavior
- Current `db.NewID` prefix convention and migration ordering

## Closure gate

- [ ] `go test ./internal/settings ./internal/db -count=1` exits 0.
