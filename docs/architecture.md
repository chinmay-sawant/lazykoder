# Architecture

## Overview

lazyKoder is a Bubble Tea TUI agent harness for the OpenCode Go API. v0.0.1
supports one provider (OpenCode Go) and one model family (default
`deepseek-v4-flash`). The app keeps its own project-local session store and
owns the tool loop: it is not a wrapper around the OpenCode CLI or its global
`~/.local/share/opencode/opencode.db`.

## Package map

| Package | Responsibility |
| --- | --- |
| `main.go` | init workspace, load key, start the tea program |
| `internal/workspace` | create `.lazykoder/`, open + migrate the db, ensure `.gitignore` |
| `internal/db` | numbered migrations + session/message/part/tool store |
| `internal/provider/opencode` | HTTP client for the OpenCode Go API |
| `internal/agent` | turn loop: user text -> provider -> parts, tool dispatch |
| `internal/policy` | bash classifier returning Allow/Ask/Deny |
| `internal/tools` | bash, read, write, edit, question, webfetch |
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
4. resume latest session for cwd, if any (transcript rebuilt from the store)
5. tea.NewProgram(chat.New(...))   # Workdir is the project cwd; env.Dir is .lazykoder for db + models.json
6. first send creates a session row, a user text part, provider calls,
   then assistant parts
```

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
model count and to feed the interactive picker (`m` in the chat view).

## Agent loop

One user turn runs in `internal/agent.Send` with a hard step bound (default
16) so a runaway model cannot spam confirm prompts:

1. Persist the user message + text part (create or resume a session).
2. Rebuild provider history from the store (messages, parts, tool calls).
3. Call the provider (bash tool advertised).
4. Write parts: `step-start`, `reasoning` (when present), `text` (when
   present), `tool` + `tool_calls` rows, `step-finish` (when usage is
   present).
5. For each tool call, classify and execute (see safety + tools docs).
6. Tool results go back to the model for the next step; loop until
   `finish_reason` is not `tool-calls`.

Everything the loop needs for a resumed session lives in the store; there is
no in-memory tool state.

## Module identity

`github.com/chinmay-sawant/lazykoder`, Go 1.26.4. The binary builds as
`bin/lk` via the Makefile.
