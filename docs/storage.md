# Storage

One small SQLite database per project at `<cwd>/.lazykoder/lazykoder.db`,
driven by `modernc.org/sqlite` (pure Go, no CGO) through `database/sql`. The
schema is normalized; the store is the single source of truth for sessions,
messages, parts and tool runs.

## Pragmas and migrations

- `journal_mode = WAL`, `foreign_keys = ON`, `busy_timeout = 30000`,
  `synchronous = NORMAL` on open.
- **`SetMaxOpenConns(1)`** (and max idle 1): SQLite allows one writer; a
  multi-connection pool is what produced `database is locked (SQLITE_BUSY)`
  when parent and sub-agents wrote in parallel. One connection serializes
  all store access safely for concurrent agents.
- Numbered migrations recorded in `schema_migrations` (version 1 creates the
  full schema; later versions alter or rebuild). Current version: **15**.
  `Migrate` is idempotent.

## Schema

```sql
sessions(id TEXT PK, title, directory, provider, model, variant,
         time_created, time_updated, time_active, status,
         parent_session_id -> sessions ON DELETE CASCADE,
         kind TEXT NOT NULL DEFAULT 'main',
         status_segments TEXT NOT NULL DEFAULT '["model","variant","tokens","cache","cost","tps","subs","models","scroll","prompt"]')
messages(id TEXT PK, session_id -> sessions ON DELETE CASCADE,
         role, agent, provider_id, model_id, variant, time_created, seq,
         UNIQUE(session_id, seq))
parts(id TEXT PK, message_id -> messages ON DELETE CASCADE,
      type, time_created, seq, text, time_start, time_end, finish_reason,
      tokens_total, tokens_input, tokens_output, tokens_reasoning,
      tokens_cache_read, tokens_cache_write, cost,
      tool_name, tool_call_id, tool_status,
      UNIQUE(message_id, seq))
tool_calls(part_id TEXT PK -> parts ON DELETE CASCADE,
           tool, call_id, status, title, time_start, time_end, exit_code,
           input_json, output, metadata_json)
subagent_jobs(id TEXT PK,
              parent_session_id -> sessions ON DELETE CASCADE,
              parent_part_id -> parts ON DELETE SET NULL,
              child_session_id -> sessions ON DELETE SET NULL,
              name, role, status, prompt, description, model, variant,
              max_steps, timeout_ms, summary, error,
              time_created, time_updated, time_started, time_finished)
todos(session_id -> sessions ON DELETE CASCADE, seq, content, status,
      time_updated, PRIMARY KEY(session_id, seq))
recap_records(id TEXT PK,
              session_id -> sessions ON DELETE CASCADE,
              source_start_seq, source_end_seq, source_start_time,
              source_end_time, source_end_message_id, model,
              artifacts_json, status, attempts, error,
              time_created, time_started, time_finished,
              UNIQUE(session_id, source_end_message_id))
memory_updates(id TEXT PK,
               workdir, source_session_id, source_end_seq,
               source_end_message_id, model, status, attempts, sha256, error,
               time_created, time_started, time_finished, stage_durations_json,
               UNIQUE(workdir, source_session_id, source_end_message_id))
```

Indexes (hot paths first):

- `UNIQUE messages(session_id, seq)`, `UNIQUE parts(message_id, seq)`
- `sessions(directory, kind, time_active DESC, …)` - resume list
- `sessions(parent_session_id, kind, time_updated DESC, …)` - child drawer
- `sessions(time_updated DESC)`, partial parent, `kind`
- `parts(type)`, partial `tool_name`; `tool_calls(tool)`, `(status)`
- `subagent_jobs(parent_session_id)`, `(status)`,
  `(parent_session_id, time_started, …)`,
  partial open jobs `WHERE status IN ('queued','running')`

`kind=subagent` sessions are hidden from `ListSessionsByDir` / resume.
Deleting a parent session cascades to child sessions (self-FK), their
messages/parts/tools, and durable `subagent_jobs`. Deleting only a child
session nulls `subagent_jobs.child_session_id` but keeps the job summary.
Child messages set `messages.agent` to the sub-agent name.
A parent compact turn sets `messages.agent` to `compaction` and writes
one `parts.type = compaction` row. That is not a schema migration.

Schema version is 15 (`schema_migrations`). Migrations 7-8 rebuild tables
to add FKs SQLite cannot express with `ALTER TABLE`; migration 9 adds the
session todo table, migration 10 adds the additive footer segment column, and
migration 11 expands legacy footer visibility into the status drawer fields.
Migration 12 adds the recap reservation and artifact lifecycle ledger. Migration
13 adds the project-scoped memory update ledger. Migration 14 adds the JSON
stage-duration map used to diagnose memory update latency. Migration 15 adds
`sessions.time_active`, backfilled from `time_updated`, for conversation
ordering. Durations use microseconds and stay in the same row as the update
status and error.

`recap_records` is keyed by the main session and newest source message. It
retains source times for audit only. Queued and interrupted running rows are
resumed when a session is reopened with recaps enabled. Artifact files remain
under `knowledge-base/recaps/` even if the session is deleted.

`memory_updates` is keyed by the project workdir, main session, and newest
source message. It has no foreign key to sessions because `memories.md` is a
project aggregate that survives session deletion. Queued and interrupted rows
are claimed again on startup. Completed rows store the SHA-256 digest of the
atomically written `knowledge-base/memories.md` file.
The worker also records claim, snapshot, aggregate-read, related-evidence,
provider, repair, merge/write, no-op, and total durations in microseconds.

`subagent_jobs` is the durable task registry: spawn/status/finish are
upserted so `task_list`, `task_status`, and `task_wait` still work after a
process restart. Open (`queued`/`running`) rows are resumed on startup via
`Manager.Recover`.

## Conventions

- Ids are readable prefixes plus 16 hex chars from crypto/rand:
  `ses_`, `msg_`, `prt_`; `tool_calls.part_id` equals the owning part id.
- Timestamps are Unix milliseconds.
- `sessions.time_created` is set once on insert. `sessions.time_updated`
  tracks general activity for age labels and the child drawer. It changes
  when a message is inserted, when visibility or session metadata changes,
  and when `TouchSession` runs. `sessions.time_active` tracks conversation
  activity and changes when a message or part is inserted. The main resume
  list orders by `time_active`, so background activity does not reorder chats.
- `seq` increments per parent (messages per session, parts per message),
  computed as MAX+1 inside a transaction.
- Nullable columns are stored as NULL, never empty strings.

## Part types

| `parts.type` | Stored fields |
| --- | --- |
| `text` | `text`, optional times |
| `reasoning` | `text`, times |
| `step-start` | marker only |
| `step-finish` | `finish_reason`, token counts, `cost` |
| `tool` | `tool_name` + a row in `tool_calls` |
| `compaction` | `text` is a JSON envelope (plain text is treated as summary-only) |

Compaction envelope fields: `summary`, `tail_start_message_id`,
`from_model`, `to_model`, `from_window`, `to_window`, `reason`
(`auto` / `overflow` / `model-shrink` / `manual`), `tokens_after`
(estimated fill of summary + kept tail). Rows stay in SQLite; only
`buildHistory` starts at the latest checkpoint. `messages.visible` is
the TUI hide bit and is not used as a compact flag.

## Store API

`CreateSession`, `InsertMessage` (bumps session `time_updated` and
`time_active`), `InsertPart` (bumps `time_active`),
`UpdatePartText` (streamed reasoning/text growth),
`InsertToolCall`, `UpdateToolCall` (upsert + part status),
`TouchSession`, `DeleteSession`, `ListMessages`, `ListParts`,
`ListSessionsByDir` (conversation activity latest first),
`ListChildSessions` (general activity latest first), `ListToolCalls` (per session),
`UpdateSessionModel` / `UpdateSessionVariant` (also bump `time_updated`),
`UpdateSessionSegments` (JSON footer visibility, also bumps `time_updated`),
`UpsertSubagentJob` / `GetSubagentJob` / `ListSubagentJobs` /
`ListOpenSubagentJobs` (durable sub-agent registry).

`ReserveRecap`, `GetRecap`, `ClaimRecap`, `RequeueRecap`, `CompleteRecap`,
`FailRecap`, `CancelRecap`, `ListOpenRecaps`, and `ListRecapsAfter` manage the
recap lifecycle. `source_end_seq` and `source_end_message_id` identify a
window; timestamps are metadata and do not sort artifacts.

`ReserveMemoryUpdate`, `GetMemoryUpdate`, `ClaimMemoryUpdate`,
`RequeueMemoryUpdate`, `CompleteMemoryUpdate`, `FailMemoryUpdate`,
`ListOpenMemoryUpdates`, and `ListMemoryUpdatesForRecovery` manage the
per-request memory lifecycle. The unique source anchor makes retries
idempotent. Recovery includes queued or interrupted rows and legacy
insufficient-context failures; provider and validation failures remain
recorded for diagnosis and are retried by a later source event.

`ReplaceTodos` replaces all rows for one session in one transaction and
assigns `seq` from zero. `ListTodos` returns the rows in display order.

## Tool call lifecycle

A tool call starts as a `parts` row (`tool_status=pending`) plus a
`tool_calls` row (`status=pending`, `input_json` = raw tool arguments). After
execution or denial the rows are updated in place: `status` (`completed`,
`denied`, `error`), `output`, `exit_code`, start/end times, and tool-specific
`metadata_json` (diff, answers, byte counts, truncated flags). Denied calls
keep `exit_code` NULL.
