---
name: avoidable-work
description: Prevent wasted engineering effort on gowkhtmltopdf — throwaway analysis scripts, uncommitted subagent work, unrepeatable verification, subagent over-escalation, overlapping codebase re-scans, and missing project conventions. Use when starting any work wave, spawning subagents, writing one-off analysis tooling, or finishing a session.
---

# Avoidable Work

Evidence from a full session-history scan of this project (170 sessions, Aug 3–15):

- ~200 throwaway analysis scripts were written inline (163 `python3` invocations in one session alone) and never saved; only 3 ever reached `scripts/`.
- A fix wave was left uncommitted overnight, forcing the next session to reverse-engineer the dirty tree (`git status` ×4, `find -mmin -15`) and re-diagnose the same bugs.
- 15 subagents across 3 waves made 882 `read_file` calls over just 189 unique files (~4.7× amplification); core files were re-read 10–13×.
- Subagent over-escalation (5 → 6 → 10 → 20 spawns) produced results too big to verify; the user then had to say "DO NOT USE ANY SUBAGENTS".
- "No git commands" had to be re-instructed in 4+ sessions; a subagent still ran git commands and was killed, discarding an entire work batch.
- The model's multimodal capability was discovered mid-project by the user ("are you multimodal?") — pixel-counting scripts were used to "see" PDFs.
- A 183-message install session and a setup→full-teardown session both ended in removal or crash-debugging with no plan upfront.

## Rules

1. **Persist reusable tooling.** Any analysis script, screenshot routine, or diagnostic used more than once goes into `scripts/` with a README. Never grep `~/.grok/sessions` or shell history to rediscover past work — that is a sign the artifact should already exist in the repo.
2. **Never leave a dirty tree at session end.** Commit (or explicitly stash with a named label) before stopping. Uncommitted subagent fixes are the #1 cause of cross-session rework in this project.
3. **Ban git in subagent prompts by default.** Every subagent prompt includes "Do NOT run any git commands" until the human explicitly permits them. A subagent running `git restore`/`git clean`-type commands discards an entire batch of work.
4. **Cap and verify subagent waves.** No more than 3–5 subagents per wave, each with exclusive package ownership (see `skills/mistakes/SKILL.md` for the concurrent-editing failure mode). A wave is not done until the main agent verifies the merged tree itself.
5. **Share scan artifacts between waves, do not re-scan.** When a wave produces a report (API surface, settings-consumer map, fixture catalog), save it under `reports/` and pass the distilled facts to the next wave. Downstream agents re-doing the same reads means the upstream report was wasted.
6. **Never race subagents against the parent.** Do not assign a subagent a bug the parent is fixing concurrently (a subagent burned 106 read/grep calls confirming the parent had already fixed the tree).
7. **Verify visually with real tools, not scripts.** Read PNG/PDF screenshots directly (multimodal). Keep local validators (`avalpdf`, `veraPDF` when available) wired instead of pasting octopdf.com results by hand.
8. **No setup→teardown experiments without a plan.** Any install/config work gets a short checklist first (what to change, how to verify, how to roll back). Do not burn 183 messages debugging a server that was never needed.
9. **Do not create empty sessions.** A session with 0 messages is dead weight; reuse the existing session or name the new one with intent.

## Completion Handoff

Before declaring a work wave done:

- [ ] All fix work committed with the repo's convention (go-only vs generated-output rules per `AGENTS.md`).
- [ ] Any script/command run more than once is saved under `scripts/`.
- [ ] Scan reports saved and referenced by downstream sessions.
- [ ] Subagent prompts all contained the git ban.
- [ ] Merged tree re-verified by the main agent (build + `make test`).
- [ ] Session ends with a status summary so the next session does not re-derive context.
