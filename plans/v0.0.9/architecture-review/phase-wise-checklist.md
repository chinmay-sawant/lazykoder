# Architecture review - phase-wise checklist

> **Parent:** `plans/v0.0.9/architecture-review/README.md`
> **Status:** phases 1-6 implemented and gated 2026-08-21; C8/C9 deferred
> **Estimated effort:** 5-8 days
> **Report:** `architecture-review.html` (candidates C1-C9)
> **Branch:** `chore/009-improve-architecture`

Mark `[x]` only with current evidence. Record the gate command beside closure
rows. Prefer package tests after each slice; run `make test` (or `go test ./...`)
at phase end.

---

## Overview

Deepen hot modules so Compaction, Session load, turn wiring, Tool dispatch,
and Subagent projection each have one owning interface. Shrink what
`chat_test.go` must prove through the whole TUI.

Shipped via four parallel package-owned agents plus parent integration
(Ask adapter, Session graph wiring into agent, lint fix, settings test fix).

---

## Executive summary

| Order | Candidate (report) | Rating | Status |
| --- | --- | --- | --- |
| 1 | C1 Compaction / fill | 9/10 | done |
| 2 | C3 Session graph | 8/10 | done |
| 3 | C4 Exclusive modes | 8/10 | done |
| 4 | C2 Turn-runtime | 9/10 | done |
| 5 | C5 + C7 Tool dispatch / task schema | 8/10, 7/10 | done |
| 6 | C6 Subagent projection + MaxDepth | 7/10 | done |
| - | C8 Layout snapshot | 6/10 | deferred |
| - | C9 Event without db.Part | 4/10 | deferred |

---

## Phase 1: Compaction and fill ownership (9/10)

### 1.1 Single source for defaults

- [x] Agent owns named runtime defaults (`DefaultCompactPercent` /
      `DefaultKeepTokens`). Settings keeps unexported 80 / 15_000 for JSON
      defaults only (no exported duplicate names).
- [x] Grep: production named consts live in `internal/agent/compact.go`.
- [x] Gate: `go test ./internal/agent/ ./internal/settings/` (pass 2026-08-21)

### 1.2 keep_tokens and ForceCompact honesty

- [x] `serializeEntries` uses `a.opts.KeepTokens` with `DefaultKeepTokens`
      fallback.
- [x] `ForceCompact` removed from `Options` / `runSteps` / `maybeCompact`.
- [x] Gate: `go test ./internal/agent/ -count=1` (pass 2026-08-21)

### 1.3 UI fill meter uses Agent helpers

- [x] TUI fill uses `agent.EstimateTokens` (private chars/4 path removed).
- [x] Compact hint path uses `agent.NeedsCompact` / Agent reasons.
- [x] Gate: `go test ./internal/ui/chat/ -count=1` (pass 2026-08-21)

### 1.4 Child Compaction Options

- [x] Child Agent sets `CompactAuto: false` (no ContextWindow; honest).
- [x] Gate: `go test ./internal/subagent/ ./internal/agent/ -count=1` (pass)

### 1.5 Phase 1 closure

- [x] `go test ./internal/agent/ ./internal/settings/ ./internal/subagent/ ./internal/ui/chat/`
- [x] KB compaction note updated (local knowledge-base)
- [x] Rows marked after gates

---

## Phase 2: Session graph and transcript projection (8/10)

### 2.1 Session graph load

- [x] `db.LoadSessionGraph` returns entries + `ToolCallsByPart` (batched parts).
- [x] `agent.sessionEntries` and `LastAssistantText` use `LoadSessionGraph`.
- [x] Gate: `go test ./internal/db/ ./internal/agent/ -count=1` (pass)

### 2.2 Transcript projection

- [x] Shared `projectSession` for main replay and Subagent log via Session graph.
- [x] Gate: `go test ./internal/ui/chat/ -count=1` (pass)

### 2.3 Phase 2 closure

- [x] `go test ./internal/db/ ./internal/agent/ ./internal/ui/chat/`
- [x] No new public provider interface

---

## Phase 3: Exclusive TUI mode ownership (8/10)

### 3.1 Focus module

- [x] `focus.go`: `setFocus` / `clearFocus` / `currentFocus` owns exclusive overlay.
- [x] Update / frame / mouse read focus state.
- [x] Gate: `go test ./internal/ui/chat/ -count=1` (pass)

### 3.2 Phase 3 closure

- [x] Full package tests pass
- [x] New focus/runtime/gate files keep chat.go under the 2k line wall

---

## Phase 4: Turn-runtime and human-gate adapter (9/10)

### 4.1 Gate adapter

- [x] `gate.go`: Confirm/Ask channel watch + hooks.
- [x] `ui/confirm` stays paint-only.
- [x] Runtime.Ask adapted via `runtimeAsk` (string options → `question.Question`).

### 4.2 Turn-runtime module

- [x] `runtime.go`: `attachSubMgr` / `Boot`, `agentOptions`, `startTurn`.
- [x] `submit` / `runCompact` / `resumeAfterLimit` share `startTurn`.
- [x] Gate: `go test ./internal/ui/chat/ ./internal/agent/ ./internal/subagent/` (pass)

### 4.3 Phase 4 closure

- [x] `go test ./...` (pass 2026-08-21)
- [x] `make lint` (pass 2026-08-21 after session_graph SQL const fix)
- [x] No new Charm/framework dependency

---

## Phase 5: Agent Tool dispatch and task schema (8/10 + 7/10)

### 5.1 Turn loop vs Tool dispatch

- [x] `tools_exec.go` holds dispatch; turn loop stays in `agent.go`.
- [x] `baseToolRunners` name→runner map for base tools.
- [x] Gate: `go test ./internal/agent/` + `go build ./...` (pass)

### 5.2 Policy gate decision locality

- [x] Allow/Ask/Deny path remains in `execBash`; `bash.Run` is process adapter.
- [x] Gate: `go test ./internal/policy/ ./internal/tools/bash/ ./internal/agent/ -count=1` (pass)

### 5.3 Wire or retire `internal/tools/task`

- [x] `subagent.Host` uses `internal/tools/task` for Specs/parse/`IsTaskTool`.
- [x] Agent `isTaskToolName` delegates to `task.IsTaskTool`.
- [x] Gate: `go test ./internal/subagent/ ./internal/agent/ ./internal/tools/task/` (pass)

### 5.4 Phase 5 closure

- [x] `go test ./...`
- [x] KB tools notes updated locally

---

## Phase 6: Subagent projection and MaxDepth honesty (7/10)

### 6.1 Drawer projection

- [x] Drawer uses `subagent.IsTerminalStatus` / Manager statuses (no success/done aliases).
- [x] Gate: `go test ./internal/ui/chat/ ./internal/subagent/ -count=1` (pass)

### 6.2 Nesting depth policy

- [x] Settings `MaxMaxDepth = 1`; Config clamps; `childSubagentHost` honors depth.
- [x] `DefaultChildMaxSteps` synced to 1000.
- [x] Gate: `go test ./internal/subagent/ ./internal/settings/` (pass)

### 6.3 Phase 6 closure

- [x] `go test ./...`
- [x] `Manager.Boot` used from chat `attachSubMgr` when recovering

---

## Deferred (explicit)

### D1 Layout snapshot (report C8, 6/10)

- [~] Deferred until the next paint/hit-test regression cluster. Owner: chat
      geometry. Next gate: reproduce a paint≠hit bug, then add a per-frame
      layout snapshot consumed by View and mouse.

### D2 Agent Event without `db.Part` (report C9, 4/10)

- [~] Speculative. Session graph shipped; Event still embeds `db.Part`. Do not
      pair with a premature multi-provider Client interface.

### D3 Provider domain types inside Agent

- [~] Worth exploring later: Agent-owned turn/message types with Client as
      wire adapter only. Blocked by product decision "OpenCode Go only"
      until a second provider is real.

---

## Closure gates (whole track)

- [x] `go test ./...` (pass 2026-08-21)
- [x] `make lint` (pass 2026-08-21)
- [x] Deferred C8/C9 left `[~]` on purpose
- [ ] Commit when user asks (no git in this implementation pass)
