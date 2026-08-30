# v0.0.12 / Phase 4 - Screenshot-driven TUI enhancements

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** `[~]` active. The screenshot handoff and source map are complete. Code and terminal proof are pending.
> **Estimated effort:** 3-5 working days
> **Priority:** P1

---

## Overview

Phase 4 turns the six supplied desktop captures into focused TUI work. They
show the intended visual direction, but do not prove terminal geometry. The
implementation must preserve one settings owner, shared catalog pickers, and
the layout snapshot that keeps mouse targets aligned with painted rows.

`/model` and `/variant` will open the settings workspace with the Model
category and the matching row selected. They will not open a separate model
card. Runtime `/agents`, `/subs`, `/subagents`, and `/spawn` views stay outside
the settings workspace.

The VS Code settings reference, `screenshots/Screenshot 2026-08-30 105333.png`,
sets the direction for the settings workspace. LazyKoder will use a category
rail and a focused content pane. It will not copy VS Code's User, Remote, or
Workspace scope controls because LazyKoder currently edits one project settings
file.

## 4.1 Screenshot handoff

- [x] Record the six supplied reference captures: `Screenshot 2026-08-29
      173110.png`, `234125.png`, `234136.png`, `234151.png`,
      `Screenshot 2026-08-30 002306.png`, and `105333.png`. Evidence: visual
      review on 2026-08-30. These are desktop captures, not `120x36` or
      `80x24` terminal proof.
- [x] Map the requested views to their current owners: question dialog and
      empty state in `internal/ui/chat/view.go`, model picker in `picker.go`,
      settings in `settings.go`, transcript cards in `transcript.go`, and
      shared painted geometry in `layout.go`. Evidence: source review on
      2026-08-30.

## 4.2 Settings workspace

### 4.2.1 Section model and navigation

- [ ] Define one settings section for every existing `settingsRow`: Appearance,
      Model, Recaps, Skills, Agent loop, Compaction, Sub-agents, Safety, and
      Request retries. Owner: `internal/ui/chat/settings.go`. Proof: a focused
      test maps every visible row to exactly one section.
- [ ] Render the existing centered settings card as one workspace with a
      left category rail and a right content pane. The selected rail item and
      selected setting row must both remain visible. Owner: `settings.go`.
      Proof: deterministic render tests at wide and compact terminal sizes.
- [ ] Route `/model` to the Model category with `settingsRowModel` selected,
      and route `/variant` to the same category with `settingsRowVariant`
      selected. Activating either row opens the existing shared picker. Owners:
      `slash.go` and `settings.go`. Proof: command tests show the selected
      category and row before a picker opens.
- [ ] Keep labels, descriptions, and controls together in the content pane.
      Use a bounded label column and a value column that starts at a stable
      display position. Do not justify values against the far edge of the
      card. Owner: `settingsKVRow` and settings row rendering. Proof: width
      assertions show readable rows without wrapping or clipped controls.
- [ ] Add a settings filter that matches setting labels and short descriptions.
      The filter must narrow the content pane without hiding the active result
      or trapping keyboard focus. Owner: `settings.go`. Proof: key and mouse
      tests cover filter entry, no-match text, clearing, and focus restoration.

### 4.2.2 Existing editors and pickers

- [ ] Keep `settingsPickerTarget` as the return path for model, variant, tool,
      and role choices. A shared picker must return to the same settings
      section and focused row instead of reopening at Appearance. Owners:
      `settings.go` and `picker.go`. Proof: focused selection and cancel tests
      cover each picker target.
- [ ] Keep in-place chevrons for fast value changes and existing input forms
      for numeric values and the bash allowlist. Do not create per-setting
      drawers or duplicate settings data. Owner: `settings.go`. Proof: current
      keyboard and mouse behavior remains covered after the layout change.
- [ ] Keep the close target and row hit map derived from the same painted
      settings frame. Owner: `internal/ui/chat/layout.go` and `settings.go`.
      Proof: settings geometry tests pass after category selection, filtering,
      scrolling, and resizing.

## 4.3 Screenshot-led layout corrections

- [ ] Give the question dialog an ask-specific maximum width. Keep wrapping,
      option spans, and mouse hit-testing derived from the same width helper.
      Owners: `internal/ui/chat/view.go` and `ask_test.go`. Proof: wide and
      `80x24` dialog tests select wrapped options correctly.
- [ ] Tighten the empty-session view. Keep the centered LazyKoder mark and
      first actions together, and suppress the detached rotating tip until the
      first transcript item exists. Owner: `view.go`. Proof: wide and compact
      view tests show one clear first action area above the composer.
- [ ] Preserve full-width transcript cards for code and diffs. Improve the
      contrast of composer status text and make truncated tool commands
      discoverable through the existing tool-card interaction, rather than
      reducing the transcript width. Owners: `transcript.go`, `view.go`, and
      the theme package. Proof: transcript, selection, and tool-card tests
      retain their current geometry.

## 4.4 Cancellation and browser status surfaces

- [ ] Keep runtime `/agents`, `/subs`, `/subagents`, and `/spawn` in their
      existing drawer and form owners. The Sub-agents settings category edits
      configuration only. It must not render live child jobs, logs, or the
      spawn form. Owners: `subagents.go`, `forms.go`, and `settings.go`. Proof:
      command tests keep the current focus mode for each runtime view.
- [~] Add the approved stop affordance and state transition to the parent chat
      view. It must show that cancellation was requested, stop live animation
      when cleanup finishes, and preserve the concise cancellation note. Next
      gate: placement inside the revised composer and status layout.
- [~] Update the sub-agent drawer to distinguish queued, running, completed,
      failed, timed out, and cancelled children without wrapping rows or
      hiding the composer. Next gate: row-density decisions within the existing
      drawer layout.
- [~] Show whether a web read used HTTP or a browser, plus a bounded active or
      cancelled state, in the existing tool activity surface. Page text must
      not enter progress labels. Next gate: screenshot-led tool-card treatment.

## 4.5 Responsive and interaction proof

- [ ] Preserve the full-screen layout at `120x36` and `80x24`, including the
      composer, settings workspace, model picker, question dialog, status area,
      cancellation state, and error text. Proof: tmux captures at both sizes
      show no clipped lines or hidden composer.
- [ ] Add focused `internal/ui/chat` view, key, mouse, and geometry tests for
      category selection, settings filtering, picker return, compact settings,
      model and variant settings entry, question-card width, empty-session
      tips, and tool command disclosure. Proof: the focused package tests pass
      before the final repository gate.
- [ ] Run `go test ./...`, `go build ./...`, and the repository lint gate after
      implementation. Record each command and exit code in this ledger before
      marking implementation rows complete.

## Dependencies

- The six supplied screenshots and the decisions recorded in this ledger
- Existing `internal/ui/chat` settings, picker, transcript, layout, key, and
  mouse owners
- Phase 1 cancellation states, Phase 2 browser status metadata, and Phase 3
  child job lifecycle states
- A real terminal for the final `120x36` and `80x24` captures

## Closure gate

- [ ] Every screenshot-led row has matching source, focused-test, and terminal
      evidence.
- [ ] The settings workspace preserves picker return, keyboard navigation,
      mouse targeting, editable values, direct model and variant entry, and
      both color themes.
- [ ] Real-terminal checks at `120x36` and `80x24` confirm no clipped lines,
      stale spinners, unreadable colors, or hidden composer.
