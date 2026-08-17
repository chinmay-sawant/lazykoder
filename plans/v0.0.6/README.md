# v0.0.6 - Transcript performance and meta keybindings

> **Parent:** chat transcript + tool cards (`internal/ui/chat`), agent tool
> output caps (`internal/agent`), live DB evidence under `.lazykoder/`
> **Status:** planned (not implemented)
> **Estimated effort:** 3-5 days across three phases
> **Priority:** P0 for keybindings (typing `t` / `e` is broken when the prompt
> is empty); P0 for expanded tool paint caps; P1 for render-path work
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Source:** code review of `renderTool` / `render_cache.go` / `keys.go` /
> `mouse.go` (2026-08-17) plus SQLite counts from this workspace's
> `.lazykoder/lazykoder.db`
> **Gate:** plain `t` and `e` always type into the composer; only `ctrl+e` and
> `ctrl+p` toggle meta blocks; expanded bash/read/grep bodies stay within a
> fixed line budget; long sessions do not rebuild the full transcript string
> on every stream delta without a memo path

---

## Overview

The product shows the whole session in one transcript: prompts, thinking,
assistant text, and tool cards (bash open means an expanded card, not a
separate PTY). That design is correct. What hurts is (1) bare `t` / `e`
stealing characters when the prompt is empty, (2) click and per-card toggles
that fight bulk expand, and (3) expanded tool bodies (and full-session
re-render) that scale with multi-thousand-line bash dumps.

This folder is the live ledger for that work. Mark `[x]` only after the
named gate command passes. Do not claim TUI feel from headless `go run`.

## Ratings (starting point)

| Lens | Before (2026-08-17 plan) | Target after v0.0.6 |
| --- | --- | --- |
| Typing / composer | broken for leading `t` / `e` | 9 / 10 |
| Transcript responsiveness | poor on fat sessions | 8 / 10 |
| Tool card readability | full dump on expand | 8 / 10 |
| Discoverability of meta keys | `t` / `e` / click / `ctrl+e` mixed | 8 / 10 |

## Evidence data (this workspace, 2026-08-17)

Source: `sqlite3 .lazykoder/lazykoder.db` on the lazykoder project DB
(~15 MB file). Counts are workspace history, not a synthetic fixture.

### Workspace totals

| Metric | Count |
| --- | ---: |
| Sessions | 236 |
| Messages | 2,431 |
| Parts | 8,454 |
| Tool calls | 2,403 |

### Tool calls by kind (volume + worst output)

| Tool | Calls | Avg output bytes | Max output bytes | Max lines (approx) |
| --- | ---: | ---: | ---: | ---: |
| bash | 1,250 | 3,166 | 124,752 | 2,229 |
| read | 558 | 5,798 | 8,000 | 344 |
| task | 140 | 217 | 1,391 | 1 |
| edit | 135 | 194 | 227 | 10 |
| grep | 126 | 4,535 | 8,000 | 101 |
| task_wait | 59 | 3,482 | 35,217 | 1 |
| webfetch | 47 | 1,453 | 8,000 | 275 |
| todowrite | 46 | 16 | 17 | 1 |
| task_status | 15 | 853 | 4,019 | 1 |
| write | 13 | 99 | 109 | 1 |
| task_list | 12 | 3,759 | 25,380 | 1 |
| question | 2 | 22 | 22 | 1 |

### Heaviest sessions by tool-output line count

If every tool card in the session were expanded with no UI cap, the
transcript would absorb roughly these line counts from tool output alone
(plus user/assistant/thinking rows).

| Session id (prefix) | Title (truncated) | Msgs | Tools | Tool output lines | Tool output bytes |
| --- | --- | ---: | ---: | ---: | ---: |
| `ses_b16f5d73…` | function-audit | 20 | 53 | 9,874 | 319,881 |
| `ses_ba60cff7…` | create branch feature/phase-3… | 191 | 187 | 9,188 | 350,396 |
| `ses_d2b1625a…` | current-responsive | 22 | 38 | 7,766 | 255,924 |
| `ses_eb33b5c9…` | subagents review… | 191 | 171 | 7,507 | 354,176 |
| `ses_ffd8c44e…` | methods-report | 20 | 49 | 7,335 | 250,111 |

One bash alone can be **~2,229 lines**. Expanding a few of those cards is
enough to push the viewport content into the **10k–20k line** regime the
user reported as “thousands of times” of work / slowness.

### How the render path operates today (code facts)

| Step | Where | Behavior |
| --- | --- | --- |
| Load session | `transcript.go` `replay` | All visible messages, parts, and tool rows load into `m.items` with full `tool.Output` strings |
| Default collapse | `applyTool` | Non-edit tools start `collapsed=true` (one header row). Edit starts open |
| Expand paint | `renderTool` | Expanded non-edit body paints `$ command` + **entire** `Output` through lipgloss (no line cap). Write preview only is capped at 400 runes |
| Agent → model cap | `agent.go` `maxToolOutput = 8000` | Model-facing payload truncated; older DB rows can still be larger |
| Rebuild | `syncTranscript` → `renderedItems` → `buildRenderedItems` | Full list re-rendered; joined into one string; `viewport.SetContent` |
| Memo | `render_cache.go` | Fingerprint includes full tool output strings; any content change rebuilds all items |
| Stream | `applyEvent` / parts | Each delta calls `syncTranscript` (full path above) |

Collapsed headers already keep **visible** height small. Cost remains high
because (a) expanded bodies have no UI budget, and (b) the whole session
string is rebuilt and fingerprinted even when only the live tail changed.

## Product decisions for this version

1. **Transcript still shows everything** (prompts, thinking, tools). No
   separate bash terminal.
2. **No bare `t` / `e` shortcuts.** Those letters must always enter the
   composer (including as the first character of a message).
3. **No click-to-toggle** on thinking or tool headers. Clicks stay for
   selection / scroll / other chrome; meta open-close is keyboard-only.
4. **`ctrl+e` toggles all tool cards** in the current transcript (main
   chat and the same rule in sub-agent log when that surface is focused).
   If any tool is expanded, collapse all; if all are collapsed, expand all.
5. **`ctrl+p` toggles all thinking blocks** with the same all-open /
   all-closed rule (not only the last reasoning item).
6. **UI truncation** on expanded tool output in the **parent/main
   transcript** only (head + tail + note). Full dump is not the default
   paint path there.
7. **Sub-agent log stays full length** when you open a child's log. That
   surface is an audit view: paint the stored tool/thinking bodies without
   the main-transcript line budget. DB is still never rewritten by this
   plan. (Parent LLM still receives only summaries / mention excerpts with
   their existing caps; that is separate from the log UI.)
8. **Render-path work** after keybindings + truncate so long sessions stay
   usable while streaming.

### Sub-agents vs the 8000 model cap (reference)

| Path | What the parent LLM gets | Cap today |
| --- | --- | --- |
| Child session in SQLite | Full child transcript (tools, text) as stored | Per-tool caps when the *child* agent ran tools (`maxToolOutput` on child bash/read/etc.) |
| `task` / `task_wait` / `task_status` JSON to parent | Status + short summary (not full child log) | Summary often from `LastAssistantText` (reuses 8000); task JSON itself is not re-clipped by a second 8000 in `execTaskTool` |
| `@agent:name` on send | Injected block appended to the send payload only | **`maxAtMentionContext = 4000`** runes per mention (includes task excerpt + last reply + last tool) |
| Sub-agent log TUI (`/agents` → enter) | N/A (you read it; not auto-sent to LLM) | v0.0.6: **no UI line budget** (full stored bodies) |

v0.0.6 does **not** raise or remove `maxToolOutput` / `maxAtMentionContext`.
If product later wants the parent model to see a full child log, that is a
separate agent-budget plan, not paint truncation.

## Phase files (live ledgers)

| File | Priority | Goal |
| --- | --- | --- |
| [phase-1-meta-keys.md](phase-1-meta-keys.md) | P0 | Remove `t`/`e`/click toggle; `ctrl+e` all tools; `ctrl+p` all thinking; docs/help |
| [phase-2-tool-output-truncate.md](phase-2-tool-output-truncate.md) | P0 | Cap expanded tool body paint on **main** transcript; sub-agent log stays full |
| [phase-3-render-path.md](phase-3-render-path.md) | P1 | Cheaper fingerprint, per-item memo, avoid full rebuild on every delta |

## Out of scope (explicit)

- Virtualizing the viewport to only the visible window (follow-up after
  phase 3 if still needed).
- Lazy-loading tool output from SQLite on expand (follow-up if resume RAM
  is the bottleneck).
- Changing `maxToolOutput` or `maxAtMentionContext` for the model (keep
  current agent budgets unless a later plan revisits them).
- Sending the full sub-agent log to the parent LLM automatically.
- New dependencies.

## Dependencies

```
phase-1-meta-keys  ──┐
                     ├──> phase-3-render-path (uses stable expand state)
phase-2-truncate  ───┘
```

Phases 1 and 2 can ship in either order or one PR each. Phase 3 should
land after expanded bodies have a line budget so benchmarks are not
dominated by multi-MB strings.
