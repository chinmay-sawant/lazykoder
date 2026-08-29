# v0.0.12 / Phase 4 - Screenshot-driven TUI enhancements

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** `[~]` intentionally deferred while waiting for user screenshots
> **Estimated effort:** 2-4 working days after screenshot handoff
> **Priority:** P1

---

## Overview

This phase converts the user's screenshots into concrete TUI acceptance rows.
The existing drawer and transcript seams remain the starting point. Final
layout, copy, color, and interaction decisions stay open until the reference
screenshots arrive.

## 4.1 Screenshot handoff

- [~] Collect the user's screenshots and record each requested visual change
      as an atomic acceptance row with the affected view, terminal size,
      interaction, and expected text. Waiting on screenshots is intentional;
      next gate: screenshots supplied by the user.
- [~] Compare each screenshot request with the existing component seams in
      `internal/ui/chat/view.go`, `subagents.go`, `drawer.go`, `transcript.go`,
      and `layout.go` before editing. Do not add a parallel overlay or widget
      when an existing drawer family owns the surface. Next gate: source map
      tied to the supplied screenshots.

## 4.2 Cancellation and browser status surfaces

- [~] Add the approved stop affordance and state transition to the parent chat
      view. It must show that cancellation was requested, stop live animation
      when cleanup finishes, and preserve the concise cancellation note. Next
      gate: screenshot-specific placement and copy.
- [~] Update the sub-agent drawer to distinguish queued, running, completed,
      failed, timed out, and cancelled children without wrapping rows or
      hiding the composer. Next gate: screenshot-specific row density and
      color decisions.
- [~] Show whether a web read used HTTP or a browser, plus a bounded active or
      cancelled state, in the existing tool activity surface. Page text must
      not enter progress labels. Next gate: screenshot-specific browser status
      treatment.

## 4.3 Responsive and interaction proof

- [~] Preserve the full-screen layout at `120x36` and `80x24`, including the
      composer, status area, drawer, cancellation state, and error text. Next
      gate: live terminal checks after the screenshot-led implementation.
- [~] Add focused `internal/ui/chat` view, key, mouse, and geometry tests for
      the approved UI changes. Next gate: concrete screenshot acceptance
      criteria.
- [~] Run `make run` in a real terminal and inspect the full screen after each
      UI slice. Do not use a piped or redirected binary as TUI evidence. Next
      gate: recorded terminal observations at both required sizes.

## Dependencies

- User screenshots and their requested interaction details
- Phase 1 cancellation states
- Phase 2 browser status metadata
- Phase 3 child job lifecycle states
- Existing `internal/ui/chat` drawer, transcript, layout, key, and mouse seams

## Closure gate

- [~] Every screenshot request has an atomic acceptance row and a named source
      owner. Next gate: screenshot handoff.
- [~] Approved UI changes pass focused view, key, mouse, and geometry tests.
      Next gate: implementation after screenshot review.
- [~] Real terminal checks at `120x36` and `80x24` confirm no clipped lines,
      stale spinners, unreadable colors, or hidden composer. Next gate: live
      terminal proof.
