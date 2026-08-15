---
name: repetitive-tasks
description: Eliminate recurring work on gowkhtmltopdf — per-fixture screenshot/fix/regenerate loops, lint waves, benchmark re-runs, golden regeneration, temp diagnostic tests, re-taught conventions, and phase-checklist reconciliation. Use when a task looks like something already done on this project; update the skill or checklist instead of repeating the loop.
---

# Repetitive Tasks

Evidence from the session-history scan (170 sessions, Aug 3–15):

- The identical per-fixture loop (screenshot PDF → list issues → fix → regenerate → commit) ran 5× verbatim in one session and ~10× in another, and the same goal was restarted in a brand-new session minutes later (Aug 12 fixes uncommitted → Aug 13 re-diagnosed from scratch).
- The same lint problems were fought in 4+ waves; `make lint` ran ~35× across two sessions; the layout lint-fix was done twice from scratch (batch-1 killed for git commands, batch-2 rebuilt the same renamer tool).
- `paintCount` (P2-12) was worked 3 separate times; P4-07 was attempted, reverted, then redone; same PDF-writer bug classes (zlib/FlateDecode, /Outlines ordering) were re-litigated across 4+ sessions.
- The 500-page benchmark was re-run 50+ times with results moving within a noise band; golden PDFs were regenerated in 5+ separate sessions; the same compliance PDFs were generated and committed twice.
- Temp `diag_*_test.go` files were created/run/deleted constantly (57 references in one session; the same file rm'd 9×).
- The em-dash rule, "commit go only / ignore PDFs", and branch-naming conventions were re-taught in 6+ sessions each.
- Phase checklists drifted from implementation, requiring explicit post-hoc "reconcile" passes.

## Rules

1. **Before repeating any loop, break the loop.** If you have done the same diagnose→fix→regenerate cycle more than twice, stop and build the durable artifact: a saved QA/screenshot harness (see `skills/avoidable-work/SKILL.md`), a golden/screenshot-diff regression gate, or a make target. Loops are a signal the tooling is missing, not that effort is needed.
2. **PDF fidelity loop.** One canned flow: render fixture → read screenshot directly (multimodal) → one engine fix per root cause → regenerate → run the golden gate → commit. Do not re-list known issues; track open fixture issues in the canonical checklist instead.
3. **Lint once per PR, never in waves.** Run `make lint` before opening any PR and fix findings in the same change. Never run `golangci-lint --fix` or scripted renames as bulk passes (see `skills/mistakes/SKILL.md`). Config fixes (e.g. gofumpt/Go-version incompatibility → exclude in `.golangci.yml`) are done once, never re-diagnosed.
4. **Golden artifacts have one source of truth.** Golden PDFs are regenerated only by `make samples` from the committed generator; `make test` must never depend on files `make samples` rewrites. If both touch the same HTML, the conflict is a bug to fix, not a ritual to repeat.
5. **Benchmarks are noise-aware.** Fix the commit and dataset, define the acceptance gate once (e.g. 500-page < 2.5s), and stop when the gate is met. Do not chase movement within the noise band; do not re-run the full matrix for micro-optimizations without a measured bottleneck.
6. **Diagnose with one durable harness.** Keep a single debug harness (e.g. a fixture-driven test with `-run` filters) instead of creating/deleting `diag_*_test.go` per fixture. A compile error 22× on the same probe means the probe should live in the repo.
7. **Conventions live in `skills/AGENTS.md`.** Style rules (no em dashes), commit rules (go-only vs generated output), branch naming, and "no git commands" are read from AGENTS.md, not re-instructed. If a convention was re-explained to you, add it to AGENTS.md.
8. **Checklists are live ledgers.** Update phase-checklist rows (`[x]`) in the same change that implements them (see `skills/phase-wise-checklist/SKILLS.md`). Never leave reconcile as a follow-up pass.
9. **Version sweeps are a single checklist.** VERSION, `internal/cli/help.go`, CHANGELOG, docs, and frontend version strings change together in one commit, verified by `TestCLIVersionMatchesVERSIONFile` before committing.

## Completion Handoff

- [ ] Any loop repeated 2+ times in this session has been converted into a script, harness, or checklist row.
- [ ] No temp `diag_*` files left in the tree.
- [ ] `make lint` clean before PR; no lint wave deferred to a follow-up.
- [ ] Phase-checklist rows match shipped work at session end.
- [ ] Benchmark/verification results recorded with commit + dataset so re-runs are comparable.
