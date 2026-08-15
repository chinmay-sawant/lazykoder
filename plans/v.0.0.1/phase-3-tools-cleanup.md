# v0.0.1 / Phase 3 - Remaining tools, replay, and prototype cleanup

> **Parent:** `plans/v.0.0.1/README.md` - part types and cleanup node
> **Status:** implemented 2026-08-15 (tools + replay + cleanup landed; closure gates green)
> **Estimated effort:** 2-3 days
> **Priority:** P1 (append to this file if a tool slips; do not start a second ledger)
> **Gate:** store all listed part/tool types; employee prototype gone; harness is `main`

---

## Overview

Finish the OpenCode-shaped part log, implement the non-bash tools, replay a session from SQLite, then delete the temporary employee app. Safety rules from Phase 2 stay unchanged.

## Executive Summary

- Tools: `read`, `write`, `edit`, `question`, `webfetch` (plus `bash` already in Phase 2).
- Every tool-call is a `parts` row + `tool_calls` row so future features can rebuild a session the way `opencode_session_logs.json` does.
- Cleanup is last and is a visible, reviewable delete of the prototype files listed in the parent cleanup node.

## 3.1 Tool: read (P1)

- [x] `internal/tools/read` takes `filePath`, returns contents + line metadata - `read.Run(filePath, rootDir) (Result{Output, Metadata{lines, truncated}}, error)`; `go test ./internal/tools/read` exit 0
- [x] Refuse paths that escape the session directory unless we later add an explicit allow (default: stay inside cwd) - lexical containment + symlink-escape check (`resolve` helper), tests for `../` and absolute escape
- [x] Persist `input_json` (`filePath`) and `output` (file text or error) - wired into the agent loop (`agent.execRead`); TestSendToolDispatch asserts completed row + content
- [x] Test: read a fixture file, output contains known line; missing file is `status=error`, no panic

## 3.2 Tool: write (P1)

- [x] `internal/tools/write` creates/overwrites `filePath` with the given contents - `write.Run(filePath, contents, rootDir)`; `go test ./internal/tools/write` exit 0
- [x] Stay inside session directory - same `resolve` containment; `../x` rejected
- [x] Persist path and byte count in `metadata_json` - Metadata{bytes, path} stored via `agent.execWrite`; agent test asserts the file lands
- [x] Test: write then read back; write outside cwd is denied

## 3.3 Tool: edit (P1)

- [x] `internal/tools/edit` applies `oldString` -> `newString` on `filePath` - `edit.Run(filePath, oldString, newString, rootDir)`; `go test ./internal/tools/edit` exit 0
- [x] Fail if `oldString` is missing or not unique - explicit errors: not found / not unique (N occurrences) / empty oldString
- [x] Persist a unified diff in `metadata_json` (same idea as the OpenCode export `filediff`) - line-based LCS diff, `@@ -a,b +c,d @@` hunks, 3-line context, capped 4000 chars; stored via `agent.execEdit`
- [x] Test: unique replace succeeds; missing oldString errors; outside cwd denied

## 3.4 Tool: question (P1)

- [x] Map OpenCode `question` input (`questions[].question/header/options`) onto the existing confirm/question UI - `question.Run(questions, ask)`; chat implements `agent.Options.Ask` with the confirm view (subject=question text, qualifier=header; y=option 0, n=option 1); chat TestAskQuestion asserts the full flow
- [x] Store answers in `tool_calls.metadata_json` (`answers`) and a short `output` summary - Metadata{answers: []string, indexes: []int}; Output "answered N question(s)"; `go test ./internal/tools/question` exit 0
- [x] Test: one question, pick option 0, stored answer matches

## 3.5 Tool: webfetch (P1)

- [x] `internal/tools/webfetch` GETs `url`, optional `format` (`markdown` / text) - `webfetch.Run(ctx, url, format, client)`; format=markdown sent as query param; `go test ./internal/tools/webfetch` exit 0
- [x] Time out; cap body size; persist truncated flag in metadata - 30s timeout, 5MB cap, Metadata{truncated, content_type}; wired via `agent.execWebfetch`
- [x] No file:// or other non-http(s) schemes - scheme whitelist, `webfetch: unsupported scheme "file"`
- [x] Test: httptest server returns a body; `file:///etc/passwd` is rejected

## 3.6 Agent mapping of part types (P1)

- [x] Provider loop writes `step-start` when a model step begins - agent.writeResponse
- [x] Writes `reasoning` when the API yields reasoning text
- [x] Writes `text` for visible assistant text
- [x] Writes `tool` + `tool_calls` for each of bash/read/write/edit/question/webfetch - all six dispatched in `agent.executeTool`; TestSendToolDispatch drives all five non-bash tools through the loop (completed rows + file side effects + webfetch body + question answers)
- [x] Writes `step-finish` with `finish_reason` and token/cost fields when the API returns usage
- [x] Unknown future tool names still insert a `tool` part (name preserved) rather than drop the row - TestSendUnknownTool + TestSendFixturePartTypes
- [x] Test: fixture provider script emits one of each part type; `SELECT type, count(*) FROM parts GROUP BY type` matches the fixture - TestSendFixturePartTypes: step-start 2, reasoning 1, text 3, step-finish 1, tool 2, exit 0

## 3.7 Session replay (P1)

- [x] On start, if a session exists for this cwd, offer the latest one (or always resume latest active) - main.go: ListSessionsByDir, resume latest; agent.Options.Session appends to it
- [x] Chat view can rebuild the transcript from `ListMessages` + `ListParts` without calling the API - chat.New loads store rows when Options.Session != nil; replay test asserts View() contains user + assistant text with no network
- [x] Reasoning can be collapsed; tool rows show name + status - rendered as `reasoning: (collapsed)` and `<tool>: <status>` cards
- [x] Test: insert a canned session, open UI model, `View()` contains the user text and assistant text - chat replay test, exit 0

## 3.8 Prototype cleanup (done early, 2026-08-15)

User asked to clean the prototype now and leave `go.mod` / `go.sum` alone. File deletes landed before the harness exists. Remaining rows wait for Phase 1 `main.go` / tests.

- [x] Delete `internal/employee/`
- [x] Delete employee JSON store (`internal/store/`)
- [x] Delete employee `internal/ui/` (chat will live in `internal/ui/chat`)
- [x] Delete `employees.json`
- [x] Delete prototype `main.go` (Phase 1 writes the harness entry)
- [x] `AGENTS.md` project blurb no longer describes an employee CRUD prototype - verified; module line updated to `lazykoder` 2026-08-15
- [x] Grep the repo for `Employee Manager` / `employees.json` leftovers after Phase 1 writes new files - grep 2026-08-15: only historical `opencode_session_logs.json` (gitignored) + plan-ledger self-references remain; `internal/employee`, `internal/store`, `employees.json` absent
- [x] Module path rename (if deferred from Phase 1.7) happens with the new `main.go` - renamed in Phase 1 kickoff; `go build ./...` exit 0
- [x] `go test ./...` and `go vet ./...` pass once harness packages exist - 2026-08-15: `go test ./... -count=1` all 13 packages ok, exit 0; `go vet ./...` exit 0

## 3.9 Optional later (explicitly deferred)

These stay `[~]` in 0.0.1. Do not implement unless a later plan file takes them.

- [~] Other providers (OpenAI, Anthropic, Gemini) - 0.0.1 is OpenCode only
- [~] Blob table for multi-MB tool output
- [~] Sticky tool permissions
- [~] Reading OpenCode's global `~/.local/share/opencode/opencode.db`
- [~] Streaming token paint in the TUI

## Dependencies

- Needs: Phase 1 store + chat, Phase 2 bash policy (file tools do not go through `rm` policy unless they shell out, which they must not)
- Blocks: nothing in 0.0.1; later versions append new phase folders (`plans/v.0.0.2/`) rather than a second 0.0.1 ledger

## Closure gates

- [x] `go test ./...` pass (record command and exit code) - `go test ./... -count=1` 2026-08-15, exit 0, all 13 packages ok
- [x] `go vet ./...` pass - exit 0
- [x] Completeness: a fixture session can be inserted with part types `text`, `reasoning`, `step-start`, `step-finish`, `tool` and tools `bash`, `edit`, `read`, `write`, `question`, `webfetch`; counts match the fixture - TestSendFixturePartTypes (part-type counts) + per-tool tests (bash/edit/read/write/question/webfetch); unknown tool names preserved (TestSendUnknownTool)
- [x] Employee prototype paths are gone (`internal/employee` removed 2026-08-15)
- [x] `.lazykoder/` still created on a clean cwd; `.gitignore` still ignores it - headless smoke run 2026-08-15 created `.lazykoder/lazykoder.db` + WAL on a clean cwd; `.gitignore` has `.lazykoder/`
- [x] `rm` confirm from Phase 2 still holds after cleanup (re-run the fake-runner test) - `go test ./internal/tools/bash ./internal/policy -count=1` exit 0 (gate tests: denied rm never reaches runner; confirm allow execs once)
