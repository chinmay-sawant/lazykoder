# AGENTS.md — gowkhtmltopdf project conventions

> This file is the shared conventions ledger for every coding agent working on
> this repo (opencode, grok, gemini, codex, antigravity, agy). Read it at
> session start. If a rule was ever re-explained to you by the user, it belongs
> here — add it instead of just following it.

## Project

Pure-Go reimplementation of wkhtmltopdf (HTML → PDF). Default branch: `master`.

## Golden rules

1. **No git commands without explicit permission.** Never run `git add`, `git
   commit`, `git push`, `git restore`, `git clean`, `git reset`, `git stash`,
   or `git checkout` unless the user asks. Subagent prompts carry this ban by
   default.
2. **No em dashes ("—") in any written output, docs, or commit messages.** Use
   plain hyphens or restructure. This rule is applied consistently to
   CHANGELOG, README, and release notes.
3. **Commit cadence:** commit at the end of each session; never leave a dirty
   tree overnight. When regenerating PDFs alongside code, commit Go code only
   unless the user explicitly asks for the PDFs too (see "Generated outputs").
4. **Branch naming:** lowercase `chore/`, `feature/`, or `fix/<short-description>`
   (e.g. `feature/29-pdf-1.7-and-2.0-support`).
5. **PRs** use `skills/PR/PR_TEMPLATE.md`; issues use `skills/PR/ISSUE_TEMPLATE.md`.
6. **Checklists are live ledgers:** update `plans/` phase rows `[x]` in the same
   change that implements them. No post-hoc "reconcile" passes (see
   `skills/phase-wise-checklist/SKILLS.md`).

## Build & test

- `make lint` — golangci-lint (must be green before any PR; run with `make lint --fix` only for safe, reviewed fixes).
- `make test` — full test suite (includes golden corpus; do not skip).
- `make samples` — regenerates golden/sample PDFs. `make samples` and `make test` must never fight over the same files; if they do, it is a bug to fix, not a ritual to repeat.
- Release flow: `skills/release-note/SKILL.md` — VERSION, `internal/cli/help.go`, CHANGELOG, docs, and frontend version strings change together; `TestCLIVersionMatchesVERSIONFile` must pass before commit; tags are immutable once published.

## Verification rules

- Rebuild the binary before any verification run (stale binaries have produced false compliance results).
- Read PNG/PDF screenshots directly (agents are multimodal) instead of pixel-counting scripts.
- Keep local validators (`avalpdf`, `veraPDF`) wired when available; do not rely on hand-pasting web validator output.
- Any visual fix is verified on the full fixture corpus, not just the fixed fixture (layout fixes have regressed other pages repeatedly).

## Subagent usage

- Max 3–5 subagents per wave; each owns disjoint packages. Never assign two agents the same package.
- Every subagent prompt contains: "Do NOT run any git commands."
- Never assign a subagent a bug the parent is fixing concurrently.
- The main agent verifies the merged tree (build + tests) after any wave.
- Shared analysis artifacts go under `reports/`; downstream agents read them instead of re-scanning the codebase.

## Skills (this folder)

- `avoidable-work/` — prevent throwaway tooling, dirty-tree carryover, subagent over-escalation.
- `repetitive-tasks/` — break loops (PDF fidelity, lint waves, golden regen, benchmarks); build the durable artifact instead of repeating.
- `mistakes/` — ledger of past mistakes and the rules that prevent them (read before engine work or releases).
- `phase-wise-checklist/` — canonical plan/ledger format.
- `release-note/` — release process.
- `debug-html-template/`, `perf-review/`, `critical-go-review/`, `ponytail*` — review and debugging workflows.

Skill files load from this folder by name; verify the path before loading (wrong paths have burned 5+ loads).

## Dependency policy

The project is stdlib-only (pure Go, no third-party libraries) by design. Any
dependency addition is a project-policy change: amend the plan/README first and
get explicit user sign-off. No silent "exceptions".
