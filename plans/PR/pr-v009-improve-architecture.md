## Summary

Deepens hot modules on the architecture-review track so Compaction, Session
load, turn wiring, Tool dispatch, and Subagent projection each have one
owning home. Residual deepen pressure on the scored candidates drops from
about 7/10 to about 2/10. No user-facing product feature; navigability and
test locality only.

## Motivation / context

- Plans: `plans/v0.0.9/architecture-review/` (phases 1-7), including
  `architecture-review.html`, `phase-wise-checklist.md`, and
  `phase-7-deferred-c8-c9-domain.md`
- Five-agent scan on hotspots (`ui/chat`, `agent`, `subagent`, `db`/provider,
  cross-cutting), then package-owned implementation
- Issues: none filed

## Changes

### Compaction, Session graph, tools (phases 1-2, 5)

- Agent owns named Compaction defaults; `serializeEntries` respects
  KeepTokens; `ForceCompact` removed
- `db.LoadSessionGraph` batches session load; agent history and
  `LastAssistantText` use it
- Tool dispatch lives in `tools_exec.go` with a name→runner map
- `subagent.Host` imports `internal/tools/task` for Specs/parse

### Chat focus, turn-runtime, Subagent honesty (phases 3-4, 6)

- `focus.go` owns exclusive overlay focus
- `runtime.go` / `gate.go` own Manager boot, `agentOptions`, Confirm/Ask,
  shared `startTurn`
- Child agents set `CompactAuto: false`; MaxDepth clamped to 1;
  `IsTerminalStatus` / `Manager.Boot` exported and used

### Layout, Event deltas, ChatMessage (phase 7)

- `layoutSnap` shared by View and mouse; settings paint cached once for hits
- `PartDelta` / `ToolDelta` on `agent.Event` (no `db.Part` on the Event seam)
- Agent-owned `ChatMessage` / `ChatToolCall`; wire map only at Client /
  compact summarizer calls. Still a concrete `*opencode.Client`; no provider
  interface

### Plans / ledger

- Architecture review HTML with ratings, phase-wise checklist, phase-7 plan
- Pointer from `plans/v0.0.9/README.md`

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Session graph uses fewer ListParts round-trips; layout cache avoids re-painting settings for every hit |
| **Memory** | Layout/settings paint cache per frame; otherwise negligible |
| **Behavior / correctness** | Same product behavior; internal seams tightened. Child auto-compact no longer pretends to fire without a window |
| **API / CLI** | None |
| **Dependencies** | None |
| **Binary size / build time** | Negligible |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| None for users | - |
| Internal Event shape | Tests and UI use PartDelta/ToolDelta; replay still projects from Session graph |

## Test plan

- [x] `go test ./...`
- [x] `make lint`
- [x] `go build ./...`
- [ ] Manual: open `/settings`, click `[x]` and a control row (layout snap)
- [ ] Manual: send a turn with tools and confirm transcript/tool cards still update

### Commands

```sh
go test ./...
make lint
go build -o bin/lk .
make run
```

## Screenshots / sample output

Architecture scoreboard (priority at review start → residual after ship):

```text
C1 Compaction        9 → 2
C2 Turn-runtime      9 → 2
C3 Session graph     8 → 2
C4 Exclusive modes   8 → 2
C5 Tool dispatch     8 → 2
C7 tools/task        7 → 1
C6 Subagent drawer   7 → 2
C8 Layout snapshot   6 → 3
C9 Event deltas      4 → 3
Avg backlog ~7.3 → ~2.1
```

Full visual report: `plans/v0.0.9/architecture-review/architecture-review.html`

## Related issues

- None filed for this track

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [x] Related issues filled (none filed)
- [x] Filled body under `plans/PR/pr-v009-improve-architecture.md`

## Follow-ups (out of scope)

- Optional `ChatToolSpec` so Host Specs leave `opencode.ToolSpec`
- Drop `db.Part` from `transcriptItem` storage (Event seam already clean)
- Full chip-level layout engine beyond outer bands + settings card

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public API / CLI changes documented
- [ ] New rules have fixture coverage when applicable
- [ ] PR has assignee and labels
- [ ] Related issues use correct Closes/Relates keywords
- [ ] No secrets or generated artifacts committed
