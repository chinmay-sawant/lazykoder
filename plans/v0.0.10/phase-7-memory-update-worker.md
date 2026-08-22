# v0.0.10 / Phase 7 - Per-request memory update worker

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 2-3 days
> **Priority:** P0
> **Gate:** each successful parent user request can schedule one hidden,
> idempotent memory update that atomically replaces `memories.md` without
> blocking the normal chat turn.

## Overview

Use a dedicated worker to refresh the aggregate after a successful parent
request. The worker reads the previous document and bounded recent evidence,
then returns typed facts. It has no tools, no child session, no transcript
event, and no visible sub-agent row.

## Executive summary

The update boundary is `eventDoneMsg` after a successful user turn has been
persisted. It is not process shutdown, because the TUI has no reliable
shutdown hook and a process can be killed. The command captures the project
workdir, session ID, source message ID, settings path, and configured recap
model, then runs with its own timeout and context.

## Phase 7: Snapshot, merge, and write

### 7.1 Build bounded memory input

- [ ] Reuse the recap snapshot selector for the newest four or five complete
      main-session messages, including bounded completed, denied, and failed
      tool facts.
- [ ] Read the current `memories.md` through a strict parser. A missing file
      means an empty document. A malformed or oversized file is recorded as a
      worker failure and is never sent as unchecked instructions.
- [ ] Include only the current source window, the parsed memory entries, and
      relevant existing recap evidence. Keep the prompt under an explicit
      input limit and never include secrets or arbitrary files.

### 7.2 Run one hidden model update

- [ ] Add a versioned memory prompt that requests one strict JSON envelope.
      It must distinguish user preferences, project decisions, avoid rules,
      open questions, recent context, and explicit supersessions.
- [ ] Use the configured recap model and its catalog endpoint. Send no tool
      definitions, create no child session, and emit no `agent.Event`.
- [ ] Run the worker after every successful parent user request when recaps
      are enabled. Do not use `recap.after_chats` to skip the memory update;
      that setting remains the artifact-recap threshold.
- [ ] Make provider errors, validation errors, disabled settings, and
      insufficient source context nonfatal to the completed parent turn.
      Record the failure in `memory_updates` for diagnosis and retry.

### 7.3 Merge and render deterministically

- [ ] Merge new facts with the prior document by normalized category and
      stable content key. A newer explicit user correction supersedes an older
      preference or decision, but the source ledger retains both anchors.
- [ ] Preserve active entries that the new envelope does not mention. The
      model may propose a supersession only when it cites supplied evidence.
- [ ] Order active entries by last-seen source sequence, then stable key. Keep
      recent context newest first and render the source ledger oldest first for
      auditability.
- [ ] Generate all Markdown and front matter in application code. Never append
      unvalidated model text directly to the file.

### 7.4 Make writes crash-safe and restart-safe

- [ ] Reserve and claim one `memory_updates` row before the provider call.
      A repeated event for the same source anchor must reuse the row.
- [ ] Serialize writers for one project and write to a temporary file in the
      destination directory, sync, close, and rename. Update the ledger only
      after the rename and digest calculation succeed.
- [ ] On startup or session resume, retry queued or interrupted memory updates
      in order. Never replace a newer aggregate with an older source anchor.
- [ ] Test concurrent completions, process restart, provider timeout, partial
      write, duplicate event replay, and a settings toggle during the request.

## Dependencies

- Phase 6 memory schema and update ledger
- Existing `eventDoneMsg` successful-turn boundary
- Existing recap worker model selection and timeout conventions

## Closure gate

- [ ] `go test ./internal/recap ./internal/db ./internal/ui/chat -count=1`
      exits 0, with the command and exit code recorded here.
