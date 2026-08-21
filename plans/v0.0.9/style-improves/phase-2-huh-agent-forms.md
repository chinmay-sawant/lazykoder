# v0.0.11 Phase 2 - Huh forms for /agent and /subagent

> **Parent:** `plans/v0.0.11/README.md` - reference `AGENTS.md` dependency policy
> **Status:** planned
> **Estimated effort:** L (3 to 4 sessions: 1 compat spike, 1 to 2 integration, 1 gates)

---

## Overview

The `/agents`, `/subs`, `/subagents` slash commands route through
`internal/ui/chat/slash.go` (`openSubagentPicker`) into the sub-agent drawer in
`internal/ui/chat/subagents.go` (1,398 lines). The drawer is hand-rolled:
rows built from `subagentRow`, key handling in `updateSubagentPickerKey`,
a log screen with its own jump bar, and settings-style cycling rows in
`internal/ui/chat/settings.go` (`adjustSettings`, `cycleDefaultModel`,
`cycleChildModel`, and friends).

Nothing here uses a form library. Values change by cycling with left/right
keys, which gets slow for text-heavy fields like prompts and allowlists.
`charmbracelet/huh` gives real form widgets: text inputs, selects, confirms,
grouped pages, inline validation, and a help column, all themeable with
lipgloss to match `internal/ui/theme`.

This phase introduces huh for the agent-facing forms while keeping every
existing field, key binding, and behavior. Nothing is removed. Where huh does
not fit (live status rows, the log viewer), the existing drawer stays.

## Executive Summary

Add `huh` behind a compatibility spike (this repo pins `charm.land/bubbletea/v2`,
so huh must be the matching v2 line). Build one shared form host that plugs
into the existing focus stack (`focusKind` in `internal/ui/chat/focus.go`) as a
new `focusForm`. Convert three surfaces first: spawn-sub-agent fields,
settings rows that are true form fields, and the confirm-before-spawn flow.
Live status, logs, and pickers stay as they are. Every conversion keeps old
behavior behind the same keys until the gates pass, then swaps rendering only.

Dependency gate: adding huh is a project-policy change under `AGENTS.md`.
No `go get` until the user signs off with the binary-size number from the spike.

## Phase 1: Compatibility spike

### 1.1 Version and API fit

- [x] Confirm huh ships a v2 module compatible with `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2`; record import path and version in this plan. Path: `go.mod` (read only). Proof: `go list -m -versions` output pasted here, mixed v1/v2 bubbletea ruled out.
- [x] Prototype a huh form inside a scratch tea.Model that delegates `Update`/`View` to `huh.Form`, driven from `internal/ui/chat` test harness style. Path: throwaway under `/tmp`, not committed. Proof: form renders at 80x24 with theme colors, esc closes, no TTY needed for the unit test.
- [x] Measure binary size delta with `go build -ldflags="-s -w"` before and after. Path: this plan. Proof: numbers recorded; growth over ~2 MB goes back to the user before proceeding.

### 1.2 Decide scope boundaries

- [x] List every field the sub-agent flow can set today by reading `internal/subagent/types.go` (`Spec`) and `internal/tools/task/task.go` (`TaskArgs`): prompt, description, model, variant, background, max steps, timeout. Path: those files. Proof: field table added to this plan, each mapped to keep/change/keep-as-is.
- [x] Mark surfaces that stay hand-rolled: live status rows (`subagentDrawerRow`), log screen (`subagentLogScreen`), model picker (`picker.go`). Path: `internal/ui/chat/subagents.go`, `internal/ui/chat/picker.go`. Proof: out-of-scope list in this plan matches code reality.

## Phase 2: Shared form host

### 2.1 Focus integration

- [x] Add `focusForm` to `focusKind` and wire it into `setFocus`/`clearFocus` so opening a form closes sibling modes exactly once. Path: `internal/ui/chat/focus.go`. Proof: `go test ./internal/ui/chat -run TestFocus -count=1` passes with a new case.
- [x] Build `formHost` in a new file `internal/ui/chat/forms.go`: owns a `*huh.Form`, forwards `tea.KeyPressMsg`, exposes `View()` bounded by drawer width, and reports Done/Canceled. Path: `internal/ui/chat/forms.go`. Proof: unit test drives keys into the host and reads back committed values.
- [x] Map huh theme to `internal/ui/theme`: Accent `#d4a0c7` for focused borders, Text `#eceae6`, Mute `#8a8680` for help, Border `#2a2a2a` card chrome on `Bg #000000`. Path: `internal/ui/chat/forms.go`. Proof: screenshot-style golden test of one rendered form matches palette constants.

### 2.2 Key parity

- [x] Preserve esc = cancel, enter = next/commit, y/n confirms, and arrow/tab navigation identical to today's drawers; document any additions in `knowledge-base/05-cheatsheets/keymap.md`. Path: `internal/ui/chat/forms.go`, `internal/ui/chat/keys.go`. Proof: `go test ./internal/ui/chat -count=1` green plus keymap diff attached.

## Phase 3: Convert agent surfaces

### 3.1 Spawn form

- [x] Replace the free-text-only spawn path with a huh group: description input, optional model select fed by `inheritModelChoices`, background toggle, steps input with validation. Pre-fill defaults from current settings so behavior is unchanged when the user just hits enter through. Path: `internal/ui/chat/forms.go`, call site in `internal/ui/chat/subagents.go`. Proof: spawned `Spec` equals the pre-change struct for default inputs (table test).
- [x] Keep the y/n delete/confirm pattern for destructive actions on top of the form using the existing confirm flow, not a second dialog style. Path: `internal/ui/chat/gate.go`, `internal/ui/confirm/confirm.go`. Proof: cancel path leaves state untouched (test).

### 3.2 Settings rows that are forms

- [x] Move text/number-valued settings rows (compact percent, max steps, concurrency, queued, child steps, timeout) from cycle-keys to huh inputs opened on enter; keep left/right cycling working until this row passes gates, then retire only the duplicate path. Path: `internal/ui/chat/settings.go`. Proof: each value round-trips through save/load in `settings_test.go` equivalents.
- [x] Leave boolean toggles and single-choice cycles as-is if huh adds no value; record the decision per row in this plan. Path: this plan. Proof: table lists row-by-row convert vs keep with reason.

## Phase 4: Gates

- [x] `go vet ./...` clean. Proof: command output in session log.
- [x] `go test ./... -count=1` green. Proof: exit 0 captured.
- [x] `wc -l` on touched files: nothing crosses 2,000 lines; split module-wise (for example `forms.go`, `forms_theme.go`) rather than growing one file. Proof: line counts recorded.
- [x] Visual pass with `make run` in a real terminal: every drawer opens, no clipped lines, colors readable, esc always exits. Proof: user confirmation on the run; agent verifies View strings via tests since headless runs fail by design.

## Dependencies

- Phase 1 glow plan lands first or in parallel; both touch `internal/ui/chat` but different files.
- User sign-off on the huh dependency before any `go.mod` edit.
- No changes to `internal/subagent` semantics; `Spec` fields drive the form.

## Risks

- huh v1 against bubbletea v2 would break the build; the spike gates this.
- Form takeover can fight the exclusive-focus model; `focusForm` keeps one owner.
- Cycling-key muscle memory: keep old keys working during transition, remove only after gates.

## Out of scope

- Whole-drawer chrome restyle: see `plans/v0.0.11/phase-3-drawer-restyle.md`.
- Removing any existing drawer, key, or field.
