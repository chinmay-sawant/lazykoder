# v0.0.9 / Phase 2 - Ctrl+C quit banner (`lk` + session id)

> **Parent:** `plans/v0.0.9/README.md`
> **Status:** complete on branch `feature/v0.0.9-resume-session`
> **Estimated effort:** 0.5-1 day
> **Priority:** P0
> **Gate:** second `ctrl+c` exits the TUI, then stdout shows the exact
> banner with `lk` and session id (or no-session line) plus resume hint;
> copy-on-ctrl+c with non-empty prompt still copies and does not quit

---

## Overview

Bubble Tea runs on the alt screen. Anything printed while the program is
still alive is easy to lose when the alt screen closes. The reliable place
to print is **after** `p.Run()` returns in `main.go`, using the final
`chat.Model` to read the session id.

## Executive Summary

- Capture the final model from `p.Run()` instead of discarding it.
- Add a small pure helper that formats the quit banner from a session id.
- On successful quit (and clean exit paths that use `tea.Quit`), print the
  banner to stdout, then exit 0.
- Keep the in-TUI two-step confirm (`ctrl+c again to quit`) unchanged.

## Current code map

| Behavior | Location |
| --- | --- |
| Arm / confirm quit | `internal/ui/chat/chat.go` `Update` ctrl+c |
| Quit cleanup | `closeDone()` closes `doneCh` |
| Overlay quit paths | settings/usage/subagents also return `tea.Quit` |
| Program exit + banner | `main.go` `p.Run()` then `FormatQuitBanner` |
| Session pointer | `chat.Model.session` / `SessionID()` |
| Banner formatter | `internal/ui/chat/quit_banner.go` |

## 2.1 Expose session id for main

- [x] `chat.Model.SessionID() string`
- [x] `chat.FormatQuitBanner(sessionID string) string`

## 2.2 Print after alt screen exit

- [x] `main.go` uses `final, err := p.Run()`
- [x] On `err != nil`, stderr + exit 1 (no banner)
- [x] On success, type-assert `chat.Model` and print banner to stdout
- [x] No print from inside `Update` before `tea.Quit`

## 2.3 Exact banner copy

- [x] ASCII lazykoder wordmark (`quitLogo`) above the session lines
- [x] With id: logo + `lk ses_...\nresume with /resume or ctrl+s\n`
- [x] Without id: logo + `lk (no session)\nresume older runs with /resume or ctrl+s\n`
- [x] Binary name is `lk`
- [x] Covered by `TestFormatQuitBanner`

## 2.4 Quit path coverage

- [x] Second `ctrl+c` after `quitConfirm` still returns `tea.Quit` /
      `closeDone` - `TestQuitKeys`
- [x] Single print site in `main` covers all `tea.Quit` exits
- [x] `ctrl+c` with text in the composer still copies - `TestPromptCtrlCAndCtrlA`

## 2.5 Busy turn / children on quit

- [x] Existing `doneCh` / cancel paths unchanged; banner prints after Run
- [x] `TestQuitKeys` asserts `SessionID` has `ses_` prefix after a started turn

## 2.6 Tests

- [x] `TestFormatQuitBanner` table
- [x] `TestSessionID`
- [x] `TestQuitKeys` SessionID assertion
- [x] `go test ./internal/ui/chat` exit 0

## Dependencies

- Phase 1 landed together on this branch.
