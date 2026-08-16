# v0.0.1 / Phase 5 - Go pattern database (lint and improvisation patterns)

> **Parent:** `plans/v0.0.1/README.md` - workspace and db conventions
> **Status:** planned 2026-08-16 (no rows landed; mark `[x]` only when the gate passes)
> **Estimated effort:** 1-2 days
> **Priority:** P1 (user requested; independent of phase-4, may land in either order)
> **Gate:** a language-specific SQLite database separate from `.lazykoder/lazykoder.db` stores Go lint/improvisation patterns with the 7-field shape, seeded with at least one bad and one good example; the stored Python scripts can be run locally

---

## Overview

The chat database (`.lazykoder/lazykoder.db`) is for sessions and messages. Lint and improvisation knowledge for Go needs its own store: a per-language pattern database in the same workspace directory but a separate file. Each pattern entry has the user-defined shape:

1. The snippet (small illustrative example)
2. The short line description
3. The actual snippet we got (real occurrence found in code)
4. The Python script we can use locally to replace the pattern
5. The pattern (the match rule)
6. The Python script we can execute
7. The performance (classified as constant or non-constant; the entry itself is a bad practice or a good practice)

For now the practice classification covers exactly two types: **bad practice** and **good practice**. No other categories in 0.0.1.

Decisions recorded 2026-08-16 (user): "these two" = bad + good practice types; DB lives in the project `.lazykoder/`, not a global home directory.

## Executive Summary

- New file `.lazykoder/patterns-go.db` (SQLite, same `modernc.org/sqlite` driver, same WAL pragmas as the chat db). One db file per language: `patterns-go.db` today, `patterns-<lang>.db` later. The `language` column pins entries to their language even inside a per-language file.
- New package `internal/patterns` owns schema, store, and validation. It is opened on demand (lazy) like the main db, never on the hot path of a chat turn.
- The seven fields map 1:1 to columns; items 4 and 6 are both stored as full Python source text. Item 4 is the replacement logic for local use; item 6 is the runnable script (same body initially, separate column so they can diverge).
- Python execution is a new tooling dependency: running `python3` is a project-policy change. The plan stores scripts and proves execution through the existing bash tool gate (policy `Classify` applies), and asks for user sign-off before any python subprocess runs in the agent loop.
- Seed content: one canonical bad/good pair as the first rows (regex compile per call vs compiled once), demonstrating the constant/non-constant split.

## 5.1 Database file and schema (P0)

- [ ] New package `internal/patterns` with `Open(dir string) (*Store, error)` that creates `<cwd>/.lazykoder/patterns-go.db` when missing - same pattern as `internal/db.Open`; file exists after first call, `go test ./internal/patterns` exit 0
- [ ] Own `schema_migrations` inside the patterns db (version 1), independent of the chat db's migration counter - the two databases never share migration numbers
- [ ] `patterns` table columns (all seven user fields + bookkeeping):
  - `id TEXT PRIMARY KEY` (prefix `pat_` + `NewID` style suffix)
  - `language TEXT NOT NULL DEFAULT 'go'`
  - `description TEXT NOT NULL` (the short line description)
  - `snippet TEXT NOT NULL` (small illustrative example)
  - `actual_snippet TEXT NOT NULL DEFAULT ''` (the real snippet we got; empty until a real occurrence is filed)
  - `pattern TEXT NOT NULL` (the match rule)
  - `replace_script TEXT NOT NULL` (Python, local replacement, item 4)
  - `execute_script TEXT NOT NULL` (Python, executable, item 6)
  - `performance TEXT NOT NULL CHECK (performance IN ('constant','non-constant'))`
  - `practice TEXT NOT NULL CHECK (practice IN ('bad','good'))`
  - `source TEXT` (file path where the actual snippet was found, nullable)
  - `time_created INTEGER NOT NULL`, `time_updated INTEGER NOT NULL`
- [ ] Same pragmas as the chat db: `journal_mode = WAL`, `foreign_keys = ON`, `busy_timeout = 5000` - set on every connection
- [ ] Index `idx_patterns_practice ON patterns(practice)` and `idx_patterns_performance ON patterns(performance)` - the two filters this phase's UI needs
- [ ] Test: open on a temp dir creates `patterns-go.db`, schema version 1, the `patterns` table exists with all 12 columns - `go test ./internal/patterns` exit 0

## 5.2 Store API (P0)

- [ ] `InsertPattern(ctx, Pattern) (id, error)` validates before writing: non-empty `description`, `snippet`, `pattern`, both scripts; `performance` and `practice` must match the CHECK enums; returns a typed error naming the field otherwise
- [ ] `ListPatterns(ctx, filter)` with filters `practice` (bad|good|any) and `performance` (constant|non-constant|any) - the two classifications from item 7 are the only filters in 0.0.1
- [ ] `GetPattern(ctx, id)`, `UpdatePattern(ctx, Pattern)` (full row replace, bumps `time_updated`), `DeletePattern(ctx, id)` - CRUD parity with `internal/db`
- [ ] `language` is constrained to `go` at the store layer for 0.0.1 (`InsertPattern` rejects anything else) so the per-language db cannot silently hold mixed-language rows - later languages get their own db file and this check broadens per file
- [ ] Test: insert + list round-trip with both filters; invalid `practice` value rejected with the field name; `language: python` rejected - exit 0

## 5.3 The seven fields: mapping and validation (P0)

- [ ] `snippet` = minimal self-contained Go example (few lines, runnable-ish); `actual_snippet` = verbatim occurrence from real code, allowed to be longer; both stored as text, no syntax validation in Go 1.26 (a broken actual snippet must still be storable when a scan finds one)
- [ ] `pattern` = the match rule in a documented form (regexp string for now; a `Pattern` struct with `Regexp string` is enough, no AST parsing in 0.0.1) - regex must compile at insert time or the insert fails with the compile error
- [ ] `description` = one short line (<= 120 runes), enforced at insert - keeps list views readable
- [ ] Items 4 and 6: `replace_script` is the script to run locally that replaces whatever matched the pattern; `execute_script` is the runnable script. Both stored as Python 3 source strings; initial seeds use identical bodies and the plan notes the columns may diverge later (replace = inline repo fix, execute = standalone standalone file run) - the plan does not unify them, the schema keeps both per the user shape
- [ ] Test: insert with an uncompilable regex fails; a 200-rune description fails; both-scripts-equal and both-scripts-different cases insert cleanly - exit 0

## 5.4 Python scripts: storage and execution (P1)

- [ ] Scripts are stored only in the db (never mirrored as loose `.py` files at rest); a `scripts/`-committed helper `dump-patterns.py` (or Go `cmd` in `scripts/`) extracts one or all patterns to `.py` files for local inspection - throwaway-tooling rule: the extractor is committed, not an inline one-liner
- [ ] Execution contract: running a stored script goes through the existing bash tool and `internal/policy` (a `python3 <script>` command is a normal bash command; `rm`-class rules still apply unchanged) - no new executor package in this phase
- [ ] User sign-off needed (AGENTS.md dependency policy): python3 as a runtime for pattern scripts. This phase only stores + extracts scripts and proves execution via a fixture script through the bash gate; no python subprocess is spawned from the agent loop without this sign-off - recorded here, pending
- [ ] Test: fixture pattern with a script that replaces `strings.ReplaceAll`-style text on a temp file runs through the bash tool gate (Allow case) and the file content changes; a script containing `rm` triggers the confirm path like any other command - exit 0

## 5.5 Practice and performance classification (P0, the two types)

- [ ] `practice IN ('bad','good')` is the only classification axis for 0.0.1 - exactly the two types the user scoped; no neutral/other bucket
- [ ] `performance IN ('constant','non-constant')` records the expected runtime class of the pattern, per item 7 ("as it should be constant"): a good practice row can still be marked non-constant if the truth says so; classification is per-row evidence, not an opinion column - the description text carries the why
- [ ] Bad + constant, bad + non-constant, good + constant, good + non-constant: all four combinations are valid inserts (the CHECK constraints above already allow all four) - test proves all four land
- [ ] List default order: `practice` asc, then `performance` asc, then `time_created` - stable, deterministic output for tests

## 5.6 Seed data (P1)

- [ ] Seed the first bad/good pair via a committed seed script in `scripts/` (or `go run ./cmd/seedpatterns`), not by hand-editing SQLite - one canonical example: `regexp.MustCompile` inside a hot function (bad, non-constant, compile per call) vs package-level compiled regexp (good, constant, compile once)
- [ ] Both seeds carry a real `snippet`, a one-line `description`, a compile-able `pattern`, and both Python scripts (item 4 replaces the bad form in a file, item 6 prints/validates the file for the good form) - scripts follow the fixture style used by `internal/tools` tests
- [ ] Seed is idempotent: re-running does not duplicate rows (upsert on `description` unique per language or explicit skip when the description exists) - `scripts/` README documents the command
- [ ] Test: seed twice -> same row count; `ListPatterns(practice=bad)` returns exactly the bad seed - exit 0

## 5.7 TUI surface (P2, minimal)

- [ ] `/patterns` command lists patterns in the status/transcript area (description, practice, performance) - list-only read view; editing stays in SQLite/seed for 0.0.1
- [ ] The list renders `bad`/`good` and `constant`/`non-constant` as styled labels consistent with the existing muted/error palette - no new widget, reuse viewport/list conventions from the transcript
- [ ] Test: chat test issues `/patterns` with two seeded rows, `View()` contains both descriptions and both classification labels - exit 0

## Dependencies

- Needs: Phase 1 `internal/db` conventions (driver, pragmas, migration pattern), Phase 2 `internal/policy` for the python execution gate
- Blocks: nothing in 0.0.1; later languages create `patterns-<lang>.db` files rather than new tables
- New dependencies: none in Go (same `modernc.org/sqlite`). Python 3 runtime is a tooling dependency for pattern scripts and needs explicit user sign-off (5.4) before the agent loop runs any stored script

## Closure gates

- [ ] `go test ./... -count=1` passes on a rebuilt tree (record exit code) - pending
- [ ] `go vet ./...` passes - pending
- [ ] `.lazykoder/patterns-go.db` created on a clean cwd, separate file from `lazykoder.db`, schema version 1 - pending
- [ ] Seven-field insert/list round-trip with the two filters (`practice`, `performance`) and all four bad/good x constant/non-constant combinations - pending
- [ ] Seed script run twice yields the same row count (idempotent), one bad and one good example present - pending
- [ ] A stored Python script executes through the bash gate and changes a fixture file; python sign-off row in 5.4 is either approved (with date) or the gate stays `[~]` deferred - pending

Manual checks (need a live terminal): `/patterns` view readable on a full screen; extractor writes a valid `.py` file the user can run with `python3`.
