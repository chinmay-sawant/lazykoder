## Summary

This branch completes the v0.0.10 local memory workflow and adds optional browser-backed URL reading. It also adds current-chat memory history with status details, error causes, and copy support.

## Motivation / context

- Plan: `plans/v0.0.10/README.md`
- Browser plan: `plans/v0.0.10/phase-11-browser-support.md`
- Issues: none linked to this branch

## Changes

### Local memory and recall

- Add bounded recap artifacts, durable `knowledge-base/memories.md` updates, strict source-message validation, first-request recall, and restart-safe memory update records.
- Add `/history` for current-chat memory entries, newest-first pagination, update status dots, failure causes, parser errors, and detail panels.
- Add mouse drag selection and `ctrl+a` / `ctrl+c` support in memory details.

### Browser-backed URL reading

- Extend `webfetch` with bounded HTML extraction, link and `mailto:` metadata, and optional browser mode.
- Add fixed Chrome and Chromium discovery, isolated browser process handling, cancellation, and public-destination checks.
- Keep page content and extracted email data untrusted. The tool never sends email or follows extracted links automatically.

### Skills, settings, and terminal UI

- Add bounded local and global skill discovery through `/skills`.
- Add persisted theme settings, semantic transcript surfaces, retry settings, and the requested assistant metadata background fix.
- Keep tests and documentation aligned with the current memory, browser, skills, and transcript behavior.

## Impact

| Area | Impact |
|------|--------|
| **Performance** | Recap, memory, and browser work stays bounded by existing request, output, timeout, and process limits. |
| **Memory** | Durable memory remains capped and source-backed. Browser requests use temporary isolated profiles. |
| **Behavior / correctness** | Memory history shows completed and failed updates with their original causes. Assistant metadata rows keep one continuous panel background. |
| **API / CLI** | Adds `/skills`, `/history`, and optional `webfetch` browser modes. Existing HTTP calls remain supported. |
| **Dependencies** | No new third-party dependency was added. |
| **Binary size / build time** | No committed binary or generated build artifact is included. |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| None | No migration required. |

## Test plan

- [x] `go test . ./internal/...`
- [x] `go vet ./internal/...`
- [x] `go build ./...`
- [x] `git diff --check`
- [x] Focused browser, memory-history, transcript, and assistant-panel tests
- [x] `go test ./...`

### Commands

```sh
go test . ./internal/...
go vet ./internal/...
go build ./...
git diff --check
```

## Screenshots / sample output

No screenshot artifact is included. Terminal rendering tests cover assistant panel backgrounds, metadata spacing, memory detail panels, and copy interaction.

## Related issues

- None linked to this branch.

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [x] Related issues checked; no issue ID is associated with this branch
- [x] Filled body committed under `plans/PR/pr-v0.0.10-memory-browser.md`

## Follow-ups (out of scope)

- Complete the remaining live Chrome fixture and full terminal validation gates.
- Finish post-redirect browser final-URL capture through a deeper browser protocol seam.

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public API and CLI changes documented
- [ ] New rules have fixture coverage when applicable
- [ ] PR has assignee and labels
- [ ] Related issues use correct keywords
- [ ] No secrets or generated artifacts committed
