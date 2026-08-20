## Summary

v0.0.9 changes how sessions start and end, and how project rules reach the
in-app model.

1. Launch always opens a blank session (no auto-resume). Past runs stay in
   SQLite and load via `/resume` or `ctrl+s`.
2. Confirmed quit prints the lazykoder wordmark plus `lk <session_id>` and a
   resume hint after the alt screen exits.
3. Workdir `AGENTS.md` (fallback `agents.md`) is prepended as a `system`
   message on every chat model call. Wire-only; not stored in the transcript.
   The TUI shows `project instructions: AGENTS.md` when the file is present.
4. Repo `AGENTS.md` now requires local knowledge-base updates with plan work,
   KB-then-code answers, and `/unslop` on prose.

## Motivation / context

- Plans: `plans/v0.0.9/` (phases 1-4)
- Product ask: fresh launch + quit banner, then inject workdir `AGENTS.md`
  into the in-app agent context (coding tools already read it; lazykoder did
  not)
- Issues: none filed

## Changes

### Fresh launch and quit banner

- `main.go` no longer passes the newest session into `chat.New`
- After `p.Run()`, print `chat.FormatQuitBanner(sessionID)` to stdout
- Empty transcript shows the same block logo as the quit screen
- Tips/docs/README aligned with fresh launch and quit banner

### In-app AGENTS.md context

- `LoadProjectInstructions` reads workdir `AGENTS.md` then `agents.md`
- Soft 200KB truncate with an explicit note
- Cached on `Agent`; prepended in `callModel` as `role=system`
- Compaction summarizer path unchanged (no AGENTS inject)
- TUI notice on alert row / empty state; cleared on send
- Tests cover loader, wire injection, and UI notice

### Agent / provider hardening (same branch)

- Expand multi-JSON tool argument payloads before dispatch
- Recover from unexpected tool panics so one bad call cannot crash the process
- Fill empty tool call IDs before sending chat requests

### Agent conventions (repo AGENTS.md)

- Keep gitignored `knowledge-base/` current with plan work
- Answer from KB first, always validate against live code
- Run `/unslop` before plans, docs, KB pages, and user-facing replies

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Extra tokens when workdir AGENTS.md is present |
| **Memory** | Cached AGENTS.md text per Agent |
| **Behavior / correctness** | No auto-resume; quit prints session id; model sees workdir AGENTS.md |
| **API / CLI** | Quit banner on stdout; `system` role on chat requests |
| **Dependencies** | None |
| **Binary size / build time** | Negligible |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| Auto-resume on launch removed | Use `/resume` or `ctrl+s` to open a past session |
| Model now sees workdir AGENTS.md | Put project rules in `AGENTS.md` at the run cwd |

## Test plan

- [x] `make test` (`go test ./...`)
- [x] `go build -o bin/lk .`
- [x] `go test ./internal/agent ./internal/ui/chat` for AGENTS inject + notice
- [~] `make lint` cleaned on branch for the prior findings; re-check CI
- [ ] Manual: launch with prior sessions present shows blank new session
- [ ] Manual: send a message, `ctrl+c` twice, see wordmark + `lk ses_...`
- [ ] Manual: relaunch blank, `/resume` loads the quit session
- [ ] Manual: quit before first send shows `lk (no session)`
- [ ] Manual: in this repo, see `project instructions: AGENTS.md` on launch

### Commands

```sh
make test
go build -o bin/lk .
make run
```

## Screenshots / sample output

Quit banner:

```text
  █    █▀▀█ ▀▀▀█ █  █ █ ▄▀ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█
  █    █▄▄█  ▄▀   ▀▀█ █▀▄  █  █ █  █ █▀▀▀ █▄▄▀
  ▀▀▀▀ ▀  ▀ ▀▀▀▀  ▀▀▀ ▀  ▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀  ▀
lk ses_<id>
resume with /resume or ctrl+s
```

TUI notice when workdir has AGENTS.md:

```text
project instructions: AGENTS.md
```

## Related issues

- None

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [x] Related issues filled (none)
- [x] Filled body under `plans/PR/pr-v009-fresh-launch-quit-banner.md`

## Follow-ups (out of scope)

- Manual TUI sign-off rows in `plans/v0.0.9/`
- Optional CLI `--resume ses_...` flag
- Committing `knowledge-base/` (stays gitignored by design)
- Settings toggle to disable AGENTS.md injection

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public API / CLI changes documented
- [ ] New rules have fixture coverage when applicable
- [ ] PR has assignee and labels
- [ ] Related issues use correct Closes/Relates keywords
- [ ] No secrets or generated artifacts committed
