# Phase 1 - Trace-backed Mermaid sequence support

> **Parent:** `plans/v0.0.3/README.md`
> **Status:** planned
> **Boundary:** this phase is the first diagram family, not the complete Mermaid milestone
> **Rule:** mark a row `[x]` only after its stated gate passes

## 1.1 Trace contract

- [ ] Add `internal/sequence` with pure types for actors, events, status,
  timestamps, correlation IDs, and bounded detail text.
- [ ] Project visible user text, `step-start`, reasoning, assistant text,
  tool calls, `step-finish`, and incomplete/error states from existing DB rows.
- [ ] Preserve stored ordering with stable tie-breakers; do not infer causal
  ordering from timestamps when sequence numbers disagree.
- [ ] Represent concurrent `task` calls as a parallel group instead of making
  completion order look causal.
- [ ] Keep child-session internals collapsed to one parent task interaction in
  v1; `/agents` remains the detailed child log.
- [ ] Exclude raw tool input/output and secrets from the default trace detail.
- [ ] Add fixtures and deterministic projection tests for empty, text-only,
  multi-step, successful tool, denied tool, failed tool, and task traces.

**Gate:** `go test ./internal/sequence -count=1` exits 0 and repeated runs
produce byte-identical trace values.

## 1.2 Mermaid source generation

- [ ] Add the first `internal/mermaid` serializer that emits:
  `sequenceDiagram`, stable `actor`/`participant` declarations, message arrows,
  `Note over` annotations, status notes, and `par`/`and`/`end` for concurrent
  task calls.
- [ ] Keep actor IDs fixed and internal; never derive Mermaid identifiers from
  user text, tool input, or model output.
- [ ] Sanitize labels by flattening newlines, removing control characters,
  bounding length, and escaping or replacing syntax-sensitive punctuation.
- [ ] Use the same source generator for the copy action and any future file
  export. Do not maintain a second hand-built source format in the TUI.
- [ ] Return structured line errors if the trace cannot be serialized rather
  than silently emitting malformed Mermaid.
- [ ] Add golden fixtures that can be pasted into a Mermaid renderer and unit
  tests for escaping, empty diagrams, tool statuses, and parallel groups.

**Gate:** `go test ./internal/mermaid -count=1` exits 0; golden output contains
only the supported Mermaid sequence syntax and no unbounded user text.

## 1.3 Terminal diagram view

- [ ] Add the sequence renderer behind the future `internal/ui/mermaid` canvas
  boundary, with a fixed lane header and compact rows for the same trace model
  used by the Mermaid serializer.
- [ ] Render request/response arrows, tool status colors, notes, parallel task
  groups, empty state, and load/serialization errors without wrapping an arrow
  row into an unreadable second row.
- [ ] Use ANSI-aware width calculations and truncate labels at the edge of the
  available viewport.
- [ ] Add viewport scrolling, `j`/`k`, arrows, `PgUp`/`PgDn`, `home`/`end`,
  `c` for copy, and `esc`/`q` to close.
- [ ] Provide a source mode so the generated Mermaid can be inspected before
  copying. Source mode is read-only and uses the same viewport.

**Gate:** renderer tests cover 80-column and 120-column layouts, long labels,
ANSI styling, empty traces, and parallel groups with no line exceeding width.

## 1.4 Chat integration

- [ ] Add `/sequence` with `/seq` alias to the slash command menu. The general
  `/mermaid` file-loading command is planned in Phase 2.
- [ ] Route the command only when idle and keep the current transcript/session
  unchanged; no agent turn, provider request, or write transaction is allowed.
- [ ] Load rows from the current main session through a Tea command, then pass
  the result into the pure projection and Mermaid serializer.
- [ ] Add a dedicated mode and viewport to `internal/ui/chat`, with resize and
  mouse-wheel routing before normal prompt handling.
- [ ] Copy Mermaid source with the existing clipboard command and show the
  existing transient copy notice.
- [ ] Return from the view without losing prompt text, scroll position, or
  transcript selection.
- [ ] Reject busy turns with a clear status message instead of rendering a
  partial trace.

**Gate:** chat tests cover command discovery, no-session behavior, busy-turn
behavior, source copy, resize, close/reopen, and no provider invocation.

## 1.5 Documentation and final verification

- [ ] Document the trace-to-Mermaid boundary and v1 non-goals in
  `docs/architecture.md`.
- [ ] Document `/sequence`, `/seq`, source mode, keys, width behavior, and
  clipboard behavior in `docs/tui.md`.
- [ ] Update the version index in `docs/plans.md` when implementation starts.
- [ ] Run `go build ./...`, `go test ./... -count=1`, and `go vet ./...` on the
  rebuilt tree and record exit codes beside the completed gates.
- [ ] Manually verify a session containing a normal response, successful tool,
  denied tool, failed tool, and parallel task calls at two terminal sizes.
- [ ] Restart the app and confirm the same Mermaid source and terminal diagram
  replay from SQLite without a network call.

## Decisions and phase boundary

- Mermaid source is the canonical interchange format; the terminal renderer
  consumes the shared trace model, not rendered HTML or SVG.
- Phase 1 supports generated sequence diagrams only. General Mermaid file
  loading and diagram-family dispatch begin in Phase 2; this is a staged
  boundary, not the final product scope.
- The full milestone must not be marked complete until every registered Mermaid
  diagram family has parser fixtures, a native terminal renderer, and a source
  fallback as tracked by Phase 4.
- No diagram snapshot table or migration is needed because the diagram is a
  deterministic projection of persisted session data.
- Provider lifecycle boundaries remain inferred from persisted parts in v1.
  Adding explicit provider-start/provider-end events is a future observability
  improvement, not a prerequisite for the visualizer.
