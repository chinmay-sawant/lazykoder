# v0.0.10 / Phase 8 - Memory-first recall and lifecycle boundaries

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** planned
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

- [ ] Update the chat-runtime recall provider to search
      `knowledge-base/memories.md` before `knowledge-base/recaps/`.
- [ ] Keep the existing `internal/tools/grep.Run` containment, timeout,
      case-insensitive matching, quoted terms, match cap, and output cap.
      Combine both sources with explicit `MEMORY` and `RECAP` labels.
- [ ] If the aggregate is missing or malformed, continue with recap search.
      A memory failure must never block the ordinary provider request.
- [ ] Keep the returned block under a separate memory cap so a large recap
      tree cannot crowd out the current conversation.

### 8.2 Recognize useful prompt terms safely

- [ ] Preserve the current tokenization and stop-word filtering. Add tests for
      prompts containing `recent`, `memory`, `preference`, `decision`,
      `avoid`, `improve`, and `recap` without adding raw prompt text to a
      regular expression.
- [ ] Keep the lookup quiet for empty, punctuation-only, or short prompts.
      Do not add a slash command or a model-visible grep tool call.
- [ ] Add tests that verify a matching preference, avoid rule, decision, and
      recent-context entry is returned from the correct source label.

### 8.3 Preserve the first-request wire-only contract

- [ ] Inject the combined block after project instructions and before chat
      history, with the existing untrusted historical-hints header.
- [ ] Clear the block after the first ordinary model response. Reuse it for
      one context-overflow retry without scanning again.
- [ ] Do not search or inject memory for tool-result follow-ups, `/continue`,
      compaction, sub-agent sessions, or child-agent requests.
- [ ] Never write the block to `messages`, `parts`, recap artifacts, or
      `memories.md`.
- [ ] Test request ordering, one lookup per send, overflow reuse, failed
      lookup silence, disabled recaps, and child-agent exclusion.

## Dependencies

- Phase 6 parsed memory document
- Phase 7 source-backed file updates
- Existing `agent.Options.Recall` and `internal/tools/grep.Run`

## Closure gate

- [ ] `go test ./internal/agent ./internal/ui/chat -count=1` exits 0, with
      the command and exit code recorded here.
