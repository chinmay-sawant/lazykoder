# v0.0.1 / Phase 3 - Remaining tools, replay, and prototype cleanup

> **Parent:** `plans/v.0.0.1/README.md` - part types and cleanup node
> **Status:** not started
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

- [ ] `internal/tools/read` takes `filePath`, returns contents + line metadata
- [ ] Refuse paths that escape the session directory unless we later add an explicit allow (default: stay inside cwd)
- [ ] Persist `input_json` (`filePath`) and `output` (file text or error)
- [ ] Test: read a fixture file, output contains known line; missing file is `status=error`, no panic

## 3.2 Tool: write (P1)

- [ ] `internal/tools/write` creates/overwrites `filePath` with the given contents
- [ ] Stay inside session directory
- [ ] Persist path and byte count in `metadata_json`
- [ ] Test: write then read back; write outside cwd is denied

## 3.3 Tool: edit (P1)

- [ ] `internal/tools/edit` applies `oldString` -> `newString` on `filePath`
- [ ] Fail if `oldString` is missing or not unique
- [ ] Persist a unified diff in `metadata_json` (same idea as the OpenCode export `filediff`)
- [ ] Test: unique replace succeeds; missing oldString errors; outside cwd denied

## 3.4 Tool: question (P1)

- [ ] Map OpenCode `question` input (`questions[].question/header/options`) onto the existing confirm/question UI
- [ ] Store answers in `tool_calls.metadata_json` (`answers`) and a short `output` summary
- [ ] Test: one question, pick option 0, stored answer matches

## 3.5 Tool: webfetch (P1)

- [ ] `internal/tools/webfetch` GETs `url`, optional `format` (`markdown` / text)
- [ ] Time out; cap body size; persist truncated flag in metadata
- [ ] No file:// or other non-http(s) schemes
- [ ] Test: httptest server returns a body; `file:///etc/passwd` is rejected

## 3.6 Agent mapping of part types (P1)

- [ ] Provider loop writes `step-start` when a model step begins
- [ ] Writes `reasoning` when the API yields reasoning text
- [ ] Writes `text` for visible assistant text
- [ ] Writes `tool` + `tool_calls` for each of bash/read/write/edit/question/webfetch
- [ ] Writes `step-finish` with `finish_reason` and token/cost fields when the API returns usage
- [ ] Unknown future tool names still insert a `tool` part (name preserved) rather than drop the row
- [ ] Test: fixture provider script emits one of each part type; `SELECT type, count(*) FROM parts GROUP BY type` matches the fixture

## 3.7 Session replay (P1)

- [ ] On start, if a session exists for this cwd, offer the latest one (or always resume latest active)
- [ ] Chat view can rebuild the transcript from `ListMessages` + `ListParts` without calling the API
- [ ] Reasoning can be collapsed; tool rows show name + status
- [ ] Test: insert a canned session, open UI model, `View()` contains the user text and assistant text

## 3.8 Prototype cleanup (done early, 2026-08-15)

User asked to clean the prototype now and leave `go.mod` / `go.sum` alone. File deletes landed before the harness exists. Remaining rows wait for Phase 1 `main.go` / tests.

- [x] Delete `internal/employee/`
- [x] Delete employee JSON store (`internal/store/`)
- [x] Delete employee `internal/ui/` (chat will live in `internal/ui/chat`)
- [x] Delete `employees.json`
- [x] Delete prototype `main.go` (Phase 1 writes the harness entry)
- [x] `AGENTS.md` project blurb no longer describes an employee CRUD prototype
- [ ] Grep the repo for `Employee Manager` / `employees.json` leftovers after Phase 1 writes new files
- [ ] Module path rename (if deferred from Phase 1.7) happens with the new `main.go`
- [ ] `go test ./...` and `go vet ./...` pass once harness packages exist

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

- [ ] `go test ./...` pass (record command and exit code)
- [ ] `go vet ./...` pass
- [ ] Completeness: a fixture session can be inserted with part types `text`, `reasoning`, `step-start`, `step-finish`, `tool` and tools `bash`, `edit`, `read`, `write`, `question`, `webfetch`; counts match the fixture
- [x] Employee prototype paths are gone (`internal/employee` removed 2026-08-15)
- [ ] `.lazykoder/` still created on a clean cwd; `.gitignore` still ignores it
- [ ] `rm` confirm from Phase 2 still holds after cleanup (re-run the fake-runner test)
