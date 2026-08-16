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

- Input: `{"url": "...", "format": "markdown"|"text"}`.
- http/https only (file:// and other schemes rejected). 30s timeout, 5 MiB
  body cap, `truncated` + `content_type` in metadata.

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
spawns; follow with `task_wait`). Default child step budget is 32
(configurable via settings `agents.child_max_steps` or per-spawn
`max_steps`). If a child hits its step limit after doing work, the job
completes with a partial summary and a note instead of status `failed`.

Schemas and pure JSON helpers live in `internal/tools/task`. Runtime
lifecycle is `internal/subagent.Manager` + `AgentRunner`. Job handles are
persisted in SQLite (`subagent_jobs`): after a crash or app restart,
`task_list` / `task_status` / `task_wait` still return finished summaries,
and open jobs are resumed (same child session when possible).

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
