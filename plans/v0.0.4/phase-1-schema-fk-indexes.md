# Phase 1 - Schema FK + index integrity

> **Track:** `plans/v0.0.4/`
> **Status:** implemented (migrations 6-8 landed 2026-08-16)
> **Code today:** `internal/db/store.go`, `queries.go`, `subagent_jobs.go`,
> `models.go`
> **Live DB checked:** project `.lazykoder/lazykoder.db` (87 main, 61
> subagent sessions, 1290 messages; `subagent_jobs` not yet applied on that
> file until app restarts migrate)

---

## 1. Audit findings (current state)

### 1.1 Tables

| Table | Purpose |
| --- | --- |
| `schema_migrations` | Numbered migration ledger |
| `sessions` | Main + subagent conversations |
| `messages` | User/assistant turns per session |
| `parts` | Text, reasoning, step, tool blocks |
| `tool_calls` | Tool payload 1:1 with tool parts |
| `subagent_jobs` | Durable task handles (`sub_...`) - migration 5 |

### 1.2 Foreign keys that already exist

Enabled at open: DSN + `PRAGMA foreign_keys = ON`.

| Child | Column | Parent | On delete |
| --- | --- | --- | --- |
| `messages` | `session_id` | `sessions(id)` | CASCADE |
| `parts` | `message_id` | `messages(id)` | CASCADE |
| `tool_calls` | `part_id` | `parts(id)` | CASCADE |
| `subagent_jobs` | `parent_session_id` | `sessions(id)` | CASCADE |

Verified on live DB via `PRAGMA foreign_key_list(...)` for messages/parts/
tool_calls. Orphan counts for those chains were **0** on the sample DB.

### 1.3 Foreign keys that are missing

| Child | Column | Should reference | Risk |
| --- | --- | --- | --- |
| `sessions` | `parent_session_id` | `sessions(id)` | Orphan subagent sessions if parent deleted without the manual two-step delete |
| `subagent_jobs` | `child_session_id` | `sessions(id)` | Dangling id after child purge; list/log broken |
| `subagent_jobs` | `parent_part_id` | `parts(id)` | Weak link to the parent `task` tool card |

**Why parent session FK was never added:** `parent_session_id` was introduced
in migration 4 via `ALTER TABLE`. SQLite cannot add a FK constraint with a
simple `ALTER TABLE ... ADD CONSTRAINT` on existing tables; it needs a table
rebuild (`CREATE new` / `INSERT` / `DROP old` / `RENAME`) or a fresh create
path only for new DBs.

**Recommended ON DELETE for each missing link:**

| Link | ON DELETE | Rationale |
| --- | --- | --- |
| `sessions.parent_session_id` | `CASCADE` | Deleting a main session should remove its subagent child sessions (same as `DeleteSession` today) |
| `subagent_jobs.child_session_id` | `SET NULL` | Job row may outlive a child session wipe for audit of summary; summary stays on the job |
| `subagent_jobs.parent_part_id` | `SET NULL` | Parent tool part can be gone while job history remains |

Optional later (not required for v1 of this track):

- `parts.tool_call_id` is a provider id string, not a local FK - **do not**
  FK to `tool_calls`.
- CHECK constraints for `sessions.kind IN ('main','subagent')`,
  `messages.role`, `parts.type`, job `status` enums - useful but separate
  from FKs.

### 1.4 Indexes that already exist

| Index | Columns | Notes |
| --- | --- | --- |
| `idx_messages_session_seq` | `(session_id, seq)` | Hot: `ListMessages` |
| `idx_parts_message_seq` | `(message_id, seq)` | Hot: `ListParts` |
| `idx_parts_type` | `(type)` | Low selectivity; optional keep |
| `idx_parts_tool` | partial `tool_name` | Partial index |
| `idx_tool_calls_tool` | `(tool)` | Rarely filtered alone |
| `idx_tool_calls_status` | `(status)` | Rarely filtered alone |
| `idx_sessions_updated` | `(time_updated DESC)` | Helps resume sort, not directory filter |
| `idx_sessions_parent` | partial `parent_session_id` | Exists but child list often uses `kind` first |
| `idx_sessions_kind` | `(kind)` | Low selectivity alone |
| `idx_subagent_jobs_parent` | `(parent_session_id)` | Hot: `ListSubagentJobs` |
| `idx_subagent_jobs_status` | `(status)` | Helps open-job scan |

### 1.5 Index / query plan gaps

From `EXPLAIN QUERY PLAN` on the sample DB:

| Query (store API) | Predicate / order | Plan today | Gap |
| --- | --- | --- | --- |
| `ListSessionsByDir` | `directory = ?` AND kind main, `ORDER BY time_updated DESC, time_created DESC, id` | Uses `idx_sessions_updated`, **scans**, temp B-tree for rest of ORDER BY | No `directory` leading index |
| `ListChildSessions` | `parent_session_id = ? AND kind = 'subagent'`, order by `time_updated` | Used `idx_sessions_kind` then temp sort | Prefer composite on parent + kind + updated |
| `ListMessages` | `session_id`, `ORDER BY seq` | Good (`idx_messages_session_seq`) | OK |
| `ListParts` | `message_id`, `ORDER BY seq` | Good | OK |
| `ListToolCalls` | join messages by session, order parts.seq | Good joins | OK |
| `ListOpenSubagentJobs` | `status IN ('queued','running')` | Status index | Prefer composite `(status, time_created)` if open set grows |
| `ListSubagentJobs` | parent, order started/created | Parent index | Prefer composite `(parent_session_id, time_started, id)` |

### 1.6 Uniqueness gaps

| Constraint | Why |
| --- | --- |
| `UNIQUE(messages.session_id, seq)` | Prevents duplicate seq after race or bug |
| `UNIQUE(parts.message_id, seq)` | Same for parts |
| Optional `UNIQUE(sessions.id)` already PK | OK |

Today seq is `MAX(seq)+1` in a transaction under `MaxOpenConns(1)`, so races
are unlikely, but uniqueness is still the correct integrity backstop.

### 1.7 Delete / cascade behavior today

`DeleteSession`:

1. `DELETE FROM sessions WHERE parent_session_id = ?` (children first)
2. `DELETE FROM sessions WHERE id = ?`

Messages/parts/tool_calls cascade from sessions via existing FKs.
`subagent_jobs` cascade from parent session when migration 5 is applied.
**Without** `sessions.parent_session_id` FK, step 1 is load-bearing.

### 1.8 Correction to the informal claim

> "We have not added any foreign keys or indexing."

**Not accurate.** Core chain FKs and most list indexes shipped in migration 1.
What is incomplete is the **second-generation graph** (session parent link,
subagent job child/part links) and a few **query-shaped indexes**.

---

## 2. Target schema (desired end state)

Logical model (only integrity-related deltas called out):

```text
sessions
  id PK
  parent_session_id  --> sessions(id) ON DELETE CASCADE   -- NEW FK
  kind  (main | subagent)
  directory, time_updated, ...

messages
  id PK
  session_id --> sessions ON DELETE CASCADE               -- already
  UNIQUE(session_id, seq)                                 -- NEW

parts
  id PK
  message_id --> messages ON DELETE CASCADE               -- already
  UNIQUE(message_id, seq)                                 -- NEW

tool_calls
  part_id PK --> parts ON DELETE CASCADE                  -- already

subagent_jobs
  id PK
  parent_session_id --> sessions ON DELETE CASCADE        -- already
  child_session_id  --> sessions ON DELETE SET NULL       -- NEW FK
  parent_part_id    --> parts ON DELETE SET NULL          -- NEW FK
```

### 2.1 Target indexes (add / replace)

**Add:**

| Index | Definition | Serves |
| --- | --- | --- |
| `idx_sessions_dir_kind_updated` | `(directory, kind, time_updated DESC, time_created DESC, id DESC)` or SQLite-legal equivalent without DESC in create if needed | `ListSessionsByDir` |
| `idx_sessions_parent_kind_updated` | `(parent_session_id, kind, time_updated DESC, time_created DESC)` | `ListChildSessions` |
| `idx_subagent_jobs_parent_started` | `(parent_session_id, COALESCE not allowed in index - use time_created, id)` or `(parent_session_id, time_started, id)` | `ListSubagentJobs` |
| `idx_subagent_jobs_open` | partial `WHERE status IN ('queued','running')` on `(status, time_created, id)` | `ListOpenSubagentJobs` / Recover |

**Keep:**

- `idx_messages_session_seq`, `idx_parts_message_seq` (or replace with UNIQUE indexes that also serve ORDER BY)
- PK autoindexes

**Review / maybe drop after EXPLAIN on real workloads:**

- `idx_parts_type` (low selectivity)
- `idx_tool_calls_tool`, `idx_tool_calls_status` if unused in store API
- standalone `idx_sessions_kind` if composite parent/kind covers it
- standalone `idx_sessions_updated` if directory composite covers resume list

Do not drop until a gate shows the new indexes win and nothing regresses.

### 2.2 Unique indexes (add)

```sql
CREATE UNIQUE INDEX idx_messages_session_seq_uq ON messages(session_id, seq);
CREATE UNIQUE INDEX idx_parts_message_seq_uq ON parts(message_id, seq);
```

If the non-unique indexes already exist with the same columns, migrate by
dropping the old index and creating the unique one (same covering effect).

---

## 3. Migration strategy (SQLite realities)

SQLite limitations that drive the plan:

1. Cannot `ADD CONSTRAINT FOREIGN KEY` on an existing table.
2. Adding a FK on `sessions` requires table rebuild.
3. Existing DBs may already have rows; rebuild must copy all columns.
4. FKs must be enabled during rebuild validation (`PRAGMA foreign_key_check`).

### 3.1 Recommended migration versioning

| Version | Content |
| --- | --- |
| 6 | New indexes only (safe, no rebuild): directory composite, parent/kind composite, subagent job composites/partial; unique seq indexes after duplicate check |
| 7 | Rebuild `sessions` with `parent_session_id REFERENCES sessions(id) ON DELETE CASCADE` |
| 8 | Rebuild `subagent_jobs` (or recreate if few rows) with child + parent_part FKs |

Split 6/7/8 so a failure mid-way is diagnosable and so index wins land even
if rebuild needs a longer bake.

### 3.2 Preflight data repair (before FK rebuilds)

Run as part of migration (or a one-shot repair helper):

```sql
-- Orphan child sessions (parent missing): reparent or delete
-- Prefer: DELETE child sessions whose parent_session_id is non-null and missing
-- (product choice: delete is consistent with CASCADE intent)

-- subagent_jobs.child_session_id pointing at missing sessions -> SET NULL
-- subagent_jobs.parent_part_id pointing at missing parts -> SET NULL
```

Gate: `PRAGMA foreign_key_check` returns zero rows after migration.

### 3.3 Sessions rebuild sketch

```sql
CREATE TABLE sessions_new (
  id TEXT PRIMARY KEY,
  ...
  parent_session_id TEXT REFERENCES sessions_new(id) ON DELETE CASCADE,
  kind TEXT NOT NULL DEFAULT 'main'
);
INSERT INTO sessions_new SELECT ... FROM sessions;
-- drop dependents? With FKs from messages -> sessions, need deferred or
-- disable FKs carefully during swap, or rebuild in dependency order.
DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;
-- recreate indexes
```

**Careful:** other tables reference `sessions(id)`. Swap strategy options:

**A. FK off during swap (pragmatic):**

1. `PRAGMA foreign_keys=OFF` inside the migration transaction (note: SQLite
   FK enforcement is connection-level; document that migrate owns the conn).
2. Rebuild sessions, re-enable FKs, run `foreign_key_check`.

**B. Cascade rebuild chain (heavy):** rebuild messages/parts/tool_calls too -
avoid unless required.

Prefer **A** with a single connection and tests that assert FKs on after
`Open`.

### 3.4 Application code changes after FKs

| Area | Change |
| --- | --- |
| `DeleteSession` | Can simplify to single `DELETE FROM sessions WHERE id = ?` if parent FK CASCADE removes children; keep explicit child delete only if we want ordered cleanup logs |
| `UpsertSubagentJob` | Ensure parent session exists before insert (already required by parent FK); child session may be NULL until runner creates it |
| Runner | Creating child session then updating job must leave no window that breaks if child insert fails (already sequential) |
| Tests | Add FK violation cases; cascade delete cases; unique seq cases |

---

## 4. Implementation checklist

### 4.1 Audit packaging (this phase file)

- [x] Document existing FKs and indexes
- [x] Document missing FKs and index gaps
- [x] Document EXPLAIN findings on sample DB
- [x] Target schema + migration split
- [x] User asked to implement the checklist

### 4.2 Migration 6 - indexes + uniqueness

- [x] Create unique seq indexes (replace non-unique with UNIQUE same name)
- [x] Create `idx_sessions_dir_kind_updated`
- [x] Create `idx_sessions_parent_kind_updated`
- [x] Create improved subagent_jobs indexes (parent_started + open partial)
- [x] Gate: `TestSchemaHasIntegrityIndexes`, `TestUniqueMessageSeq` exit 0
- [x] Gate: `go test ./internal/db/ -count=1` exit 0

### 4.3 Migration 7 - sessions parent FK

- [x] Preflight orphan parent_session_id cleanup (delete orphan children)
- [x] Rebuild sessions with self-FK CASCADE (`migrate_rebuild.go`)
- [x] Recreate all sessions indexes after rename
- [x] Simplify `DeleteSession` to single DELETE by id
- [x] Gate: `TestForeignKeysEnabled`, `TestParentSessionFKRejectsOrphan` exit 0
- [x] Gate: `TestChildSessionsCascadeOnParentDelete` exit 0

### 4.4 Migration 8 - subagent_jobs FKs

- [x] Preflight null-out bad child_session_id / parent_part_id
- [x] Rebuild table with FKs (SET NULL on child/part)
- [x] Gate: `TestSubagentJobChildFKSetNull` exit 0
- [x] Gate: `TestParentDeleteRemovesSubagentJobs` exit 0
- [x] Gate: durable manager tests still pass (`./internal/subagent`)

### 4.5 Docs and hygiene

- [x] Update `docs/storage.md` schema + index list
- [x] Manager soft-retries optional FKs on persist (stale part/session ids)
- [ ] Consider dropping unused low-value indexes only after EXPLAIN proof (deferred)

---

## 5. Test plan

| Test | Asserts |
| --- | --- |
| `TestMigrateIdempotent` | version count includes 6-8; tables present |
| `TestForeignKeysEnabled` | `PRAGMA foreign_keys` = 1 after Open |
| `TestMessagesCascadeOnSessionDelete` | already-ish; keep |
| `TestChildSessionsCascadeOnParentDelete` | children gone with single parent delete after migration 7 |
| `TestParentSessionFKRejectsOrphan` | insert with bad parent_session_id fails |
| `TestSubagentJobChildFKSetNull` | delete child session nulls job.child_session_id |
| `TestUniqueMessageSeq` | second insert same seq fails |
| `TestListSessionsByDirUsesDirIndex` | optional: `EXPLAIN QUERY PLAN` string contains new index name |
| Manager durable tests | still green with FKs on |

Manual:

- Open existing project DB once after upgrade; resume list still works.
- Spawn sub-agent, restart app, `task_list` still shows job; no FK errors in logs.

---

## 6. Risks

| Risk | Mitigation |
| --- | --- |
| Table rebuild corrupts live DB | Migrate in a transaction; backup note in docs; test on copy of real DB |
| Self-referential FK on sessions + insert order | Insert parents before children (already true for main then subagent) |
| Unique seq fails on dirty data | Preflight query; repair duplicates before unique index |
| SET NULL child_session_id hides bugs | Keep summary on job; UI falls back to summary when session missing |
| Over-indexing slows writes | Prefer few composite indexes matching real queries; drop unused after measure |

---

## 7. Effort estimate

| Slice | Effort |
| --- | --- |
| Migration 6 indexes + uniques + tests | small (half day) |
| Migration 7 sessions rebuild + delete tests | medium (half to one day) |
| Migration 8 subagent_jobs FKs + durable tests | small-medium |
| Docs + EXPLAIN gates | small |

Total: about 1-2 focused sessions if preflight is clean.

---

## 8. Decision log

| Decision | Choice | Date |
| --- | --- | --- |
| parent_session_id ON DELETE | CASCADE | 2026-08-16 |
| child_session_id ON DELETE | SET NULL | 2026-08-16 |
| parent_part_id ON DELETE | SET NULL | 2026-08-16 |
| Simplify DeleteSession to one DELETE | Yes | 2026-08-16 |
| Drop idx_parts_type / tool_calls status indexes | Deferred | 2026-08-16 |
| Persist when optional FK missing | Retry upsert without optional cols | 2026-08-16 |

---

## 9. Explicit non-goals this phase

- No new dependency
- No multi-writer pool
- No full SQL schema dump as one JSON blob
- No change to id prefixes (`ses_`, `msg_`, `prt_`, `sub_`)
- No todos table (that remains later product work)

---

## 10. Sign-off

- [x] CASCADE for child sessions on parent delete
- [x] SET NULL for job child_session_id / parent_part_id
- [x] Implementation complete (migrations 6-8 + tests + docs)
