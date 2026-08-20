# v0.0.9 / Phase 1 - Fresh launch (no auto-resume)

> **Parent:** `plans/v0.0.9/README.md`
> **Status:** complete on branch `feature/v0.0.9-resume-session`
> **Estimated effort:** 0.5 day
> **Priority:** P0
> **Gate:** starting the app with existing sessions in `.lazykoder/` opens a
> blank new-session UI; `/resume` / `ctrl+s` still lists and loads those
> sessions; `go test ./internal/ui/chat` and a focused main/workspace check
> pass

---

## Overview

`main.go` currently loads the newest main session and passes it into
`chat.New`, which replays the transcript. This phase stops that automatic
attach so every launch behaves like today's `/new`.

## Executive Summary

- Remove the `ListSessionsByDir` → `Options.Session` path from `main.go`.
- Keep `chat.New` able to accept a session for tests and for `/resume`.
- Empty-state copy stays honest: new session header / empty transcript hint.
- Update any test that assumed process launch auto-resumes (most tests pass
  `Session:` explicitly and should stay green).

## Current code map

| Step | Location |
| --- | --- |
| List + pick newest | `main.go` (removed) |
| Wire into chat | `main.go` `chat.Options` without `Session` |
| Replay | `internal/ui/chat/chat.go` `New` when `m.session != nil` |
| Explicit resume | `sessions.go` `openSessionPicker` / `loadSession` |
| Explicit new | `slash.go` `/new` → `loadSession(nil)` |

## 1.1 Stop auto-resume in main

- [x] Delete the startup `ListSessionsByDir` block in `main.go` so
      `chat.Options.Session` is always `nil` on normal launch
- [x] Confirm `settings.LoadFile`, models cache path, and API-key error
      wiring are unchanged
- [x] Confirm `workspace.Init` still opens the same DB (sessions remain on
      disk)

## 1.2 Keep explicit resume paths

- [x] `/resume`, `/session`, `/sessions`, and `ctrl+s` still open the
      picker from `ListSessionsByDir` (unchanged code paths)
- [x] Selecting a row still calls `loadSession` and replays transcript,
      model, variant, todos, and status segments
- [x] `/new` still clears to a nil session without deleting DB rows

## 1.3 Empty launch UX

- [x] Fresh launch shows the new-session empty state (same as `/new`), not
      a previous transcript - `TestFreshLaunchDoesNotReplayExistingSession`
- [x] Project settings (`model.default`, slot, agents, compaction) still
      apply to the blank session via existing settings load
- [x] Footer/status do not show stale fill from a session that was not
      loaded

## 1.4 Sub-agent recover on fresh launch

- [x] Documented: with `Session: nil`, `Manager.Recover` still runs for open
      DB jobs (background continuity) and does not attach them as the blank
      UI transcript (`knowledge-base/03-concepts/sessions-and-resume.md`)
- [x] When the user later `/resume`s the parent that owned open jobs,
      existing load/replay paths remain unchanged

## 1.5 Tests

- [x] `TestFreshLaunchDoesNotReplayExistingSession` - store has prior text,
      `Session: nil` View has no prior text and shows `new session`
- [x] Existing resume tests that pass `Session: &sess` stay green
- [x] `go test ./internal/ui/chat` exit 0

## Dependencies

- None. Phase 2 can land after or beside this; quit banner needs a session
  id only after the user has sent (or resumed) a session.
