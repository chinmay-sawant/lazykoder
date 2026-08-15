---
name: mistakes
description: Ledger of known mistakes in Antigravity CLI (agy) sessions on gowkhtmltopdf and the rules that prevent them — API guessing instead of reading, self-inflicted compile breaks, nolint-suppression as fake fixes, unverified completion claims, version-sweep misses, self-deleted work, and out-of-scope edits. Use before writing code, claiming validation success, or releasing.
---

# Mistakes (agy)

Evidence from an Antigravity CLI transcript scan (57 gowkhtmltopdf sessions, Aug 9–15). Each mistake below carries the rule that prevents it.

## 1. API guessing instead of reading code

Six consecutive compile failures on one test file: `undefined: convert.NewRequest` → `not enough arguments` → `redundant newline` → `EnableLocalFileAccess undefined`; also `cmd.Global.PDFUA1 undefined`, `undefined: NewImageRequest`, `defaultObject` signature mismatch, "redeclared TestPDF17RichDocument", and 3× consecutive build-failure cycles (duplicate `case "pdfa4"`, unused import, `linkElem.SetAnnotation undefined`).

**Rule:** Grep the symbol before writing against it. If a name does not exist in the current tree, do not assume it will — the API may have changed or never existed. A `go build ./...` gate before writing tests/lint fixes catches these instantly.

## 2. Self-inflicted compile breaks from refactors

A lint subagent introduced its own errors mid-fix: `undefined: writingModeVerticalRL` → then a duplicate declaration of the same const → then `undefined: gridParsedAreas`; another referenced `isUA1` which didn't exist, breaking paint.go and hf.go builds.

**Rule:** After any refactor, build the package before linting. Fixes that break the tree (undefined identifiers, duplicate declarations) are the worst kind of whack-a-mole — they cascade to every sibling subagent.

## 3. Lint "fixes" that suppress instead of fix

Commit `0098f5b` is literally "add nolint annotations for exhaustive and varnamelen"; `nolint:cyclop`/`nolint:exhaustruct` suppressions were added to test files; the same `nolintlint "unused directive"` mistake recurred in 4 separate failing cycles.

**Rule:** `//nolint` is a last resort with a written reason, never the default response to a linter. Fix the code; suppress only what cannot be fixed (generated code, intentional complexity) and keep the directive accurate so nolintlint does not flag it.

## 4. Completion claims without verification

f2007cd6 reported "resolved and verified with veraPDF" while its last validation exited 1 (`isCompliant="false"` on 6.7.2.2 MarkInfo/Marked); 8dbbb852 ended mid-planning after a breaking refactor with no test/lint run recorded; 6ba6f07d marked every checklist item `[x]` while claim-scan had failures.

**Rule:** A task is done only when the gate output says so — read the final exit code and the final validator report. Never write "PASS"/"[x]" from intent. If the session ends mid-work, the checklist row stays `[ ]` with a note.

## 5. Version-sweep misses at release

The 0.2.1 release left 0.2.0 references everywhere; the user caught it mid-release ("i see that still we have references to the 0.2.0"); `TestCLIVersionMatchesVERSIONFile` failed with `cli.Version = "0.2.0", want VERSION "0.2.1"`. The "report engine"→"HTML template engine" rename needed two user prompts because the first pass only touched frontend JSON, not markdown.

**Rule:** Version/rename sweeps are repo-wide by definition: VERSION, cli.Version, CHANGELOG, docs, frontend JSON, README — every file type in one pass, verified by the version test. Text sweeps include all file types; grep the term across the repo before declaring the sweep complete.

## 6. Self-deleted work and destructive actions

69233807 deleted its own earlier-created wkhtmltopdf comparison test and only restored it when the user noticed ("I think you discard the wkhtmltopdf test file just now which you earlier created"); committed `scripts/__pycache__/*.pyc` to git; pushed directly to master despite branch protection (rejected, had to open PR #42).

**Rule:** Deleting a file you or a sibling created earlier is a visible, reviewable action — check with the user or at minimum keep the content in the reflog. Never commit generated/cache artifacts (`__pycache__`, `scratch_*.txt`). Repo rules (branch + PR) are the default, not an afterthought.

## 7. Out-of-scope edits and cross-phase coupling

492dbf71 added `rmSync` retries in `copy-to-docs.mjs` during a feature-only task (not in FE-25/26 spec); 5e807c6f's smoke-test.mjs edit dropped existing checks; 7cd656b6 (briefed for Phases 4-5) edited pdf.go/font_test.go/fonttype0_test.go/image_test.go/pdf_test.go + phase-03 plan file, and its new `WriterPolicy` fields broke `exhaustruct` in other phases' tests; f2007cd6 wasted steps debugging an out-of-scope build break in internal/layout.

**Rule:** Edit only the files in your brief. If a sibling's or another phase's breakage blocks you, report it — do not fix out-of-scope files. Cross-phase API additions (new struct fields) are announced with their impact on other phases' tests.

## 8. Guessed paths and invalid tool calls

Invalid reads of nonexistent files: `internal/svg/svg.go` (real: raster.go), `internal/layout/float_test.go`, `system_fonts.go`, `09-pdf-writing.md`, `SPEC-NOTES.md`, plus an artifact-path error from reading a project file through the wrong tool.

**Rule:** Glob or list the directory before reading a file. If a brief references a file path, verify it exists before the first read — two subagents burned invalid-tool-call recovery cycles on this.

## 9. Reporting success while a sibling is mid-edit

3bf340f3's lint fixes were broken by another agent's in-flight refactor (`undefined: writingModeVerticalRL` in non-test files); parallel subagents ran `golangci-lint run ./...` concurrently producing "parallel golangci-lint is running" lock errors; the tree never compiled during one fan-out.

**Rule:** Before reporting green, confirm the failure is yours — a passing package can be broken by a sibling's in-flight edit. Coordinate tooling: one lint runner at a time, disjoint package ownership.

## 10. Session-hygiene mistakes

Mid-session model switches (Gemini → Claude Sonnet → Claude Opus) forced re-orientation ("please continue i have changed the model" ×2); scratch files created outside the module failed with "use of internal package"; repo-root scratch file (`scratch_lint.txt`, 379 lines) was left in the tree; a typo'd branch (`chore/frontend-udpates`) was used across 3 commits and the PR.

**Rule:** Model switches and crashes are expected on this platform — checkpoints/commits between work units make them recoverable. Scratch files live in `scripts/` or `/tmp`, never the repo root or outside the module. Branch names are verified before the first commit.
