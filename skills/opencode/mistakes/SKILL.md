---
name: mistakes
description: Ledger of known mistakes in opencode sessions on gowkhtmltopdf and the rules that prevent them — semantic-breaking lint renames, parallel agents on a shared tree, force-reverted fixes, brittle-test-driven code changes, release/module-path errors, blind-fix regressions, silent coverage gaps, and broken brief commands. Use before launching parallel work, running lint fixes, or planning a release.
---

# Mistakes (opencode)

Evidence from an opencode session-DB scan (263 sessions, Aug 3–15). Each mistake below carries the rule that prevents it.

## 1. Mass lint-fixing broke program semantics

A `\bx\b` regex rename over-replaced struct fields; a lint agent changed a return statement but not the signature (layout.go:874) breaking compilation for the whole wave; lint fixes silently changed mm→pt math, case-sensitivity (TestRenderImageDataURI, custom.ttf), and dropped flex/grid display; 5+ regression-fix commits followed in 48h.

**Rule:** Never auto-rename or bulk-`--fix` code. `gopls rename` only for single, mechanically-safe identifiers, each followed by `go build` + package tests. "No behavior changes" is verified by diff review and a `make test` gate, not assumed. Lint-only changes that alter semantics are reverted.

## 2. Parallel agents on a shared tree with no effective locking

"Disjoint file ownership" did not hold in practice: css.go/api.go/doc.go/line.go got touched by 5–7 agents in one wave; two debug agents ran in the same `internal/layout` package (one deleted the other's scratch test); layout+convert agents froze reading files mid-mutation; 361 golangci-lint runs were wasted on other agents' in-flight edits.

**Rule:** One agent owns one package; agents never edit files a sibling is assigned or has open. No two agents run `golangci-lint` or `--fix` passes on the same tree. If the tree is mid-edit by a sibling, stop and coordinate — do not work around it. Verify ownership in the brief, not just the prompt.

## 3. A real fix was force-reverted, discarding its regression test

Commit e8813e8 (pending-settings CLI fix) was hard-reset and force-pushed away; the artifact-object bug came back and had to be re-fixed from scratch, costing a second full diagnosis-and-fix cycle.

**Rule:** Before a hard reset/force-push, inspect the commit's diff and its tests. Revert only the offending hunks, never the whole commit with its regression test. If the user asks to "revert the last commit", show what will be lost before executing.

## 4. Brittle tests drove production code changes

The "0 pages" bug was a false alarm: tests counted the literal `/Type /Page\n` string, so the agent changed the PDF writer's dict format (space-joined → newline-joined) to satisfy the brittle assertion.

**Rule:** Fix the test, not the production format. Assertions on raw serialized output belong in the test harness with a parser, not string counting. When a test's expectation contradicts correct output, treat the test as the bug.

## 5. Trivial build artifacts shipped broken through 9 phases

`make build` was `go build ./...` (never emitted a binary); `make samples` didn't create `output/` — both reported by the user a day later, after the orchestrator ran ~4 hours of phase work before producing any visible PDF.

**Rule:** Exercise end-to-end artifacts (binary emitted, sample PDF generated, `make samples` → `make test` round-trip) at the start of a build-out, not the end. Never go multiple phases without a runnable artifact.

## 6. Release errors: wrong module path, untested version bump

The go.mod module path (`gowkhtmltopdf` instead of the GitHub path) broke `go install` at release, forcing deletion/recreation of the published v0.2.2 tag and spawning a v0.2.3 with sumdb failures. The version bump was committed without running tests (cli.Version still 0.2.1 while VERSION said 0.2.2).

**Rule:** Use `skills/release-note/SKILL.md` and the release checklist: module path correct, `go install` smoke-tested before tagging, `make test` green after the version bump, all version files changed together. Tags and releases are immutable once published.

## 7. Blind-fix policy caused a 537MB regression

Fix agents were forbidden from `go build`/`go test`/benchmarks; the style-interning fix landed with 170MB of reflect.DeepEqual boxing + 84MB escapes, regressing the baseline 335.8MB → 537MB.

**Rule:** Performance changes are verified by measurement, not static reasoning. At minimum run `go build` + the package tests before landing; benchmark gates run on the merged wave. Static estimates (240K ops predicted vs 174K actual) are hypotheses, not facts.

## 8. Silent coverage gaps

The issue scrape covered 100 of 1,329 issues (7.5%) via `rows[:100]` after the user asked for "all open issues only"; 42 assessment subagent spawns were needed to repair it. No completeness gate existed.

**Rule:** Any fan-out over a dataset gets a scripted completeness check (one verdict per item, counts match the source) run before the wave is declared done. Never trust a wave's self-reported coverage.

## 9. Broken commands propagated into subagent briefs

The lint verification command `golangci-lint run . --disable-all -E …` conflicts with the repo's `enable-all: true`; all 7 subagents independently rediscovered the workaround (and the `--no-config` fallback loses repo settings).

**Rule:** Every command in a subagent brief is executed once by the parent first. A one-line fix in the brief prevents 7× rediscovery and divergent workarounds.

## 10. Subagents violated their own contracts

fix-convert ran `git status` despite "NEVER run git commands"; a health-check reviewer ran `git stash -q` during a read-only review; an agent edited api.go/convert.go/imageout.go outside its scope, forcing a stash dance.

**Rule:** The git ban is in every subagent prompt (see `skills/opencode/avoidable-work/SKILL.md`). Read-only reviews get a working-tree snapshot and never run mutating commands. Scope violations are stopped at review, not after commit.

## 11. Misplaced and mis-scoped artifacts

Skills were moved to `~/.grok/skills/` instead of the repo's `skills/grok/` and had to be re-moved after user pushback; a PR was raised using `skills/PR/PR_TEMPLATE.md` headed "goslop" (a different project); a dossier update reformatted 1,300 rows (47K-line diff) to edit 8 rows; a push to master was rejected by repo rules.

**Rule:** Verify the destination folder and template ownership before writing. Edit JSON with formatting-preserving tools (`python -m json.tool` with the original indent) or targeted edits, never full reformat. Repo rules (branch + PR) are followed as the default, not improvised after rejection.

## 12. Session-hygiene mistakes

12 wave-3 lint subagents died instantly (0 tokens) with no health check; a subagent abandoned its deliverable and went native (built binaries + ~20 harnesses, never wrote its output); the same dark-theme bug was requested twice and fixed by neither; an MCP handshake failure aborted a session at startup.

**Rule:** After spawning a wave, confirm every session has assistant turns (see `skills/opencode/avoidable-work/SKILL.md`). Deliverables are written early and checked to exist. Known-open UI bugs are tracked in the checklist with an owner, not re-requested. Flaky MCP servers are removed or pinned before sessions depend on them.
