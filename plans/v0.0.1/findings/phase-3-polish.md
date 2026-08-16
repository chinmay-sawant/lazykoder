# v0.0.1 / Findings / Phase 3 - Polish

> **Parent:** `plans/v0.0.1/findings/README.md` - evidence items 1, 7, 8, 10
> **Status:** implemented 2026-08-16 (gates green)
> **Estimated effort:** 1-2 days
> **Priority:** P1 (after findings phase 2)
> **Gate:** empty state is useful, help is an overlay that wraps, one palette is used everywhere, edit/write cards show a diff, `@` can pick a project file

---

## Overview

Finish the chat surface so it feels like a product instead of a prototype.

## Executive Summary

- Empty project / new session teaches the first action.
- `/help` is an overlay, not a clipped transcript line.
- One palette in `internal/ui/theme`.
- `edit` / `write` cards show a path and a diff or preview.
- `@` file mention inserts a project-relative path.

## 3.1 Empty state (P1)

- [x] When `m.items` is empty, the transcript area renders a short empty state: what the app is, one example prompt, `/` hint
- [x] Empty state disappears as soon as the first user or replayed line exists
- [x] Test: new model with no session; `View()` contains the example hint - `TestEmptyStateShown`, exit 0
- [x] Test: after a line the empty state is gone - `TestEmptyStateShown`, exit 0

## 3.2 Help overlay (P1)

- [x] `/help` and `?` (empty prompt) open a centered overlay of key bindings. They do not append a transcript line
- [x] Overlay lists send, newline, slash, sessions, model, cancel turn, quit, scroll, copy
- [x] `esc` / `?` / `q` close the overlay. No `tea.Quit`
- [x] Test: `?`; `View()` contains the overlay and does not grow `m.items` - `TestHelpOverlayDoesNotGrowTranscript`, exit 0
- [x] Test: at width 80 help is fully visible (no `q qui` clip) - same test + tmux 80x24, 2026-08-16

## 3.3 One palette (P1)

- [x] `internal/ui/theme/theme.go`: background, surface, text, mute, accent, danger, border
- [x] Header, prompt, tool cards, slash, picker, confirm overlay, markdown code cards, selection, and status consume that palette
- [x] No new dependency. No theme picker
- [x] Test: `TestPaletteUsedForUserText`; existing view tests still pass

## 3.4 Diff-first edit / write cards (P1)

- [x] Collapsed `edit` / `write` header shows the path
- [x] Expanded `edit` card renders the stored unified diff
- [x] Expanded `write` card shows a short preview, not an unbounded dump
- [x] `bash` cards stay command + output
- [x] Test: fixture `edit` with diff metadata; expanded `View()` contains `@@` and the new string - `TestEditDiffCard`, exit 0
- [x] Test: fixture `write`; collapsed `View()` contains the path and not the full contents - `TestWriteCardHidesFullBody`, exit 0

## 3.5 `@` file picker (P1)

- [x] Typing `@` in the prompt opens a file picker over the project cwd. Walk skips `.git`, `.lazykoder`, `bin`, `node_modules`
- [x] Filter as the user types after `@`. `↑`/`↓` select, `enter` inserts the relative path, `esc` closes
- [x] Inserted path becomes part of the sent user text
- [x] Test: temp dir with `hello.go`; type `@hel`; `View()` lists `hello.go`; enter; prompt contains `hello.go` - `TestFilePickerInsertsPath`, exit 0
- [x] Test: `esc` closes the picker without submitting - `TestFilePickerEsc`, exit 0

## 3.6 Docs (P1)

- [x] `docs/tui.md` rewritten for header, compact status, textarea keys, slash popover, overlays, `@`, session picker, cancel-turn
- [x] `docs/architecture.md` launch sequence mentions project cwd vs `.lazykoder` dir
- [x] Parent `plans/v0.0.1/README.md` confirm section points at the overlay spec

## Dependencies

- Needs: findings phase 2
- Does not need: phase 4 streaming, phase 5 pattern db
- New dependencies: none

## Closure gates

- [x] `go test ./internal/ui/... -count=1` exit 0 - 2026-08-16
- [x] `go test ./... -count=1` exit 0 - 2026-08-16
- [x] `go vet ./...` exit 0 - 2026-08-16
- [x] tmux 80x24: empty state readable; `/help` overlay fully visible; `@` lists files - 2026-08-16
- [x] `docs/tui.md` matches the running keys
