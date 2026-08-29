# v0.0.11 - Orchestrator, OpenAI provider, recap in loop, pluggable catalogs

> **Parent:** current chat, settings, SQLite, first-request agent path, and local knowledge base
> **Status:** phases 1-5 complete; phase 6 draft - pluggable providers/tools/roles
> **Priority:** P1
> **Skill:** `skills/phase-wise-checklist/SKILLS.md`
> **Gate:** automated provider, orchestration, memory, UI, database, build, lint, and test gates pass. Live provider-key and terminal scenarios remain explicit human checks.

---

## Overview

This ledger turns the competitor gap analysis (Roo Code, Goose, Codex CLI,
Aider, Claude Code) into phase-wise work. Phase order is dependency-driven:

| Phase | Ledger | Scope | Effort |
| --- | --- | --- | --- |
| 1 | `phase-1-recap-memory.md` | Recap/memory surfaced into the loop | 2-3 days |
| 2 | `phase-2-openai-provider.md` | OpenAI provider package | 3-4 days |
| 3 | `phase-3-orchestrator.md` | Orchestrator over `internal/subagent` | 5-7 days |
| 4 | `phase-4-commit-push-button.md` | Commit and push action button in the composer | 3-4 days |
| 5 | `phase-5-memory-speed-history-churn.md` | Memory update speed and history list churn | 2-3 days |
| 6 | `phase-6-pluggable-providers-tools-roles.md` | Pluggable providers, tools, and roles matching the skills pattern | 7-10 days |

Each phase file is the canonical execution ledger for its rows. Status is
maintained there only; this file is the index.

## Executive summary

Six items close the gap on mixed-model multi-agent orchestration; the sixth makes the codebase itself extensible:

1. Recap/memory surfaced into the loop: auto-load the bounded
   `knowledge-base/memories.md` aggregate, model-based selection when grep
   misses, and a `/memory` view of what will be injected.
2. OpenAI provider as a second package behind the Provider seam so a GPT main
   agent can run with Kimi/DeepSeek/GLM children without any aggregator.
3. Planner/orchestrator layer over `internal/subagent`: structured plan,
   per-role model assignment, summary review, one re-spawn. Depth capped at 1.
4. Commit and push action button: after a successful change a button appears
   above the Enter/send input box for 90 seconds; clicking it asks the LLM to
   scan the diff, write a detailed commit message, commit, and push to the
   current remote branch.
5. Pluggable providers, tools, and roles matching the skills pattern: the
   hardcoded provider catalog, tool registry, and three-role switch become
   registries with approved-root discovery, the same drawer and slash families
   as `/skills` (`/provider`, `/tools`, `/roles`), and bounded diagnostics
   that never block chat. See `phase-6-pluggable-providers-tools-roles.md`.

## Dependencies

- Phase 2 does not depend on Phase 1; Phase 3 prefers Phase 2 landed
  (cross-provider fan-out) but works OpenCode-only.
- Phase 4 is independent of all others; it reuses the existing provider loop,
  policy gate, and composer layout seams.
- All phases reuse existing settings seams (`recap.enabled`, `recap.model`,
  `agents.model_override`) instead of parallel config.
- Dependency policy: any new module requires explicit announcement and
  sign-off; none anticipated.
