# v0.0.9 - Fresh launch and quit session banner

> **Parent:** `main.go` auto-resumes newest session; `chat.Update` two-step
> `ctrl+c` quit; knowledge-base `03-concepts/sessions-and-resume.md`
> **Status:** implemented on branch; manual TUI gates open
> **Branch:** `feature/v0.0.9-resume-session`
> **Estimated effort:** 2-3 days across four phases
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** launching `bin/lk` always opens a blank new session; confirming
> quit with `ctrl+c` leaves the alt screen, prints `lk <session_id>` plus a
> resume hint on the normal console, then exits 0; `/resume` / `ctrl+s`
> still loads past sessions; workdir `AGENTS.md` is sent as a system primer
> on chat model calls

This folder is the live ledger. Mark `[x]` only after the named gate
command passes. Do not claim TUI feel from headless `go run`.

---

## Overview

Today every launch picks the newest `kind=main` session for the cwd and
replays it into the transcript. That is convenient when you always want to
continue, and annoying when you wanted a clean slate.

Quit is also quiet. After the two-step `ctrl+c` confirm, Bubble Tea leaves
the alt screen and the process exits with no session id on the normal
console. If you closed the wrong run, you have to open `/resume` and guess.

v0.0.9 flips the default:

1. **Launch always starts fresh.** No auto-resume in `main.go`. Past runs
   stay in SQLite and stay reachable via `/resume` or `ctrl+s`.
2. **Confirmed quit prints a banner.** After the TUI tears down, stdout
   shows `lk` plus the session id (when one exists) plus a one-line resume
   hint, then the process exits.
3. **Workdir `AGENTS.md` becomes in-app context.** Each chat model call
   prepends it as a `system` message (wire-only). The TUI notes when it
   loaded.

User decisions locked for this plan:

- Fresh session on every launch (not opt-in).
- Banner shape: `lk` + session id + resume hint (not bare `LZ`).
- Inject workdir `AGENTS.md` only (fallback `agents.md`) as system on wire,
  with a TUI notice.

---

## Current code map (facts, do not re-discover)

| Behavior | Location |
| --- | --- |
| Auto-resume newest session | `main.go` `ListSessionsByDir` → `chat.Options.Session` |
| Replay on `New` | `internal/ui/chat/chat.go` `New` when `opts.Session != nil` |
| Explicit resume UI | `internal/ui/chat/sessions.go` `/resume`, `ctrl+s` |
| `/new` clears to nil session | `internal/ui/chat/slash.go` `loadSession(nil)` |
| Two-step quit | `internal/ui/chat/chat.go` `Update` ctrl+c → `quitConfirm` → `tea.Quit` |
| Quit cleanup channel | `internal/ui/chat/keys.go` `closeDone` |
| Alt screen | `internal/ui/chat/view.go` `newView` sets `AltScreen` |
| Program run ignores final model | `main.go` `if _, err := p.Run()` |
| Session id on model | `chat.Model.session` (`*db.Session`, id like `ses_...`) |
| Session created on first send | `internal/agent` / chat `adoptSession` path |

## Non-goals

- No CLI `--resume ses_...` flag in this version (hint points at in-app
  `/resume` / `ctrl+s`). A later plan can add argv resume if needed.
- No change to the two-step quit confirm itself (`ctrl+c` still arms, second
  `ctrl+c` quits; copy-when-prompt-has-text stays).
- No schema migration. Sessions already persist.
- No change to sub-agent `Manager.Recover` for open child jobs when a
  previous parent is not loaded (fresh launch means no parent session is
  attached; open jobs remain in SQLite and can be resumed when that parent
  is loaded via `/resume`).

## Phase files

| File | Goal |
| --- | --- |
| [phase-1-fresh-launch.md](phase-1-fresh-launch.md) | Stop auto-resume; launch blank; keep `/resume` |
| [phase-2-quit-banner.md](phase-2-quit-banner.md) | Print `lk <id>` + hint after alt screen exits |
| [phase-3-docs-kb-gates.md](phase-3-docs-kb-gates.md) | Docs, knowledge-base, tips/help, full gates |
| [phase-4-agents-md-context.md](phase-4-agents-md-context.md) | Inject workdir AGENTS.md as system primer |

## Shared invariants

- Print the banner **after** `p.Run()` returns so it lands on the normal
  console, not inside the alt screen buffer.
- If the user quits before the first send, there is no `ses_...` yet. Print
  `lk` plus a clear "no session yet" line, still with the resume hint for
  older runs.
- Banner text is fixed and tested; do not invent witty variants per quit.
- `/resume` remains the only resume path in this version.
- Existing sessions in `.lazykoder/lazykoder.db` are never deleted by this
  change.

## Suggested quit banner (exact copy)

Always starts with the lazykoder ASCII wordmark (`quitLogo` in
`internal/ui/chat/quit_banner.go`), then:

When a session id exists:

```text
lk ses_<16hex>
resume with /resume or ctrl+s
```

When no session was created yet:

```text
lk (no session)
resume older runs with /resume or ctrl+s
```

Plain stdout, no color required.

## Closure gates (all phases)

- [x] `go build ./...` exit 0
- [x] `go test ./internal/ui/chat` exit 0
- [x] `make test` exit 0
- [~] `make lint` exit 1 - pre-existing `mnd` / `unused` only (not from this change)
- [ ] Manual TUI: launch shows empty/new session even when older runs exist
- [ ] Manual TUI: send one message, `ctrl+c` twice, see banner with that
      `ses_` id on the normal console
- [ ] Manual TUI: relaunch is fresh; `/resume` still lists and loads the
      quit session
