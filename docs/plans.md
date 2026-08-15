# Plans and shipping

## The planning workflow

Plans live under `plans/v0.0.1/` and are live ledgers, not snapshots:

| File | Role |
| --- | --- |
| `README.md` | architecture, schema, safety contract, closure gates for 0.0.1 |
| `phase-1-foundation.md` | workspace, sqlite, auth, first OpenCode turn, chat TUI, policy stub |
| `phase-2-safety-bash.md` | confirm view, policy-to-executor seam, bash tool, tool parts |
| `phase-3-tools-cleanup.md` | remaining tools, part mapping, replay, prototype cleanup, `lk` run entry |

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
- Streaming token paint in the TUI

Future work gets its own phase folder (`plans/v.0.0.2/`) rather than a
second 0.0.1 ledger.
