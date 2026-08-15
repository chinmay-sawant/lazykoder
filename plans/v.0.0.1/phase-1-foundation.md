# v0.0.1 / Phase 1 - Foundation, workspace, and first OpenCode turn

> **Parent:** `plans/v.0.0.1/README.md` - architecture and schema
> **Status:** not started
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

- [ ] Add `internal/workspace` with `Init(cwd string) (Dir, error)` that creates `<cwd>/.lazykoder` at `0755` when missing
- [ ] `Init` is idempotent: a second start on an existing dir does not wipe the db or any files inside it
- [ ] `Init` opens `<cwd>/.lazykoder/lazykoder.db` (create if missing)
- [ ] `Init` appends `.lazykoder/` to `<cwd>/.gitignore` only if that line is absent; never rewrite an existing ignore file from scratch at runtime
- [ ] Repo `.gitignore` already lists `.lazykoder/`, `opencode_session_logs.json`, `.env` / `.env.*`, and Go binary/coverage paths (done at plan time; re-check if someone edits it)
- [ ] Test: temp dir with no `.lazykoder` -> `Init` -> dir exists, db file exists, ignore line present
- [ ] Test: `Init` twice -> one db file, ignore file not duplicated into garbage

## 1.2 SQLite schema and store (P0)

- [ ] User sign-off recorded for adding `modernc.org/sqlite` (policy change; do not `go get` before this)
- [ ] Add `modernc.org/sqlite` and open via `database/sql` (no CGO, no `mattn/go-sqlite3`)
- [ ] `internal/db` runs numbered migrations; `schema_migrations` records version 1 after first open
- [ ] Tables match parent plan: `sessions`, `messages`, `parts`, `tool_calls` with the listed indexes
- [ ] WAL + `foreign_keys=ON` + `busy_timeout=5000` set on every connection
- [ ] Store API: `CreateSession`, `InsertMessage`, `InsertPart`, `ListMessages`, `ListParts` (tool_calls insert can be a no-op helper until Phase 2)
- [ ] IDs use a readable prefix (`ses_`, `msg_`, `prt_`) plus a unique suffix
- [ ] Test: migrate empty file -> all four tables exist (`sqlite_master` count matches)
- [ ] Test: insert session + user text part + assistant text part, reload process, counts match
- [ ] Test: deleting a session cascades messages and parts

## 1.3 OpenCode auth and client (P0)

- [ ] `internal/provider/opencode` reads `OPENCODE_API_KEY`, then `OPENCODE_ZEN_API_KEY`
- [ ] Empty / missing key returns a typed `ErrMissingAPIKey`, never a 401 surprise later
- [ ] Client default base: `https://opencode.ai/zen/go/v1`
- [ ] `Chat(ctx, req)` POSTs `/chat/completions` with `Authorization: Bearer <key>`, `model: deepseek-v4-flash`, `messages: [{role, content}]`
- [ ] Request has no `tools` field in this phase
- [ ] Response maps to assistant text; HTTP errors become a readable error (status + body snippet)
- [ ] Key is never logged, never written to SQLite, never rendered in the TUI
- [ ] Test: missing env -> `ErrMissingAPIKey`
- [ ] Test: httptest server -> `Chat` sends Bearer header and model id, returns the stubbed content
- [ ] Optional P1: `GET /models` health check on startup (do not block `hi` on it)

## 1.4 Agent turn (hi only) (P0)

- [ ] `internal/agent` `Send(ctx, sessionID, userText)` writes the user message, calls the provider, writes the assistant message
- [ ] User part type is `text`
- [ ] Assistant reply is stored as `text` (and `step-start` / `step-finish` only if the API actually returns usage; otherwise skip rather than invent)
- [ ] `reasoning` is stored when the payload has it; do not fake reasoning text
- [ ] One session per app start for 0.0.1 is enough (`title` can be the first user line, truncated)
- [ ] `sessions.directory` is the cwd used at `Init`
- [ ] Test: fake provider returns `"hello"` -> db has 2 messages, 2 text parts, roles `user` then `assistant`

## 1.5 Chat TUI (P0)

- [ ] New package `internal/ui/chat` (do not overwrite employee `internal/ui` yet)
- [ ] Views: transcript, prompt (`textinput`), status line
- [ ] Enter sends the current prompt; empty send is ignored
- [ ] Sending sets status to a busy state; reply appends to the transcript
- [ ] `ErrMissingAPIKey` renders as a red status, prompt stays usable
- [ ] Provider / network errors render as a red status; the user text remains in the transcript
- [ ] `q` / `ctrl+c` quits; `tea.WithAltScreen()` in `main`
- [ ] `Update` stays pure: HTTP and db writes happen in `tea.Cmd`
- [ ] `main.go` starts workspace.Init then the chat model (employee `Seed` path removed from the running entry, packages left on disk)
- [ ] Manual gate: `OPENCODE_API_KEY=... go run .`, type `hi`, a non-empty assistant line appears
- [ ] Manual gate: full screen readable, no clipped prompt, no unreadable colors

## 1.6 Policy stub (P0, no execution)

- [ ] Add `internal/policy` with `Classify(command string) Decision` (`Allow`, `Ask`, `Deny`)
- [ ] Any `rm` program token returns `Ask` (see parent rules)
- [ ] Recursive flags (`-r`, `-rf`, `-fr`, `--recursive`) set `Decision.Destructive = true`
- [ ] Non-rm simple commands (`ls`, `go test`) return `Allow`
- [ ] Unparsed / empty command returns `Ask` or `Deny`, never `Allow`
- [ ] Table tests covering: `rm file`, `rm -rf /tmp/x`, `/bin/rm ./a`, `sudo rm -rf .`, `xargs rm`, `find . -exec rm {} +`, `find . -delete`, `git rm x`, `ls`, `echo room`, `chmod`
- [ ] `echo room` is not `Ask` (the token is not `rm`)
- [ ] No package in Phase 1 calls `os/exec` for model commands

## 1.7 Module identity (P1)

- [ ] Decide whether `module employee-tui` stays until Phase 3 or is renamed now (rename only with user sign-off)
- [ ] If renamed, grep the old module path in one pass (`go.mod`, imports, tests) and `go build` after

## Dependencies

- Needs: pinned Charm v2 stack already in `go.mod`
- Blocks: Phase 2 (modal + bash) and Phase 3 (other tools, replay)

## Closure gates

- [ ] `go test ./internal/workspace ./internal/db ./internal/provider/opencode ./internal/agent ./internal/policy ./internal/ui/chat` pass (record output)
- [ ] `go vet ./...` pass
- [ ] First-run on a clean cwd creates `.lazykoder/lazykoder.db` and does not delete user files
- [ ] `hi` round-trip proven against the live OpenCode Go API or a recorded httptest; db contains the user + assistant text
- [ ] No employee prototype paths remain (`internal/employee`, `employees.json`)
