# v0.0.1 / Findings - TUI and product-feel ledger

> **Parent:** `plans/v0.0.1/README.md` - chat TUI, confirm view, session replay
> **Status:** implemented 2026-08-16 (all three phases landed; automated gates green, tmux checked)
> **Estimated effort:** 4-6 days across three phases
> **Priority:** P0 for phase 1 (the app is unusable as a chat surface); P1 for phases 2-3
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Source:** live tmux inspection of `go run .` on 2026-08-16 (`lazykoder-ui-qa` at 120x36 and 80x24), plus the matching code paths
> **Gate:** a user can type any letter including `q`, reopen the latest project session, watch tool cards appear during a turn, and read a chat layout that is not a prefix dump

---

## Overview

Phases 1-3 of v0.0.1 shipped a working OpenCode Go harness: workspace, SQLite, policy-gated bash, six tools, model picker, slash commands. The data model is ahead of the view.

This folder is the live ledger for the 2026-08-16 TUI findings. It is not a second copy of phase 4 (streaming / tokens/sec / todos) or phase 5 (Go pattern db). Those stay in their own files. Streaming is called out here only as a dependency so this work does not re-plan it.

Inspected on a real pty (not a pipe). Headless `go run` cannot verify this TUI.

## Ratings recorded with the findings (not gates)

| Lens | Score | Note |
| --- | --- | --- |
| OpenCode-style coding chat | 3.5 / 10 | Loop works; not a daily driver |
| TUI / terminal GUI | 3 / 10 | Reads as a store dump, not a chat surface |
| Initiative / v0.0.1 foundation | 7 / 10 | Seams, store, policy, tests are solid |

Do not treat a rating as a checklist row. Close rows only with tests or a recorded tmux capture.

## Evidence (2026-08-16, do not re-diagnose from memory)

Confirmed against the running binary and `.lazykoder/lazykoder.db`:

1. Idle screen is the word `lazykoder`, ~20 blank rows, then a hint dump and a one-line prompt.
2. 35 sessions / 110 messages / 55 tool calls exist. Latest session is `ses_c7aafc6068d060aa` ("say hello world in the golang") with user + assistant + bash parts. The live transcript was empty.
3. Every session row stores `directory = <cwd>/.lazykoder`. Startup looks up `ListSessionsByDir(cwd)`. Replay is implemented and misses every row.
4. Typing `hel` then `q` killed `go run` and the dedicated tmux session. `internal/ui/chat/keys.go` handles `q` / `Q` before the prompt.
5. At 80x24 the status line wraps mid-phrase (`enter to` / `send`) and occupies three rows.
6. Slash card is ~80% width with a box inside a box. At 80 columns the selected description wraps into the next command row.
7. Model picker is the strongest screen. Left copy wraps `for` onto its own line at 120 columns. Scrollbar thumb sat near the bottom while `deepseek-v4-flash` was selected at the top.
8. `/help` writes one faint transcript line and clips at 80 columns (`q qui`).
9. `watchCmd` in `internal/ui/chat/keys.go` drains the whole event channel, then paints once. Busy state is `sending...`.
10. All 55 stored tool calls are `bash` (54 completed, 1 denied). `read` / `write` / `edit` / `webfetch` / `question` never appeared in real sessions.

## Phase files

| File | Priority | Goal |
| --- | --- | --- |
| [phase-1-p0-unusable.md](phase-1-p0-unusable.md) | P0 | `q` is a letter, replay finds sessions, live event paint, Esc cancels, session picker |
| [phase-2-chat-chrome.md](phase-2-chat-chrome.md) | P1 | Header, compact status, turn layout, collapsible tools, multiline prompt, slash, confirm overlay |
| [phase-3-polish.md](phase-3-polish.md) | P1 | Empty state, help overlay, one palette, edit/write diffs, `@` file picker |

## Out of scope (already ledgers elsewhere)

- Provider SSE, live tokens/sec, status-segment config, `todowrite`: [../phase-4-tokens-status-todos.md](../phase-4-tokens-status-todos.md). Phase 2 of this folder compacting the status line must not invent a second tps design. Leave a hook for phase 4.
- Go lint/improvisation pattern db: [../phase-5-go-pattern-db.md](../phase-5-go-pattern-db.md). Side quest. Do not start it from this folder.

## Rules that stay

- `Update` stays deterministic. Side effects in `tea.Cmd`.
- No new third-party dependencies without explicit user sign-off (`AGENTS.md`).
- Prefer existing Charm widgets (`textarea`, `viewport`, `list`) over hand-rolled chrome.
- `internal/ui/chat/chat.go` must not cross ~2,000 lines. Split by responsibility (`header.go`, `status.go`, `turns.go`) if a slice would push it.
- Never run the binary headless to claim a TUI gate. Use `go test ./internal/ui/chat` plus tmux capture (`skills/tmux-debug/SKILL.md`) at 120x36 and 80x24.
- No em dashes in docs, UI copy, or commit messages.
- Mark `[x]` only when the gate passed. Record the command and exit code. `[~]` needs a reason and a pointer.

## Dependencies

- Phase 1 of this folder unblocks phase 2 (replay + live events are the data the new chrome renders).
- Phase 2 unblocks phase 3 (palette and diffs land on the new turn/tool cards).
- Phase 4 streaming should land after 1.4 (live event paint). If phase 4 ships first, it must consume the incremental event path, not re-batch.
- Confirm overlay (2.7) evolves the parent "full view switch" spec. The y/n copy and deny-by-default stay. The full-screen wipe does not.

## Closure gates (whole findings folder)

- [x] `go test ./... -count=1` passes on a rebuilt tree - exit 0, 2026-08-16
- [x] `go vet ./...` passes - exit 0, 2026-08-16
- [x] Typing `question` in the prompt does not quit the app - `TestQTypesInPrompt` + tmux 2026-08-16
- [x] Restart in the project root replays the latest session for that project - tmux 2026-08-16, title `say hello world in the golang`
- [x] Tool cards appear while a turn is in flight - `TestLiveToolCardBeforeTurnEnds`
- [x] Full-screen render at 120x36 and 80x24: header + compact status, help overlay readable, slash footer, empty state, `@` picker - tmux 2026-08-16
- [x] `docs/tui.md` matches the new keys and layout

Manual checks recorded in tmux session `lazykoder-ui-qa` (killed after captures). Live API turn / Esc-cancel-on-real-key left to the user in a real terminal.
