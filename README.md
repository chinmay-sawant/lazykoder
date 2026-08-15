# lazyKoder

A Bubble Tea TUI agent harness for the OpenCode Go API. Type a message, get a
reply, and let the agent run real tools (bash, file edits, web fetches) with a
human safety gate on every destructive command. Conversations persist to a
project-local SQLite database.

## What it does

- Chat with OpenCode Go (`deepseek-v4-flash`) from a terminal TUI
- Persists sessions, messages and tool runs in `./.lazykoder/lazykoder.db`
- Runs tools (bash, read, write, edit, question, webfetch) through a policy
  gate: any `rm`-class command needs your explicit y/n confirmation
- Model picker: fetch the model list and switch models per session
- Session replay: reopening the app in the same directory resumes the latest
  session

## Quick start

Requirements: Go 1.26.4.

```sh
make build   # builds bin/lk
make run     # builds (if needed) and starts the app
```

Set your API key via the environment or a `.env` file in the repo root:

```
OPENCODE_API_KEY=your-key
```

(alias: `OPENCODE_ZEN_API_KEY`; real environment variables win over `.env`)

## Layout

| Path | Purpose |
| --- | --- |
| `main.go` | entry: workspace init, key load, chat program |
| `internal/` | workspace, db, provider, agent, policy, tools, ui |
| `plans/v.0.0.1/` | live plan ledgers with the closure gates |
| `docs/` | thorough documentation, split by topic |
| `Makefile` | build / run / test / vet / clean |

## Documentation

Thorough docs live in [docs/](docs/README.md): architecture, storage schema,
safety rules, tools, TUI, development workflow and the planning process.

## License

MIT, see [LICENSE](LICENSE).
