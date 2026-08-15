# AGENTS.md — Antigravity CLI (agy) conventions for gowkhtmltopdf

> This file is the Antigravity CLI (agy) conventions ledger for agents running
> in this repo through `agy` (a shortcut that opens Antigravity CLI). Read it at
> session start alongside the project-wide `skills/grok/AGENTS.md`. It encodes
> the lessons from a 57-session agy transcript scan (Aug 9-15). If a rule was
> ever re-explained to you by the user, it belongs here.

## Project

Pure-Go reimplementation of wkhtmltopdf (HTML → PDF). Default branch: `master`.
Sessions live under `/home/chinmay/.gemini/antigravity-cli/brain/<id>/.system_generated/logs/transcript.jsonl`
(JSONL: `step_index`, `source`, `type`, `status`, `created_at`, `content`).

## Golden rules

1. **No git commands without explicit permission.** Never run `git add`,
   `git commit`, `git push`, `git restore`, `git clean`, `git reset`,
   `git stash`, or `git checkout` unless the user asks. Subagent prompts carry
   this ban by default. (This instruction was repeated 4+ times per session
   across sessions and still violated — it is durable config, not a reminder.)
2. **Commit at session end; checkpoint at work units.** This tool crashes on
   quota/API overload and truncates context mid-work. A crashed session loses
   everything uncommitted: save partial files immediately, commit progress at
   each natural unit, and keep the phase checklist as the source of truth.
3. **No em dashes ("—") in any written output, docs, or commit messages.**
4. **Branch naming:** lowercase `chore/`, `feature/`, or `fix/<short-description>`
   (verify the branch name before the first commit — a typo'd
   `chore/frontend-udpates` lived across 3 commits and a PR).
5. **PRs** use `skills/PR/PR_TEMPLATE.md`; issues use `skills/PR/ISSUE_TEMPLATE.md`.
   Direct pushes to master are rejected by branch protection — branch + PR is
   the default, not an improvisation.
6. **Checklists are live ledgers:** update `plans/` phase rows `[x]` only when
   the gate actually passed — never mark `[x]` from intent. A task is done when
   the final exit code and validator output say so.

## Build & test

- `make lint` — golangci-lint. **Fix all linter categories in one pass** (wsl,
  gci, cyclop, funlen, goconst, nolintlint together); never iterate one linter
  per run. `//nolint` is a last resort with a written reason — never the
  default response. Keep directives accurate or nolintlint flags them (4
  recurring cycles observed on one session).
- `make test` — full suite. Run targeted package tests after each edit; run
  the full gate once at wave/session end. `(cached)` results are valid — do
  not re-run to disprove them.
- `make samples` — regenerates golden/sample PDFs. Regenerate only when the
  input changed; the 169-thumbnail corpus was regenerated 4× in 3 minutes with
  no input change.
- Release flow: `skills/release-note/SKILL.md` — version sweeps are
  repo-wide (VERSION, cli.Version, CHANGELOG, docs, frontend JSON, README in
  one pass) and verified by `TestCLIVersionMatchesVERSIONFile`. A 0.2.0
  leftover survived a full 0.2.1 release before the user caught it.
- Grep/read before writing: verify symbols exist (`go build`), verify file
  paths exist (glob before read — `svg.go`, `float_test.go`, `SPEC-NOTES.md`
  were all guessed paths that didn't exist).

## Verification rules

- Validators are cross-checked: an in-repo script reported PASS while veraPDF
  CLI reported FAIL — the same PDF/UA-1 bug survived 6 rebuild cycles because
  only one validator was trusted. Wire both and compare outputs.
- A completion claim requires the final gate output: read the exit code and
  the validator report. "Resolved and verified with veraPDF" was claimed while
  the last run exited 1.
- Rebuild before validating (stale binaries and outputs have produced false
  results).
- Whole-file re-reading is avoided: view targeted ranges/diffs, not 2300-line
  files in 15 chunks. Line-by-line claims are backed by actual coverage, not
  "lines 1 to 800" partial reads.

## Subagent usage (agy-specific)

- Max 3-5 subagents per wave; each owns disjoint packages, verified in the
  brief. Parallel agents editing the same packages broke each other's builds
  (`writingModeVerticalRL` duplicate, `isUA1` undefined) and locked the
  golangci-lint runner ("parallel golangci-lint is running").
- Every subagent prompt contains: "Do NOT run any git commands."
- Read-only subagents write their findings file early — two audit/validation
  agents died on API overload with no deliverable, costing a parent redo.
- After a crash/429, check whether the task landed before re-issuing it (the
  Sealed Request API task was re-spawned across 5+ sessions).
- The parent verifies the merged tree (build + tests) after any wave.
- Polling loops are avoided: subagent completion is reported; do not emit 14
  status-poll messages.

## Skills (this folder)

- `avoidable-work/` — checkpointing, no-failed-command-retries, dead-session
  detection, deliverable-before-context-end, targeted verification.
- `repetitive-tasks/` — break lint whack-a-mole, duplicate artifacts,
  re-issued dead tasks, validator mistrust loops, audit-wave duplication.
- `mistakes/` — ledger of 10 agy mistakes with prevention rules (API guessing,
  self-inflicted compile breaks, nolint suppression, unverified completion
  claims, version-sweep misses, self-deleted work, out-of-scope edits).
- Cross-tool project conventions live in `skills/grok/AGENTS.md` (git ban,
  em dashes, commit cadence, dependency policy) and opencode-specific rules in
  `skills/opencode/AGENTS.md`.

Skill files load from this folder by name; verify the path before loading
(wrong paths have burned multiple loads).

## Dependency policy

The project is stdlib-only (pure Go, no third-party libraries) by design. Any
dependency addition is a project-policy change: amend the plan/README first and
get explicit user sign-off. No silent "exceptions".
