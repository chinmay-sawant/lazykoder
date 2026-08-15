# v0.0.1 - OpenCode agent harness (lazyKoder)

> **Parent:** none - first canonical ledger for this product
> **Status:** implemented (all phases landed 2026-08-15; automated gates green, manual TUI rows pending user terminal)
> **Estimated effort:** 5-8 days across three phases
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **App version:** 0.0.1
> **Runtime:** Go 1.26.4 (`go.mod`)
> **TUI stack (already pinned in `go.mod` / `go.sum`):**
> `charm.land/bubbletea/v2 v2.0.8`, `charm.land/bubbles/v2 v2.1.1`, `charm.land/lipgloss/v2 v2.0.6`
> **Provider for this release:** OpenCode Go only

---

## Overview

lazyKoder is a Bubble Tea TUI agent harness. v0.0.1 supports one provider: OpenCode Go. The first shippable loop is: read `OPENCODE_API_KEY` from the environment, send a `hi` message, show the reply, and persist the conversation in a project-local SQLite database.

This is not a wrapper around the OpenCode CLI or its global `~/.local/share/opencode/opencode.db`. We call the OpenCode Go HTTP API ourselves, keep our own session store, and own the tool loop. Tools are designed now and wired later so the safety contract exists before any bash command can run.

Phase files (live ledgers, update `[x]` only after the gate passes):

| File | Priority | Goal |
| --- | --- | --- |
| [phase-1-foundation.md](phase-1-foundation.md) | P0 | Workspace, SQLite, auth, send `hi`, persist text |
| [phase-2-safety-bash.md](phase-2-safety-bash.md) | P0 | `rm` y/n confirm, bash tool, deny-by-default |
| [phase-3-tools-cleanup.md](phase-3-tools-cleanup.md) | P1 | Remaining tools, replay, employee-prototype cleanup |
| [phase-4-tokens-status-todos.md](phase-4-tokens-status-todos.md) | P1 | Streaming tokens/sec, status line customizations, tracked todos |

---

## Executive Summary

1. On launch, create `./.lazykoder/` in the current working directory if it is missing, open `./.lazykoder/lazykoder.db`, and run migrations. Never write a global OpenCode-style database.
2. Auth is environment-only for v0.0.1: `OPENCODE_API_KEY` (alias `OPENCODE_ZEN_API_KEY`). Missing key is a visible TUI error, not a crash with no message.
3. Phase 1 talks to `https://opencode.ai/zen/go/v1/chat/completions` with model `deepseek-v4-flash` (the model already used in `opencode_session_logs.json`). No tools in that request.
4. Conversations are stored as sessions / messages / parts, matching the OpenCode export shape in `opencode_session_logs.json`: `text`, `reasoning`, `step-start`, `step-finish`, `tool`. Tool rows further name `bash`, `edit`, `read`, `write`, `question`, `webfetch`.
5. Safety is a hard invariant: any bash invocation that is an `rm` command opens the employee-style y/n confirm (`Delete <subject>?` / `y confirm  •  n cancel`). `rm -rf` and other recursive deletes never run unless the user explicitly allows that one call. Decline, Escape, or dismiss means deny. There is no "always allow". The same confirm is used when stopping a sub-agent: the sub-agent name is the highlighted subject.
6. The employee CRUD prototype is gone. Only its y/n confirm layout is kept as the spec for rm and sub-agent prompts. Phase 1 writes a new `main.go`.

---

## Cleanup node (temporary employee prototype)

Done (user request, 2026-08-15). Prototype files removed. `go.mod` and `go.sum` were left untouched so the Charm v2 pin stays.

Removed:

- `main.go` employee wiring
- `internal/employee/`
- `internal/store/`
- `internal/ui/` (list/add/edit/delete + `ui_test.go`)
- `employees.json`

Kept on purpose: `go.mod`, `go.sum`, `AGENTS.md`, `.gitignore`, `plans/`, `skills/`, `opencode_session_logs.json`.

New harness code is written into empty packages (`internal/workspace`, `internal/db`, `internal/provider/opencode`, `internal/agent`, `internal/policy`, `internal/tools`, `internal/ui/chat`, `internal/ui/confirm`). Phase 1 creates `main.go` again as the chat entry.

The delete-confirm layout from the prototype is the design source of truth for every later y/n prompt (rm, sub-agent stop). See "Confirm view" below.

---

## Architecture

### Package map (target)

```
main.go                         # init workspace, load key, start tea program
internal/workspace              # mkdir .lazykoder, open db, append gitignore
internal/db                     # migrations + session/message/part/tool store
internal/provider/opencode      # HTTP client for OpenCode Go
internal/agent                  # turn loop: user text -> provider -> parts
internal/policy                 # bash classifier + Decision (allow/deny/ask)
internal/tools                  # bash, edit, read, write, question, webfetch
internal/ui/chat                # transcript, prompt, streaming status
internal/ui/confirm             # employee-style y/n view (rm + sub-agents)
```

Side effects live in `tea.Cmd`. `Update` stays deterministic. Confirm is a dedicated view, not an OS dialog.

### Launch sequence

```
cwd = process working directory
1. workspace.Init(cwd)
     mkdir .lazykoder/          (0755, exist-ok)
     open  .lazykoder/lazykoder.db
     migrate schema
     ensure .gitignore lists .lazykoder/ and secrets (append, do not clobber)
2. read OPENCODE_API_KEY (fallback OPENCODE_ZEN_API_KEY)
3. tea.NewProgram(chat.New(...), tea.WithAltScreen())
4. first user send ("hi") creates a session row, a user text part,
   one provider call, then assistant parts
```

### Provider (OpenCode Go only)

| Item | Value |
| --- | --- |
| Env | `OPENCODE_API_KEY` (alias `OPENCODE_ZEN_API_KEY`) |
| Default model | `deepseek-v4-flash` |
| Config id | `opencode-go/deepseek-v4-flash` |
| Chat URL | `https://opencode.ai/zen/go/v1/chat/completions` |
| Models URL | `https://opencode.ai/zen/go/v1/models` |
| Auth header | `Authorization: Bearer <key>` |

Phase 1 uses the chat-completions endpoint with no `tools` array. Streaming is optional (P1). Non-streaming is enough to prove the loop. Later phases map streamed deltas onto `step-start` / `reasoning` / `text` / `tool` / `step-finish` parts so the store matches `opencode_session_logs.json`.

Do not add an OpenAI/Anthropic/Gemini provider in 0.0.1.

### Local store (project SQLite, not OpenCode's global db)

OpenCode keeps one heavily shared database under the user home directory. We keep one small database per project:

```
<cwd>/.lazykoder/lazykoder.db
```

Schema is normalized. Do not dump the whole export JSON as a single blob. Keep hot query columns typed; keep only tool-specific leftovers as JSON.

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);

CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,
  title        TEXT    NOT NULL DEFAULT '',
  directory    TEXT    NOT NULL,
  provider     TEXT    NOT NULL DEFAULT 'opencode-go',
  model        TEXT    NOT NULL DEFAULT 'deepseek-v4-flash',
  variant      TEXT,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  status       TEXT    NOT NULL DEFAULT 'active'
);

CREATE TABLE messages (
  id           TEXT PRIMARY KEY,
  session_id   TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  role         TEXT    NOT NULL,          -- user | assistant
  agent        TEXT,
  provider_id  TEXT,
  model_id     TEXT,
  variant      TEXT,
  time_created INTEGER NOT NULL,
  seq          INTEGER NOT NULL
);

CREATE TABLE parts (
  id                  TEXT PRIMARY KEY,
  message_id          TEXT    NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  type                TEXT    NOT NULL,   -- text | reasoning | step-start | step-finish | tool
  time_created        INTEGER NOT NULL,
  seq                 INTEGER NOT NULL,
  text                TEXT,               -- text + reasoning body
  time_start          INTEGER,
  time_end            INTEGER,
  finish_reason       TEXT,               -- step-finish.reason (tool-calls, stop, ...)
  tokens_total        INTEGER,
  tokens_input        INTEGER,
  tokens_output       INTEGER,
  tokens_reasoning    INTEGER,
  tokens_cache_read   INTEGER,
  tokens_cache_write  INTEGER,
  cost                REAL,
  tool_name           TEXT,               -- bash | edit | read | write | question | webfetch
  tool_call_id        TEXT,
  tool_status         TEXT                -- pending | running | completed | error | denied
);

CREATE TABLE tool_calls (
  part_id       TEXT PRIMARY KEY REFERENCES parts(id) ON DELETE CASCADE,
  tool          TEXT NOT NULL,
  call_id       TEXT NOT NULL,
  status        TEXT NOT NULL,
  title         TEXT,
  time_start    INTEGER,
  time_end      INTEGER,
  exit_code     INTEGER,
  input_json    TEXT NOT NULL,            -- compact tool input
  output        TEXT,                     -- stdout / result
  metadata_json TEXT                      -- diff, answers, truncated, diagnostics
);

CREATE INDEX idx_messages_session_seq ON messages(session_id, seq);
CREATE INDEX idx_parts_message_seq    ON parts(message_id, seq);
CREATE INDEX idx_parts_type           ON parts(type);
CREATE INDEX idx_parts_tool           ON parts(tool_name) WHERE tool_name IS NOT NULL;
CREATE INDEX idx_tool_calls_tool      ON tool_calls(tool);
CREATE INDEX idx_tool_calls_status    ON tool_calls(status);
CREATE INDEX idx_sessions_updated     ON sessions(time_updated DESC);
```

Part types (from `opencode_session_logs.json`):

| `parts.type` | What we store |
| --- | --- |
| `text` | `text`, optional `time_start` / `time_end` |
| `reasoning` | `text`, `time_start` / `time_end` |
| `step-start` | marker only |
| `step-finish` | `finish_reason`, token counts, `cost` |
| `tool` | `tool_name` + row in `tool_calls` |

Tool names we will persist (Phase 2-3):

| Tool | Input (from the export) | Extra metadata |
| --- | --- | --- |
| `bash` | `command`, optional `workdir` | `exit`, `truncated`, stdout |
| `edit` | `filePath`, `oldString`, `newString` | `diff`, `filediff` |
| `read` | `filePath` | preview, line range |
| `write` | `filePath`, contents | path, bytes written |
| `question` | `questions[]` with options | `answers` |
| `webfetch` | `url`, `format` | content-type, truncated |

Large tool output stays in `tool_calls.output` for 0.0.1. A blob table is out of scope unless a single part exceeds a few megabytes.

SQLite driver: `modernc.org/sqlite` via `database/sql` (pure Go, no CGO). This is a new dependency. Per `AGENTS.md` it is a project-policy change and needs explicit user sign-off before `go get`. **Approved 2026-08-15 (user); `modernc.org/sqlite v1.56.0` added to `go.mod`.**

### Safety (hard invariant)

The executor never sees a raw model command. Every bash call goes through `internal/policy` first.

```
model tool-call (bash)
        |
        v
 policy.Classify(command)  -->  allow | ask | deny
        |
        +-- deny  --> store tool_calls.status = denied, do not exec
        +-- allow --> exec (non-rm only; rm never returns allow)
        +-- ask   --> ui/confirm view (same layout as employee delete)
                          +-- y     --> exec once, store completed/error
                          +-- n/esc --> status = denied, do not exec
```

Rules for 0.0.1:

1. Any command whose program token is `rm` (including `/bin/rm`, `sudo rm`, `command rm`, `xargs rm`, `find ... -exec rm`) is `ask`. Always. Current directory or not does not matter. `rm file.txt` in `.` still pops the modal.
2. `rm -r`, `rm -rf`, `rm --recursive`, `rm -fr` are still `ask`, but the modal title marks them as recursive force-delete. Copy must say the command will not run unless the user confirms.
3. There is no allow-list, no sticky approval, no "remember this path".
4. Uncertain parse is `ask`, never silent allow.
5. Default of the modal is deny. Focus starts on Deny. Enter on an untouched modal must not execute.
6. The modal is the only path to execution for `rm`. Tests must prove a classified `rm` cannot reach `exec.Command` without a confirm message.

`git rm` also contains an `rm` token. Prompt for it too. `rmdir`, `unlink`, `shred`, and `find -delete` are the same confirm class even if the binary is not named `rm`.

### TUI

Phase 1 screen: transcript (user + assistant text) + one-line prompt + status (key missing / sending / error). Phase 2 switches to the confirm view when policy returns `ask`. Phase 3 adds tool cards and session replay.

Reuse Charm widgets already in the module (`textinput`, `viewport` if needed). Do not hand-roll a prompt box.

### Confirm view (copy the employee delete view)

The removed prototype had one confirm screen. That is the only confirm design for this app. Do not invent a boxed overlay, button row, or "allow once" copy.

Layout (two blocks, same styles as the old `renderDelete`):

```
Delete <subject> (<qualifier>)?

y confirm  •  n cancel
```

- Line 1: error color for `Delete ` and the trailing qualifier/`?`; focused/bold color for the subject
- Blank line
- Hint line in muted color: `y confirm  •  n cancel`
- Full view switch (like `viewDelete`). Keys do not leak to the list, chat prompt, or transcript
- `y` / `Y` = confirm once
- `n` / `N` / `esc` / `q` = cancel
- `ctrl+c` = quit the app, never confirm
- Every other key is ignored

Subject mapping:

| Prompt | Subject (highlighted like an employee name) | Qualifier |
| --- | --- | --- |
| `rm` / destructive bash | the command string, or the path being removed | `rm` / `rm -rf` |
| Sub-agent stop, cancel, or dismiss | the sub-agent name | `sub-agent` |
| Binary `question` tool | the question text | optional header |

Example for a sub-agent named `lint-fix`:

```
Delete lint-fix (sub-agent)?

y confirm  •  n cancel
```

Example for `rm -rf /tmp/x`:

```
Delete rm -rf /tmp/x (rm -rf)?

y confirm  •  n cancel
```

A later sub-agent list, if we add one, follows the employee list: show the name as the item title, `d` (or the stop key) opens this same confirm with that name highlighted. Do not use a different confirm for sub-agents.

### Dependency policy

Already allowed (in `go.mod`): bubbletea v2, bubbles v2, lipgloss v2.

Needs sign-off before add:

- `modernc.org/sqlite` - project-local session store, CGO-free

No other new modules in 0.0.1. No OpenCode JS SDK. No CGO sqlite.

### What 0.0.1 will not do

- Other providers
- OpenCode CLI embedding / local opencode server
- Global `~/.local/share/opencode` reads or writes
- Auto-run of any `rm`
- Sticky tool permissions
- Employee CRUD (prototype removed; confirm layout kept as the y/n spec)

---

## Dependencies

- Phase 2 needs Phase 1 store + chat model (modal sits on the same tea.Model).
- Phase 3 tools need Phase 2 policy (bash) and Phase 1 parts schema.
- Employee cleanup is last so the Charm stack keeps compiling while the harness lands.

## Closure gates (whole 0.0.1)

- [x] `go test ./...` passes on a rebuilt tree - `go test ./... -count=1` 2026-08-15, exit 0, all 13 packages
- [x] `go vet ./...` clean - exit 0
- [x] App start with no `.lazykoder/` creates the dir and an empty migrated db - headless smoke: dir + lazykoder.db + WAL created, schema_migrations v1, 5 tables + 7 indexes
- [x] `OPENCODE_API_KEY` set: sending `hi` shows a reply and writes session/message/text parts - recorded-httptest proof (TestSendPhase1Gate, chat tests); live run left for the user (no key here)
- [x] Missing key: TUI shows an error, no panic - InitialErr path tested; headless run with no key exits cleanly
- [x] Any `rm` (including `rm ./file` and `rm -rf ...`) opens the y/n confirm view; decline does not execute - policy table (33 cases) + bash gate tests + chat confirm-flow tests; fake-runner proof that denied rm never reaches the runner
- [x] `.gitignore` contains `.lazykoder/`, `opencode_session_logs.json`, `.env`, and Go build artifacts - verified
- [x] Employee prototype removed only after the harness is the `main.go` entry (Phase 3) - prototype removed early by user request; harness main.go is now the only entry

Manual checks pending (need a live terminal / API key, cannot be gated in this session): phase-1 1.5 manual rows, phase-2 2.6 UI proof.

Do not mark a row `[x]` from intent. Record the command and exit code beside the row when it lands.
