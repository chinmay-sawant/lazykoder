# lazyKoder documentation

Index of the docs for the lazyKoder TUI agent harness. The docs complement the
live plan ledgers in `plans/v0.0.1/`; the ledgers are the source of truth for
status and closure gates, these pages explain the design and usage.

## Parts

| Doc | Covers |
| --- | --- |
| [architecture.md](architecture.md) | package map, launch sequence, provider, model handling |
| [storage.md](storage.md) | SQLite schema, migrations, store API, ids and timestamps |
| [safety.md](safety.md) | policy classifier, the confirm view, the executor gate |
| [tools.md](tools.md) | the six tools, agent loop wiring, part types |
| [tui.md](tui.md) | screens, keys, model picker, session replay |
| [development.md](development.md) | build, run, test, environment, project layout |
| [plans.md](plans.md) | how the plan ledgers work and what is shipped |

## Quick map

```
main.go                         init workspace, load key, start tea program
internal/workspace              mkdir .lazykoder, open db, append gitignore
internal/db                     migrations + session/message/part/tool store
internal/provider/opencode      HTTP client for OpenCode Go
internal/agent                  turn loop: user text -> provider -> parts
internal/policy                 bash classifier + Decision (allow/deny/ask)
internal/tools                  bash, read, write, edit, question, webfetch
internal/ui/chat                transcript, prompt, status, model picker
internal/ui/confirm             employee-style y/n view (rm + questions)
internal/envfile                stdlib-only .env loader
```

Stack: Go 1.26.4, `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`, `modernc.org/sqlite` (pure Go, no CGO). No other
dependencies.
