# v0.0.9 - architecture review follow-up checklist

> **Parent:** `plans/v0.0.9/README.md`
> **Status:** implementation complete and validated
> **Estimated effort:** 7-11 focused working days
> **Report:** `architecture-review.html`
> **Frozen revision:** `7e29f271da5c56aa4d3be0e27d30ea4f666b034a`
> **Review date:** 2026-08-21

---

## Overview

This ledger began as a review of the OpenCode Go Bubble Tea harness using the
`/improve-codebase` architecture, extension, and Go-practice lenses. The
follow-up implements every active row below. The three review passes stayed
read-only. This follow-up adds focused regressions and current validation.

The frozen worktree had only unrelated plan changes outside this review:
deleted old plan files and an untracked `plans/v0.0.9/style-improves/`
directory. This ledger does not absorb them.

The 2026-08-20 architecture track under
`plans/v0.0.9/architecture-review/` is a completed historical ledger. This
review does not reopen its C1-C9 rows without current regression evidence.

## Executive summary

`webfetch` now resolves an address again at dial time and connects to that
approved IP. Its cloned per-request client does not change the caller's
redirect callback.

Subagent task calls now keep parent state per invocation. Jobs use declared
task result shapes, a selected child-model profile, copied capability slices,
and durable state errors that reach the manager and TUI.

| Review input | Count |
| --- | ---: |
| Candidate findings before synthesis | 15 |
| Active checklist rows | 12 |
| P0 rows | 1 |
| P1 rows | 6 |
| P2 rows | 5 |
| Refused or filtered items | 4 |

`EXT-04` and `PRAC-02` merge into P0 `EXT-03`: one per-request egress
client must both preserve caller ownership and validate the address it dials.
The former session-usage suggestion is filtered because it would reopen the
completed SessionGraph work without proof of a broken current contract.

## Phase 0: egress integrity

### 0.1 Make webfetch validate the address it connects to

- [x] `EXT-03` - In `internal/tools/webfetch`, construct a per-call HTTP
      client from a shallow copy of the injected client. Its transport must
      resolve and reject private or loopback IPs immediately before every
      dial, including redirects. Keep TLS hostname verification intact.
      This absorbs the duplicate caller-client mutation finding `EXT-04` /
      `PRAC-02`. Proof: a resolver or dial test changes a hostname from a
      public result at preflight to loopback at dial time and proves no
      connection happens. A separate test proves the injected client's
      `CheckRedirect` remains unchanged. Affected path:
      `internal/tools/webfetch/webfetch.go:21-78`.

### 0.2 Phase 0 proof

- [x] Add focused allow, deny, redirect, rebinding, and caller-client
      ownership tests in `internal/tools/webfetch`.
- [x] Run `go test -race ./internal/tools/webfetch -count=1` after 0.1. Passed on 2026-08-21.

## Phase 1: subagent and runtime contracts

### 1.1 Keep task invocation state local

- [x] `PRAC-01` - Stop `internal/subagent.Host.Execute` writing the caller's
      parent session ID into `Host.ParentSessionID`. Resolve the parent ID for
      one invocation and pass it to task, list, wait, and cancel handlers.
      Parallel task calls must never share mutable parent state. Proof: a
      parallel Host test with two parent IDs creates jobs under their own
      parents and passes `go test -race ./internal/agent ./internal/subagent`.
      Affected path: `internal/subagent/host.go:39-129`.

### 1.2 Make task response types authoritative

- [x] `EXT-02` - Map Host snapshots and results to the declared
      `internal/tools/task` response types and use that package's encoders for
      spawn, list, status, wait, and cancel. Preserve a stable decoded shape
      for one-task and all-task wait responses. Proof: Host integration tests
      decode every successful operation into its declared task result type.
      Affected paths: `internal/subagent/host.go:66-129` and
      `internal/tools/task/task.go:78-109,333-355`.

### 1.3 Do not start jobs that cannot be durable

- [x] `PRAC-04` - Make initial subagent-job persistence return errors. Refuse
      to launch a durable job until its queued row is stored, and surface
      later persistence or recovery-write failures through the manager and
      chat. Proof: a failed store write prevents the runner from starting;
      recovery-write failure is visible to the caller. Affected paths:
      `internal/subagent/manager.go:196,755-792` and
      `internal/ui/chat/runtime.go:45`.

### 1.4 Resolve child model overrides as one profile

- [x] `EXT-01` - Resolve child model ID, endpoint, supported variant, and
      context window together after choosing the final child model. Pass that
      profile to `subagent.Job`; do not inherit the parent's endpoint or
      unsupported variant. Keep model-cache routing at the chat/model boundary.
      Proof: a parent Go model with a child Zen override captures the child
      endpoint and valid variant in the Job and outgoing request. Affected
      paths: `internal/subagent/manager.go:318-350`,
      `internal/subagent/runner.go:100-113`, and
      `internal/ui/chat/runtime.go:105-109`.

### 1.5 Clone capability policy at Agent construction

- [x] `PRAC-03` - Clone `agent.Options.ToolNames` and
      `agent.Options.BashAllowlist` in `agent.New` before retaining Options.
      Preserve nil versus empty semantics deliberately. Proof: mutate both
      caller slices after construction and prove the Agent's advertised tools
      and bash classification stay unchanged. Affected path:
      `internal/agent/agent.go:29-103`.

### 1.6 Keep model policy out of persistence

- [x] `ARC-01` - Require Agent, chat, and subagent callers to resolve a
      session model before `db.CreateSession`. `db` should persist a supplied
      session, not import `settings` to pick a user-facing model default.
      Proof: caller-level session creation tests retain the selected model and
      `internal/db` no longer imports `internal/settings`. Affected path:
      `internal/db/queries.go:11-37`.

### 1.7 Phase 1 proof

- [x] Run `go test -race ./internal/agent ./internal/subagent -count=1`. Passed on 2026-08-21.
- [x] Run focused chat, db, task, and model-cache tests named by rows 1.1-1.6. Passed on 2026-08-21.

## Phase 2: one owner for repeated policies

### 2.1 Normalize settings through one loader

- [x] `EXT-05` - Make exported `settings.Load` and `settings.LoadFile` use
      the same parsing and default-restoration path. A partial JSON file must
      have identical default-true values through both functions. Proof: a
      parity test loads one partial file through both exports and compares the
      resulting Settings. Affected path: `internal/settings/settings.go:170-183,379-438`.

### 2.2 Use one role capability table

- [x] `EXT-06` - Move role-to-tool capabilities into one dependency-neutral
      table consumed by settings presentation and Manager job construction.
      Proof: table-driven tests cover every role and prove effective
      `Job.Tools` equals the public role capability result. Affected paths:
      `internal/settings/settings.go:247-258` and
      `internal/subagent/manager.go:350,888-897`.

### 2.3 Share workspace containment checks

- [x] `EXT-07` - Extract the existing lexical, symlink, and real-path
      containment algorithm into one dependency-free package for read, write,
      and edit. Keep operation-specific errors at each tool. Proof: shared
      tests cover allow, lexical escape, existing symlink, and a nonexistent
      leaf below a symlink; each tool keeps one operation test. Affected paths:
      `internal/tools/read/read.go:56-112`,
      `internal/tools/write/write.go:43-90`, and
      `internal/tools/edit/edit.go:66-95`.

### 2.4 Give step limits a stable error contract

- [x] `PRAC-05` - Add an Agent-owned step-limit sentinel or typed error and
      make Runner and chat use `errors.Is`, not a substring of an error
      message. Proof: tests cover `errors.Is`, partial child completion, and
      the chat `/continue` hint without depending on wording. Affected paths:
      `internal/agent/agent.go:228`, `internal/subagent/runner.go:179-180`,
      and `internal/ui/chat/chat.go:1044-1045`.

### 2.5 Put OpenCode route metadata in the provider package

- [x] `ARC-03` - Expose a small pure OpenCode route helper in
      `internal/provider/opencode` and use it from model-cache enrichment and
      cache fallback. Keep the concrete OpenCode client. Proof: one route-table
      test covers Go, Zen, and free model IDs. Affected paths:
      `internal/modelscache/catalog.go:99-116` and
      `internal/provider/opencode` route helpers.

### 2.6 Phase 2 proof

- [x] Run focused settings, read/write/edit, provider, model-cache, Agent,
      Runner, and chat tests named by rows 2.1-2.5. Passed on 2026-08-21.

## Phase 3: closure

### 3.1 Whole-repository gates

- [x] Run `go test ./...` after phases 0-2 pass their focused proof. Passed on 2026-08-21.
- [x] Run `go vet ./...` after phases 0-2 pass their focused proof. Passed on 2026-08-21.
- [x] Run `make lint` after phases 0-2 pass their focused proof. Passed on 2026-08-21.

### 3.2 TUI and safety acceptance

- [x] Verify the subagent drawer and child routing in a real terminal at
      120x36 and 80x24. Drawer and composer remained readable in tmux captures on 2026-08-21. Keep runtime proof separate from source tests.
- [x] Record webfetch's rebinding and redirect tests as the trust-boundary
      proof. `TestRunRejectsDNSRebindingAtDial` and `TestRunRejectsPrivateRedirectAndPreservesClient` passed. Do not replace them with an HTML or transcript golden.

## Refused and filtered items

| Item | Decision | Reason and next proof |
| --- | --- | --- |
| `ARC-R01` concrete OpenCode client | Refused | There is one production provider. A one-adapter Provider interface would add a second abstraction. Reconsider only for a second incompatible provider. |
| `ARC-R02` replay keeps db rows | Refused | Events own live deltas and db owns durable replay. Mapping stored rows back through event types would add a lossy lifecycle. |
| `ARC-R03` depth-one subagents | Refused | Configuration clamps depth to one and child jobs expose no nested host. Reconsider only with a nested-host recovery contract. |
| `EXT-08` child preflight compaction | Refused for now | Child context metadata is not resolved. Reconsider after `EXT-01` supplies a selected child context window. |
| SessionGraph usage rollup | Filtered | The prior C3 ledger is complete. Current direct usage pricing is not regression evidence. Reconsider only if a new persisted usage field must be projected twice. |

## Dependencies

```text
EXT-03
  -> Phase 0 proof

PRAC-01 + EXT-02 + PRAC-04 + EXT-01 + PRAC-03 + ARC-01
  -> Phase 1 proof

EXT-05 + EXT-06 + EXT-07 + PRAC-05 + ARC-03
  -> Phase 2 proof

Phase 0 proof + Phase 1 proof + Phase 2 proof
  -> Phase 3 closure
```

## Review evidence

The three independent source passes reported the following current-code facts:

- `webfetch.Run` checks DNS before `client.Do`, while the transport resolves
  again at dial time. It also overwrites a caller client redirect callback.
- Task-family calls run concurrently, while Host stores the invocation parent
  session ID in mutable state.
- Child model selection can split one model from its endpoint and variant.
- Task result structs exist but Host returns several ad hoc response shapes.
- Subagent persistence, Agent option ownership, settings load defaults, role
  capabilities, workspace containment, step-limit classification, and model
  routing each have current source evidence above.

This implementation does not add a provider registry, plugin system, virtual
filesystem, extra settings hierarchy, performance work, or a ponytail deletion
pass.
