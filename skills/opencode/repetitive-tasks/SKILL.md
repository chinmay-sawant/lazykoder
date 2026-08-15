---
name: repetitive-tasks
description: Eliminate recurring work in opencode sessions on gowkhtmltopdf — lint whack-a-mole campaigns, same-file fix waves, perf waves executed twice, re-derived findings, re-loaded grounding, duplicated fix dispatches, and repeated user prompts. Use when a task looks like something already done in a prior session; update the skill or checklist instead of repeating the loop.
---

# Repetitive Tasks (opencode)

Evidence from an opencode session-DB scan (263 sessions, Aug 3–15):

- The "fix the make lint" task was executed 4× in ~2 hours (10 + 11 aborted + 12 + 12 agents, ~3M input tokens) because waves were never verified and no shared checklist of already-fixed findings survived between rounds.
- The same files were assigned to 3 different rounds: paint.go, style.go, layout.go, flex.go, grid.go, reflect.go, css.go all appear in round-1, round-3, and round-2 titles.
- The entire perf wave (5 review + 5 fix + 2 lint agents) was executed twice in 24h — once manual, once skill-driven — covering the same five areas and same <100MB target.
- Same perf findings re-discovered wave after wave: ResolvedStyle 1.33KB copied by value (Aug 8 F2 / Aug 9), flow-index rebuild (Aug 8 H1 / Aug 9 F4), ToLower-per-lookup (Aug 8 F1 / Aug 9).
- Shared grounding re-loaded ~30×: README.md read 21 times, SCHEMA.md 19, compatibility-matrix.md 10, PROTOCOL.md 8 — one context load per subagent instead of once.
- Same verification battery (`go test ./...`, `make lint`, `CGO_ENABLED=0 go build`) re-run independently in ~8 sessions instead of one shared gate.
- Same failing-test fix dispatched twice (TestLengthToPt: one ran, the duplicate aborted empty).
- The user repeated the identical prompt 3× for the same request (audit question, make-samples, 10-subagent scan) because the first response didn't act.
- The same dossier was verified in three sequential sweeps within 13 hours (~23 subagent sessions, ~1.5M input tokens; the third sweep corrected only 8 of 601 verdicts).
- The same dark-theme page-number UI bug was requested twice back-to-back and fixed by neither.
- Every subagent repeated the identical exploration ceremony (read layout.go, style.go, paint.go, tests, `go test`, `git log`, `git status`) and re-derived the same facts in ≥4 sessions.
- The version sweep ("unreleased" → 0.2.2 → 0.2.3) was executed twice in the same session because the first release had to be redone.

## Rules

1. **Before repeating any loop, break the loop.** If the same task has been done more than once, stop and build the durable artifact: a shared checklist of fixed findings, a lint playbook, a findings registry, or a script. Loops are a signal the tooling is missing.
2. **Lint once per PR, never in campaigns.** Run `make lint` before opening any PR and fix findings in the same change. Never launch multi-wave fan-outs over `golangci-lint` findings — the Aug 7–15 campaigns produced ≥5 rounds, ~46 subagents, and ~3M tokens for one lint fix. Prefer config decisions (exclude noisy linters, fix `--disable-all` vs `enable-all` conflicts) over per-finding waves.
3. **Keep a shared findings registry.** Perf and lint findings (ResolvedStyle size, flow-index rebuild, ToLower-per-lookup) were re-discovered in every wave because no registry existed. Record each finding once with file:line + status; new waves diff against it.
4. **Share one grounding fact-sheet.** Before a fan-out, the parent compiles one distilled brief (API surface, counts, file:line refs, doc paths). Subagents read the brief, not README/matrix/docs from scratch — observed 8–30× re-load amplification.
5. **One baseline per benchmark.** Fix the commit and dataset once, define the gate once, and share the number. Four conflicting numbers (335.8MB, 1.10M, 3.93M, 14.3M) existed within 24h; waves launched from stale baselines.
6. **Never dispatch the same fix twice.** Check open sessions/tasks before spawning a fix subagent for a known failing test or bug. TestLengthToPt was dispatched twice; the same UI bug was requested twice and fixed by neither.
7. **Respond to the first ask.** Repeated identical user prompts (3× each for audit, make-samples, and scan tasks) indicate the first response didn't act. If a request needs clarification, ask; if it's actionable, do it fully the first time.
8. **One verification pass per sweep.** A dossier/checklist sweep verifies everything in one pass with a completeness gate (one verdict per item, verified by script). Three overlapping sweeps in 13 hours corrected only 8/601 verdicts.
9. **Instrumentation recipes are codified once.** The "how to debug the layout engine" recipe (go test -overlay vs repo copies) was rebuilt 3× in parallel. Save the winning recipe in a skill; two repo copies in /tmp went unused.

## Completion Handoff

- [ ] Any loop repeated 2+ times this session converted into a script, checklist, or registry entry.
- [ ] Findings registry updated (file:line, status) for every finding touched.
- [ ] Lint clean in the same PR; no deferred lint wave.
- [ ] Grounding fact-sheet saved under `reports/` for the next wave.
- [ ] No duplicate fix dispatches outstanding (open tasks checked).
