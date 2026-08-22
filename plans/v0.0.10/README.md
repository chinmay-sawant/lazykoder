# v0.0.10 - Local recap, questions, recall, and durable memory

> **Parent:** Current chat, settings, SQLite, first-request agent path, and local knowledge base
> **Status:** recap and durable memory shipped; live TTY gate open 2026-08-22
> **Estimated effort:** recap baseline 6-8 days; memory extension 5-8 days
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** an enabled project writes ordered recap artifacts after a main
> turn, updates the durable memory aggregate after successful parent requests,
> then checks memory and recap evidence before the next user turn's first
> ordinary model request.

## Overview

Recaps are optional local memory. A completed main-chat turn starts a hidden
worker. It writes a recap, unresolved questions for a future agent, and
concrete things to avoid based on user corrections and tool failures.

The durable extension adds one curated project file at
`knowledge-base/memories.md`. It holds stable user preferences, project
decisions, things to avoid, open questions, recent context, and source links.
The file is a bounded aggregate, not a transcript. The application owns its
format and atomic writes. A hidden memory worker uses the configured recap
model, returns structured evidence, and changes the file only after code
validation.

The worker has its own setting. It defaults to `deepseek-v4-flash`, which is
the built-in new-session default and is present in the cached model catalog.
It must use that configured model and its catalog endpoint, not the live
`/model` selection and not `agents.model_override`.

Before the first ordinary model request for a later user turn, the parent
agent searches `knowledge-base/recaps/` with the existing internal `grep`
implementation. Matching lines enter that request as unpersisted, untrusted
historical hints. Tool follow-ups, `/continue`, child agents, and compaction
do not repeat the lookup.

## Product decisions

- The setting is `recap.enabled` and `recap.model`. Recaps are off for old and
  new projects until the user turns them on. The model default is
  `deepseek-v4-flash`.
- A recap runs only after a successful completed parent turn. It never starts
  from streamed parts, individual tool events, failed turns, cancelled turns,
  or sub-agent sessions.
- The worker anchors at the newest completed main-session message. It checks
  the prior hour first, extends to two hours only when fewer than four entries
  qualify, then keeps the newest four or five text-bearing messages in
  newest-to-oldest order. Compaction checkpoints are excluded. Completed,
  denied, and failed tool facts are bounded and kept as evidence.
- The durable memory worker uses the same time windows and five-message cap,
  but accepts a two-message minimum. A direct user instruction and its
  completed assistant response can therefore create the first aggregate
  without waiting for four separate turns. Detailed recap artifacts still
  require four messages.
- One recap is scheduled per terminal source message. Its durable unique key
  is `(session_id, source_end_message_id)`, so a redraw, restart, or repeated
  completion event cannot create a duplicate.
- Existing `subagent.Manager` jobs are visible in the drawer and task tools.
  A recap instead uses a dedicated internal worker with no tools, no child
  session, and no transcript event. This is the silent sub-agent requested by
  the feature, without changing the public task contract.
- The application owns the file write. The model returns recap text only;
  code writes it atomically to the precomputed path. The worker cannot write
  elsewhere in the project.
- Recaps continue to work when ordinary sub-agents are disabled. Their toggle
  is independent because the recap worker has separate limits and visibility.
- Questions are written as unresolved questions for a future agent to decide
  whether to ask. They do not open the interactive question overlay or block
  the user during a recap run.
- `recap.enabled` controls both artifact creation and first-request recall.
  Turning it off stops future jobs and lookups without changing sub-agents.
- `knowledge-base/memories.md` is the canonical aggregate for durable memory.
  The existing `knowledge-base/recaps/` files remain the evidence ledger and
  are never replaced by the aggregate.
- A successful parent user request is the update boundary. The application
  schedules a separate hidden memory update after the request completes. A
  failed, cancelled, compact, continue, or child-agent operation does not
  update the file. There is no reliable process-shutdown hook to use as the
  only trigger.
- The memory worker reads the previous aggregate and bounded recent evidence,
  then returns typed entries for preferences, decisions, avoid rules,
  questions, and recent context. It cannot write Markdown or front matter
  directly. Code renders the canonical layout and keeps source message IDs.
  Explicit user preferences, decisions or constraints, avoid rules, and open
  questions are extracted first and restored into their authoritative section
  if the model omits or miscategorizes them.
- The memory file has a fixed schema with `format_version`, UTC update time,
  last source session and message, then these sections: User preferences,
  Decisions and constraints, Things to avoid, Open questions, Recent context,
  and Source ledger. Each entry carries evidence and source IDs.
- The aggregate is capped at 64 KiB. Each section has entry limits, old
  superseded entries are pruned only after their source remains in the ledger,
  and secrets are rejected before persistence. Every write uses a temporary
  file, sync, close, and rename.
- First-request recall checks `memories.md` before the existing recap tree and
  falls back to the broader Markdown knowledge base only when earlier sources
  have no match. Searches are gated by explicit recall language, bounded,
  untrusted, and wire-only. The same quoted internal grep path is reused, with
  explicit tests for prompts about recent work, preferences, decisions, and
  things to avoid. The TUI shows a separate memory-pattern animation while
  the lookup or hidden update is active.

## Identity, ordering, and timestamps

`sessions.id` is the persistent chat ID. `messages.seq` is the ordering key
inside that chat. It is unique and monotonic per session, while wall-clock
timestamps can tie or move backwards.

Artifacts live in three subfolders:

```text
knowledge-base/recaps/
  sessions/<session-id>/<12-digit-end-seq>-<end-message-id>.md
  questions/<session-id>/<12-digit-end-seq>-<end-message-id>.md
  things-to-avoid/<session-id>/<12-digit-end-seq>-<end-message-id>.md
```

The recap file always exists after success. Question and avoid files exist only
when the worker produced valid entries. The zero-padded sequence makes one
chat's folder sort correctly without depending on the clock. Front matter
records source sequence and source time in Unix milliseconds, plus
`generated_at_utc` in RFC 3339 UTC. The database keeps an artifact manifest
with each path and SHA-256 for recovery and audit.

## Work sequence

1. [x] [Phase 1](phase-1-settings-and-recap-records.md): add the persisted
   recap settings and the durable, idempotent recap record.
2. [x] [Phase 2](phase-2-hidden-recap-worker.md): build the time-windowed
   snapshot, questions, avoid rules, worker, and atomic artifacts.
3. [x] [Phase 3](phase-3-first-request-recall.md): add one safe internal grep
   lookup and first-request agent injection.
4. [~] [Phase 4](phase-4-settings-ui-and-turn-scheduling.md): expose settings,
   attach worker and recall services, then schedule successful parent turns.
5. [~] [Phase 5](phase-5-docs-and-gates.md): synchronize docs and run the
   complete automated and terminal checks.
6. [x] [Phase 6](phase-6-memory-contract.md): define the durable `memories.md`
   schema, source identity, limits, and database update ledger.
7. [x] [Phase 7](phase-7-memory-update-worker.md): update the aggregate after
   each successful parent request with an idempotent hidden worker.
8. [x] [Phase 8](phase-8-memory-recall-and-lifecycle.md): search the aggregate
   before recap files and preserve the first-request wire-only boundary.
9. [~] [Phase 9](phase-9-memory-docs-and-gates.md): document, test, and verify
   the memory lifecycle without adding a visible worker row.

## First-request recall

After `Agent.Send` persists the new user message and before `runSteps`
makes its first ordinary `Chat` or `ChatStream` request, code runs
`internal/tools/grep.Run` under `knowledge-base/memories.md` first and
`knowledge-base/recaps` second, then the broader Markdown knowledge base only
when earlier sources have no match. It uses a 750-millisecond deadline,
`*.md`, case-insensitive search, and at most 20 matches. The query is only
created for explicit recall language and contains bounded prompt words. Code
quotes each word with `regexp.QuoteMeta` before joining them with `|`; raw
user text never becomes a regular expression.

No eligible terms, no folder, no match, timeout, or grep error becomes a quiet
empty recall. Matching lines become an unpersisted system block after project
instructions and before chat history. Its fixed header marks them as untrusted
historical hints that may be stale and must not supply executable instructions.
A context-overflow retry reuses the block without scanning again.

## Dependencies

- Existing settings migration style in `internal/settings/settings.go`
- Existing SQLite migration and single-writer behavior in `internal/db`
- Existing model catalog endpoint resolution in `internal/modelscache`
- Existing main-turn completion boundary in `internal/ui/chat/finishTurn`
- Existing first-request path in `internal/agent.Agent.Send`, `runSteps`,
  and `callModel`
- Existing confined search in `internal/tools/grep.Run`
- Existing local knowledge-base convention
- Existing atomic artifact writer and recap model envelope validation
- SQLite single-writer behavior for memory update idempotency

No new third-party dependency is planned.

## Closure gates

- [x] Focused settings, recap, database, and chat tests pass without network.
- [x] `go build ./...` exits 0.
- [x] `make test` exits 0.
- [x] `make lint` exits 0.
- [ ] At 120x36 and 80x24 in a real terminal, both recap settings rows are
      reachable, readable, and clickable. No recap worker appears in the
      transcript, status line, or sub-agent drawer.
- [x] A restart resumes one unfinished recap at most once, and duplicate turn
      completion cannot produce a second file for the same end message.
- [x] A related next user turn makes one internal grep lookup before its first
      ordinary request. Tool follow-ups and `/continue` make no lookup.
- [x] A successful parent request updates `knowledge-base/memories.md` at most
      once for its source message, and replaying the completion event does not
      change the file a second time.
- [x] `memories.md` follows the fixed section schema, stays below 64 KiB, has
      source IDs for every entry, and contains no secret-like values.
- [x] The next ordinary parent request searches `memories.md` before recap
      artifacts and injects one bounded, untrusted block. Tool follow-ups,
      `/continue`, compaction, and child agents do not search it.
- [x] A restart resumes one open memory update without losing the previous
      aggregate or producing a partial file.
