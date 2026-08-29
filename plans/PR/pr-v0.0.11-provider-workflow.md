## Summary

This branch completes the v0.0.11 multi-provider agent workflow. It adds provider-aware model routing, orchestration, durable recap and memory context, pluggable providers, tools, and roles, and a Diff drawer for reviewing changes before the policy-gated commit action.

The final closure pass fixes the memory prompt regression test, clears the lint findings, bounds test parallelism, and proves the Diff drawer lifecycle in a real terminal.

## Motivation / context

- Plans: `plans/v0.0.11/README.md` and `plans/v0.0.11/phase-4b-commit-push-drawer.md`
- Issues: no open issue is associated with this branch

## Changes

### Provider and agent workflow

- Add OpenAI, Codex, and Grok provider routing alongside OpenCode.
- Add structured parent planning, role-based child assignment, summary review, and bounded retry behavior.
- Add recap selection, durable memory updates, memory-first recall, and timing improvements.
- Make providers, tools, and roles discoverable through bounded project-local and global catalogs with diagnostics and policy checks.

### TUI and release closure

- Add the Diff drawer with real file summaries, unified diff previews, hunk navigation, scrolling, mouse support, and the policy-gated `commit and push` action.
- Reset the drawer lifetime after interaction and cancel stale expiry after collapse or activation.
- Update the memory regression test, lint fixes, Makefile test parallelism, plans, documentation, and knowledge-base pages.

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Test execution uses two package workers and two test workers. Memory prompt construction keeps a bounded recent-context payload. |
| **Memory** | Durable memory remains bounded and source-backed. The production memory implementation is unchanged by the regression-test fix. |
| **Behavior / correctness** | Model routing, child orchestration, catalog loading, Diff drawer navigation, and expiry behavior have focused coverage. |
| **API / CLI** | Adds provider, tool, role, `/memory`, `/skills`, `/tools`, `/roles`, and `/diff` workflows. Existing OpenCode defaults remain unchanged. |
| **Dependencies** | No new dependency. |
| **Binary size / build time** | `make build` passes. |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| None | Existing settings and OpenCode defaults remain supported. |

## Test plan

- [x] `make test`
- [x] `make lint`
- [x] `make vet`
- [x] `make build`
- [x] `CGO_ENABLED=0 go build -o bin/lk .`
- [x] Real TTY checks at 120x36 and 80x24
- [x] Diff drawer expiry check after 95 seconds without input

### Commands

```sh
make test
make lint
make vet
make build
CGO_ENABLED=0 go build -o bin/lk .
```

## Screenshots / sample output

```text
120x36: /diff opened the changed-file list. Enter opened the selected diff.
Escape returned to the separated change list.
80x24: startup rendered without clipping.
95 seconds without input: the Diff drawer disappeared.
```

## Related issues

- No related issue is open for this branch.
- The v0.0.11 plan is the tracking record: `plans/v0.0.11/README.md`.

## PR metadata checklist (author)

- [x] Self-assigned with `--assignee @me`
- [x] Labels applied with `enhancement` and `documentation`
- [x] Related issues checked. No issue exists for this branch.
- [x] Filled body stored under `plans/PR/pr-v0.0.11-provider-workflow.md`

## Follow-ups (out of scope)

- Finish the open v0.0.10 browser-backed `webfetch` acceptance and safety gates.
- Run the live provider-key commit and push scenario with a human-controlled repository action.
- Revisit the model catalog plugin follow-on audit after the user-facing browser work.

## Reviewer checklist

- [ ] Behavior matches the summary and test plan
- [ ] No unrelated changes in the branch diff
- [ ] Public API and CLI changes are documented
- [ ] New behavior has focused test coverage
- [ ] PR has assignee and labels
- [ ] Related issue references are accurate
- [ ] No secrets or generated artifacts are committed
