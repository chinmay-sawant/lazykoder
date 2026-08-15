# v0.0.1 / Findings / Phase 3 - Polish

> **Parent:** `plans/v0.0.1/findings/README.md` - evidence items 1, 7, 8, 10
> **Status:** planned 2026-08-16 (no rows landed; mark `[x]` only when the gate passes)
> **Estimated effort:** 1-2 days
> **Priority:** P1 (after findings phase 2; do not start while the P0 bugs are open)
> **Gate:** empty state is useful, help is an overlay that wraps, one palette is used everywhere, edit/write cards show a diff, `@` can pick a project file

---

## Overview

Finish the chat surface so it feels like a product instead of a prototype. These slices sit on the chrome from findings phase 2. They do not include streaming tokens/sec (phase 4) or the Go pattern db (phase 5).

## Executive Summary

- Empty project / new session should teach the first action, not show 20 blank rows.
- `/help` must not be a clipped transcript line.
- Colors are currently ANSI 1/3/5/6/8/15 plus two hex greys, declared in several files. One palette, one place.
- Real sessions have never used `edit` / `write` (55/55 tool calls are bash). When those tools do run, the card should show a path and a diff, not a raw dump.
- `@` file mention is what turns a chatbot-with-bash into a coding tool.

## 3.1 Empty state (P1)

- [ ] When `m.lines` is empty, the transcript area renders a short empty state: one line of what the app is, one example prompt, one hint that `/` opens commands. Centered or top-aligned in the pane, not 20 blank rows plus a title
- [ ] Empty state disappears as soon as the first user or replayed line exists
- [ ] Test: new model with no session; `View()` contains the example hint and does not rely on a lone `lazykoder` word as the only content - exit 0
- [ ] Test: after replay of a fixture session the empty state is gone - exit 0

## 3.2 Help overlay (P1)

Evidence: `/help` appended `help: enter send  •  ...  •  q qui` and clipped at 80 columns.

- [ ] `/help` and `?` (when the prompt is empty) open a centered overlay card of key bindings, wrapped to the card width. They do not append a transcript line
- [ ] Overlay lists send, newline, slash, sessions, model, cancel turn, quit (`ctrl+c`), scroll, copy. Content matches `docs/tui.md`
- [ ] `esc` / `?` / `q` (overlay only) close the overlay and return to chat. No `tea.Quit`
- [ ] Test: run `/help`; `View()` contains the overlay and does not grow `m.lines` - exit 0
- [ ] Test: at width 80 every help line is fully visible (no `q qui` clip); card width < terminal width - exit 0

## 3.3 One palette (P1)

- [ ] Introduce a small palette in `internal/ui` (e.g. `internal/ui/theme/theme.go` or `internal/ui/chat/theme.go`): background, surface, text, mute, accent, danger, border. Hex (or lipgloss adaptive) values, not ad-hoc ANSI indexes scattered in `view.go`, `chat.go`, `picker.go`, `slash.go`, `transcript.go`, `confirm.go`, `markdown.go`
- [ ] Header, prompt, tool cards, slash, picker, confirm overlay, markdown code cards, selection, and status all consume that palette
- [ ] No new dependency. No theme picker in this phase (one default dark theme)
- [ ] Test: compile + existing view tests still pass. Spot-check that `View()` still contains expected words (not color codes). Optional: one test that a known style uses the palette hex rather than `Color("6")` for user text

## 3.4 Diff-first edit / write cards (P1)

Evidence: `edit` already stores a unified diff in `metadata_json`. The current `renderTool` ignores it and prints `output` only. Real db has zero `edit` / `write` rows, so this is unexercised, not unused code we can skip.

- [ ] Collapsed `edit` / `write` header shows the path (from input JSON or title), not the whole file body
- [ ] Expanded `edit` card renders the stored unified diff (add/remove lines styled via the palette). Fall back to output text if metadata is missing
- [ ] Expanded `write` card shows path + byte count + a short preview, not the entire contents unless expanded further or already small
- [ ] `bash` cards stay command + output (findings 2.4). Do not force a diff onto bash
- [ ] Test: fixture `edit` tool_call with `metadata_json` diff; expanded `View()` contains a hunk header (`@@`) and the new string - exit 0
- [ ] Test: fixture `write`; collapsed `View()` contains the path and does not contain the full contents - exit 0

## 3.5 `@` file picker (P1)

- [ ] Typing `@` in the prompt (not inside a pasted block) opens a file picker over the project cwd. List is files under cwd, respecting `.gitignore` if cheap (or a bounded walk: skip `.git`, `.lazykoder`, `bin`). No new dependency
- [ ] Filter as the user types after `@`. `↑`/`↓` select, `enter` inserts the relative path (and closes the picker), `esc` closes and leaves `@` in the prompt
- [ ] Inserted path becomes part of the sent user text. The agent already has `read`; no extra tool required in this phase
- [ ] Test: temp dir with `hello.go` and `skip/me.go` optional; type `@hel`; `View()` lists `hello.go`; enter; prompt contains `hello.go` - exit 0
- [ ] Test: `esc` closes the picker without submitting - exit 0

## 3.6 Docs (P1, same change as the slices)

- [ ] `docs/tui.md` rewritten for header, compact status, textarea keys, slash popover, overlays, `@`, session picker, cancel-turn. No leftover `q` global-quit or `user:` prefix description unless those still exist
- [ ] `docs/architecture.md` launch sequence mentions project cwd vs `.lazykoder` dir (findings 1.2)
- [ ] Parent `plans/v0.0.1/README.md` confirm section points at the overlay spec if 2.7 landed

## Dependencies

- Needs: findings phase 2 (header, cards, overlays, textarea). `@` can share the slash-popover geometry
- Does not need: phase 4 streaming, phase 5 pattern db
- New dependencies: none. File walk is stdlib `os` / `filepath`

## Closure gates

- [ ] `go test ./internal/ui/... -count=1` exit 0 - pending
- [ ] `go test ./... -count=1` exit 0 - pending
- [ ] `go vet ./...` exit 0 - pending
- [ ] tmux 80x24: empty state readable; `/help` overlay fully visible; `@` lists a file - pending
- [ ] `docs/tui.md` matches the running keys - pending
