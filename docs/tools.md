# Tools

Six tools are wired into the agent loop. Every tool call is persisted as a
`parts` row (type `tool`) plus a `tool_calls` row, and the result is sent
back to the model for the next loop step.

## Dispatch

`internal/agent` dispatches by `tool_calls` name in `executeTool`. Unknown
names never crash the loop: they are stored with `status = denied` and
`output = "unknown tool: <name>"`, and the model gets the denial. This keeps
future tool names preserved in the store instead of dropped.

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

## webfetch

- Input: `{"url": "...", "format": "markdown"|"text"}`.
- http/https only (file:// and other schemes rejected). 30s timeout, 5 MiB
  body cap, `truncated` + `content_type` in metadata.

## Part mapping per step

Each provider step writes, in order: `step-start`; `reasoning` (only when
the API returns reasoning); `text` (only when content is present); `tool` +
`tool_calls` for every requested tool; `step-finish` (only when usage is
present, carrying `finish_reason`, token counts and cost).
