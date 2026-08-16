# Plans and shipping

## The planning workflow

Plans live under `plans/v0.0.1/` (shipped foundation) and `plans/v0.0.2/`
(sub-agents). They are live ledgers, not snapshots:

| File | Role |
| --- | --- |
| `v0.0.1/README.md` | architecture, schema, safety contract, closure gates for 0.0.1 |
| `v0.0.1/phase-*.md` | foundation through go-pattern-db phases |
| `v0.0.1/findings/` | 2026-08-16 TUI findings |
| `v0.0.2/README.md` | sub-agent harness design + closure gates |
| `v0.0.2/phase-1-registry-settings.md` | tool registry + agents settings |
| `v0.0.2/phase-2-manager-tools.md` | Manager, task tools, Host seam |
| `v0.0.2/phase-3-runner-storage.md` | AgentRunner + child sessions |
| `v0.0.2/phase-4-tui-parallel.md` | parallel tasks, confirm queue, TUI |

Each checklist row is marked `[x]` only when the gate actually passed, with
the command and exit code recorded beside the row. Rows are never checked
from intent. Manual TUI gates that need a live terminal stay open until a
human runs them.

## Shipped so far (v0.0.1)

- Project-local workspace + SQLite store with migrations
- OpenCode Go chat client (env + `.env` auth, model override)
- Chat TUI with transcript, prompt, status and session replay
- Policy gate: every `rm`-class command requires an explicit confirm
- Tools: bash, read, write, edit, question, webfetch
- Model list at startup + interactive model picker
- `lk` binary via the Makefile

## What 0.0.1 deliberately does not do

- Other providers (OpenAI, Anthropic, Gemini)
- OpenCode CLI embedding / local opencode server
- Reading OpenCode's global `~/.local/share/opencode/opencode.db`
- Auto-run of any `rm`, sticky tool permissions
- Live tokens/sec, status-segment toggles, and todowrite (phase 4 remainder)

TUI findings from the 2026-08-16 review live in `plans/v0.0.1/findings/`
so they stay next to the harness they audit. New product versions still
get their own folder (`plans/v0.0.2/`), not a second root ledger.
