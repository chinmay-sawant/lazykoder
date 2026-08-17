# v0.0.7 - Status visibility and OpenCode account profiles

> **Status:** phase 1 complete 2026-08-17; phase 2 planned
> **Scope:** status visibility first, then multiple OpenCode account profiles
> **Provider scope:** OpenCode only; other providers remain out of scope

This version keeps the chat footer compact and makes its detailed status a
discoverable drawer. It also adds a project-level way to describe multiple
OpenCode accounts or plans, select the active one, and keep each credential in
the environment rather than in project settings.

## Phase files

| File | Status | Goal |
| --- | --- | --- |
| [phase-1-status-drawer.md](phase-1-status-drawer.md) | complete | compact footer and persisted status visibility |
| [phase-2-opencode-accounts.md](phase-2-opencode-accounts.md) | planned | dynamic OpenCode account profiles with an active selection |

## Shared invariants

- Settings contain profile metadata and environment-variable names, never API
  key values.
- The active OpenCode profile is visible in settings and is explicit for new
  sessions.
- A profile switch cannot replace the client underneath an in-flight parent or
  sub-agent request.
- Model discovery and resumed sessions must not silently use data from a
  different profile.
- Legacy `OPENCODE_API_KEY` and `OPENCODE_ZEN_API_KEY` behavior remains valid
  when no profile settings have been configured.
