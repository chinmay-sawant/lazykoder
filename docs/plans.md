# Plans and shipping

## The planning workflow

Plans live under `plans/v0.0.1/` (shipped foundation), `plans/v0.0.2/`
(sub-agents), and later version folders. They are live ledgers, not
snapshots:

| File | Role |
| --- | --- |
| `v0.0.1/README.md` | architecture, schema, safety contract, closure gates for 0.0.1 |
| `v0.0.1/phase-*.md` | foundation through go-pattern-db phases |
| `v0.0.1/findings/` | 2026-08-16 TUI findings (shipped) |
| `v0.0.2/README.md` | sub-agent harness design + closure gates |
| `v0.0.2/phase-1-registry-settings.md` | tool registry + agents settings |
| `v0.0.2/phase-2-manager-tools.md` | Manager, task tools, Host seam |
| `v0.0.2/phase-3-runner-storage.md` | AgentRunner + child sessions |
| `v0.0.2/phase-4-tui-parallel.md` | parallel tasks, confirm queue, TUI |
| `v0.0.4/README.md` | SQLite schema FKs + indexes |
| `v0.0.5/README.md` | 2026-08-17 TUI layout + settings UX (implemented) |
| `v0.0.5/phase-1-trust-overlays.md` | opaque cards, `@` viewport, settings safety rows, `/help` |
| `v0.0.5/phase-2-chrome-settings.md` | slash palette, todos, footer chips, settings card |
| `v0.0.5/phase-3-polish-compact.md` | resume/model polish, overlay recipe, 80x24 |
| `v0.0.6/README.md` | transcript performance and meta keybindings |
| `v0.0.6/phase-1-meta-keys.md` | composer-safe meta keybindings |
| `v0.0.6/phase-2-tool-output-truncate.md` | bounded main-transcript tool output |
| `v0.0.6/phase-3-render-path.md` | cached transcript render path |
| `v0.0.7/README.md` | status visibility and OpenCode account profiles |
| `v0.0.7/phase-1-status-drawer.md` | compact status footer and persisted status drawer |
| `v0.0.7/phase-2-opencode-accounts.md` | dynamic OpenCode account profiles and active selection |
| `v0.0.8/README.md` | auto-compaction and mid-session model switch |
| `v0.0.8/phase-1-policy-prompts.md` | embedded compact prompt and pure policy helpers |
| `v0.0.8/phase-2-history-checkpoint.md` | request-time prune and compaction-part history |
| `v0.0.8/phase-3-llm-compact.md` | tools-off summarizer call and overflow retry |
| `v0.0.8/phase-4-model-switch-slash.md` | picker shrink hint, `/compact`, settings |
| `v0.0.8/phase-5-gates.md` | full test/build and architecture/TUI docs |
| `v0.0.9/README.md` | fresh launch + quit banner (`lk` + session id) |
| `v0.0.9/phase-1-fresh-launch.md` | stop auto-resume; launch blank; keep `/resume` |
| `v0.0.9/phase-2-quit-banner.md` | print `lk <ses_...>` + resume hint after alt screen |
| `v0.0.9/phase-3-docs-kb-gates.md` | docs, knowledge-base, tips/help, full gates |
| `v0.0.9/phase-4-agents-md-context.md` | inject workdir AGENTS.md as system primer |
| `v0.0.9/phase-4-agents-md-context.md` | inject workdir AGENTS.md as system primer |
| `v0.0.10/README.md` | planned local recap, questions, avoid rules, and first-request recall |
| `v0.0.10/phase-1-settings-and-recap-records.md` | recap settings and idempotent artifact records |
| `v0.0.10/phase-2-hidden-recap-worker.md` | time-windowed snapshot, questions, avoid rules, and atomic artifacts |
| `v0.0.10/phase-3-first-request-recall.md` | bounded internal grep before the first normal user request |
| `v0.0.10/phase-4-settings-ui-and-turn-scheduling.md` | settings rows, worker wiring, and post-turn scheduling |
| `v0.0.10/phase-5-docs-and-gates.md` | docs, local knowledge base, and release checks |

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

## Shipped later

- v0.0.2: parent `task` tools and concurrent child agents
- v0.0.5-v0.0.7: settings card, status drawer, usage, transcript polish
- v0.0.8: auto-compact, `/compact`, mid-session shrink hint, live fill
  meters (not a lifetime peak), child cost on `/agents` and status

## In progress

- v0.0.9 (branch `feature/v0.0.9-resume-session`): every launch opens a
  fresh session (no auto-resume); confirmed `ctrl+c` quit prints
  `lk <session_id>` plus a `/resume` hint on the normal console after the
  alt screen exits; workdir `AGENTS.md` is prepended as a system message on
  chat model calls. Automated gates green; manual TUI rows open. Ledger:
  `plans/v0.0.9/`.
- v0.0.10: planned optional recap artifacts. It will write ordered recaps,
  questions, and things to avoid under `knowledge-base/recaps/` after
  completed main-chat turns. The next parent user turn will run one bounded
  internal grep lookup before its first ordinary provider request. Ledger:
  `plans/v0.0.10/`.

## What 0.0.1 deliberately does not do

- Other providers (OpenAI, Anthropic, Gemini)
- OpenCode CLI embedding / local opencode server
- Reading OpenCode's global `~/.local/share/opencode/opencode.db`
- Auto-run of any `rm`, sticky tool permissions

TUI findings from the 2026-08-16 review live in `plans/v0.0.1/findings/`
so they stay next to the harness they audit. New product versions still
get their own folder (`plans/v0.0.2/`), not a second root ledger.
