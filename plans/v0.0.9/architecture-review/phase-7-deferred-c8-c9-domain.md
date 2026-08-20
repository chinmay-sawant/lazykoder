# Plan: C8 layout snapshot, C9 Event deltas, Agent domain messages

> **Branch:** `chore/009-improve-architecture` (phases 1-6 already pushed as `f9cd1d7`)
> **Status:** implemented 2026-08-21 (C8 + C9 + ChatMessage domain types)
> **Sequence (user choice):** C8 → C9 → domain types
> **Constraints:** no multi-provider interface; OpenCode Go stays the only Client; no new deps; module-wise splits; `go test ./...` + `make lint` per phase
> **Gate:** `go test ./...` and `make lint` passed 2026-08-21

---

## Context

Phases 1-6 deepened Compaction, Session graph, focus, turn-runtime, and tool dispatch. Three deferred items remain from the architecture review:

| ID | Rating | Goal |
| --- | --- | --- |
| C8 | 6/10 | One layout snapshot per frame for paint and hit-test |
| C9 | 4/10 | Agent `Event` stops embedding `db.Part` / leaking SQL part types to UI |
| Domain | blocked as "provider iface" | Agent-owned message types; `*opencode.Client` stays concrete and maps wire at the call boundary |

---

## Phase A: C8 Layout snapshot (chat-only)

### Problem (current code)

- `composerTop` (`view.go` ~336-352) re-renders slash/picker/subagent/status/err views to reserve rows.
- `transcriptRenderHeight` and mouse paths re-derive the same vertical bands.
- `settingsCloseRect` / `settingsRowAtScreenY` (`settings.go` ~891-916) call `settingsScreen()` again and scan painted lines for hit-test. Paint and hit can drift.

### Deepening

Add `internal/ui/chat/layout.go` with a small **layout snapshot** module:

```text
type layoutSnap struct {
  width, height int
  focus         focusKind
  // vertical bands (screen rows)
  headerBottom, transcriptTop, transcriptBottom int
  alertRow, jumpBarRow, composerTop, footerTop int
  // optional overlay bands
  statusTop, subagentDrawerTop, slashTop, pickerTop int
  // settings card (only when focusSettings)
  settingsCloseX0, settingsCloseX1, settingsCloseY int
  settingsCloseOK bool
  settingsRowYs   []int // control row index -> screen Y, or parallel maps
}
```

**Build once** from Model state (and, for settings, from the same string `settingsScreen()` already produces during View). Store on `Model` as `layout layoutSnap` (or `lastLayout`), refreshed:

1. At the start of `View` / `frame` (authoritative for paint).
2. On mouse paths: if snap is stale (size/focus/mode fingerprint mismatch), rebuild; else hit-test against snap fields.

### Migration order (keep tests green)

1. Introduce `layoutSnap` + `buildLayout()` that **calls existing** `composerTop` / `transcriptRenderHeight` / etc. (behavior-identical wrapper).
2. Point `jumpBarRow`, mouse drawer/composer ordering, and footer chip Y at snap fields.
3. For settings: when building snap in settings focus, run `settingsScreen()` once, parse close rect + row Y map into snap; change `settingsCloseRect` / `settingsRowAtScreenY` to read snap (fallback rebuild if empty).
4. Stop double-rendering in hot mouse paths where snap is fresh.
5. Add/extend `settings_geom_test.go` + one mouse test asserting hit uses snap without requiring a second divergent paint path.

### Out of scope for C8

- Full absolute positioning engine for every chip glyph.
- Rewriting transcript item hit maps (already viewport-relative); only share outer bands.

### Gate A

- `go test ./internal/ui/chat/ -count=1`
- `make lint` on touched files / full lint

---

## Phase B: C9 Event without `db.Part` leakage

### Problem

```go
// agent.Event today
Part db.Part
Tool db.ToolCall
```

UI `chat.go` switches `EventPart` → `applyPart(ev.Part)` and `transcript.applyPart` switches on SQL `p.Type` (`text`, `reasoning`, `step-finish`, compaction, …). Persistence schema is the TUI contract.

### Deepening (UI-facing deltas)

Keep persisting `db.Part` / `db.ToolCall` **inside agent only**. On the Event seam emit typed deltas:

```go
type PartDelta struct {
  Kind      PartDeltaKind // Text, Reasoning, StepStart, StepFinish, Compaction, ...
  ID        string
  MessageID string
  Text      string
  // step-finish / usage fields the UI already reads
  TokensTotal, TokensInput, TokensOutput, TokensReasoning int64
  TokensCacheRead, TokensCacheWrite int64
  Cost float64
  TimeCreated int64
  // compaction envelope fields UI needs, or pre-parsed notice text
}

type ToolDelta struct {
  PartID, CallID, Name, Status, Title string
  InputJSON, Output string
  ExitCode *int
  // metadata the transcript tool card needs
}
```

`Event` becomes:

```go
type Event struct {
  Kind EventKind
  SessionID, MessageID, Role string
  Part PartDelta   // zero unless EventPart / related
  Tool ToolDelta   // zero unless EventTool
  TokenDelta, TokensOutput, ElapsedMS, TokensUsed int64
  Err error
}
```

### Migration order

1. Add `PartDelta` / `ToolDelta` + converters `partDeltaFromDB(db.Part) PartDelta` next to emitters (agent-internal).
2. Change `Event` fields; update all `emit(...)` call sites in `agent.go`, `stream.go`, `compact_run.go`, `tools_exec.go`.
3. Change UI `applyPart` to take `agent.PartDelta` (or keep a thin adapter). Update tests that construct `db.Part` for live apply (`thinking_test.go`, `chat_test.go`, `wrapping_test.go`) to use deltas or a test helper.
4. **Replay stays on Session graph → transcript items** (already unified in `project.go`); do not force replay through Event.
5. Grep UI for `db.Part` on the live event path; persistence imports in chat may remain for Session/Todo/Store, which is fine.

### Gate B

- `go test ./internal/agent/ ./internal/ui/chat/ ./internal/subagent/ -count=1`
- `go build ./...`

---

## Phase C: Agent domain message types (ADR-safe)

### Refuse

- Do **not** add a `Provider` interface with one OpenCode adapter.
- Do **not** rename the product to multi-provider.

### Deepen

Introduce agent-local types (names illustrative):

```go
// internal/agent/chatmsg.go
type ChatMessage struct {
  Role string // user|assistant|system|tool
  Content string
  ToolCalls []ChatToolCall
  ToolCallID string // for tool results
}
type ChatToolCall struct { ID, Name, Arguments string }
type ChatToolSpec struct { Name, Description string; Parameters json.RawMessage /* or map */ }
```

**Mapping lives at the Client boundary**, not as a fake interface:

- Prefer helpers in `internal/provider/opencode` such as `MessagesFromAgent` / `ToAgent` **or** thin functions in agent that call existing `opencode.Message` only inside `callModel` / `streamStep` / compact summarizer request.
- `SubagentHost.Specs() []opencode.ToolSpec` → change to agent `[]ChatToolSpec` (or keep opencode specs only behind Host until slice 2). Smallest first slice: history + estimate/prune + callModel use `[]ChatMessage`; ToolSpec can stay opencode until a follow-up if blast radius is high.

### Phased compile-green slices

1. Add `ChatMessage` + bidirectional map with `opencode.Message`.
2. Switch `buildHistory` / `PruneToolOutputs` / `EstimateMessages` / `withProjectInstructions` to `[]ChatMessage`; map to opencode only in `callModel` / compact HTTP call.
3. Switch tool loop `opencode.ToolCall` to `ChatToolCall` inside agent; map when reading assistant message from ChatResponse.
4. Optionally move `ToolSpec` to agent types; Host + `tools/task` return agent specs (task package currently returns `[]opencode.ToolSpec` - update in same slice).
5. Leave `*opencode.Client` concrete everywhere (main, chat, subagent runner).

### Gate C

- `go test ./...`
- `make lint`
- Confirm still exactly one Client implementation; no new interface in `provider/`.

---

## Docs / checklist

After each phase, update:

- `plans/v0.0.9/architecture-review/phase-wise-checklist.md` deferred D1/D2/D3 → `[x]` with gate lines
- Local `knowledge-base/02-architecture/architecture-review-v009.md`
- Brief notes in `component-map.md` / `data-flow.md` if Event or layout ownership changes

No git until user asks (or one commit per phase if user prefers).

---

## Risk register

| Risk | Mitigation |
| --- | --- |
| C8 snap stale vs View | Fingerprint width/height/focus/drawer flags; rebuild on mismatch |
| C9 test churn on `applyPart(db.Part)` | Test helper `partDelta(...)`; keep replay on graph |
| Domain types touch Host + task | Slice ToolSpec last; history/callModel first |
| Accidental provider interface | Explicit refuse in review; keep `*opencode.Client` in structs |

## Success criteria

- Paint and hit-test share one layout artifact for outer bands + settings card.
- Live Event path does not expose `db.Part` / `db.ToolCall` to UI.
- Agent history/tool loop speak `ChatMessage` / `ChatToolCall`; Client remains the HTTP/wire adapter with no provider interface.
- `go test ./...` and `make lint` green after each phase.
