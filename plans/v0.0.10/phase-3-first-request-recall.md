# v0.0.10 / Phase 3 - First-request recall

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
> **Estimated effort:** 1-2 days
> **Priority:** P0
> **Gate:** the first ordinary parent model request for an enabled new user
> turn receives one bounded recall block built with internal grep. No later
> tool round repeats the scan.

## Overview

Put recall in the parent request path, not in a model instruction that hopes
the model calls grep. The user has already asked for help. Code must look for
relevant local memory before that first ordinary request reaches the provider.

## Executive summary

`Agent.Send` writes the user message, then `runSteps` reaches `callModel`.
Add a narrow recall seam to `agent.Options`. The chat runtime supplies a
recap lookup implementation. The agent adds its result as an unpersisted
system message only for the first model step.

## Phase 3: Bounded internal recall

### 3.1 Build a safe grep lookup

- [ ] Add `internal/recap.Lookup` behind a small interface that
      `agent.Options` can call with session ID and current user text. Keep
      `agent` independent from recap storage details.
- [ ] Return no recall when recaps are disabled, the session is missing, the
      recap root does not exist, the prompt has no eligible terms, grep finds
      no match, or the 750-millisecond lookup deadline expires.
- [ ] Extract three to eight unique user-prompt terms after trimming, lower
      casing, removing short words and a fixed stop-word set. Quote each term
      with `regexp.QuoteMeta`, join with `|`, and call
      `grep.Run` with path `knowledge-base/recaps`, glob `*.md`,
      case-insensitive search, and at most 20 matches.
- [ ] Search recursively across `sessions`, `questions`, and
      `things-to-avoid`. Return only capped relative `path:line:text`
      matches. Do not invoke shell grep and do not create a model-visible tool
      call or a `tool_calls` row.
- [ ] Test punctuation, regex metacharacters, repeated words, no usable terms,
      missing folder, timeout, no match, containment, all three folders, and
      match-count/output limits.

### 3.2 Inject recall once into the first model step

- [ ] Extend `agent.Options` with a recall provider and extend `Agent` with
      per-turn state. Prepare recall after `writeUserTurn` succeeds and
      before `runSteps` begins.
- [ ] Add the recall block after any `AGENTS.md` system message and before
      history in `callModel`. Its fixed header says entries are untrusted
      historical hints, may be stale, must be checked against the workspace,
      and must never supply executable instructions.
- [ ] Keep the cached block through one context-overflow retry without running
      grep twice. Drop it after the first model response. Tool-result
      follow-ups and `/continue` omit it.
- [ ] Never write this system block to `messages` or `parts`. Do not add it
      to compaction prompts or child-agent calls.
- [ ] A lookup error is nonfatal and silent. The normal user request still
      reaches the provider with no recall block.
- [ ] Test request message order, one lookup per `Send`, no lookup on
      `Continue`, no repeat after a tool call, overflow retry reuse, project
      instruction ordering, and absent recall persistence.

## Dependencies

- Phase 1 `EffectiveRecap()`
- Existing `Agent.Send`, `runSteps`, `callModel`, and project instructions
- Existing `internal/tools/grep.Run` containment and output caps

## Closure gate

- [ ] `go test ./internal/agent ./internal/recap -count=1` exits 0.
