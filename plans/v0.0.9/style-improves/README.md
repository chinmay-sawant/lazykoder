# v0.0.11 - Charm polish: glow rendering, huh forms, unified drawers

> **Status:** planned
> **Scope:** render AI responses with the glow/glamour stack, give /agent and
> /subagent flows real form layouts with huh, and restyle every drawer onto one
> shared frame
> **Constraint:** nothing is removed; all existing fields, keys, rows, and mouse
> targets survive

The user asked for two library integrations that are not in `go.mod` today:
`charmbracelet/glow` for AI response markdown, and `charmbracelet/huh` for the
agent/sub-agent layout surfaces. Both are dependency additions under
`AGENTS.md`, so each phase starts with a spike and an explicit sign-off gate
before any `go.mod` edit. The drawer restyle needs no new deps.

## Phase files

| File | Status | Goal |
| --- | --- | --- |
| [phase-1-glow-markdown-rendering.md](phase-1-glow-markdown-rendering.md) | completed | glamour-backed markdown for assistant turns, custom renderer kept as fallback |
| [phase-2-huh-agent-forms.md](phase-2-huh-agent-forms.md) | completed | huh form host wired into the focus stack; spawn fields and text/number settings become real inputs |
| [phase-3-drawer-restyle.md](phase-3-drawer-restyle.md) | completed | one shared drawer frame (title, meta, body, hint bar) across slash, picker, sub-agents, status, settings |

## Shared invariants

- No feature removal: every current key binding, drawer row, field, and mouse
  hit target keeps working through each conversion.
- The exclusive focus model (`focusKind` in `internal/ui/chat/focus.go`) stays;
  new surfaces join it as `focusForm` rather than bypassing it.
- Theme colors come only from `internal/ui/theme/theme.go`
  (`#000000` bg, `#eceae6` text, `#8a8680` mute, `#d4a0c7` accent,
  `#2a2a2a` border); no hardcoded hex in new code.
- Streaming stays incremental: rendered output is cached per width + content
  hash (`internal/ui/chat/render_cache.go`), never re-rendered wholesale per
  delta.
- File size guard: no Go file crosses 2,000 lines; splits are module-wise.

## Sequencing

Phase 1 and phase 3 touch different files and can run in parallel. Phase 2
depends on the huh sign-off and lands after its spike. Phase 3 last avoids
restyling drawer contents twice.
