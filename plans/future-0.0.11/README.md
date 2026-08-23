# future-0.0.11 - Orchestrator, OpenAI provider, recap in loop

> **Parent:** current chat, settings, SQLite, first-request agent path, and local knowledge base
> **Status:** proposed, not scheduled
> **Estimated effort:** item 3 (recap in loop) 2-3 days; item 2 (OpenAI provider) 3-4 days; item 1 (orchestrator) 5-7 days
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** a session with both keys configured runs an OpenAI main model that decomposes a task into subtasks, spawns OpenCode children on distinct models by role, and the first response of a later fresh session demonstrably respects stored memories, with `/memory` showing exactly what was injected.

---

## Overview

This ledger turns the competitor gap analysis (Roo Code, Goose, Codex CLI,
Aider, Claude Code) into phase-wise work. Phase order is dependency-driven:
Phase 1 (recap/memory surfaced into the loop) is smallest and independent.
Phase 2 (OpenAI provider) is independent and unblocks cross-provider
fan-out. Phase 3 (orchestrator over `internal/subagent`) is the biggest gain
and benefits from Phase 2 existing.

## Executive summary

Three items close the gap on mixed-model multi-agent orchestration:

1. Recap/memory surfaced into the loop. Recall already exists
   (`internal/agent/recall`): recaps are grepped before the first ordinary
   request of a turn as unpersisted hints. Missing pieces: auto-loading the
   bounded `knowledge-base/memories.md` aggregate, model-based selection when
   grep misses, and a `/memory` view of what will be injected.
2. OpenAI provider as a second package behind the Provider seam so a GPT main
   agent can run with Kimi/DeepSeek/GLM children without any aggregator.
3. Planner/orchestrator layer over `internal/subagent`: the parent emits a
   structured plan, assigns children by model strength, reviews summaries,
   and may re-spawn failures. Depth stays capped at 1.

## Phase 1: Recap and memory surfaced into the loop

### 1.1 Auto-load the memories aggregate

- [ ] Add a loader in `internal/recap/memory.go` that reads
      `knowledge-base/memories.md`, validates format_version, and returns a
      bounded context block; missing file yields an empty block, not an error.
- [ ] In `internal/agent`, inject the aggregate alongside recall hints before
      the first ordinary request of a user turn: unpersisted, untrusted,
      clearly sectioned, once per turn (no repeats from tool follow-ups,
      `/continue`, children, or compaction), mirroring existing recall rules.

### 1.2 Model-based selection when grep misses

- [ ] When recall grep returns zero hits for a turn, run one hidden selection
      call using the configured `recap.model` over recent recap titles plus
      summaries; selected lines enter the same unpersisted hint channel.
- [ ] Bound the pass: single call, no tools, capped input, failure falls back
      to grep-only behavior silently.

### 1.3 `/memory` view and toggle

- [ ] Add `/memory` slash command (`chat.go` + `slash.go`) rendering the
      exact context the next turn carries: memories sections and matched
      recap lines with source paths.
- [ ] Add a per-session injection toggle in the view; toggling off suppresses
      both aggregate and hint injection for the rest of the session only.

### 1.4 Docs and gate

- [ ] Update `docs/` and knowledge-base pages (recaps, memory concepts) in
      the change that ships the behavior.
- [ ] Gate (documentation-only rows exempt from lint/test per skill rule;
      this phase touches code, so record outcomes):
      `make lint` PASS, `make test` PASS, then live TTY check by user:
      fresh session respects a stored preference and an avoid rule without
      being told, and `/memory` lists those exact lines.

## Phase 2: OpenAI provider

### 2.1 Provider package

- [ ] Create `internal/provider/openai` implementing the same Provider
      interface as `internal/provider/opencode`; chat-completions wire format
      at `https://api.openai.com/v1/chat/completions`; no new dependencies.
- [ ] Unit tests against a fake HTTP server covering tool calls, streaming,
      and error mapping, matching opencode client test coverage shape.

### 2.2 Settings and keys

- [ ] Add `provider.active` setting with values `opencode` (default) and
      `openai`; key resolution via `OPENAI_API_KEY` env or `.env`, mirroring
      `OPENCODE_API_KEY` handling including error text when missing.
- [ ] Model catalog: static curated list (or cached `/v1/models`) written to
      `.lazykoder/models.json` cache path with the same 15 minute TTL
      semantics; `/model` picker and `r` refresh work unchanged.

### 2.3 Cross-provider wiring

- [ ] Verify the parent can run on OpenAI while `task` children resolve their
      models through the OpenCode client (per-role overrides unchanged);
      policy gate, persistence, and compaction behave identically.
- [ ] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      GPT main agent + Kimi/DeepSeek explore child in one session, drawer
      shows child jobs, final answer cites child output.

## Phase 3: Orchestrator layer over internal/subagent

### 3.1 Plan emission

- [ ] Add orchestrator prompt path in `internal/agent`: when sub-agents are
      enabled and the task looks decomposable, the first request asks for a
      structured plan (subtasks, role, suggested model class); persist the
      plan as a message part so `/resume` restores it.
- [ ] Fallback: if the plan call fails or returns malformed structure, run
      the turn as today with no orchestration.

### 3.2 Strength table and assignment

- [ ] Add settings for role-to-model-class defaults (`orchestrator.*`),
      shipping built-in defaults (flash tier for explore, pro coder tier for
      general); users override in settings UI; validate against the cached
      model catalog at load time.
- [ ] Extend Host dispatch to accept per-subtask model class from the plan,
      resolving through the same override chain used today
      (`ConfigFromSettings`).

### 3.3 Review and re-spawn

- [ ] After children finish, the parent reviews summaries against the plan;
      failed or incomplete subtasks may be re-spawned once, still respecting
      MaxDepth=1, budget caps, and wall-clock timeouts.
- [ ] Drawer and transcript render the plan and per-child status without
      breaking existing layout rules (single-line rows, truncation to width).

### 3.4 Docs and gate

- [ ] Update `docs/`, knowledge-base sub-agent concept page, and glossary in
      the same change.
- [ ] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      "audit these N packages" produces one plan message, N concurrent
      children on distinct models, ordered summaries, final answer citing
      each child.

## Dependencies

- Phase 2 does not depend on Phase 1; Phase 3 prefers Phase 2 landed
  (cross-provider fan-out) but works OpenCode-only.
- All phases reuse existing settings seams (`recap.enabled`, `recap.model`,
  `agents.model_override`) instead of parallel config.
- Dependency policy: any new module requires explicit announcement and
  sign-off; none anticipated.
