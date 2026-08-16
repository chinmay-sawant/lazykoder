# Phase 2 - Mermaid source loading and parser registry

> **Parent:** `plans/v0.0.3/README.md`
> **Status:** planned
> **Depends on:** Phase 1 trace and Mermaid contracts

## 2.1 Source loading

- [ ] Add a read-only Mermaid source loader with a bounded input size and
  normalized UTF-8 text.
- [ ] Accept `.mmd` and `.mermaid` files directly.
- [ ] Accept Markdown files and extract fenced blocks whose language is
  `mermaid`, `mermaid `, or case-insensitive equivalent.
- [ ] Return block locations so a Markdown file with multiple diagrams can be
  shown in a picker before parsing.
- [ ] Restrict the file picker and explicit paths to regular files under the
  project directory or `$HOME/Desktop` when that directory exists.
- [ ] Reject directories, missing files, oversized files, unsupported
  extensions, and paths that escape the allowed roots with readable errors.
- [ ] Preserve the original source bytes for source mode and clipboard copy;
  parsing must never rewrite the loaded file.

**Gate:** loader tests cover direct Mermaid files, multiple Markdown fences,
missing Desktop directories, path traversal attempts, symlinks, size limits,
and source byte preservation.

## 2.2 Diagram detection and shared IR

- [ ] Detect the top-level diagram family from the first meaningful directive,
  ignoring comments and front matter where the selected format permits it.
- [ ] Normalize aliases such as `graph`/`flowchart` and `gitGraph`/`gitgraph`
  into stable internal kinds without changing the source text.
- [ ] Define a shared diagram model containing nodes, edges, labels, groups,
  notes, directions, styles, source locations, and diagnostics.
- [ ] Keep type-specific data in typed extensions so the common canvas does not
  need to understand every grammar detail.
- [ ] Retain source line and column information on parsed elements so a clicked
  terminal element can show the relevant source location.
- [ ] Define parser and renderer registration contracts at the real seam:
  one package owns dispatch, while each diagram family owns its grammar and
  conversion into the shared model.

**Gate:** detection and IR tests cover every registered top-level family,
aliases, comments, blank input, unknown directives, and source locations.

## 2.3 Native parser registry

- [ ] Register the Phase 1 sequence parser and serializer as the reference
  implementation.
- [ ] Add parser slots and fixtures for flowchart/graph, class, state, entity
  relationship, user journey, gantt, pie, quadrant, requirement, gitgraph,
  mindmap, timeline, sankey, xychart, block, packet, kanban, architecture,
  radar, treemap, C4, and any additional diagram family included by the pinned
  Mermaid syntax version.
- [ ] Require each registered parser to either produce a renderable model or a
  line-level diagnostic that leaves source mode available.
- [ ] Never classify an unknown or malformed diagram as an empty successful
  diagram.
- [ ] Add a source-only fallback for recognized syntax that has not reached its
  native renderer yet; expose the missing capability in the view rather than
  failing silently.

**Gate:** registry tests prove deterministic dispatch, no parser panics on
malformed input, and source fallback for every incomplete capability.

## 2.4 Slash command and picker

- [ ] Add `/mermaid` with `/diagram` alias to the slash command menu.
- [ ] Support `/mermaid <path>` for direct loading and `/mermaid` for the
  interactive picker.
- [ ] Add a file picker mode that shows the allowed roots, file extension,
  modified time, and a short path without leaking file contents into the chat.
- [ ] When a Markdown file contains multiple Mermaid blocks, show block titles
  or source line ranges and require a selection.
- [ ] Keep file loading local and read-only; opening a diagram must not create a
  session, call the provider, or write a diagram copy.
- [ ] Preserve the current prompt and transcript when the picker is cancelled.

**Gate:** chat tests cover command discovery, aliases, direct paths, picker
cancel, Markdown block selection, malformed files, and no provider/database
write behavior.

## 2.5 Documentation

- [ ] Document accepted extensions, Markdown fences, allowed roots, direct
  paths, source preservation, and parser diagnostics in `docs/tui.md`.
- [ ] Document the parser registry and shared IR in `docs/architecture.md`.
- [ ] Record the pinned Mermaid syntax version and update the support matrix
  whenever a parser family is added.
