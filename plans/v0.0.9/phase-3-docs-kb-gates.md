# v0.0.9 / Phase 3 - Docs, knowledge-base, and closure gates

> **Parent:** `plans/v0.0.9/README.md`
> **Status:** code/docs complete on branch `feature/v0.0.9-resume-session`;
> manual TUI rows still open for human sign-off
> **Estimated effort:** 0.5 day
> **Priority:** P1
> **Gate:** docs and knowledge-base match shipped behavior; `make test`
> and `make lint` recorded; manual TUI checklist signed off

---

## Overview

Phases 1-2 change a user-visible contract that the knowledge-base and
`docs/` still describe as auto-resume-on-launch. This phase updates the
narrative and runs the full gates.

## Executive Summary

- Rewrite resume-on-startup claims to "fresh launch + explicit `/resume`".
- Document the quit banner copy and when it appears.
- Sync tips/help if they still imply auto-resume.
- Run build/test/lint and the manual TUI rows from the parent README.

## 3.1 Documentation

- [x] `docs/tui.md` - fresh launch, quit banner, `/resume`
- [x] `docs/architecture.md` - launch sequence + quit banner
- [x] `docs/tips.md` / `internal/tips` - fresh launch + quit banner tips
- [x] Root `README.md` feature bullets + screenshot caption
- [x] `docs/plans.md` - v0.0.9 indexed

## 3.2 Knowledge-base

- [x] `knowledge-base/03-concepts/sessions-and-resume.md`
- [x] `knowledge-base/01-overview/what-is-lazykoder.md`
- [x] `knowledge-base/01-overview/quick-start.md`
- [x] `knowledge-base/01-overview/glossary.md`
- [x] `knowledge-base/05-cheatsheets/keymap.md`

## 3.3 In-app help / chrome

- [x] Idle tips updated (`SessionsResume`, `CtrlCCopyQuit`)
- [x] Quit arm text stays `ctrl+c again to quit` (banner is post-exit)

## 3.4 Closure gates

- [x] `go build ./...` exit 0
- [x] `go test ./internal/ui/chat` exit 0
- [x] `make test` exit 0 (`go test ./...`)
- [~] `make lint` exit 1 - pre-existing findings only
      (`mnd` in `compact.go`/`settings.go`, `unused` `addCost` in
      `transcript.go`); none introduced by this change
- [ ] Manual: launch with prior sessions present → blank new session
- [ ] Manual: send a message → quit twice → console shows matching
      `lk ses_...` + resume hint
- [ ] Manual: relaunch → still blank → `/resume` → load that session →
      transcript matches
- [ ] Manual: quit before first send → `lk (no session)` banner
- [ ] Manual: `ctrl+c` with draft text copies, does not quit

## Dependencies

- Phases 1 and 2 code present on this branch.
