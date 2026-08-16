# Development

## Requirements

- Go 1.26.4 (see `go.mod`)
- An OpenCode API key for live chats (`OPENCODE_API_KEY`, alias
  `OPENCODE_ZEN_API_KEY`)

## Build, run, verify

| Command | What it does |
| --- | --- |
| `make build` | builds `bin/lk` (creates `bin/` on demand) |
| `make run` | rebuilds and runs `./bin/lk` |
| `make test` | full gate: `go test ./...` |
| `make vet` | `go vet ./...` |
| `make clean` | removes `bin/lk` |

The `lk` binary is gitignored (`/bin/`). Run the app from the directory you
want to work in: it creates `.lazykoder/` there and persists sessions for
that directory.

## Environment and secrets

- Keys are read from the process environment first, then from a `.env` file
  in the current working directory (stdlib-only parser; real env wins).
- `.env`, `.lazykoder/`, `opencode_session_logs.json` and `bin/` are
  gitignored. The key is never logged, persisted or rendered.

## Project layout

```
main.go                       entry: workspace init, key load, chat program
internal/
  workspace/                  .lazykoder init, db open, gitignore
  db/                         schema, migrations, store API
  envfile/                    .env loader
  provider/opencode/          OpenCode Go HTTP client
  agent/                      turn loop, part mapping, tool dispatch
  subagent/                   Manager, Host, AgentRunner (concurrency)
  settings/                   project settings including agents caps
  policy/                     bash classifier
  tools/{bash,read,write,
         edit,question,
         webfetch,task}/      tool implementations / schemas
  ui/chat/                    chat TUI (transcript, prompt, picker)
  ui/confirm/                 y/n confirm view
plans/v0.0.1/                0.0.1 plan ledgers
plans/v0.0.2/                sub-agent plan ledgers
docs/                         this documentation set
skills/                       agent-harness conventions (AGENTS.md)
```

## Dependencies

Pinned in `go.mod`: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`, `modernc.org/sqlite` (pure Go, no CGO). Adding a
dependency is a project-policy change and needs explicit sign-off; prefer
stdlib and the existing Charm stack.

## Verification habits

- Rebuild before verifying (`make build` / `go test ./...`); never trust a
  stale binary.
- After a UI/layout change, run the app and check the full screen, not just
  the changed area.
- A task is done only when the final gate exits 0; record the command and
  exit code next to the plan rows when they land.
