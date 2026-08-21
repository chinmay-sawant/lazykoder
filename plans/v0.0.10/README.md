# v0.0.10 - Local recap, questions, and recall

> **Parent:** Current chat, settings, SQLite, first-request agent path, and local knowledge base
> **Status:** planned 2026-08-21
> **Estimated effort:** 6-8 days across five phases
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** an enabled project writes ordered recap artifacts after a main
> turn, then checks those artifacts once before the next user turn's first
> ordinary model request.

## Overview

Recaps are optional local memory. A completed main-chat turn starts a hidden
worker. It writes a recap, unresolved questions for a future agent, and
concrete things to avoid based on user corrections and tool failures.

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

## First-request recall

After `Agent.Send` persists the new user message and before `runSteps`
makes its first ordinary `Chat` or `ChatStream` request, code runs
`internal/tools/grep.Run` under `knowledge-base/recaps`. It uses a
750-millisecond deadline, `*.md`, case-insensitive search, and at most 20
matches. The query contains three to eight meaningful prompt words. Code
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
