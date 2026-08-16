# Phase 4 - Complete Mermaid diagram-family coverage

> **Parent:** `plans/v0.0.3/README.md`
> **Status:** planned
> **Depends on:** Phases 1 through 3

This phase closes the gap between a sequence-only prototype and full Mermaid
support. The exact syntax target must be pinned before implementation so parser
fixtures and behavior do not drift with upstream Mermaid releases.

## 4.1 Support matrix

- [ ] Record the pinned Mermaid syntax version and its official diagram-family
  list in this file before adding family-specific code.
- [ ] Track each family with four independent gates: detection, parsing, native
  terminal layout, and source/diagnostic fallback.
- [ ] Add at least one valid fixture, one malformed fixture, one long-label
  fixture, and one zoom/pan fixture for every family.
- [ ] Treat aliases and directives as part of the family contract, not as
  undocumented parser special cases.
- [ ] Keep source mode available for newly released Mermaid syntax until a
  parser and renderer have been added.

## 4.2 Family implementation order

- [ ] Complete structural graph families: flowchart/graph, class, state,
  entity relationship, architecture, block, and C4.
- [ ] Complete interaction and timeline families: sequence, user journey,
  gantt, gitgraph, timeline, and mindmap.
- [ ] Complete data and chart families: pie, quadrant, xychart, sankey, radar,
  treemap, and packet.
- [ ] Complete planning and workflow families: requirement, kanban, and any
  additional family in the pinned syntax version.
- [ ] Add a renderer capability table so the UI can state exactly why a source
  is in source-only mode.

## 4.3 Shared layout quality

- [ ] Reuse one terminal canvas coordinate system for nodes, edges, labels,
  lanes, groups, and chart cells across all families.
- [ ] Keep zoom semantics consistent: the same six zoom levels, background
  control strip, keyboard controls, wheel behavior, and pan clamps apply to
  every family.
- [ ] Make dense charts degrade by reducing labels and detail only at the
  display boundary; source and parsed values remain unchanged.
- [ ] Test color-disabled terminals and narrow widths so color is never the
  only way to understand an edge, status, or diagnostic.
- [ ] Ensure unsupported constructs are visible as source-line diagnostics and
  never cause a blank canvas, panic, or false successful render.

## 4.4 Completion gates

- [ ] Every family in the pinned Mermaid support matrix has parser, native
  renderer, source fallback, and fixture coverage.
- [ ] Every family passes the common canvas interaction tests for background
  zoom controls, keyboard zoom, wheel zoom, pan, source mode, and copy.
- [ ] `/mermaid` loads each fixture from the project and `$HOME/Desktop` roots.
- [ ] `/sequence` still renders the persisted agent trace through the generic
  Mermaid canvas after all family additions.
- [ ] `go build ./...`, `go test ./... -count=1`, and `go vet ./...` exit 0.
- [ ] Manual review confirms no diagram family is silently omitted or rendered
  as an unrelated diagram type.

## Explicit non-goals

- No browser or webview is embedded in the terminal application.
- No Mermaid source is rewritten when it is loaded, displayed, or copied.
- No diagram is persisted in SQLite unless a later requirement needs saved
  bookmarks or history; the source file remains the source of truth.
- An external renderer such as `mmdc` may be added later as an optional export
  backend, but native terminal coverage is the completion criterion here.
