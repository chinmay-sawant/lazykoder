# AGENTS.md — opencode conventions for gowkhtmltopdf

> This file is the opencode-specific conventions ledger for agents running in
> this repo through opencode. Read it at session start alongside the
> project-wide `skills/grok/AGENTS.md`. It encodes the lessons from a 263-session
> opencode history scan (Aug 3-15). If a rule was ever re-explained to you by
> the user, it belongs here.

## Project

Pure-Go reimplementation of wkhtmltopdf (HTML → PDF). Default branch: `master`.
Sessions live in `~/.local/share/opencode/opencode.db`; prior session history is
the first place to check before assuming a task has never been attempted.

## Golden rules

1. **No git commands without explicit permission.** Never run `git add`,
   `git commit`, `git push`, `git restore`, `git clean`, `git reset`,
   `git stash`, `git checkout`, or `git status`-mutating flows unless the user
   asks. Subagent prompts carry this ban by default. (Violations happened:
   a fix agent ran `git status` despite the ban; a "read-only" reviewer ran
   `git stash -q`; both were audit findings.)
2. **Commit at session end; never leave a dirty tree.** Cancelled agents leave
   half-written trees (style.go regressed 53 → 135 findings mid-edit). If you
   are interrupted, record exactly what landed so the next session does not
   re-derive it from `git status` archaeology.
3. **No em dashes ("—") in any written output, docs, or commit messages.**
4. **Branch naming:** lowercase `chore/`, `feature/`, or `fix/<short-description>`.
5. **PRs** use `skills/PR/PR_TEMPLATE.md` (verify it is the correct project's
   template before using — a wrong-project "goslop" template was once used);
   issues use `skills/PR/ISSUE_TEMPLATE.md`.
6. **Checklists are live ledgers:** update `plans/` phase rows `[x]` in the same
   change that implements them. Fixer agents update their own checklist rows;
   do not leave a 52-edit parent bookkeeping pass.

## Build & test

- `make lint` — golangci-lint. **Repo config uses `enable-all: true`; never run
  `--disable-all` (it conflicts and silently changes results).** Do not fall
  back to `--no-config` (it loses repo settings). Fix findings in the same PR;
  never launch multi-wave lint fan-outs. Config decisions (exclude a noisy
  linter) beat per-finding waves.
- `make test` — full suite (includes golden corpus). Run after every behavior
  change; "no behavior changes" claims are verified by diff review + tests.
- `make samples` — regenerates golden/sample PDFs. `make samples` and `make test`
  must never fight over the same files; if they do, it is a bug, not a ritual.
- Release flow: `skills/release-note/SKILL.md` — module path check, `go install`
  smoke test, VERSION/cli.Version/CHANGELOG changed together, tests green before
  tagging. Tags are immutable once published.
- End-to-end artifacts are exercised early: a long phase run once shipped 9
  phases with `make build` emitting no binary and `make samples` missing its
  `output/` dir. Verify the runnable artifact at the start, not the end.

## Verification rules

- Rebuild the binary before any verification run (stale binaries produced false
  results).
- Read PNG/PDF screenshots directly when the model is multimodal; do not burn
  sessions on pixel-counting or PIL analysis that the model admits it cannot
  interpret.
- Fix the test, not the production format: assertions on raw serialized output
  (`/Type /Page\n` string counts) once drove a production-format change that was
  actually a test bug.
- Any visual fix is verified on the full fixture corpus, not just the fixed
  fixture.
- Never update a test's expectation to match a behavior change without explicit
  sign-off — changing the oracle in the same commit risks encoding the bug.

## Subagent usage (opencode-specific)

- Max 3-5 subagents per wave; each owns disjoint packages, verified in the
  brief (not just the prompt). Never assign two agents the same package or file
  (two debug agents once collided in `internal/layout`; one deleted the other's
  scratch test).
- Every subagent prompt contains: "Do NOT run any git commands."
- Never assign a subagent a bug the parent is fixing concurrently; never
  dispatch the same fix twice (TestLengthToPt was spawned twice).
- After spawning a wave, confirm every session has an assistant turn within
  minutes (12 dead spawns once went unnoticed for 5+ minutes).
- Diagnose before re-spawning: a failed agent re-spawned identically burned
  ~500K tokens with zero results ("going in circles"). Split the task smaller
  after 2 failures.
- Check cancelled/partial agents for landed work (`git diff`, partial outputs)
  before re-doing — 4 "crashed" agents had actually completed 124 files.
- Run the wave's gate (`make lint`/`make test`) between waves, and only spawn
  wave N+1 after wave N passes. Untested commands in briefs propagate to all
  subagents — execute every brief command once in the parent first.
- Deliverables (JSON/report/patch) are written early and checked to exist; one
  rescan agent went native and never wrote its output file.
- The main agent verifies the merged tree (build + tests) after any wave.
- Shared artifacts: one grounding fact-sheet under `reports/`, one benchmark
  baseline (commit + dataset), one findings registry. Downstream agents read
  them instead of re-scanning (~30x re-load amplification observed).

## Skills (this folder)

- `avoidable-work/` — wave gates, dead-spawn checks, diagnose-before-respawn,
  no parent duplication of subagent work.
- `repetitive-tasks/` — break loops (lint campaigns, same-file fix waves, perf
  waves, re-derived findings); keep a findings registry.
- `mistakes/` — ledger of 12 opencode mistakes with prevention rules (lint
  semantics breaks, shared-tree collisions, force-reverted fixes, brittle-test
  code changes, release errors, blind-fix regressions, coverage gaps).
- Cross-tool project conventions live in `skills/grok/AGENTS.md` (git ban,
  em dashes, commit cadence, dependency policy).

Skill files load from this folder by name; verify the path before loading
(wrong paths have burned 5+ loads in grok sessions).

## Dependency policy

The project is stdlib-only (pure Go, no third-party libraries) by design. Any
dependency addition is a project-policy change: amend the plan/README first and
get explicit user sign-off. No silent "exceptions".
