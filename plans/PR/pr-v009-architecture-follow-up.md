## Summary

This PR closes the active findings from the 2026-08-21 architecture review.
It hardens outbound fetches, makes subagent state durable and invocation-local,
and gives shared policies one owner. It also moves the already-written UI style
plans under the active v0.0.9 plan directory without changing their text.

## Motivation / context

- Plans: `plans/v0.0.9/architecture-review-21082026-2121/` and
  `plans/v0.0.9/style-improves/`
- Review report: `plans/v0.0.9/architecture-review-21082026-2121/architecture-review.html`
- Issues: none filed for this review track

## Changes

### Egress and tool contracts

- `webfetch` validates public addresses before a request and again at dial
  time. It rejects private redirects and DNS rebinding attempts.
- The tool shallow-copies a supplied HTTP client, preserving the caller's
  redirect callback while installing a validated direct transport.
- Task tools now encode their declared result types for spawn, list, status,
  wait, and cancel operations.

### Subagent runtime and persistence

- Parent session selection stays local to each Host invocation, so concurrent
  task calls do not share mutable state.
- The manager stores a queued job before starting its runner and reports later
  persistence or recovery write failures through the TUI.
- Child model selection carries its endpoint, compatible variant, and context
  window together. Child auto-compaction uses a known window only.

### Shared policy and plans

- Settings loading, role capabilities, workspace containment, step-limit
  classification, and OpenCode routing now have one owning implementation.
- The architecture checklist, HTML report, and reference docs record the
  completed remediation.
- Five existing style-plan documents move under `plans/v0.0.9/style-improves/`
  with no content changes.

## Impact

| Area | Impact |
|------|--------|
| **Performance** | No measured performance change. |
| **Memory** | No material expected change. |
| **Behavior / correctness** | Blocks private-network webfetches at preflight and dial time. Prevents jobs from starting before their queued state is durable. |
| **API / CLI** | No CLI change. Task JSON now follows the declared result structs and `task_cancel` exposes `cancelled_count` for cancel-all calls. |
| **Dependencies** | None. |
| **Binary size / build time** | No dependency change. Not separately measured. |

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| Task result JSON consumers | Decode the documented `internal/tools/task` result type for each operation instead of an ad hoc manager snapshot. |
| User-facing CLI | None. |

## Test plan

- [x] `make lint`
- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `go test -race ./internal/tools/webfetch ./internal/agent ./internal/subagent -count=1`
- [x] Real-terminal subagent drawer and composer check at 120x36 and 80x24

### Commands

```sh
make lint
go test ./...
go vet ./...
go test -race ./internal/tools/webfetch ./internal/agent ./internal/subagent -count=1
```

## Screenshots / sample output

```text
make lint: exit 0
go test ./...: exit 0
go vet ./...: exit 0
focused race suites: exit 0
```

## Related issues

- None filed for this review track.

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [x] Related issues filled (none filed)
- [x] Filled body committed under `plans/PR/pr-v009-architecture-follow-up.md`

## Follow-ups (out of scope)

- Reconsider a provider interface only when a second incompatible provider is
  ready to ship.
- Reconsider nested subagents only with a recovery contract for their child
  sessions.

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public API and CLI changes documented
- [ ] New behavior has regression coverage
- [ ] PR has assignee and labels
- [ ] Related issue section is accurate
- [ ] No secrets or generated artifacts committed
