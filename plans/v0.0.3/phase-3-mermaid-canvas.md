# Phase 3 - Full-screen Mermaid canvas and interaction

> **Parent:** `plans/v0.0.3/README.md`
> **Status:** planned
> **Depends on:** Phases 1 and 2

## 3.1 Full-screen view

- [ ] Add `internal/ui/mermaid` as a dedicated Bubble Tea view rather than
  extending the chat transcript renderer.
- [ ] Render a header with the source name, diagram family, parser status, and
  current zoom percentage.
- [ ] Render a bounded canvas region with a footer for keys, diagnostics, and
  copy/reload notices.
- [ ] Keep source mode in the same full-screen view and render source lines with
  line numbers and parser diagnostics.
- [ ] Show an actionable empty state for no diagram, no selected Markdown block,
  and a source-only diagram.
- [ ] Keep all side effects in Tea commands; canvas updates and hit testing must
  remain deterministic.

## 3.2 Layout and rendering

- [ ] Add a shared grid layout pass that converts the Mermaid IR into terminal
  cells, node rectangles, edge routes, labels, notes, and source hit regions.
- [ ] Keep type-specific layout adapters behind the renderer registry so the
  canvas owns scrolling and interaction, not Mermaid grammar.
- [ ] Use ANSI-aware width calculations and terminal-safe glyph fallbacks. A
  diagram must remain legible when box-drawing characters are unavailable.
- [ ] Truncate labels only at the display boundary; never mutate the original
  Mermaid source.
- [ ] Color nodes, edges, statuses, and diagnostics through the existing theme.
- [ ] Expose selected element details without obscuring the diagram's source or
  changing the layout model.

## 3.3 Zoom controls and mouse behavior

- [ ] Define discrete zoom levels: 50%, 75%, 100%, 125%, 150%, and 200%.
- [ ] Treat a click outside all diagram hit regions as a background click. A
  background click toggles a floating control strip near the lower-right edge:
  `[-] 100% [+] reset`.
- [ ] Give the control strip explicit hit boxes. Clicking `[-]`, `[+]`, or
  `reset` changes zoom without selecting a node or leaking the click into the
  chat transcript.
- [ ] Hide the strip when the background is clicked again, when a node is
  selected, or after a short idle timeout if that matches existing notice
  behavior.
- [ ] Support mouse-wheel zoom over the canvas and mouse-wheel scrolling over
  the source view. Do not make wheel behavior depend on terminal pixel APIs.
- [ ] Add keyboard equivalents: `+`/`=` zoom in, `-` zoom out, `0` reset, and
  arrows or `h`/`j`/`k`/`l` pan.
- [ ] Clamp zoom and pan to known bounds. At 100% a small diagram should center;
  at other levels the viewport should never expose uninitialized cells.
- [ ] Preserve the zoom and pan state while switching between diagram and
  source mode, but reset it when a different file is loaded.

## 3.4 Commands and lifecycle

- [ ] Add `s` for source mode, `c` for copying the original or generated
  Mermaid source, `r` for reloading the current file, and `esc`/`q` for return.
- [ ] Refresh a loaded file only after an explicit `r`; never poll the file or
  mutate the user's source in the background.
- [ ] Return to chat without losing prompt text, transcript scroll, selection,
  model, or session state.
- [ ] Route window-size, mouse, and key messages to the Mermaid view before
  normal chat handling while it is open.
- [ ] Make `/sequence` use the same canvas and source mode as `/mermaid`.

## 3.5 Verification

- [ ] Add renderer tests for 80x24, 120x36, narrow labels, large diagrams,
  diagnostics, source mode, and all zoom levels.
- [ ] Add interaction tests for background clicks, control-strip hit boxes,
  node selection, wheel zoom, keyboard zoom, pan clamps, and event isolation.
- [ ] Add chat tests for open, close, reload, copy, resize, and prompt
  preservation.
- [ ] Manually verify a loaded Desktop file and a generated sequence diagram in
  a real terminal at 80x24 and 120x36.
