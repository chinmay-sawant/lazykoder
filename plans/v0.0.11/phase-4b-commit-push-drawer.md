# Phase 4b - Diff drawer with commit and push action

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** in progress (Diff drawer, manual `/diff`, real diff payloads, separated change navigation, auto-expanded file selection, and right-side code scrolling landed; docs updated 2026-08-29; lint green for commit_* files, remaining 10 pre-existing lint findings in add_provider/picker tracked separately)
> **Estimated effort:** 4-5 days
> **Predecessor:** `phase-4-commit-push-button.md` (complete - button above composer with 90s auto-hide)

## Overview

Replace the transient commit-and-push button with a Diff drawer that mirrors the `/model` channel. When the worktree is dirty after a successful change, the drawer shows the changed file list with line counts and a real diff preview. `/diff` opens the same drawer on demand, including a `no changes` state for a clean worktree. Enter or a click on a file opens its first `@@` section directly in a scrollable code context view. Escape returns to a bordered change list split at each `@@` section. Users navigate files with arrows, `j/k`, and mouse input, and collapse the drawer from the right-hand side. Commit and push remain an explicit user action and still route through the existing policy gate.

## 4b.1 Drawer shell and parity with /model

- [x] New drawer mode in `internal/ui/chat` (e.g. `commitDrawerMode`) that reuses `drawer.go:drawerChrome` and `drawerRowLine` so header, meta, body, and hint match the model drawer. Landed as `commit_drawer.go:commitDrawerView` + `commitDrawerChrome` reusing the same surface/background/width frame.
- [x] Header shows Diff context (branch, ahead/behind or dirty count) and meta shows file count. Hint footer mirrors `/model` style. Header is `Diff · <branch>` with `· <n> files` meta and hint `↑/↓ select · click file · enter view diff · esc or [x] collapse · scroll`.
- [x] Collapse control on the right-hand side of the header (same placement as model drawer). Click or `esc` collapses. State is transient, not persisted to SQLite. `[x]` is right-aligned like the model drawer's close control; `handleCommitDrawerKey` and `commitDrawerCloseRect` handle esc/click, no SQLite persistence.

## 4b.2 Changed files and line counts

- [x] Source changed files without running git commands directly in the TUI process. Reuse the existing checker seam injected from `main.go` (the same seam phase 4 uses for `git status --porcelain`). Tests use the seam with fixtures. `commit_action.go:WorktreeInfo` + `worktreeDirtyChecker` now returns branch, files, counts, and per-file unified diff payloads; `DefaultWorktreeDirty` combines status, numstat, staged fallback, and untracked-file diff reads; view layer only consumes `WorktreeInfo` via `worktreeStatusMsg`.
- [x] For each file show `+added -removed` or total changed lines. Compute from the diff supplied by the checker or from a cached diff payload. Handle renames and binary files with a placeholder count. `commitFileCounts` renders `+N -M`, `binary`, or `changed`; renames collapsed to ` -> new` target in `parsePorcelainFiles`/`worktreeNumstat`.
- [x] Sort and truncate like the model list. Rows use `drawerRowLine` with selection marker `▸`. `parsePorcelainFiles` sorts, `commitDrawerView` caps at 8 rows with `commitDrawerMaxRows` and scrolls the window so the selected row stays visible.

## 4b.3 Diff preview and file navigation

- [x] Right pane or detail area shows a preview of the selected file diff. Use the existing markdown/diff rendering path if available, otherwise plain truncated diff. `DefaultWorktreeDirty` supplies per-file unified diff payloads through `WorktreeInfo.Diffs`; `commitDrawerView` renders six preview rows, and `openCommitDiffDetail` splits the payload into `@@` change sections with horizontal separators. Selecting a file by click or Enter opens all sections directly in a bordered code viewport using `renderDiff`. The code viewport reserves one scrollbar column on the right, keeps horizontal separators between sections, and avoids wrapping the scrollbar into orphan rows. The popup header shows the file path, `+added -removed` summary, file position such as `2 of 10 files`, and change position such as `change 2 of 4`. Missing payloads still show a clear file or binary placeholder. `UnifiedDiff` drops only the synthetic trailing split line so multi-line edits do not render an extra blank row.
- [x] Left-click selects a file and updates the preview. Right-click also selects (parity with request). Support mouse scroll for the file list, change list, and expanded code view. `commitDrawerHit` handles both MouseLeft and MouseRight; the drawer wheel cycles `commitDrawerSelected`, the change-list wheel selects sections, and the expanded-code wheel updates its viewport.
- [x] Keyboard: `up`/`down` or `j`/`k` moves file selection in the drawer. `enter` opens all of the selected file's `@@` sections in expanded code. Escape returns to the change list, where the same keys select sections; Enter or a click expands the selected section while keeping all sections visible. Up/Down or wheel scrolls expanded code. Left/Right switches to the previous or next file and refreshes the path, summary, file count, change count, and view. Enter or Escape returns from expanded code to the change list; Escape closes the popup from the list. Down from the final file focuses the action row, where Enter calls `activateCommitPush`.

## 4b.4 Mouse and hit-testing

- [x] Extend `internal/ui/chat/prompt_mouse.go` hit-testing to the drawer rows, preview pane, change sections, expanded code view, previous/next controls, and collapse button on the right-hand side. Implemented as `commit_drawer.go:commitDrawerCloseRect`, `commitDiffDetailCloseRect`, `commitDiffDetailNavRect`, `commitDiffHunkIndexAtScreenY`, `commitDrawerActionRect`, `commitDrawerIndexAtScreenY`, and `pointerInCommitDrawer`; `mouse.go:mousePress` checks the detail popup before drawer and prompt geometry and respects both left and right buttons.
- [x] Mouse clicks work when the drawer is open alongside the composer. Status chip and other drawers remain clickable and do not conflict. Drawer check is early in `mousePress` but only when `commitDrawerVisible()` and within drawer bounds; otherwise falls through to status chip, sub-agent drawer, and prompt handling; transcript selection is unaffected.

## 4b.5 Trigger and lifecycle

- [x] Same trigger as phase 4: a completed assistant turn with zero tool errors plus a non-empty worktree per the checker seam. Setting `pushPromptUntil` now opens the drawer instead of showing the button row. `chat.go:eventDoneMsg` sets `commitFiles`/`commitBranch` from `WorktreeInfo` and opens the drawer; `view.go:chatScreen` and `composerTop`/`transcriptRenderHeight` already reserve space, and `commitPushRow` is suppressed while the drawer is visible.
- [x] Manual `/diff` runs the same injected checker and opens the same timed drawer. Dirty worktrees show their file rows and previews; clean worktrees show the branch and `no changes` instead of silently doing nothing. `slashCommands` registers `/diff`, and `runSlashArg` routes it to the checker seam.
- [x] Auto-hide timer (90s `tea.Tick`) collapses the drawer if not interacted with. Any click or key inside the drawer resets or cancels the timer. Activation (commit and push) collapses immediately. `scheduleCommitPushExpiry` + `commitActionExpiredMsg` collapse; `resetCommitDrawerTimer` extends `pushPromptUntil` on file select, wheel, and `j`/`k`/`up`/`down`; `activateCommitPush` zeroes `pushPromptUntil` immediately.
- [x] No new git commands in the view layer. The LLM commit-and-push flow from phase 4.3 stays unchanged and still goes through the policy gate. All `git` calls remain in `commit_action.go` behind the injected seam; `commit_drawer.go` only renders the cached preview and detail payload.

## 4b.6 Commit and push action inside the drawer

- [x] Drawer exposes a focused action row or button (e.g. `Commit and push`) at the bottom, styled like `forms.go` focused buttons. `commitDrawerView` renders a centered `Bold+Border` `[ commit and push ]` line with `Background ColorBorder` like the former button, at the bottom of the drawer body.
- [x] Clicking it builds the same one-shot agent turn as phase 4.3 (status, diff, recent log, conventional-commit instruction) and executes `git add -A && git commit && git push` via the policy gate. Failures render an alert row. `commitDrawerActionRect` hit-tests the button and delegates to `activateCommitPush`; the prompt `commitActionPrompt` and flow are unchanged.

## 4b.7 Docs, knowledge base, and gate

- [x] Update `docs/tui.md`, keymap cheatsheet, component map, and `knowledge-base/` in the same change. The docs call the UI the Diff drawer, document `/diff`, describe the separated change list, horizontal rules, expandable code context, file and change counts, and keyboard/mouse controls. They keep the `commit and push` action name for the policy-gated operation. `knowledge-base/wiki/architecture/component-map.md` documents the shared automatic/manual checker path, `WorktreeInfo.Diffs`, and the detail view extensions.
- [ ] Gate: `make lint` PASS, `make test` PASS (including drawer, spacing, change-list, mouse, and checker-seam tests), then live TTY check by user. Make a real edit, see the drawer with file list and counts, select a file with click or Enter and confirm all of its changes open automatically with horizontal separators, use Escape to inspect the horizontally separated change list, select changes with Up/Down or mouse wheel, open the selected position with Enter or a click while keeping all sections available in the expanded viewport, scroll its code with Up/Down and mouse wheel, confirm the scrollbar stays on the right without extra rows, switch files with Left/Right or the popup controls, collapse with `[x]`, and confirm auto-hide after 90s when idle. Record outcomes beside these rows. No git commands run outside the policy-gated agent turn. Verification on 2026-08-29: `go build ./...` PASS, `go vet ./...` PASS, `go test ./internal/tools/edit ./internal/ui/chat -count=1` PASS, and `go test ./internal/ui/chat -count=1` PASS. `go test ./... -count=1` remains blocked by the unrelated existing `internal/recap` failure `TestBuildMemoryPromptCompactsToCurrentSourceEntries`; `make lint` remains blocked by the existing findings in `add_provider.go`, `picker.go`, and `provider_delete.go`. Live TTY verification on 2026-08-29 confirmed `/diff`, multi-section navigation, `change 2 of 10`, scrolling, and the right-side scrollbar. The final human idle-time check remains open because the app must not be run headless.

## Out of scope

- Full editor for staging hunks or editing diffs inline.
- Persisting drawer open state across restarts.
- Any `git` invocation outside the injected checker seam and the policy-gated commit flow.
