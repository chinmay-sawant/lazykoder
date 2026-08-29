# Development

## Requirements

- Go 1.26.4 (see `go.mod`)
- A supported provider authentication method for live chats. See
  [providers.md](providers.md) for API keys, subscription sign-in, and
  `/provider` behavior.

## Build, run, verify

| Command | What it does |
| --- | --- |
| `make build` | builds `bin/lk` (creates `bin/` on demand) |
| `make run` | rebuilds and runs `./bin/lk` |
| `make test` | full gate: `go test -p 2 -parallel 2 ./...` |
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
- `/provider` distinguishes selected from not selected. API-key rows show key
  set or missing. Codex and Grok rows check their official CLI session and
  show sign-in state without reading tokens. Do not claim a provider is
  reachable solely because a key or CLI exists.

## Project layout

```
main.go                       entry: workspace init, key load, chat program
internal/
  workspace/                  .lazykoder init, db open, gitignore
  db/                         schema, migrations, store API
  envfile/                    .env loader
  provider/                    shared provider contract, catalog, and factory
  provider/opencode/           OpenCode Go HTTP client
  provider/openai/             OpenAI-compatible client wrapper
  agent/                      turn loop, history, compact, tool dispatch
  prompts/                    embedded compact.md summarizer prompt
  subagent/                   Manager, Host, AgentRunner (concurrency)
  settings/                   project settings: slot, model, agents, compaction, retries
  policy/                     bash classifier
  tools/{bash,read,grep,write,
         edit,question,
         webfetch,task}/      tool implementations / schemas
  ui/chat/                    chat TUI (transcript, prompt, picker)
  ui/confirm/                 y/n confirm view
plans/v0.0.1/                0.0.1 plan ledgers
plans/v0.0.2/                sub-agent plan ledgers
plans/v0.0.8/                auto-compaction + mid-session model switch
docs/                         this documentation set
skills/                       agent-harness conventions (AGENTS.md)
```

## Dependencies

Pinned in `go.mod`: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`, `modernc.org/sqlite` (pure Go, no CGO). Adding a
dependency is a project-policy change and needs explicit sign-off; prefer
stdlib and the existing Charm stack.

## Verification habits

- Rebuild before verifying (`make build` / `make test`); never trust a
  stale binary.
- After a UI/layout change, run the app and check the full screen, not just
  the changed area.
- A task is done only when the final gate exits 0; record the command and
  exit code next to the plan rows when they land.
