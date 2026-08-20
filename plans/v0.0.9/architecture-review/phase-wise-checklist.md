# Architecture review - phase-wise checklist

> **Parent:** `plans/v0.0.9/architecture-review/README.md`
> **Status:** not started
> **Estimated effort:** 5-8 days
> **Report:** `architecture-review.html` (candidates C1-C9)

Mark `[x]` only with current evidence. Record the gate command beside closure
rows. Prefer package tests after each slice; run `make test` (or `go test ./...`)
at phase end.

---

## Overview

Deepen hot modules so Compaction, Session load, turn wiring, Tool dispatch,
and Subagent projection each have one owning interface. Shrink what
`chat_test.go` must prove through the whole TUI.

---

## Executive summary

| Order | Candidate (report) | Rating | Why this phase |
| --- | --- | --- | --- |
| 1 | C1 Compaction / fill | 9/10 | Proven dual homes; keep_tokens hardcode |
| 2 | C3 Session graph | 8/10 | Unlocks resume + child log + history tests |
| 3 | C4 Exclusive modes | 8/10 | Unblocks safer chat splits |
| 4 | C2 Turn-runtime | 9/10 | Composition root leaves the View package |
| 5 | C5 + C7 Tool dispatch / task schema | 8/10, 7/10 | Agent locality + delete orphan |
| 6 | C6 Subagent projection + MaxDepth | 7/10 | After runtime exists |
| - | C8 Layout snapshot | 6/10 | Deferred until next paint/hit bug cluster |
| - | C9 Event without db.Part | 4/10 | Speculative; after Session graph settles |

---

## Phase 1: Compaction and fill ownership (9/10)

### 1.1 Single source for defaults

- [ ] Remove duplicate Compaction default constants so only one package defines
      percent / keep_tokens (today both `internal/agent/compact.go` and
      `internal/settings/settings.go`). Settings keep persisted knobs;
      agent (or one shared home) owns runtime defaults.
- [ ] Grep `DefaultCompactPercent` / `DefaultKeepTokens` after the change;
      every production path resolves through the single home.
- [ ] Gate: `go test ./internal/agent/ ./internal/settings/`

### 1.2 keep_tokens and ForceCompact honesty

- [ ] `serializeEntries` in `compact_run.go` uses the Agent's keep-tokens
      option, not a hardcoded `DefaultKeepTokens`.
- [ ] `ForceCompact` is either set by a real caller (UI / shrink path) or
      removed from `Options` and `runSteps`.
- [ ] Gate: `go test ./internal/agent/ -run Compact`

### 1.3 UI fill meter uses Agent helpers

- [ ] Delete or wrap the private TUI `estimateCharsPerToken` /
      `estimateTokens` path in `transcript.go` so fill math calls the same
      estimate helpers Compaction uses.
- [ ] `refreshCompactHint` / `pendingCompactReason` only display policy that
      Agent would apply on the next send (no separate shrink rule).
- [ ] Gate: `go test ./internal/ui/chat/ -run 'Compact|Usage|Token'`

### 1.4 Child Compaction Options

- [ ] Child Agent in `subagent/runner.go` either gets a real context window +
      percent/keep from settings/catalog, or stops setting `CompactAuto: true`
      when Compaction cannot fire.
- [ ] Gate: `go test ./internal/subagent/ ./internal/agent/`

### 1.5 Phase 1 closure

- [ ] `go test ./internal/agent/ ./internal/settings/ ./internal/subagent/ ./internal/ui/chat/`
- [ ] Update `knowledge-base/03-concepts/compaction.md` if ownership wording changed
- [ ] Mark matching rows in this file only after gates pass

---

## Phase 2: Session graph and transcript projection (8/10)

### 2.1 Session graph load

- [ ] One load path returns messages + parts + tool_calls (and optional usage
      inputs) for a Session id. Call sites stop orchestrating
      `ListMessages` + per-message `ListParts` + `ListToolCalls` themselves.
- [ ] Migrate `agent.sessionEntries` / `LastAssistantText` consumers first.
- [ ] Gate: `go test ./internal/db/ ./internal/agent/ -run 'History|Compact|Summary'`

### 2.2 Transcript projection

- [ ] Main `replay` and Subagent `loadSubagentLogItems` share one Session→
      `transcriptItem` projection with explicit knobs (reasoning collapse,
      usage/compact notices, input-history seed).
- [ ] Gate: `go test ./internal/ui/chat/ -run 'Replay|Subagent|Transcript'`

### 2.3 Phase 2 closure

- [ ] `go test ./internal/db/ ./internal/agent/ ./internal/ui/chat/`
- [ ] Update `knowledge-base/02-architecture/data-flow.md` Session load note
- [ ] Confirm no new public provider interface was introduced

---

## Phase 3: Exclusive TUI mode ownership (8/10)

### 3.1 Focus module

- [ ] Replace the hand-cleared `*Mode` bool cascade with one exclusive focus
      owner used by open/close helpers.
- [ ] `Update` key cascade, `frame` branches, and mouse priority tables read
      the same focus state.
- [ ] Gate: `go test ./internal/ui/chat/ -run 'Status|Settings|Picker|Slash|Subagent|Confirm|Ask|Session'`

### 3.2 Phase 3 closure

- [ ] Full package: `go test ./internal/ui/chat/`
- [ ] Spot-check View strings for overlay exclusivity (one screen at a time)
- [ ] File sizes stay under ~2000 lines; no length-only splits

---

## Phase 4: Turn-runtime and human-gate adapter (9/10)

### 4.1 Gate adapter

- [ ] Confirm/Ask channel watch + resolve + cancel unblock live in one adapter
      used by Agent Options and Subagent Runtime.
- [ ] `ui/confirm` stays paint-only.
- [ ] Gate: targeted tests for allow / deny / cancel without a full transcript
      Model (new or moved tests).

### 4.2 Turn-runtime module

- [ ] Manager boot (`New` / `SetStore` / `SetRuntime` / `Recover` / rebuild)
      and per-turn `agentOptions` / Host construction move behind one
      turn-runtime module.
- [ ] `submit` / `runCompact` / `resumeAfterLimit` call the same start-turn
      path.
- [ ] Chat `Model` keeps transcript, prompt, overlays; side effects stay in
      `tea.Cmd`.
- [ ] Gate: `go test ./internal/ui/chat/ ./internal/agent/ ./internal/subagent/`

### 4.3 Phase 4 closure

- [ ] `make test` or `go test ./...`
- [ ] Update `knowledge-base/02-architecture/component-map.md` with the new
      module's ownership paragraph
- [ ] No new Charm/framework dependency

---

## Phase 5: Agent Tool dispatch and task schema (8/10 + 7/10)

### 5.1 Turn loop vs Tool dispatch

- [ ] Split `agent.go` along responsibility: turn orchestration vs tool
      execution (same package unless a real second adapter appears).
- [ ] Registry (or dispatch table) maps base Tool name → runner; `executeTool`
      is not an ever-growing switch of business logic.
- [ ] Gate: `go test ./internal/agent/` + `go build ./...`

### 5.2 Policy gate decision locality

- [ ] One path produces Allow / Ask / Deny for bash (classifier + confirm
      policy + nil Confirm). `bash.Run` remains the process adapter, not a
      second policy author.
- [ ] Gate: `go test ./internal/policy/ ./internal/tools/bash/ ./internal/agent/ -run Bash`

### 5.3 Wire or retire `internal/tools/task`

- [ ] Either Host production ads/parse import `internal/tools/task`, or the
      unused package is removed and docs/KB name Host as the schema owner.
- [ ] Collapse duplicate `IsTaskTool*` helpers to one source.
- [ ] Gate: `go test ./internal/subagent/ ./internal/agent/ ./internal/tools/task/`
      (or confirm package deletion with `go test ./...`)

### 5.4 Phase 5 closure

- [ ] `go test ./...`
- [ ] Update `knowledge-base/03-concepts/tools.md` task-tools section

---

## Phase 6: Subagent projection and MaxDepth honesty (7/10)

### 6.1 Drawer projection

- [ ] Subagent drawer rows come from Manager.List (+ exported terminal/live
      helpers). Stop inventing `"completed"` / `"success"` / `"done"` status
      aliases that Manager never emits.
- [ ] Orphan child sessions (store-only) stay an explicit fallback, not a
      parallel lifecycle.
- [ ] Gate: `go test ./internal/ui/chat/ -run Subagent` + `go test ./internal/subagent/`

### 6.2 Nesting depth policy

- [ ] `Config.MaxDepth` either drives child "may spawn" at job construction,
      or settings stop advertising editable `max_depth` above the enforced 1
      until nesting ships.
- [ ] Sync or delete `DefaultChildMaxSteps` mismatch (32 vs settings 1000).
- [ ] Gate: `go test ./internal/subagent/ ./internal/settings/`

### 6.3 Phase 6 closure

- [ ] `go test ./...`
- [ ] Update `knowledge-base/03-concepts/tools.md` (subagent) and component-map

---

## Deferred (explicit)

### D1 Layout snapshot (report C8, 6/10)

- [~] Deferred until the next paint/hit-test regression cluster. Owner: chat
      geometry. Next gate: reproduce a paint≠hit bug, then add a per-frame
      layout snapshot consumed by View and mouse.

### D2 Agent Event without `db.Part` (report C9, 4/10)

- [~] Speculative. Do after Session graph (phase 2). Do not pair with a
      premature multi-provider Client interface.

### D3 Provider domain types inside Agent

- [~] Worth exploring later: Agent-owned turn/message types with Client as
      wire adapter only. Blocked by product decision "OpenCode Go only"
      until a second provider is real. Record as ADR if rejected for
      load-bearing reasons.

---

## Dependencies

```
Phase 1 (fill) ──┬──► Phase 2 (Session graph)
                 │
Phase 3 (modes) ─┼──► Phase 4 (turn-runtime) ──► Phase 6 (drawer)
                 │
Phase 1 ─────────┴──► Phase 5 (tool dispatch) can start after 1;
                      prefer after 4 if touching keys.go wiring
```

Phase 3 can run in parallel with phase 1-2 (chat-only). Phase 4 should wait
for phase 1 so Compaction Options assembly is stable.

---

## Closure gates (whole track)

- [ ] `go test ./...`
- [ ] `make lint` (or project lint target) clean on touched packages
- [ ] HTML report still matches shipped seams (update or annotate if a
      candidate was rejected with an ADR)
- [ ] `knowledge-base/02-architecture/` page for this review updated
- [ ] No dirty tree left at session end (commit when user asks)
