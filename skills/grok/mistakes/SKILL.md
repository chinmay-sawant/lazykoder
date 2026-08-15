---
name: mistakes
description: Ledger of known mistakes on gowkhtmltopdf and the rules that prevent them — stdlib-only violations, semantic-breaking lint renames, parallel-agent build breakage, release/version errors, stale-binary verification, and fix-regression cycles. Use before writing engine code, planning a release, or launching parallel work.
---

# Mistakes

Evidence from the session-history scan (170 sessions, Aug 3–15). Each mistake below carries the rule that prevents it.

## 1. Stdlib-only promise broken by one-off "exception"

The project premise (pure Go, no third-party libraries, per every planning session and README) was abandoned for the first hard problem: an SVG logo fix added `tdewolff/canvas` + `go-text/typesetting` (~30 indirect deps) as a go.mod "exception".

**Rule:** Any dependency addition is a project-policy change. It requires amending the canonical plan/README first and explicit user sign-off — never a silent "exception". If a feature fights the constraint, solve it in-engine first (the builtin rasterizer existed before being deleted).

## 2. Lint cleanup introduced 16+ semantic bugs

A golangci-lint "no behavior changes" pass swapped return orders, silently dropped `display:flex/grid`, swapped jpegDims W/H, and broke `LengthToPt` float precision — the repair wave then spawned 3 more subagents. Scripted renames added more damage: "DOUBLE RENAME BUG", `globalCfglobalCfg`, broken rune literals, struct-field keys renamed in composite literals, `idx == idx` shadowing.

**Rule:** Never auto-rename or bulk-`--fix` code. `gopls rename` only for single-identifier, mechanically-safe renames, each followed by `go build` + the package tests. Prefer excluding a noisy linter in `.golangci.yml` over bulk edits. "No behavior changes" is verified by diff review, not assumed.

## 3. Parallel agents on coupled packages broke the build repeatedly

Concurrent sessions editing `settings`/`pdf`/`outline` APIs produced `ColorMode undefined` (×2), `ScanFontDir undefined`, forced `git stash` dances, sleep-poll loops, and a plan-file reference (`vertical.go`) that did not exist on the branch.

**Rule:** One agent owns one package; agents never edit packages a sibling imports. Wave-planning assigns ownership explicitly. No two agents run `golangci-lint --fix` on the same tree. If a sibling's change breaks the build, stop and coordinate — do not work around it.

## 4. Release errors from missing checks

- Wrong `go.mod` module path (`module gowkhtmltopdf` instead of the GitHub path) broke `go install` at release time and forced deleting/recreating the published v0.2.2 tag.
- Version bump committed without running tests (`cli.Version` still 0.2.1 while VERSION said 0.2.2).
- CI version-stamp check failed 3× (awk `$$` escaping) and version was maintained in two places.

**Rule:** Use `skills/release-note/SKILL.md` and the release checklist: module path correct, `go build ./... && make test` green, all version files changed together (VERSION, cli.Version, CHANGELOG, docs, frontend), `go install` smoke-tested before tagging. Tags and releases are immutable once published.

## 5. Verification against stale artifacts

PDF/UA compliance validators ran against an old binary ("The CLI binary wasn't rebuilt — still serving the old code"), and stale golden PDFs/benchmark corpora were used as ground truth.

**Rule:** Before any verification: rebuild (`make build`/`make samples`), confirm binary timestamp, and confirm golden outputs were regenerated from the current generator. Never trust committed PDFs as proof of current engine behavior.

## 6. Fixes regressed each other with no visual gate

Spacing fixes caused full-document text overlap ("throughout the whole freaking application there is too much overlaps text now"); underline fixes broke page 14; border fixes broke page 3. Regressions were found only by the user re-inspecting.

**Rule:** Every visual fix is verified with a screenshot-diff/regression check (full-document, not just the fixed fixture) before commit. If a fix touches layout metrics, re-render the whole fixture corpus and eyeball diffs. No fix ships "locally green" without the corpus pass.

## 7. Engine approximations presented as real CSS

The "subgrid lite" copy-inherit behavior was engine-invented, and the agent admitted "it is not supposed to match a browser" — only after the user asked whether it was real CSS.

**Rule:** Never claim a feature matches a CSS spec the engine does not implement. Docs and answers distinguish "engine behavior" from "CSS-standard behavior". When the engine diverges from browsers, say so explicitly and mark it in the fidelity docs.

## 8. Engine-vs-template fix policy was absent

Fixture-31 went CSS fix → engine fix → engine revert → template CSS, with the skill updated only after the churn.

**Rule:** For look-only layout issues, prefer template/author CSS first. Engine fixes are for structural bugs (pagination, borders, overflow), and each engine fix states its blast radius. Decide before editing, and record the decision in the checklist.

## 9. Planning/scope mistakes

- Committed another application's plan files into this repo, then deleted and rewrote them.
- Ticket #35 was created, fully rewritten (subprocess→cgo), then updated a 3rd time — intent needed 2–3 explanations.
- Scope creep: a Makefile/generate.go session edited unrelated `convert.go`.

**Rule:** Verify plan files belong to this repo before committing. Ask one clarifying question when a ticket's approach contradicts the user's stated intent. Touching files outside the task's ownership requires explicit justification.

## 10. Session-hygiene mistakes

- 5 failed skill loads from wrong paths in one session.
- Subagent violated an explicit no-git instruction.
- Empty sessions created (0 messages) before subagent storms.

**Rule:** Skill paths are relative to the repo `skills/` root — verify with a read before loading. The git ban is in every subagent prompt (see `skills/avoidable-work/SKILL.md`). Do not create a session without intent.
