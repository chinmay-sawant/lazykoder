---
name: avoidable-work
description: Prevent wasted engineering effort in opencode sessions on gowkhtmltopdf — silent-empty subagents, dead spawns, respawn-without-diagnosis, waves without verification gates, duplicated profiling, do-it-twice pipelines, and parent duplication of subagent work. Use when starting any subagent wave, re-delegating a failed task, or running multi-agent fan-outs.
---

# Avoidable Work (opencode)

Evidence from an opencode session-DB scan (263 sessions, Aug 3–15):

- ~35% of lint-fix subagents made zero edits (16 of 46 across 4 waves); 5 of ~20 early delegated tasks returned empty after read-only "exploration".
- Wave 3 of the Aug 7 lint campaign: 12 subagents spawned at 18:53 all died instantly (prompt-only, 0 tokens); the parent session ended right after spawning and nobody noticed until the next session found 398 lint errors.
- Failed agents were re-spawned without diagnosis: fix-layout and fix-convert redos burned ~500K tokens across 4 sessions with zero landed changes until the user said "you are going in circles".
- Cancelled-agent results were opaque: 4 "crashed" lint agents had actually done 124 files of work; a cancelled root-api agent's work was complete, yet a 44K-token verify agent was respawned to discover that.
- The same 500-page benchmark/profile was re-run ~25× in one day by parallel scan agents instead of sharing one baseline.
- Docs were written twice: 10 "Write X page" subagents, then 10 "Detailed X page" subagents 7 minutes later overwriting all of them (SCHEMA v2 raised the quality bar after wave 1).
- A round-1 lint brief shipped a broken verification command (`--disable-all` conflicts with the repo's `enable-all`); all 7 subagents independently rediscovered the workaround.
- The parent probed the opencode DB itself (8 sqlite3 calls) AND dispatched 10 subagents to scan the same DB.

## Rules

1. **Verify a wave before the next one.** Run `make lint` / `make test` (or the wave's own gate) between waves. Never spawn wave N+1 from wave N's claim of success; the Aug 7 campaign re-fixed the same files 4 times because no gate ran between rounds.
2. **Diagnose before re-spawning.** If a subagent returned empty or failed, find out why (tool failure, model issue, oversized task) before re-delegating. Respawn-without-diagnosis produced ~500K tokens of zero work. If a task failed twice, split it smaller instead of re-trying identically.
3. **Check for dead spawns.** After spawning a wave, confirm each session has an assistant turn within minutes. 12 dead spawns went unnoticed for 5+ minutes and cost a full re-spawn.
4. **Never duplicate the subagents' work in the parent.** If 10 agents are scanning X, the parent does not scan X too. Compute once, delegate once, or delegate and verify only.
5. **Check cancelled-agent output before re-doing.** A cancelled agent's work may have landed (124 files in one case). Run `git status`/`git diff` and read partial outputs before assuming nothing happened.
6. **Share one baseline and one fact-sheet.** For benchmarks: one authoritative baseline with commit + dataset, shared by all agents. For grounding: a parent-compiled fact sheet (API surface, counts, file:line refs) instead of each agent re-reading README/matrix/docs (~30× re-loads observed).
7. **Test commands before putting them in briefs.** The broken `--disable-all` lint command propagated into 7 subagents. Every command in a brief is executed once by the parent first.
8. **Spec the full quality bar up front.** The "Detailed" spec (ASCII rules, block counts, toc/callout) that forced a full docs rewrite existed only after wave 1. Write the final spec before the first spawn.
9. **Write the output first.** Subagents that must produce a deliverable (JSON, report, patch) write it early and verify it exists; one rescan subagent went native (built binaries, ~20 harnesses) and ended without writing its output file, costing a 140K-token retry.

## Completion Handoff

Before declaring a wave done:

- [ ] Gate run between waves (`make lint`/`make test`/count check) and recorded.
- [ ] No dead spawns (every session has assistant turns).
- [ ] Cancelled/partial agents checked for landed work before re-delegation.
- [ ] One shared baseline/fact-sheet used by all agents; no duplicate parent scans.
- [ ] Commands in briefs pre-tested by the parent.
- [ ] Deliverables verified to exist (file written) before the agent is marked done.
