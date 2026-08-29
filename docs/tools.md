# Tools

Base tools plus an optional task control plane for sub-agents. Every tool
call is persisted as a `parts` row (type `tool`) plus a `tool_calls` row,
and the result is sent back to the model for the next loop step.

## Dispatch

`internal/agent` advertises tools from an allowlist (`ToolNames`, default
parent set) plus task tools when a `SubagentHost` is configured. Dispatch
is by name in `executeTool`. Unknown names never crash the loop: they are
stored with `status = denied` and `output = "unknown tool: <name>"`.
Allowlisted tools that are forbidden for the current role get
`tool not allowed: <name>`.

## Pluggable tools

`internal/agent/toolplugin` owns the registry for compiled and discovered
extensions. `internal/agent/tools_registry.go` keeps the built-in agent
handlers and parent allowlists. A compiled extension can call
`internal/tools.Register`, which delegates to the shared registry.

Set `tools.allow_discovered` to `true` in `.lazykoder/settings.json` and add a
JSON array to `<workdir>/.lazykoder/tools.json`:

```json
[
  {
    "name": "format-go",
    "description": "Format one Go file",
    "parameters": {
      "type": "object",
      "properties": {"file": {"type": "string"}},
      "required": ["file"]
    },
    "command": "gofmt -w {file}",
    "binaries": ["gofmt"]
  }
]
```

The optional global mirror is `~/.config/lazykoder/tools.json`; local names
replace global names. Descriptors are bounded and symlink files are rejected.
Discovery only reads and validates JSON. It never runs the command. Arguments
are shell-quoted before substitution. At execution time the command still
passes `policy.ClassifyWithAllowlist`, and its working directory must pass
`workspace.Resolve`. A missing executable or a failed command becomes an
error tool result. Discovered tools cannot replace built-in names.

The `tools.enabled` map controls which registered tools are advertised to the
model. `/tools` lists the current registry, filters by name or description,
and toggles each entry with Enter. Escape closes the picker.

Compiled tools implement the narrow contract in `internal/agent/toolplugin`:

```go
type Tool interface {
    Spec() opencode.ToolSpec
    Run(context.Context, string, toolplugin.Context) (string, string, string, error)
    Title(string) string
}
```

## bash

- Input: `{"command": "...", "workdir": "..."}`.
- Classified by `internal/policy` before anything runs (see safety).
- Output captured as stdout + stderr with exit code and timestamps.
- Result JSON to the model: `{"exit_code": N, "output": "...", "truncated":
  true?}` (capped at 8000 runes).

## read

- Input: `{"filePath": "..."}`. Path must stay inside the session directory
  (lexical containment + symlink escape checks).
- Output capped at 1 MiB with `truncated` in metadata; `lines` count in
  metadata. Missing file -> `status = error`, no panic.

## grep

Fast content search under the workdir (ripgrep when installed, pure-Go
fallback otherwise). Prefer this over reading many files to find symbols.

- Input: `{"pattern": "...", "path?": "...", "glob?": "*.go",
  "caseInsensitive?": false, "maxMatches?": 50}`.
- `path` is a file or directory under the workdir (default: workdir root).
- Output is `path:line:match` hits (capped; `matches` / `truncated` /
  `engine` in metadata). No matches returns `no matches` as a completed
  result (not an error).

## write

- Input: `{"filePath": "...", "contents": "..."}`. Inside session directory;
  parent dirs are not auto-created.
- Metadata: `bytes` written and the resolved `path`.

## edit

- Input: `{"filePath": "...", "oldString": "...", "newString": "..."}`.
- Fails when `oldString` is absent, not unique, or empty.
- Metadata: a line-based unified diff (LCS, `@@ -a,b +c,d @@` hunks, 3-line
  context, capped at 4000 chars).

## question

- Input: `{"questions": [{"question", "header", "options": [...]}]}`.
- Asks the human through the chat UI using the confirm view per question:
  subject = question text, qualifier = header. `y` picks option 0, `n`
  option 1 (questions with fewer than two options fail as an error).
- Metadata: `answers` (chosen option texts) and `indexes`.
- Parent only: child agents never receive this tool.

## webfetch

- Input: `{"url": "...", "format": "markdown"|"text", "mode": "auto"|"http"|"browser"}`.
- http/https only (file:// and other schemes rejected). 30s timeout, 5 MiB
  body cap, `truncated` + `content_type` in metadata.
- Local, private, link-local, multicast, and metadata destinations are
  rejected before the request and again at dial time. Redirects use the same
  check. `webfetch` copies a supplied client and never changes its redirect
  callback.
- Auto mode uses the guarded HTTP path first and falls back to an isolated
  Google Chrome process, with Chromium as fallback, for blocked or
  JavaScript-rendered pages. The browser uses a local validating proxy for
  page requests, redirects, and subresource origins.
- HTML responses return readable text plus bounded title, final URL, ordinary
  links, `mailto:` links, visible email addresses, mode, and truncation
  metadata. Links are reported as data and are not followed automatically.
- Browser mode records the requested URL as `final_url`; redirect-target
  capture requires a deeper browser protocol integration and remains open in
  the phase ledger.
- Browser mode never reuses the user's profile, cookies, saved credentials,
  extensions, or downloads. It does not submit forms or send email.

## Task tools (parent only)

When `settings.agents.enabled` is true, the parent agent gets:

| Tool | Purpose |
| --- | --- |
| `task` | Spawn a sub-agent (`prompt` required; optional `name`, `role`, `background`, `model`, `max_steps`) |
| `task_list` | List jobs for this parent session |
| `task_status` | Status for one id |
| `task_wait` | Wait for one id or all |
| `task_cancel` | Cancel one id or all |

Default `task` waits until the child finishes and returns a JSON summary.
`background: true` returns a handle immediately (preferred for parallel
spawns; follow with `task_wait`). Default child step budget is 1000
(configurable via settings `agents.child_max_steps` or per-spawn
`max_steps`). If a child hits its step limit after doing work, the job
completes with a partial summary and a note instead of status `failed`.

Background jobs do not inherit cancellation from the parent turn. They keep
running after the parent response ends and still obey their configured timeout.
`task_cancel` and manager shutdown cancel them explicitly. When a child model
has no explicit variant, the manager selects the first supported variant in
that model's profile.

Child wall-clock lifetime is **not** a `task` argument. It always comes
from project settings `agents.default_timeout_sec` (default 600s / 10m).
Model-supplied `timeout_ms` / `timeout_sec` fields are ignored so a parent
model cannot invent a 1s budget and kill every child.

Schemas and pure JSON helpers live in `internal/tools/task`. Runtime
lifecycle is `internal/subagent.Manager` + `AgentRunner`. Job handles are
persisted in SQLite (`subagent_jobs`): after a crash or app restart,
`task_list` / `task_status` / `task_wait` still return finished summaries,
and open jobs are resumed (same child session when possible).
Task responses use declared `SpawnResult`, `ListResult`, `StatusResult`,
`WaitResult`, and `CancelResult` JSON shapes.

### Child roles

| Role | Tools |
| --- | --- |
| `explore` / `plan` | `bash`, `read`, `grep`, `webfetch` (no write/edit; bash still gated by policy for rm) |
| `general` | `bash`, `read`, `grep`, `write`, `edit`, `webfetch` |

Children never get `task` or `question` tools (depth 1). Concurrent
`general` writers are serialized unless `allow_parallel_writers` is on.
Hard concurrent cap is 20 (default 4).

Multiple `task` tool calls in one step run in parallel under the manager
semaphore; other tools stay sequential.

## Part mapping per step

Each provider step writes, in order: `step-start`; `reasoning` (only when
the API returns reasoning); `text` (only when content is present); `tool` +
`tool_calls` for every requested tool; `step-finish` (only when usage is
present, carrying `finish_reason`, token counts and cost).

A compact run is not a field on that step. It inserts a separate
assistant message (`messages.agent = compaction`) with
`parts.type = compaction`. See [storage.md](storage.md) and
[architecture.md](architecture.md).
