## Summary

This branch lands the core of v0.0.12: a hardened parent and child cancellation lifecycle, safe public `webfetch` browser reading, completed phase-3 sub-agent orchestration, a first cut of screenshot-led TUI work (settings category rail, bounded ask overlay, footer and cancel clarity), and externalized prompts plus tool schemas.

Phases 1-3 are the main ship. Phase 4 is partial. Phase 5 docs and full closure gates remain open.

## Motivation / context

- Plans: `plans/v0.0.12/README.md`, plus phases 1-4 under `plans/v0.0.12/`
- Carry-over: `plans/v0.0.10/phase-11-browser-support.md`
- Issues: no GitHub issue is associated with this branch

## Changes

### Cancellation lifecycle

- Parent cancel signals immediately through the shared contract.
- Tools and jobs persist `cancelled` through a short independent persist context.
- Terminal tool and job rows stay immutable.
- Provider stream and retry stop on local context cancel. No remote cancel API is claimed.
- UI uses `RequestCancel` without blocking Bubble Tea `Update`.

### Public webfetch browser reading

- DevTools-based Chrome/Chromium reader over a stdlib websocket.
- Returns the actual `final_url`, categorized browser errors, and context-tied process/proxy cleanup.
- Keeps URL and egress bounds, plus metadata caps (`content_truncated`, `browser_truncated`, `output_truncated`, 64 KiB).
- Auto HTTP to browser fallback metadata when needed.
- No new Go module dependencies. Chrome/Chromium remains an optional system binary.

### Sub-agent orchestration

- Non-blocking `RequestCancel` / `RequestCancelAll`.
- `task_cancel` returns before cleanup; callers use `task_status` / `task_wait`.
- Status, wait, and cancel stay scoped to the parent session.
- Durable-only cancel, sibling failure isolation, and recovery/shutdown race coverage.
- Cancel and timeout errors normalize to `subagent: cancelled|timed out`.

### Chat TUI and settings

- Settings becomes a category rail plus filter workspace (`settings_workspace.go`).
- `/model` and `/variant` (and status chip clicks) open settings sections for new-session defaults.
- Ask overlay width is capped on wide terminals.
- Help overlay column grid alignment, composer footer contrast, quieter empty-session tips, and clearer long tool-command expand hints.

### Prompts and tool schemas

- Authored Markdown and tool JSON live under embeddable defaults.
- Workspace seeds `.lazykoder/prompts/` without overwriting existing files.
- Agent, orchestrator, recap, provider, subagent, and tool specs load through `prompts.Store` with embedded fallback.
- Adds `docs/prompts.md`.

### Plans and docs

- Splits the v0.0.12 checklist into phase ledgers under `plans/v0.0.12/`.
- Updates tools, architecture, storage, and TUI docs for the shipped surfaces.

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Browser path adds DevTools wait/settle and DOM caps. Cancel avoids UI stalls. No broad hot-path rewrite. |
| **Memory** | Bounded DOM, body, and metadata. Temporary Chrome profile is cleaned. No profile reuse. |
| **Behavior / correctness** | Cancel stops parent, children, and tools locally and leaves terminal SQLite rows. Webfetch browser mode returns a real final URL and clearer failure modes. Settings UX is reorganized. |
| **API / CLI** | Tool metadata keys and truncation markers. `task_cancel` is signal-only. Status/wait/cancel refuse foreign parent IDs. Prompt and tool text are file-backed. |
| **Dependencies** | No new Go module dependency. Optional system Chrome/Chromium for browser mode. |
| **Binary size / build time** | Higher embed size from prompt and schema defaults. No new runtime libraries. |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| `/model` and `/variant` open settings | They set new-session defaults. They no longer open the live session model picker. |
| Foreground `task` children | They follow the parent turn and cancel with it. Use background tasks for post-turn work. |
| `task_cancel` | Returns after signaling. Poll with `task_status` or `task_wait` for cleanup. |
| Webfetch metadata | May include truncation markers. Oversized metadata collapses to `{"metadata_truncated":true}`. |
| Prompt customization | Edit `.lazykoder/prompts/`. Bad tool JSON falls back to embedded defaults. Existing prompt files are not overwritten on seed. |
| Browser mode ops | Needs Chrome or Chromium on PATH (`google-chrome`, `google-chrome-stable`, `chromium`, or `chromium-browser`). |

## Test plan

- [x] `make test`
- [x] `make lint`
- [x] `make vet`
- [x] `make build`
- [x] `CGO_ENABLED=0 go build -o bin/lk .`
- [ ] Real TTY checks via `make run` at 120x36 and 80x24
- [ ] Manual cancel of parent with active child and tool rows
- [ ] Manual webfetch browser path when Chrome/Chromium is available

### Commands

```sh
make test
make lint
make vet
make build
CGO_ENABLED=0 go build -o bin/lk .
make run
```

Focused packages with new coverage on this branch include `internal/db`, `internal/agent`, `internal/provider/opencode`, `internal/subagent`, `internal/tools/webfetch`, `internal/ui/chat`, and `internal/prompts`.

## Screenshots / sample output

```text
Branch tip: 36573f7 Externalize prompts and tool schemas
Compare: https://github.com/chinmay-sawant/lazykoder/compare/master...feature/012-browser-agents-ui

Local screenshot deletes under screenshots/*.png are dirty working-tree state only
and are not part of this PR.
```

## Related issues

- No related GitHub issue exists for this branch.
- Tracking record: `plans/v0.0.12/README.md` (phases 1-4 implementation; phase 5 still open).
- Relates to prior browser carry-over: `plans/v0.0.10/phase-11-browser-support.md`.

## PR metadata checklist (author)

- [x] Self-assigned with `--assignee @me`
- [x] Labels applied with `enhancement` and `documentation`
- [x] Related issues checked. No issue exists for this branch.
- [x] Filled body stored under `plans/PR/pr-v0.0.12-browser-agents-ui.md`

## Follow-ups (out of scope)

- Finish remaining phase-4 layout, filter, and responsive rows.
- Close phase-5 documentation, knowledge-base, and full gate ledger.
- Live Chrome fixture and Medium manual browser acceptance.
- Reconcile `docs/plans.md` with the closed phase-2 and phase-3 ledger rows if still stale.

## Reviewer checklist

- [ ] Behavior matches the summary and test plan
- [ ] No unrelated changes in the branch diff
- [ ] Public API and CLI changes are documented
- [ ] New behavior has focused test coverage
- [ ] PR has assignee and labels
- [ ] Related issue references are accurate
- [ ] No secrets or generated artifacts are committed
