# Architecture

## Overview

lazykoder is a Bubble Tea TUI agent harness for API-key and subscription
providers. OpenCode Go remains the default provider. `/provider` selects the
parent provider, while `orchestrator.provider` selects the child provider. The
app keeps its own project-local session store and owns the tool loop: it is not
a wrapper around the OpenCode CLI or its global `~/.local/share/opencode/opencode.db`.

## Package map

| Package | Responsibility |
| --- | --- |
| `main.go` | init workspace, load catalogs and key, start the tea program |
| `internal/workspace` | create `.lazykoder/`, bootstrap catalog files, open + migrate the db, ensure `.gitignore` |
| `internal/db` | numbered migrations + session/message/part/tool/recap/memory store |
| `internal/catalog` | approved local/global roots, bounded reads, and diagnostics |
| `internal/provider/opencode` | HTTP client for the OpenCode Go API |
| `internal/provider` | shared client contract, provider registry, discovery, auth, and factory |
| `internal/provider/openai` | OpenAI chat-completions client and model catalog |
| `internal/provider/subscription` | constrained Codex and Grok CLI adapters that retain lazykoder's tool boundary |
| `internal/agent` | turn loop, `buildHistory`, compact policy and summarizer run |
| `internal/orchestrator` | bounded no-tools planning and strict plan parsing |
| `internal/recap` | time-windowed snapshots, hidden no-tools workers, and atomic local artifacts |
| `internal/prompts` | embedded `compact.md` via `go:embed` (`prompts.Must`) |
| `internal/subagent` | Manager + Host + AgentRunner for concurrent children |
| `internal/policy` | bash classifier returning Allow/Ask/Deny |
| `internal/agent/toolplugin` | executable contract and registry for compiled or discovered tools |
| `internal/tools` | declarative shell-tool catalog plus bash, read, grep, write, edit, question, webfetch, and task schemas |
| `internal/roles` | built-in and discovered child-role registry and policy descriptors |
| `internal/settings` | project settings: provider, model, orchestration, slot, `agents` caps, tools, `compaction`, `recap`, skills, and API retry policy |
| `internal/ui/chat` | transcript, prompt, status line, model, provider, tool, and role pickers |
| `internal/ui/confirm` | the y/n confirm view (rm and question flows) |
| `internal/envfile` | stdlib-only `.env` loader |

Side effects live in `tea.Cmd`; `Update` stays deterministic. The confirm
screen is a dedicated full view, not an OS dialog.

## Launch sequence

```
cwd = process working directory
1. workspace.Init(cwd)
     mkdir .lazykoder/             (0755, exist-ok)
     open .lazykoder/lazykoder.db  (create if missing)
     migrate schema
     ensure .gitignore lists .lazykoder/ (append only)
     ensure settings.json, providers.json, tools.json, roles.json (0600)
2. envfile.Load(<cwd>/.env)        # keys; real env wins
3. load provider, tool, and role catalogs; load `provider.active`; create the parent client and create the child
   client from `orchestrator.provider` (OpenCode is the child default)
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

`bin/lk init` runs the same workspace bootstrap, prints only files created by
that invocation, and exits without starting Bubble Tea. A normal launch uses
the same idempotent path, so the explicit command is optional.

## Pluggable catalogs

Providers, tools, and roles use the same `Registry + Discover + Diagnostics`
shape. `internal/catalog` resolves only the project `.lazykoder` directory and
the configured global directory, rejects symlinked roots and files, and caps
descriptor file size and entry count. The provider, tool, and role loaders
merge global entries first and local entries second, so local IDs win. A bad
entry is reported beside valid entries and never blocks chat.

Compiled extensions register through `provider.Register`,
`internal/tools.Register`, or `roles.Register`. Declarative providers,
shell-backed tools, and child roles live in `providers.json`, `tools.json`, and
`roles.json` under `.lazykoder`; global mirrors live under
`~/.config/lazykoder`. Discovery reads metadata only. It never starts a CLI,
opens a provider connection, or runs a tool command. A discovered shell tool
still passes the policy classifier and workspace containment check at runtime.

The first ordinary parent send persists the user row, then resolves the
request-time tool and role providers alongside skills before calling the model.
Continue, hidden turns, compaction, and child sessions do not rescan. Child
jobs carry their explicit tool allowlist and role ID; the manager reads the
registered role's writer and model-class policy.

## Providers

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
- `ChatRequest.Endpoint` is the full provider URL from
  `.lazykoder/models.json`. Most Go models use
  `/zen/go/v1/chat/completions`; catalog routes that advertise the Responses
  protocol use `/zen/go/v1/responses`.
  Free Zen models (for example `deepseek-v4-flash-free`) use
  `/zen/v1/chat/completions`. Empty falls back to the client default.
- Responses map defensively: `reasoning` or `reasoning_content`, usage with
  common token key variants, tool calls with raw JSON arguments.
- HTTP errors become readable errors with status code and a body snippet.
- The API key is never logged, never persisted and never rendered.

The OpenCode client selects the wire protocol from the stored route. Chat
routes send `messages`; Responses routes translate the same transcript into
`input`, function tools, function calls, and function-call outputs, then parse
Responses SSE events back into lazykoder deltas. `GET /models` may advertise an
endpoint or API format, and that metadata owns the route without a model-ID
table. When a refresh returns only the generic chat route, an already cached
specialized route is retained until the provider supplies a replacement.

Provider catalogs are fetched at startup with a non-blocking 10s timeout to
show the model count and feed the interactive picker (`/model` or the footer
model chip). OpenCode uses its HTTP models endpoint. Codex uses
`codex app-server` and `model/list`. Grok uses `grok models`.

OpenAI uses `https://api.openai.com/v1/chat/completions` with
`OPENAI_API_KEY`, and xAI uses `https://api.x.ai/v1/chat/completions` with
`XAI_API_KEY`. Codex uses the persistent session created by `codex login` and
reads its current account-scoped model catalog through `codex app-server`
`model/list`. It does not carry a hard-coded Codex model name. Grok uses the
persistent session created by `grok login --device-auth` and reads its actual
available models through `grok models`. The subscription adapters request strict JSON from
the official CLI, encode tool arguments as JSON strings for the strict schema
contract, validate every requested lazykoder tool, and leave tool execution to
the existing policy layer. The hidden orchestrator plan call uses
the parent client; child jobs use the separately configured child client and
model settings. Provider identity is carried with each model row and session,
so duplicate model IDs cannot select the wrong client. Child model profiles
are narrowed to the configured child provider before execution.

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
    When the active provider owns a live default model, the worker resolves the
    configured memory model or current session model before reserving the
    update. If no concrete model is available yet, it skips the update instead
    of writing an invalid empty model. An empty variant resolves to the first
    supported variant for the selected model when the catalog provides one.
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

12. When sub-agents are enabled and a request is long enough to be safely
    decomposed, the parent makes one hidden no-tools planning call. A strict
    JSON plan is stored as an assistant `plan` part. The parent uses the task
    tool with the plan role and model class, keeps depth at one, and reviews
    failed or empty child summaries after `task_wait`. Each failed assignment
    gets at most one retry. Malformed or failed planning falls back to the
    ordinary turn.

13. After a successful parent turn leaves a dirty worktree, a Diff drawer
    appears above the composer for 90 seconds. `/diff` opens the same drawer
    manually. Its file rows show real unified diffs, and Enter opens a change
    list split at each `@@` section. Enter or clicking a change opens that
    section in a scrollable detail popup. Clicking its `commit and push` action
    or reaching the action row with Down starts a hidden control prompt in the
    normal agent loop. The existing bash policy gate confirms status, diff,
    commit, and push commands, and failures remain visible.

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

`/settings` exposes **auto-compact** and **compact at** (5% steps). The form
uses the same 5-99 bounds as the settings package, and a child timeout of `0`
means no timeout. Keyboard navigation follows the rows that are actually
painted, including the compact and full skill layouts.
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
still skip percent preflight. Overflow retry remains available. For
orchestrated tasks, the settings card owns model selection: the explore model
overrides the common child model for explore jobs, and either configured model
overrides the planner's `model_class`. The planner class is only a fallback
when the relevant setting is empty.

**Tool registry.** Base tool specifications and runners must have matching
names. The agent validates that registry before advertising or executing base
tools, so a partial registration cannot expose a model-visible tool that will
always return `unknown tool`.

**Recap cancellation.** Context-aware recap, memory, artifact, and evidence
entry points reject a nil context with `recap.ErrNilContext`. Deliberate root
callers create `context.Background()` or a timeout before entering the recap
package, so cancellation ownership is not silently discarded at a boundary.

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
