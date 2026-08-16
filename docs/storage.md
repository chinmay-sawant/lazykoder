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
  full schema; later versions alter). `Migrate` is idempotent.

## Schema

```sql
sessions(id TEXT PK, title, directory, provider, model, variant,
         time_created, time_updated, status,
         parent_session_id TEXT, kind TEXT NOT NULL DEFAULT 'main')
messages(id TEXT PK, session_id -> sessions ON DELETE CASCADE,
         role, agent, provider_id, model_id, variant, time_created, seq)
parts(id TEXT PK, message_id -> messages ON DELETE CASCADE,
      type, time_created, seq, text, time_start, time_end, finish_reason,
      tokens_total, tokens_input, tokens_output, tokens_reasoning,
      tokens_cache_read, tokens_cache_write, cost,
      tool_name, tool_call_id, tool_status)
tool_calls(part_id TEXT PK -> parts ON DELETE CASCADE,
           tool, call_id, status, title, time_start, time_end, exit_code,
           input_json, output, metadata_json)
```

Indexes: `messages(session_id, seq)`, `parts(message_id, seq)`,
`parts(type)`, `parts(tool_name)` (partial), `tool_calls(tool)`,
`tool_calls(status)`, `sessions(time_updated DESC)`,
`sessions(parent_session_id)` (partial), `sessions(kind)`.

`kind=subagent` sessions are hidden from `ListSessionsByDir` / resume.
Deleting a parent session also deletes its child sessions. Child messages
set `messages.agent` to the sub-agent name.

## Conventions

- Ids are readable prefixes plus 16 hex chars from crypto/rand:
  `ses_`, `msg_`, `prt_`; `tool_calls.part_id` equals the owning part id.
- Timestamps are Unix milliseconds.
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

## Store API

`CreateSession`, `InsertMessage`, `InsertPart`, `UpdatePartText`
(streamed reasoning/text growth), `InsertToolCall`,
`UpdateToolCall` (upsert + part status), `DeleteSession`, `ListMessages`,
`ListParts`, `ListSessionsByDir` (latest first), `ListToolCalls` (per
session), `UpdateSessionModel` (model picker persistence).

## Tool call lifecycle

A tool call starts as a `parts` row (`tool_status=pending`) plus a
`tool_calls` row (`status=pending`, `input_json` = raw tool arguments). After
execution or denial the rows are updated in place: `status` (`completed`,
`denied`, `error`), `output`, `exit_code`, start/end times, and tool-specific
`metadata_json` (diff, answers, byte counts, truncated flags). Denied calls
keep `exit_code` NULL.
