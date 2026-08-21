# v0.0.10 - Local recap memory

> **Parent:** Current chat, settings, SQLite, and sub-agent runtime
> **Status:** planned 2026-08-21
> **Estimated effort:** 4-6 days across four phases
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** an enabled project writes one ordered, durable recap after a
> completed main-chat turn without changing the transcript, sub-agent drawer,
> or the main turn's result.

## Overview

Recaps are optional local memory. When enabled, lazykoder takes the latest
four or five persisted main-chat messages after a successful turn, sends that
bounded snapshot to a hidden recap worker, and writes the result below the
project's `knowledge-base/recaps/` directory.

The worker has its own setting. It defaults to `deepseek-v4-flash`, which is
the built-in new-session default and is present in the cached model catalog.
It must use that configured model and its catalog endpoint, not the live
`/model` selection and not `agents.model_override`.

This version creates memory. It does not yet alter the parent agent's tool
loop to retrieve that memory.

## Product decisions

- The setting is `recap.enabled` and `recap.model`. Recaps are off for old and
  new projects until the user turns them on. The model default is
  `deepseek-v4-flash`.
- A recap runs only after a successful completed parent turn. It never starts
  from streamed parts, individual tool events, failed turns, cancelled turns,
  or sub-agent sessions.
- The input is the most recent five text-bearing main-session messages. When
  exactly four are available, use four. Fewer than four means no job. Exclude
  compaction checkpoints. Include structured completed-tool facts only when
  they belong to those messages and cap their text in the recap input.
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

## Identity, ordering, and timestamps

`sessions.id` is the persistent chat ID. `messages.seq` is the ordering key
inside that chat. It is unique and monotonic per session, while wall-clock
timestamps can tie or move backwards.

Each file lives at:

```text
knowledge-base/recaps/<session-id>/<12-digit-end-seq>-<end-message-id>.md
```

For example:

```text
knowledge-base/recaps/ses_a1b2c3d4e5f60708/000000000042-msg_cafe1234.md
```

The zero-padded sequence makes one chat's directory sort correctly without
depending on the clock. Front matter records source sequence and source time
in Unix milliseconds, plus `generated_at_utc` in RFC 3339 UTC. The database
keeps creation, start, and finish times for recovery and cross-session audit.

## Work sequence

1. [ ] [Phase 1](phase-1-settings-and-recap-records.md): add the persisted
   recap settings and the durable, idempotent recap record.
2. [ ] [Phase 2](phase-2-hidden-recap-worker.md): build the bounded snapshot,
   direct model worker, atomic knowledge-base writer, and recovery rules.
3. [ ] [Phase 3](phase-3-settings-ui-and-turn-scheduling.md): expose the two
   settings rows and schedule jobs after successful parent turns.
4. [ ] [Phase 4](phase-4-docs-and-gates.md): document the behavior, run the
   complete checks, and capture real-terminal evidence for the settings card.

## Parked retrieval behavior

Do not add a mandatory `grep knowledge-base/recaps` before tool calls in this
version. That behavior needs its own policy: when a search is relevant, how a
query is formed, how many hits enter context, how stale or conflicting recaps
are handled, and what happens when the directory is empty. It also changes
every tool turn and needs an evaluation corpus. Keep it as a post-v0.0.10
follow-up once recap files exist to test against.

## Dependencies

- Existing settings migration style in `internal/settings/settings.go`
- Existing SQLite migration and single-writer behavior in `internal/db`
- Existing model catalog endpoint resolution in `internal/modelscache`
- Existing main-turn completion boundary in `internal/ui/chat/finishTurn`
- Existing local knowledge-base convention

No new third-party dependency is planned.

## Closure gates

- [ ] Focused settings, recap, database, and chat tests pass without network.
- [ ] `go build ./...` exits 0.
- [ ] `make test` exits 0.
- [ ] `make lint` exits 0.
- [ ] At 120x36 and 80x24 in a real terminal, both recap settings rows are
      reachable, readable, and clickable. No recap worker appears in the
      transcript, status line, or sub-agent drawer.
- [ ] A restart resumes one unfinished recap at most once, and duplicate turn
      completion cannot produce a second file for the same end message.
