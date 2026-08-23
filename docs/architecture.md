# Architecture

## Overview

lazykoder is a Bubble Tea TUI agent harness for the OpenCode Go API. v0.0.1
supports one provider (OpenCode Go) and one model family (default
`deepseek-v4-flash`). The app keeps its own project-local session store and
owns the tool loop: it is not a wrapper around the OpenCode CLI or its global
`~/.local/share/opencode/opencode.db`.

## Package map

| Package | Responsibility |
| --- | --- |
| `main.go` | init workspace, load key, start the tea program |
| `internal/workspace` | create `.lazykoder/`, open + migrate the db, ensure `.gitignore` |
| `internal/db` | numbered migrations + session/message/part/tool/recap/memory store |
| `internal/provider/opencode` | HTTP client for the OpenCode Go API |
| `internal/agent` | turn loop, `buildHistory`, compact policy and summarizer run |
| `internal/recap` | time-windowed snapshots, hidden no-tools workers, and atomic local artifacts |
| `internal/prompts` | embedded `compact.md` via `go:embed` (`prompts.Must`) |
| `internal/subagent` | Manager + Host + AgentRunner for concurrent children |
| `internal/policy` | bash classifier returning Allow/Ask/Deny |
| `internal/tools` | bash, read, grep, write, edit, question, webfetch with HTTP and isolated browser reading, task schemas |
| `internal/settings` | project settings: slot, model, `agents` caps, `compaction`, `recap`, skills, and API retry policy |
| `internal/ui/chat` | transcript, prompt, status line, model picker |
| `internal/ui/confirm` | the y/n confirm view (rm and question flows) |
| `internal/envfile` | stdlib-only `.env` loader |

Side effects live in `tea.Cmd`; `Update` stays deterministic. The confirm
screen is a dedicated full view, not an OS dialog.

## Launch sequence

```
cwd = process working directory
1. envfile.Load(<cwd>/.env)        # keys; real env wins
2. workspace.Init(cwd)
     mkdir .lazykoder/             (0755, exist-ok)
     open .lazykoder/lazykoder.db  (create if missing)
     migrate schema
     ensure .gitignore lists .lazykoder/ (append only)
3. read OPENCODE_API_KEY (fallback OPENCODE_ZEN_API_KEY)
4. tea.NewProgram(chat.New(...))   # Session is nil: every launch is fresh
                                   # Workdir is the project cwd; env.Dir is .lazykoder for db + models.json
5. first send creates a session row, a user text part, provider calls,
   then assistant parts
6. on quit, after alt-screen teardown, print `lk <session_id>` (or
   `lk (no session)`) plus a `/resume` hint to stdout
```

Past runs stay in SQLite. Load one explicitly with `/resume` or `ctrl+s`.

A missing key is not a crash: the TUI starts and shows the error in the
status line; the prompt stays usable.

## Provider (OpenCode Go only)

| Item | Value |
| --- | --- |
| Env | `OPENCODE_API_KEY` (alias `OPENCODE_ZEN_API_KEY`), optionally from `.env` |
| Default model | `deepseek-v4-flash` |
| Default chat URL | `https://opencode.ai/zen/go/v1/chat/completions` |
| Go models URL | `https://opencode.ai/zen/go/v1/models` |
| Zen models URL | `https://opencode.ai/zen/v1/models` |
| Auth header | `Authorization: Bearer <key>` on every Go and Zen request |

The client is OpenAI-compatible:

- `Chat(ctx, ChatRequest)` posts `model`, `messages` and optionally `tools`.
  The `tools` key is omitted entirely when no tools are advertised.
- `ChatRequest.Model` overrides the client default per request when non-empty
  (used by the model picker).
- `ChatRequest.Endpoint` is the full chat-completions URL from
  `.lazykoder/models.json`. Go models use `/zen/go/v1/chat/completions`.
  Free Zen models (for example `deepseek-v4-flash-free`) use
  `/zen/v1/chat/completions`. Empty falls back to the client default.
- Responses map defensively: `reasoning` or `reasoning_content`, usage with
  common token key variants, tool calls with raw JSON arguments.
- HTTP errors become readable errors with status code and a body snippet.
- The API key is never logged, never persisted and never rendered.

`GET /models` is fetched at startup (non-blocking, 10s timeout) to show the
model count and to feed the interactive picker (`/model` or the footer
model chip).

## Project instructions (AGENTS.md)

When the session workdir contains `AGENTS.md` (fallback `agents.md`), every
chat model call prepends a `role=system` message with that file's contents.
The primer is wire-only: it is not stored in SQLite and does not appear in
`/resume` transcripts. Compaction summarizer calls do not include it. The
TUI shows `project instructions: AGENTS.md` on the alert row / empty state
when the file loaded. Oversized files are truncated around 200KB with an
explicit note.

## Agent loop

One user turn runs in `internal/agent.Send` with a hard step bound (default
16) so a runaway model cannot spam confirm prompts:

1. Persist the user message + text part (create or resume a session).
2. Rebuild provider history from the store (`buildHistory`). If a
   compaction checkpoint exists, history starts at that summary plus the
   kept tail. Older tool bodies become `[old tool result cleared]` in
   the request only.
3. Preflight compact when needed (see Compaction). Then rebuild history
   again so the next call sees the checkpoint.
4. Call the provider with the advertised tool set (base tools + task tools
   when a `SubagentHost` is wired). One provider overflow retries after a
   compact; a second overflow is returned as an error.
5. A normal chat step writes `step-start`, `reasoning` (when present),
   `text` (when present), `tool` + `tool_calls` rows, and `step-finish`
   (when usage is present). A compact run writes a **separate** assistant
   message (`messages.agent = compaction`) with one `parts.type =
   compaction` envelope. It is not a field on the chat step.
6. For each tool call, classify and execute (see safety + tools docs).
   Task-family tools in one step run concurrently under the subagent
   semaphore; other tools stay sequential.
7. Tool results go back to the model for the next step; loop until
   `finish_reason` is not `tool-calls`.
8. After the configured number of successful completed main-session turns, the
   TUI schedules one hidden `internal/recap` worker. It snapshots up to five
   newest eligible messages, writes recap/question/avoid artifacts, and marks
   the SQLite record complete only after all required renames succeed.
   The worker requests compact JSON with a 4,000-token output ceiling and
   rejects non-stop provider finishes before parsing the envelope. The parser
   escapes raw control characters inside model string values, then applies the
   strict field and citation checks.
   Provider, validation, and artifact failures mark that record failed with a
   bounded error. `/agents` reloads the record and displays that error while
   keeping the worker out of the transcript.
9. Before the first ordinary provider request for a parent turn, the agent
   runs the bounded recall and skill providers after persisting the user row.
   Skills come from `<workdir>/skills`, `<workdir>/.agents/skills`, and the
   configured global roots. Explicit activation precedes local and then global
   automatic matches. The selected bodies are one untrusted, wire-only system
   block and are never stored in SQLite, recap artifacts, or the memory file.
   Skill scans do not run for tool follow-ups, `/continue`, compaction, child
   sessions, or hidden workers. The TUI reports a separate skill-scan status.
10. After every successful completed main-session turn, a separate hidden memory
   worker uses a bounded two-to-five-message snapshot. It reads the
   current project `knowledge-base/memories.md` and a bounded grep of related
   local knowledge evidence, then asks the selected recap model for a strict
   JSON memory envelope. Explicit user signals are restored into their
   authoritative sections before valid facts are merged into the aggregate and
   written with an atomic rename. The model sees a redacted aggregate without
   historical message IDs or the source ledger. The prompt lists the current
   snapshot IDs as the only valid citation choices. A correction is stored as
   a supersession, which keeps the old fact marked `superseded` instead of
   deleting it. If only short-lived `recent_context` citations are missing,
   the worker makes one repair call and drops that context if it remains
    untraceable. Durable sections remain strict. A SQLite `memory_updates` row
    records the source anchor, model, attempts, digest, stage durations, and
    any failure so restarts can retry safely. The worker keeps one final prompt
    budget, trims related evidence before current memory content, and compacts
    large memory documents to entries sourced by the current window. It skips
    a no-op update when the stored aggregate already covers the new user-only
    context. Aggregate reading and related-evidence search run concurrently.
11. `/history` reuses the sub-agent drawer shell to show only memory entries
    sourced from the active chat. Entries are ordered by `last_seen_utc`, show
    twenty per page, and open in the same scrollable detail card. A real
    memory-worker error stays in the history view with its original cause.
    Memory-document parser errors also appear in that view and clear after a
    successful reload. The detail card supports app-level mouse drag
    selection and `ctrl+a` / `ctrl+c` copy without arming quit confirmation.
    Insufficient source context does not interrupt chat, but its failed ledger
    row can still appear in history. Each row
    also reads its matching `memory_updates` record and starts with a colored
    dot and status label: green for completed, red for failed, and accent for
    queued or running. Failed attempts without a memory entry still appear so
    the history can show what failed. The detail card uses the edit, assistant,
    and error panel backgrounds already used by transcript and recap views.

Everything the loop needs for a resumed session lives in the store; there is
no in-memory tool state for the parent transcript.

### First-request recall

When `recap.enabled` or `skills.enabled` is true, `internal/ui/chat` runs one bounded, quoted
grep after persisting a new parent user message and before its first ordinary
provider request when the prompt contains recall language. It searches
`knowledge-base/memories.md` first, then `knowledge-base/recaps/`, and finally
the broader Markdown knowledge base only when earlier sources have no match.
Matches become one wire-only system block after `AGENTS.md` instructions. The
block is marked untrusted and is not persisted. Tool follow-ups, `/continue`,
compaction, and child sessions do not repeat the lookup. Missing or malformed
memory files are treated as empty recall sources. The chat status line uses a
separate animated marker while this lookup or the hidden memory update runs.

### Skill catalog and settings

`internal/skills` resolves only approved project and configured global roots.
It accepts `SKILL.md` and legacy `SKILLS.md`, parses bounded metadata, rejects
symlinked roots and files, and returns deterministic diagnostics instead of
failing the chat turn. `/skills` and `/skill` use the model-drawer interaction
family to list and activate a descriptor for the next parent request. The
persisted `skills` settings group controls discovery, automatic matching,
source scopes, reference remembering, and byte limits. Skill references are
merged into the version 2 `Skills` section of `knowledge-base/memories.md`
using code-owned paths and hashes. Skill bodies are never accepted from the
model memory envelope.

## Compaction

The TUI still paints the full human transcript. Compaction only shrinks
what the next provider call sees.

**Trigger.** Auto-compact fires when
`max(tokensUsed, chars/4 of the request) > window * percent / 100`.
Default `percent` is 80 (range 5-99). Exactly 80% does not fire. Unknown
window (`0`) never fires. Estimate is 4 characters per token.

**Four paths.**

| Path | Gated by `compaction.auto`? |
| --- | --- |
| Same-model preflight over the percent threshold | yes |
| Mid-session shrink (`reason = model-shrink`) | no |
| One provider overflow retry (`reason = overflow`) | no |
| Manual `/compact` (`reason = manual`) | no |

`auto: false` turns off same-model percent preflight only. `/compact`,
shrink-on-next-send, and the overflow retry still run.

**Settings** live in `.lazykoder/settings.json`:

```json
"compaction": { "auto": true, "percent": 80, "keep_tokens": 15000 }
```

`/settings` exposes **auto-compact** and **compact at** (5% steps).
`keep_tokens` is JSON-only (default 15,000). `0` or omitted is treated
as 15,000. There is no `buffer` key; an old `buffer` field is ignored.

Transient chat failures use the project retry policy. The default is five
retries after the initial request, with a ten-second delay between attempts.
Only HTTP 500 and 503 responses qualify. Authentication failures do not retry.
The policy is stored as `retry.max_retries` and `retry.delay_seconds` and can
be changed in `/settings`.

**History.** The latest `compaction` part wins. `buildHistory` injects
one synthetic user message (`This session continues from a compacted
conversation...` plus the summary), then messages from
`tail_start_message_id` onward. SQLite rows are never deleted.
`messages.visible` is TUI history-delete only and is ignored here.

**Summarizer.** Tools off, `MaxTokens` 4096, prompt from
`internal/prompts/compact.md` (eight headings). Extra `/compact` notes
append a Compact instructions block. If the incoming window cannot hold
the head, the outgoing (larger) model writes the checkpoint. A huge head
is split and combined. Empty summary or a head that still cannot fit is
a hard error; the session is kept.

**Meters.** Token fill is the latest request input, or the checkpoint
`tokens_after` (summary + tail). It is not a lifetime peak. Parent cache
hit/miss reset to 0 at compact, then grow again. Cost uses
`parts.cost` when set, otherwise `messages.model_id` plus
`.lazykoder/models.json` prices.

**Children.** The chat model boundary passes the selected child model's ID,
endpoint, supported variants, and known context window to `subagent.Job`.
When the cache has a window, child auto-compaction can use it. Unknown windows
still skip percent preflight. Overflow retry remains available.

## Sub-agents

Parent turns may call `task` tools. `internal/subagent.Manager` caps
concurrency (default 4, hard max 20), runs each child as `agent.Agent` on a
hidden child session (`kind=subagent`), and returns only a final summary to
the parent model. Settings live under `agents` in
`.lazykoder/settings.json`. Depth is 1: children cannot spawn further tasks.
The manager stores the queued job before starting its runner. A later store or
recovery failure is surfaced through the manager and the TUI.

## Module identity

`github.com/chinmay-sawant/lazykoder`, Go 1.26.4. The binary builds as
`bin/lk` via the Makefile.
