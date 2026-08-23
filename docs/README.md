# lazykoder documentation

Index of the docs for the lazykoder TUI agent harness. The docs complement the
live plan ledgers in `plans/v0.0.x/`; the ledgers are the source of truth for
status and closure gates, these pages explain the design and usage.

## Parts

| Doc | Covers |
| --- | --- |
| [architecture.md](architecture.md) | package map, providers, agent loop, orchestration, compaction |
| [storage.md](storage.md) | SQLite schema, migrations, store API, ids and timestamps |
| [safety.md](safety.md) | policy classifier, the confirm view, the executor gate |
| [tools.md](tools.md) | base tools, task tools, agent loop wiring, part types |
| [tui.md](tui.md) | screens, keys, `/compact`, `/settings`, `/status`, session replay |
| [development.md](development.md) | build, run, test, environment, project layout |
| [plans.md](plans.md) | how the plan ledgers work and what is shipped |
| [tips.md](tips.md) | rotating in-app hints (keep in sync with `internal/tips`) |

## Quick map

```
main.go                         init workspace, load key, start tea program
internal/workspace              mkdir .lazykoder, open db, append gitignore
internal/db                     migrations + session/message/part/tool store
internal/provider               shared provider contract
internal/provider/opencode      HTTP client for OpenCode Go
internal/provider/openai        OpenAI chat-completions client
internal/agent                  turn loop, history, compact checkpoint
internal/prompts                go:embed compact.md summarizer prompt
internal/settings               project defaults: provider, model, agents, orchestration, compaction, retries
internal/policy                 bash classifier + Decision (allow/deny/ask)
internal/tools                  bash, read, grep, write, edit, question, webfetch, task
internal/ui/chat                transcript, prompt, status, model picker
internal/ui/confirm             y/n confirm view (rm + questions)
internal/envfile                stdlib-only .env loader
```

Stack: Go 1.26.4, `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`, `modernc.org/sqlite` (pure Go, no CGO). No other
dependencies.
