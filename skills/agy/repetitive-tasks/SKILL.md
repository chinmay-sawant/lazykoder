---
name: repetitive-tasks
description: Eliminate recurring work in Antigravity CLI (agy) sessions on gowkhtmltopdf — lint whack-a-mole, nolint churn, sandbox retry loops, re-issued dead tasks, duplicate file creation after truncation, redundant audit waves, and re-verified unchanged work. Use when a task looks like something already done in a prior session; update the skill or checklist instead of repeating the loop.
---

# Repetitive Tasks (agy)

Evidence from an Antigravity CLI transcript scan (57 gowkhtmltopdf sessions, Aug 9–15):

- Lint whack-a-mole in nearly every implementation session: golangci-lint run 5–8× per session (ef49c299 ran it 7×, f2007cd6 5× + `--fix`, 769405e4 ~17 test/lint runs, b2930e5f 7×); the same `nolintlint "unused directive"` mistake recurred in 4 separate failing cycles of one session; funlen fixes shifted statements into other functions causing iterated fails (41>40 → 62>60).
- Test files rewritten repeatedly to satisfy the linter: policy_test.go written 3× chasing lint, font_test.go refactored twice, structure.go got a `nolint:cyclop` added.
- The same full verification chain re-run dozens of times; the same scoped golangci-lint command executed 3× consecutively, all failing, re-surfacing the same ~20 errors one linter at a time (wsl → gci → cyclop → funlen → goconst → nolintlint).
- Dead tasks re-issued: the Sealed Request API task string appears in 5+ transcripts; Phase 3 (6a638def) and Phase 4 (26f6612f) and Phase 26 (8682248e) and Phase 25 (fdd76f15) sessions produced zero work and were redone elsewhere.
- Files created twice after truncation: policy.go/icc.go/outputintent.go/metadata.go at 21:11 then again at 21:14; critical-golang-architecture-review.md ×2; phase-wise-implementation-checklist.md ×2; request.go back-to-back.
- Duplicate output folders with identical content: `output/pdf-1.7-compliance` and `output/1.7-compliance` ("Why are we having two of the PDF compliance folders, brother?"); skills.md + SKILLS.md both created.
- The same PDF/UA-1 veraPDF failure re-pasted by the user 3–4× with no change in outcome, after 6 full build+convert+validate cycles; the in-repo script reported PASS while veraPDF CLI failed, masking the bug until a second validator (avalpdf) was demanded.
- Full-codebase audit waves re-run over the same files the same day: 4-subagent review waves at 16:38/17:03/18:50/19:00, each re-reading api.go/convert.go/layout.go/load.go/pdf.go in full; validation subagents fully re-read the 5 files the discovery agents had just reported on.
- Same "no git commands / no commit" instruction repeated in every session prompt (6d820e84 ×4, b0d73cdc, b2930e5f, 1c5d7f3f) and still violated by subagents running `git status`.
- Whole-file re-views repeated: pdf.go (1000+ lines) viewed in 3 chunks then ~15 more times, including immediately-consecutive identical-range views (99/101, 105/107); global.css re-read in ~15 chunked views.
- Redundant regenerate→validate cycles: f2007cd6 ran the full regenerate→validate cycle 3×, with the last cycle re-running after a lint marathon and yielding the same still-failing report.

## Rules

1. **Lint-aware writing, not lint-after-writing.** Fresh code that needs 5–8 golangci-lint iterations is a sign the linters were not followed while writing (varnamelen, cyclop, line-length). Match the repo's existing style before writing; one `golangci-lint run ./...` pass per feature, not per linter. Never use `//nolint` to silence something you can fix.
2. **Fix all linter categories in one pass.** When a run surfaces wsl → gci → cyclop → funlen → goconst, fix the whole set together instead of iterating one category at a time. Prefer `.golangci.yml` config decisions over per-file suppressions.
3. **Check the transcript before re-issuing a task.** A task that produced zero work in a prior session is either already landed elsewhere (Sealed Request API in 5 transcripts) or needs a different approach — re-issuing the identical task string is the most common re-do in this tool's history.
4. **One canonical artifact per output.** Never create duplicate folders/files (two compliance folders, skills.md + SKILLS.md, report written twice). If an artifact exists, update it; if naming is ambiguous, ask.
5. **Validators are verified against each other.** When a local script says PASS but a validator (veraPDF CLI) says FAIL, trust the discrepancy and escalate — the same UA-1 bug survived 6 rebuild cycles because only one validator was trusted. Wire both validators and compare outputs.
6. **Audit waves diff against prior findings.** A validation subagent re-reading all 5 core files that discovery agents already reported on is a 3rd full pass over identical content. Pass the findings doc, not the file paths.
7. **Repeated instructions become config.** "No git commands" repeated 4× per session across sessions belongs in AGENTS.md (see `skills/agy/AGENTS.md`) — the user should never have to say it twice in one session.
8. **Regenerate only on actual change.** The 169-thumbnail corpus regenerated 4× in 3 minutes and the regenerate→validate cycle run 3× with unchanged output are pure churn; verify the input changed before regenerating.

## Completion Handoff

- [ ] One lint pass per feature, all categories fixed together; no new `//nolint` suppressions without a reason.
- [ ] No duplicate artifacts created; existing files updated in place.
- [ ] Validator outputs cross-checked (two validators when compliance is claimed).
- [ ] Any repeated instruction this session added to `skills/agy/AGENTS.md`.
- [ ] No regeneration without a changed input.
