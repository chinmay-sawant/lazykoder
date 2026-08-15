# v0.0.1 / Phase 1 - Foundation, workspace, and first OpenCode turn

> **Parent:** `plans/v.0.0.1/README.md` - architecture and schema
> **Status:** implemented 2026-08-15 (automated gates green; 2 manual TUI rows left for a live terminal)
> **Estimated effort:** 1.5-2.5 days
> **Priority:** P0 (this is the 0.0.1 goal)
> **Gate:** send `hi` with `OPENCODE_API_KEY`, show a reply, persist it under `./.lazykoder/`

---

## Overview

Stand up the harness on a clean tree. The employee prototype is already gone (`go.mod` / `go.sum` kept). End of phase: a chat TUI that reads the OpenCode key from the environment, sends one text turn, renders the assistant text, and writes a session into project-local SQLite.

## Executive Summary

- Create `./.lazykoder/` and `lazykoder.db` on every start (exist-ok).
- Migrate the sessions / messages / parts / tool_calls schema from the parent plan.
- HTTP client for OpenCode Go chat-completions only. No tools in the request.
- Create `main.go` as the chat entry and add `internal/ui/chat`. Confirm views, when added in Phase 2, must use the employee-delete layout in the parent plan.
- Policy package is compiled and unit-tested in this phase but not wired to an executor. No bash runs in Phase 1.

## 1.1 Workspace and ignore files (P0)

- [x] Add `internal/workspace` with `Init(cwd string) (Dir, error)` that creates `<cwd>/.lazykoder` at `0755` when missing - implemented 2026-08-15 (workspace.go); `go test ./internal/workspace` exit 0
- [x] `Init` is idempotent: a second start on an existing dir does not wipe the db or any files inside it - tested (init twice -> 1 db file)
- [x] `Init` opens `<cwd>/.lazykoder/lazykoder.db` (create if missing) - via `db.Open` + `Migrate`
- [x] `Init` appends `.lazykoder/` to `<cwd>/.gitignore` only if that line is absent; never rewrite an existing ignore file from scratch at runtime - append-only, full-line match
- [x] Repo `.gitignore` already lists `.lazykoder/`, `opencode_session_logs.json`, `.env` / `.env.*`, and Go binary/coverage paths (done at plan time; re-check if someone edits it) - verified present 2026-08-15
- [x] Test: temp dir with no `.lazykoder` -> `Init` -> dir exists, db file exists, ignore line present - `go test ./internal/workspace` exit 0
- [x] Test: `Init` twice -> one db file, ignore file not duplicated into garbage - `go test ./internal/workspace` exit 0

## 1.2 SQLite schema and store (P0)

- [x] User sign-off recorded for adding `modernc.org/sqlite` (policy change; do not `go get` before this) - approved 2026-08-15 via interactive question (`go get modernc.org/sqlite@latest` -> v1.56.0)
- [x] Add `modernc.org/sqlite` and open via `database/sql` (no CGO, no `mattn/go-sqlite3`) - v1.56.0, DSN `file:...?_pragma=foreign_keys=on&_pragma=busy_timeout=5000` + `PRAGMA journal_mode=WAL` after open
- [x] `internal/db` runs numbered migrations; `schema_migrations` records version 1 after first open - tested idempotent
- [x] Tables match parent plan: `sessions`, `messages`, `parts`, `tool_calls` with the listed indexes - all 7 index statements verbatim from README
- [x] WAL + `foreign_keys=ON` + `busy_timeout=5000` set on every connection - via DSN pragmas + WAL exec
- [x] Store API: `CreateSession`, `InsertMessage`, `InsertPart`, `ListMessages`, `ListParts` (tool_calls insert can be a no-op helper until Phase 2) - plus `InsertToolCall`, `ListSessionsByDir`, `UpdateToolCall`, `DeleteSession`
- [x] IDs use a readable prefix (`ses_`, `msg_`, `prt_`) plus a unique suffix - `NewID` crypto/rand 8 bytes hex
- [x] Test: migrate empty file -> all four tables exist (`sqlite_master` count matches) - `go test ./internal/db` exit 0 (7 tests)
- [x] Test: insert session + user text part + assistant text part, reload process, counts match - persist-and-reopen test
- [x] Test: deleting a session cascades messages and parts - `DeleteSession` test

## 1.3 OpenCode auth and client (P0)

- [x] `internal/provider/opencode` reads `OPENCODE_API_KEY`, then `OPENCODE_ZEN_API_KEY` - `APIKeyFromEnv`
- [x] Empty / missing key returns a typed `ErrMissingAPIKey`, never a 401 surprise later
- [x] Client default base: `https://opencode.ai/zen/go/v1`
- [x] `Chat(ctx, req)` POSTs `/chat/completions` with `Authorization: Bearer <key>`, `model: deepseek-v4-flash`, `messages: [{role, content}]`
- [x] Request has no `tools` field in this phase - `json:"tools,omitempty"`, nil slice omits the key (tested)
- [x] Response maps to assistant text; HTTP errors become a readable error (status + body snippet) - `opencode: chat request failed: status 401: ...` (cap ~300 chars)
- [x] Key is never logged, never written to SQLite, never rendered in the TUI - key not in any error string; TUI render uses it only in Authorization header
- [x] Test: missing env -> `ErrMissingAPIKey` - `go test ./internal/provider/opencode` exit 0 (10 tests)
- [x] Test: httptest server -> `Chat` sends Bearer header and model id, returns the stubbed content
- [x] Optional P1: `GET /models` health check on startup (do not block `hi` on it) - implemented 2026-08-15 as part of the model-list feature: `Models` client method (tested via httptest) + startup fetch in the chat model (10s timeout, async, never blocks sending). Status line shows "models: N available" or a red error. Interactive picker added on top: `m` opens a bubbles-list of models, `enter` selects, persisted via `db.UpdateSessionModel` to `sessions.model`, and sent as `ChatRequest.Model` on every turn (`agent.Options.Model`; per-request override in the opencode client). Tests: `TestChatRequestModelOverride`, `TestUpdateSessionModel`, `TestSendModelOption`, `TestModelsFetchedOnStartup`, `TestModelPickerSwitchAndPersist`, `TestModelPickerCancel` - all exit 0, `-race` clean
- [x] User sign-off + addendum 2026-08-15: load `./.env` before auth so `OPENCODE_API_KEY` / `OPENCODE_ZEN_API_KEY` can come from the repo `.env` - new `internal/envfile` (stdlib-only parser: comments, quotes, `export` prefix; real process env wins; missing file is a no-op; no new dependency). Wired in `main.go` before `APIKeyFromEnv`; `go test ./internal/envfile` exit 0 (4 tests)

## 1.4 Agent turn (hi only) (P0)

- [x] `internal/agent` `Send(ctx, sessionID, userText)` writes the user message, calls the provider, writes the assistant message - agent.Send full loop (session create/resume, history rebuild from store, tool loop); `go test ./internal/agent` exit 0 (8 tests incl. TestSendPhase1Gate)
- [x] User part type is `text`
- [x] Assistant reply is stored as `text` (and `step-start` / `step-finish` only if the API actually returns usage; otherwise skip rather than invent) - step-finish written only when resp.Usage != nil
- [x] `reasoning` is stored when the payload has it; do not fake reasoning text - reasoning part only when resp.Reasoning != ""
- [x] One session per app start for 0.0.1 is enough (`title` can be the first user line, truncated) - title = first 60 runes; resume wired in main.go
- [x] `sessions.directory` is the cwd used at `Init` - agent created with workdir = env.Dir
- [x] Test: fake provider returns `"hello"` -> db has 2 messages, 2 text parts, roles `user` then `assistant` - TestSendPhase1Gate, exit 0

## 1.5 Chat TUI (P0)

- [x] New package `internal/ui/chat` (do not overwrite employee `internal/ui` yet) - new package, employee `internal/ui` already removed (cleanup done early)
- [x] Views: transcript, prompt (`textinput`), status line - chat.go view.go; `go test ./internal/ui/chat` exit 0 (9 tests, -race clean)
- [x] Enter sends the current prompt; empty send is ignored - tested
- [x] Sending sets status to a busy state; reply appends to the transcript - event-batched; user line appended on Enter
- [x] `ErrMissingAPIKey` renders as a red status, prompt stays usable - InitialErr path, tested
- [x] Provider / network errors render as a red status; the user text remains in the transcript - 500-turn test asserts "user: hi" retained
- [x] `q` / `ctrl+c` quits; `tea.WithAltScreen()` in `main` - v2 API: `tea.View.AltScreen = true` set in chat View (v2.0.8 has no WithAltScreen); q + ctrl+c tested
- [x] `Update` stays pure: HTTP and db writes happen in `tea.Cmd` - agent.Send runs in a tea.Cmd goroutine; events via channel watcher
- [x] `main.go` starts workspace.Init then the chat model (employee `Seed` path removed from the running entry, packages left on disk) - main.go written 2026-08-15; `go build ./...` exit 0
- [ ] Manual gate: `OPENCODE_API_KEY=... go run .`, type `hi`, a non-empty assistant line appears - NEEDS LIVE KEY + TTY (user must run); automated equivalent: TestSendPhase1Gate + chat tests
- [ ] Manual gate: full screen readable, no clipped prompt, no unreadable colors - NEEDS TTY (user must run); headless smoke: clean-cwd run created `.lazykoder/lazykoder.db` + migrated schema, no-TTY/missing-key exit clean (no panic)

## 1.6 Policy stub (P0, no execution)

- [x] Add `internal/policy` with `Classify(command string) Decision` (`Allow`, `Ask`, `Deny`) - `Action` + `Decision{Action, Destructive, Reason}`; `go test ./internal/policy` exit 0 (33-case table)
- [x] Any `rm` program token returns `Ask` (see parent rules) - tokenizer splits on whitespace + `| & ;` + backslash-newline; delete-class rules incl. `git rm`, `xargs rm`, `find -exec rm`, `find -delete`, `sudo/env/command rm`, `/bin/rm`
- [x] Recursive flags (`-r`, `-rf`, `-fr`, `--recursive`) set `Decision.Destructive = true` - any arg token starting with `-` containing `r`, or `--recursive`
- [x] Non-rm simple commands (`ls`, `go test`) return `Allow`
- [x] Unparsed / empty command returns `Ask` or `Deny`, never `Allow` - empty -> `Ask`
- [x] Table tests covering: `rm file`, `rm -rf /tmp/x`, `/bin/rm ./a`, `sudo rm -rf .`, `xargs rm`, `find . -exec rm {} +`, `find . -delete`, `git rm x`, `ls`, `echo room`, `chmod` - all in policy_test.go, exit 0
- [x] `echo room` is not `Ask` (the token is not `rm`) - `echo rm` is `Allow` too (guarded regression)
- [x] No package in Phase 1 calls `os/exec` for model commands - bash tool (Phase 2) is the only `os/exec` user; policy/agent/chat/tools are clean

## 1.7 Module identity (P1)

- [x] Decide whether `module employee-tui` stays until Phase 3 or is renamed now (rename only with user sign-off) - decided 2026-08-15: rename now to `lazykoder` (user sign-off given with Phase 1 kickoff)
- [x] If renamed, grep the old module path in one pass (`go.mod`, imports, tests) and `go build` after - renamed in `go.mod` 2026-08-15 (no code files existed yet; `go build ./...` clean, no old-path references in tree)

## Dependencies

- Needs: pinned Charm v2 stack already in `go.mod`
- Blocks: Phase 2 (modal + bash) and Phase 3 (other tools, replay)

## Closure gates

- [x] `go test ./internal/workspace ./internal/db ./internal/provider/opencode ./internal/agent ./internal/policy ./internal/ui/chat` pass (record output) - `go test ./... -count=1` 2026-08-15 23:1x: all 13 packages ok, exit 0 (workspace 0.101s, db 0.143s, opencode 0.029s, agent 0.332s, policy 0.003s, chat 0.309s)
- [x] `go vet ./...` pass - exit 0
- [x] First-run on a clean cwd creates `.lazykoder/lazykoder.db` and does not delete user files - headless smoke in /tmp/opencode/lk-smoke: dir + db + WAL created, schema_migrations version 1, tables 5 + indexes present; user files untouched (workspace tests)
- [x] `hi` round-trip proven against the live OpenCode Go API or a recorded httptest; db contains the user + assistant text - recorded httptest: TestSendPhase1Gate (2 messages, 2 text parts, roles user then assistant); live-API run left for the user (no key in this environment)
- [x] No employee prototype paths remain (`internal/employee`, `employees.json`) - grep clean; only historical `opencode_session_logs.json` (gitignored artifact) mentions them
