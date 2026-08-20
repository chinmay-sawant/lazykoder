## Summary

Every launch opens a blank session instead of auto-resuming the latest run.
Past sessions stay in SQLite and load via `/resume` or `ctrl+s`. Confirmed
quit prints the lazykoder wordmark plus `lk <session_id>` and a resume hint
after the alt screen exits. `AGENTS.md` now requires local knowledge-base
updates with plan work, KB-then-code answers, and `/unslop` on prose.

## Motivation / context

- Plans: `plans/v0.0.9/`
- Issues: none filed; product ask was fresh launch + quit banner, then agent
  conventions for KB and unslop

## Changes

### Fresh launch and quit banner

- `main.go` no longer passes the newest session into `chat.New`
- After `p.Run()`, print `chat.FormatQuitBanner(sessionID)` to stdout
- Empty transcript shows the same block logo as the quit screen
- Tips/docs/README aligned with fresh launch and quit banner

### Agent / provider hardening (same branch)

- Expand multi-JSON tool argument payloads before dispatch
- Recover from unexpected tool panics so one bad call cannot crash the process
- Fill empty tool call IDs before sending chat requests

### Agent conventions

- `AGENTS.md`: keep gitignored `knowledge-base/` current with plan work
- Answer from KB first, always validate against live code
- Run `/unslop` before plans, docs, KB pages, and user-facing replies

## Impact

| Area | Impact |
|------|--------|
| **Performance** | None material |
| **Memory** | None material |
| **Behavior / correctness** | Launch no longer auto-resumes; quit prints session id for `/resume` |
| **API / CLI** | Console quit banner after TUI exit |
| **Dependencies** | None |
| **Binary size / build time** | Negligible |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| Auto-resume on launch removed | Use `/resume` or `ctrl+s` to open a past session |

## Test plan

- [x] `make test` (`go test ./...`)
- [x] `go build -o bin/lk .`
- [~] `make lint` still reports pre-existing findings unrelated to this PR
- [ ] Manual: launch with prior sessions present shows blank new session
- [ ] Manual: send a message, `ctrl+c` twice, see wordmark + `lk ses_...`
- [ ] Manual: relaunch blank, `/resume` loads the quit session
- [ ] Manual: quit before first send shows `lk (no session)`

### Commands

```sh
make test
go build -o bin/lk .
make run
```

## Screenshots / sample output

```text
  █    █▀▀█ ▀▀▀█ █  █ █ ▄▀ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█
  █    █▄▄█  ▄▀   ▀▀█ █▀▄  █  █ █  █ █▀▀▀ █▄▄▀
  ▀▀▀▀ ▀  ▀ ▀▀▀▀  ▀▀▀ ▀  ▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀  ▀
lk ses_<id>
resume with /resume or ctrl+s
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

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public API / CLI changes documented
- [ ] New rules have fixture coverage when applicable
- [ ] PR has assignee and labels
- [ ] Related issues use correct Closes/Relates keywords
- [ ] No secrets or generated artifacts committed
