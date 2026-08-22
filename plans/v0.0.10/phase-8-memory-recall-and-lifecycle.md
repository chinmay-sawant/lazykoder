# v0.0.10 / Phase 8 - Memory-first recall and lifecycle boundaries

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** complete
> **Estimated effort:** 1-2 days
> **Priority:** P0
> **Gate:** the first ordinary parent request searches `memories.md` before
> recap evidence and injects one bounded, untrusted block without persisting
> or repeating it in tool follow-ups.

## Overview

Extend the existing recall seam instead of teaching the model to decide when
to grep. Code searches the project memory file first, then the session recap
tree, and passes only matching lines as wire-only historical hints.

## Phase 8: Search and request boundaries

### 8.1 Search the aggregate first

- [x] Update the chat-runtime recall provider to search
      `knowledge-base/memories.md` before `knowledge-base/recaps/`, then fall
      back to the broader Markdown knowledge base only when earlier sources
      have no match.
- [x] Keep the existing `internal/tools/grep.Run` containment, timeout,
      case-insensitive matching, quoted terms, match cap, and output cap.
      Return the first matching source with an explicit label.
- [x] If the aggregate is missing or malformed, continue with recap search.
      A memory failure must never block the ordinary provider request.
- [x] Keep the returned block under a separate memory cap so a large recap
      tree cannot crowd out the current conversation.

### 8.2 Recognize useful prompt terms safely

- [x] Preserve bounded tokenization and quoted patterns. Gate searches on
      explicit recall language such as `recent`, `memory`, `preference`,
      `decision`, `avoid`, and `recap` without adding raw prompt text to a
      regular expression.
- [x] Keep the lookup quiet for empty, punctuation-only, or short prompts.
      Do not add a slash command or a model-visible grep tool call.
- [x] Add tests that verify a matching preference, avoid rule, decision, and
      recent-context entry is returned from the correct source label.

### 8.3 Preserve the first-request wire-only contract

- [x] Inject the combined block after project instructions and before chat
      history, with the existing untrusted historical-hints header.
- [x] Clear the block after the first ordinary model response. Reuse it for
      one context-overflow retry without scanning again.
- [x] Do not search or inject memory for tool-result follow-ups, `/continue`,
      compaction, sub-agent sessions, or child-agent requests.
- [x] Never write the block to `messages`, `parts`, recap artifacts, or
      `memories.md`.
- [x] Test request ordering, one lookup per send, overflow reuse, failed
      lookup silence, disabled recaps, and child-agent exclusion.
- [x] Show a distinct animated memory-pattern status during the local lookup
      and hidden post-turn memory update.

## Dependencies

- Phase 6 parsed memory document
- Phase 7 source-backed file updates
- Existing `agent.Options.Recall` and `internal/tools/grep.Run`

## Closure gate

- [x] `go test ./internal/agent ./internal/ui/chat -count=1` exits 0, with
      the command and exit code recorded here.

Evidence: `go test ./internal/agent ./internal/ui/chat -count=1` exited 0 on
2026-08-22. `internal/ui/chat/runtime.go` searches `memories.md` before the
recap tree and the existing agent recall tests cover one-shot injection,
overflow reuse, failed lookup silence, `/continue`, and child exclusion.
