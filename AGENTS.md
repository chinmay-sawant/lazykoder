# AGENTS.md - lazyKoder (OpenCode agent harness)

> This file is the conventions ledger for every coding agent working in this
> repo (opencode, grok, gemini, codex, antigravity/agy, claude). Read it at
> session start. It encodes the hard-won lessons from a 490-session audit of
> the sibling gowkhtmltopdf project (170 grok + 263 opencode + 57 agy
> sessions, Aug 2026) — the mistakes, repeat work, and avoidable waste we made
> there, so we do not repeat them here.

## Project

Bubble Tea (charmbracelet) TUI agent harness written in Go. v0.0.1 supports
OpenCode Go only. Project workspace is `.lazykoder/` in the current working
directory. Small repo, small team: **one agent at a time, no subagent waves
until the codebase grows**.
Module: `github.com/chinmay-sawant/lazykoder`. GitHub repo: `https://github.com/chinmay-sawant/lazykoder`. Default branch: `master`.

## Golden rules (from the gowkhtmltopdf audit)

1. **No git commands without explicit permission.** Never run `git add`,
   `git commit`, `git push`, `git restore`, `git clean`, `git reset`, or
   `git stash` unless the user asks. Subagent prompts carry this ban by
   default. (In the audit this had to be re-instructed in 4+ sessions per
   tool and was still violated — it is durable config, not a reminder.)
2. **No em dashes ("—") in any written output, docs, or commit messages.**
   Use plain hyphens or restructure. (Re-taught in every docs session before
   it was codified.)
3. **Commit at session end; never leave a dirty tree.** Uncommitted work was
   the #1 cause of cross-session rework: fixes made on Aug 12 were lost and
   re-diagnosed from scratch on Aug 13. If interrupted, record what landed.
4. **Branch naming:** lowercase `chore/`, `feature/`, or `fix/<short-description>`
   (a typo'd `chore/frontend-udpates` once lived across 3 commits and a PR —
   verify the branch name before the first commit).
5. **PRs** use `skills/PR/PR_TEMPLATE.md`; issues use `skills/PR/ISSUE_TEMPLATE.md`.
   Push to master via branch + PR when branch protection exists; do not
   improvise after a rejected push.
6. **Checklists are live ledgers:** if a phase/plan file exists under `plans/`,
   update its rows `[x]` in the same change that implements them — and only
   when the gate actually passed. Never mark `[x]` from intent (a session once
   marked every item `[x]` while claim-scan still failed).

## Things to AVOID (repeat-work and waste from the audit)

1. **Lint whack-a-mole.** The single most expensive loop in the audit: the
   same "fix make lint" task was executed 4× in ~2 hours (46 subagents, ~3M
   tokens). Rules:
   - Run `golangci-lint`/`go vet` before opening any PR; fix findings in the
     same change.
   - Fix all linter categories in one pass (wsl, gci, cyclop, funlen,
     goconst together) — never one linter per run.
   - `//nolint` is a last resort with a written reason, never the default
     response (a commit literally named "add nolint annotations" shipped).
   - Never auto-rename or bulk-`--fix` code. `gopls rename` only for
     mechanically-safe identifiers, each followed by `go build` + tests.
     (Lint cleanup once introduced 16+ semantic bugs: swapped return orders,
     dropped `display:flex/grid`, W/H swaps, float precision breaks.)
2. **Verifying against stale artifacts.** Rebuild before any verification
   run — compliance validators once ran against an old binary, and committed
   PDFs were trusted as proof of current behavior. In this repo: `make run`
   / `go test ./...` before claiming anything works.
3. **Claiming completion without the gate output.** A task is done only when
   the final exit code says so. One session claimed "resolved and verified"
   while its last validation exited 1; another said PASS when the real
   validator said FAIL. Read the output, do not assume.
4. **Fixes that regress each other.** Layout/spacing fixes repeatedly broke
   other pages, found only when the user re-inspected. For a TUI: after any
   view/layout change, run the app and verify the full screen, not just the
   changed area. Keep golden/screenshot tests if views grow.
5. **Guessing APIs and paths.** Grep the symbol / glob the file before
   writing against it (agents burned cycles on `undefined: NewRequest`,
   `svg.go`, `SPEC-NOTES.md` that never existed). `go build` before writing
   tests.
6. **API guessing → write → compile-fail loops.** Six consecutive compile
   failures on one test file because the API was assumed, not read.
7. **Silent coverage gaps.** If a task spans a dataset (`.lazykoder` fixtures,
   fixtures), add a scripted completeness check (counts match the source)
   before declaring done — one scrape silently covered 7.5% of items.
8. **Scope creep.** Edit only the files in the task. A feature task once
   edited an unrelated infra script; a phase agent edited 6 out-of-scope
   files and broke another phase's lint.
9. **Whole-file re-reading.** View targeted ranges/diffs; do not read a
   2300-line file in 15 chunks. Line-by-line claims require actual coverage.
10. **Duplicate artifacts.** Never create a second folder/file when one
    exists (duplicate `1.7-compliance` folders, `skills.md` + `SKILLS.md`,
    issues.json synced by hand in two places). Update in place.
11. **Repeated full-suite verification after every micro-fix.** Run targeted
    package tests after edits; run the full gate once at session end.
    Cached results are valid — do not re-run to disprove "(cached)".
12. **Throwaway tooling.** Any script used more than once goes into
    `scripts/` with a README (the audit found ~200 inline throwaway scripts;
    a screenshot QA harness was re-invented every session). In this repo:
    any seed-data or fixture generator is a committed script, not an inline
    python one-liner.
13. **User-in-the-loop aesthetic loops.** Image/logo/spacing tweaks consumed
    a 483-message session and 3 fix cycles for one nit. For TUI: verify
    layout by running the app and reading the output yourself before asking
    the user; do not iterate blind.
14. **Dead/empty sessions and dead subagents.** After spawning a wave (when
    the repo grows enough to need one), confirm every session has assistant
    turns; ~35% of lint subagents once made zero edits and 12 died with 0
    tokens. Diagnose before re-spawning: a failed agent re-spawned
    identically burned ~500K tokens with zero results ("going in circles").
    Check cancelled agents for landed work before re-doing (4 "crashed"
    agents had completed 124 files).
15. **Parallel agents on one shared tree.** One agent owns one package;
    agents never edit packages a sibling imports; no two agents run lint on
    the same tree. For a repo this small: do not fan out at all until the
    codebase justifies it.

## Mistakes to never repeat (short ledger)

- Stdlib-only promises broken by a quiet "exception" (a project premise died
  via a one-off go.mod exception pulling ~30 deps) — **any dependency
  addition is a project-policy change: amend the plan and get explicit user
  sign-off.**
- Release errors: wrong module path broke `go install` and forced deleting a
  published tag; version bumps committed without tests. **Version files
  change together and `make test` passes before tagging.**
- Engine approximations presented as real behavior ("it is not supposed to
  match a browser" — admitted only when asked). **Never claim a feature does
  what it does not; document divergences explicitly.**
- Force-reverted a real fix + its regression test ("revert the last commit"),
  and the bug came back. **Inspect what a reset would destroy before doing
  it.**
- Self-deleted work: an agent deleted its own test file and restored it only
  when the user noticed. **Deleting files you or a sibling created is a
  visible, reviewable action.**
- Brittle tests drove production-format changes (a "0 pages" bug was a test
  counting raw serialized output). **Fix the test, not the production code.**
- Version/rename sweeps touched only one file type ("report engine" → "HTML
  engine" needed 2 prompts; a 0.2.0 leftover survived a 0.2.1 release).
  **Sweeps are repo-wide: grep the term across every file type in one pass.**
- Subagents violated explicit contracts (ran `git status`, `git stash -q`
  during read-only reviews). **The git ban is in every prompt; read-only
  means read-only.**
- Validator outputs trusted without cross-checking (in-repo PASS vs veraPDF
  FAIL hid a bug for 6 rebuild cycles). **When two validators disagree, both
  are suspect until explained.**
- Planner/session noise: ~940 filler steps, 14 status-poll messages, 15×
  background-task polling. **Do not poll; wait on the event.**
- Mid-session model switches and quota crashes wiped uncommitted progress.
  **Checkpoint at work units; a crashed session loses everything uncommitted.**

## Bubble Tea specifics (this repo)

- UI lives in `internal/ui/chat` (bubbletea `tea.Model` with `Init/Update/View`).
  Confirm views copy the y/n delete layout: highlight the subject
  name (command path, or sub-agent name), then `y confirm  •  n cancel`.
- Keep `Update` pure and deterministic; side effects belong in `tea.Cmd`.
- Run with `make run` (requires `nodemon`; watches `*.go` and restarts
  `go run main.go` on every change) to verify; the app uses alt screen and
  creates `.lazykoder/` at startup (idempotent - do not make init destructive
  to user data). The model list is cached in `.lazykoder/models.json` (15 min
  TTL, `r` in the model picker or `/refresh` to force a reload) so nodemon
  restarts do not hit the models endpoint every time.
- **Never run the binary headless.** `go run .` / `bin/lk` piped, redirected
  (`</dev/null`), or under `timeout` fails with
  `bubbletea: error opening TTY: open /dev/tty: no such device or address`,
  and a `| head` / `echo "exit: $?"` pipeline masks it as exit 0 (8 silent
  false-success smoke runs on 2026-08-15). It cannot verify the TUI. Verify
  rendering with `go test ./internal/ui/chat` (View output), and let the user
  run `make run` in a real terminal for anything visual.
- When adding a view/component, run `go test ./...` and verify the full
  screen renders (no clipped lines, no unreadable colors) before asking the
  user to look.
- Do not re-invent bubbletea idioms: use `bubbles` (list, table, textinput)
  rather than hand-rolling widgets, and follow charmbracelet conventions.

## Code structure (this repo)

- **File size hard limit:** no Go file may grow past ~2,000 lines. The
  absolute maximum is 2,500 lines and needs a written reason. The largest
  file today is `internal/ui/chat/chat.go` (~1,280 lines) - do not let it
  (or anything else) cross 2,000.
- **Split module-wise, not length-wise:** divide code by responsibility
  (each package, view, store, or tool gets its own focused file), not by
  "cut the file here" chunks. When a file approaches the limit, extract a
  cohesive piece into a new file in the same package (e.g. `views.go`,
  `keys.go`, `messages.go`) rather than appending.
- **Use abstractions properly:** introduce interfaces at real seams
  (storage, provider, runners, renderers), keep constructors small, and
  prefer composition over fat structs. Abstractions exist to make the
  module testable and navigable - one purpose per abstraction, no
  speculative generality.
- Verify with `go build ./...` + tests after any file split; do not
  reorder or rename code during a split (see lint whack-a-mole rule).

## Skills (this folder)

- `skills/grok/`, `skills/opencode/`, `skills/agy/` — the audit-ledgers
  (avoidable-work, repetitive-tasks, mistakes + AGENTS.md per tool) this file
  summarizes; consult them when a situation matches.
- `skills/phase-wise-checklist/` — plan/checklist format for this project.
- `skills/release-note/`, `skills/perf-review/`, `skills/critical-go-review/`,
  `skills/ponytail*` — review and release workflows (apply when the project
  matures).

Skill files load from this folder by name; verify the path before loading.

## Dependency policy

Keep dependencies minimal and justified (bubbletea + bubbles + lipgloss are
the stack). Every new dependency is announced with its purpose; no silent
additions. Prefer stdlib and the existing charmbracelet stack first.

---

## FAQ — does the root AGENTS.md get read automatically?

**Yes.** Creating `AGENTS.md` at the repo root is the standard, and it is
read automatically by every major tool:

- **opencode** — auto-loads `AGENTS.md` (and `.opencode/AGENTS.md` overrides)
  at session start.
- **codex** — auto-loads `AGENTS.md` / `agents.md` at session start.
- **gemini CLI** — reads `AGENTS.md` from the working directory.
- **antigravity / agy** — reads `AGENTS.md` from the project root.
- **grok** — reads `AGENTS.md` / project conventions from the repo root.
- **claude code / cursor / other agents.md-spec tools** — same convention.

You do **not** need to create anything under `.agents/` — that is not a
standard location; the agents.md convention is specifically the root (and
optionally per-directory) `AGENTS.md`. Keep this one at the root, and
everything that opens this repo picks it up automatically.
