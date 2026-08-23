# Phase 4 - Commit and push action button in the composer

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** not started
> **Estimated effort:** 3-4 days

---

## Overview

After a successful change (a turn where the assistant finished and the
worktree is dirty), show a "commit and push" button just above the Enter/send
input box. Activating it sends a hidden request to the LLM: scan `git diff`
and `git status`, write a detailed commit message, commit, and push to the
current upstream branch. The button auto-hides after 1-2 minutes. This is a
UI convenience over git; it does not change the agent's no-git-without-permission
rule for autonomous runs (`AGENTS.md` golden rule 1) because every run here is
an explicit user click.

## 4.1 Trigger detection

- [ ] Detect "successful change": a completed assistant turn with zero tool
      errors plus a non-empty worktree (`git status --porcelain` via the bash
      tool seam or a `tea.Cmd` exec); reuse the existing gate/policy checks
      before any git command runs.
- [ ] Add a transient state flag on the chat model (e.g. `pushPromptUntil
      time.Time`) set on detection; nothing persists to SQLite.

## 4.2 Button above the composer

- [ ] Render a single-line button row directly above the prompt textarea in
      `internal/ui/chat` view composition (composer lives in `chat.go`,
      layout in `view.go`); style matches the focused-button styles already
      defined in `forms.go`; truncates to width like other single-line rows.
- [ ] Mouse click activates it (extend `prompt_mouse.go` hit-testing to the
      button row); keyboard shortcut (e.g. `ctrl+g`) as an alternative path.
- [ ] Auto-hide: a `tea.Tick` timer hides the button after 90 seconds (within
      the 1-2 minute window) if not clicked; hide immediately after activation.

## 4.3 LLM commit-and-push flow

- [ ] On activation, build a one-shot agent turn with a dedicated prompt:
      provide `git status`, `git diff`, and recent log; instruct the model to
      produce a detailed conventional-commit message, then execute
      `git add -A && git commit && git push` against the current branch.
- [ ] Route the git commands through the same policy gate as other tools so
      the user still sees them in the transcript; failure of push (no
      upstream, rejected) renders an alert row instead of failing silently.
- [ ] No new dependencies; use the existing provider/stream loop.

## 4.4 Docs and gate

- [ ] Update `docs/`, keymap cheatsheet, component map, and this knowledge
      base page in the same change.
- [ ] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      make a real edit, see the button appear above Enter/send within a few
      seconds, wait past 90s to confirm auto-hide, then re-trigger and click:
      transcript shows the LLM-written message, commit lands, push reaches the
      remote branch. Record outcomes beside these rows when they run.
