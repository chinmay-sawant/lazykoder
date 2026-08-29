# v0.0.10 ponytail audit

> **Parent:** `plans/v0.0.10/README.md` - current product plan
> **Status:** remediation complete, with PT-10 intentionally deferred
> **Date:** 2026-08-24
> **Scope:** whole repository, with live Go source and tests as evidence

---

## Overview

This is a repo-wide over-engineering review. It looks for dead code, duplicate
ownership, speculative runtime paths, unnecessary wrappers, and repeated
control logic. It does not review correctness, security, or performance except
when those concerns show that a duplicate implementation is unnecessary.

The requested seven-way spawn first hit `agent thread limit reached`. Eight
read-only worker result sets eventually arrived from the initial attempts, and
further spawns remained rejected. I also ran seven separated local read-only
passes over the same slices:

1. package boundaries and abstractions
2. `internal/ui/chat`
3. `internal/agent` and `internal/subagent`
4. settings, SQLite, recap, and memory
5. providers, model cache, webfetch, and workspace paths
6. bootstrap, scripts, docs, plans, skills, and test support
7. adversarial cross-check of the strongest candidates

The implementation pass removed or consolidated every high-confidence finding.
PT-10 remains deferred because its mouse, keyboard, and form-entry behavior is
not a mechanical duplication. No Git command was run.

## Executive summary

Seventeen candidates survived the evidence check. Sixteen are now removed,
consolidated, or narrowed. PT-10 remains a separately scoped TUI refactor.

| ID | Priority | Candidate | Estimated cut | Status |
| --- | --- | --- | ---: | --- |
| PT-01 | P2 | unused child-tool allowlist | 4 lines | [x] |
| PT-02 | P1 | permanently disabled nested-host path | 10 lines | [x] |
| PT-04 | P2 | unused catalog-route wrapper | 5 lines | [x] |
| PT-05 | P2 | test-only model-route wrapper | 5 lines | [x] |
| PT-06 | P1 | duplicate workspace containment code | 35 lines | [x] |
| PT-07 | P1 | duplicate sub-agent defaults and bounds | 35 lines | [x] |
| PT-08 | P1 | duplicate recap snapshot loading | 35 lines | [x] |
| PT-09 | P1 | duplicate recap related-search setup | 25 lines | [x] |
| PT-10 | P1 | repeated settings-row behavior | 80-120 lines | deferred |
| PT-11 | P1 | duplicate slash-palette command names | 35-50 lines | [x] |
| PT-12 | P1 | five parallel settings picker booleans | 20-35 lines | [x] |
| PT-13 | P2 | one-use forwarding helpers | 30 lines | [x] |
| PT-14 | P2 | unused model-cache helpers | 25 lines | [x] |
| PT-15 | P2 | unused runtime fields | 7 lines | [x] |
| PT-16 | P1 | duplicate base-tool registries | design-dependent | [x] |
| PT-17 | P2 | provider interface no-op methods | design-dependent | [x] |
| PT-18 | P2 | custom helpers replaceable by Go stdlib | 30 lines | [x] |

No dependency removal survived this audit.

## Finding records

### PT-01 - unused child-tool allowlist

`delete: DefaultChildTools is never read. Remove it and its comment. [internal/agent/tools_registry.go:36-39]`

Evidence: the only repository matches are the declaration and its comment.
`roles.Tools` supplies the actual child allowlist at runtime.

Replacement: nothing.

Confidence: high.

### PT-02 - nested-host path that always returns nil

`delete: childSubagentHost has a condition whose both branches return nil. Remove the dead nested-host branch and factory call, but preserve persisted MaxDepth fields until the product plan explicitly migrates them. [internal/subagent/runner.go:210-218; internal/subagent/manager.go:387-388; internal/subagent/types.go:105-108]`

Evidence: `childSubagentHost` returns nil after both the depth check and the
fall-through. The product cap is one, so a child cannot currently create a
nested host. The setting is persisted for compatibility, so this row does not
recommend deleting the persisted setting or job fields.

Replacement: leave `Host` nil until nested tasks become a shipped feature.

Confidence: high.

### PT-04 - unused catalog-route wrapper

`delete: RouteForCatalogProvider has no callers. Remove the wrapper and call RouteForCatalogModel when a model id is available. [internal/provider/opencode/client.go:127-131]`

Evidence: the repository has no call site or test for `RouteForCatalogProvider`.
The model-aware function is the only route helper used by the cache.

Replacement: `RouteForCatalogModel`.

Confidence: high.

### PT-05 - test-only model-route wrapper

`yagni: ChatURLForModel only forwards to RouteForModel and is used by two tests. Call RouteForModel(...).Endpoint in those tests and remove the wrapper. [internal/provider/opencode/client.go:71-75; internal/provider/opencode/client_test.go:541-544]`

Evidence: production code uses `RouteForModel` directly. The wrapper adds a
second public name for the same operation inside an internal package.

Replacement: `RouteForModel(base, id).Endpoint`.

Confidence: high.

### PT-06 - duplicate workspace containment code

`shrink: agent and grep each reimplement lexical and symlink containment. Reuse workspace.Resolve and retain grep's file-kind check. [internal/agent/tools_exec.go:606-625; internal/tools/grep/grep.go:302-335; internal/workspace/containment.go:13-42]`

Evidence: `withinWorkspace` and `grep.resolvePath` each normalize absolute
paths, compare relative paths, evaluate symlinks, and produce escape errors.
`workspace.Resolve` already owns that operation for read, write, and edit.

Replacement: call `workspace.Resolve`, then keep only grep's `os.Stat` check
and grep-specific error prefix.

Confidence: high.

### PT-07 - duplicate sub-agent defaults and bounds

`yagni: settings and subagent each own defaults for concurrency, queue size, timeout, depth, and child steps. Keep product defaults in settings and make ConfigFromSettings the runtime boundary. [internal/settings/settings.go:34-53,240-246,474-503; internal/subagent/config.go:10-23,53-90]`

Evidence: `subagent/config.go` repeats values already declared in settings,
including a comment that `DefaultChildMaxSteps` must stay in sync. Both
packages also normalize the same values.

Replacement: use one settings-owned default and bounds table. Keep
`subagent.Config` as a runtime value without a second product policy.

Confidence: high.

### PT-08 - duplicate recap snapshot loading

`shrink: BuildSnapshot and BuildAnchorSnapshot repeat session validation, graph loading, candidate filtering, and sorting. Share a private loader while keeping window selection separate. [internal/recap/snapshot.go:87-200]`

Evidence: both functions validate the same session, load the same graph, build
the same candidate list, and sort it with the same comparator. The anchor path
has a different selection contract, so only the common loader and ordering
should be shared.

Replacement: one private source-message loader plus the existing distinct
window and anchor selection paths.

Confidence: high.

### PT-09 - duplicate recap related-search setup

`shrink: RelatedAvoid and RelatedRecapEvidence repeat pattern creation, timeout setup, grep options, and output truncation. Share the search setup while preserving their different paths and fallback behavior. [internal/recap/worker.go:142-161; internal/recap/memory_run.go:215-246]`

Evidence: both functions derive `relatedPattern`, create the same timeout, run
the same bounded grep, and truncate to the same limit. Their search paths and
fallback rules are the behavior that must remain distinct.

Replacement: one private related-search helper with explicit path and fallback
options.

Confidence: high.

### PT-10 - settings behavior repeated across five paths

`shrink: settings rows repeat the same control identities across painting, navigation, activation, adjustment, toggle, and mouse handling. Let one row definition own the value and action behavior. [internal/ui/chat/settings.go:107-148,362-490,631-737,833-1008,1568-1800]`

Evidence: the row definition table owns only ids and labels. The same ids then
appear in separate switches for enter, arrow keys, toggle, and mouse input.
The paint list is another hand-maintained row order. A new control must be
added in several places.

Replacement: keep one settings-row table with visibility, display, and action
functions, then derive navigation and hit testing from the painted rows.

Confidence: medium. This is the largest possible cut, but it needs a focused
UI refactor and geometry tests.

### PT-11 - duplicate slash-palette command names

`shrink: slashPaletteGroups repeats command names already owned by slashCommands. Put palette grouping on the canonical slashCmd records and remove the second name registry and seen loops. [internal/ui/chat/slash.go:19-30,58-100,295-320; internal/ui/chat/chat.go:394-450]`

Evidence: `slashPaletteGroups` stores only names. `slashCommands` already owns
the command name, description, aliases, and handler metadata. The palette
build then maintains a second list and deduplicates it manually.

Replacement: add group metadata to the canonical command records or derive the
group from those records in one place.

Confidence: high. A separate three-line `runSlash` forwarding wrapper can be
removed at the same time. [internal/ui/chat/slash.go:191-194]

### PT-12 - five parallel settings picker booleans

`shrink: replace settingsPickDefault, settingsPickRecap, settingsPickChild, settingsPickExplore, and settingsPickChildVariant with one picker target enum. [internal/ui/chat/chat.go:186-190; internal/ui/chat/settings.go:98-104,752-767; internal/ui/chat/picker.go:521-552,629-639,822-904]`

Evidence: the model already has `settingsModelPickerTarget` for most picker
targets, while five booleans are independently set, cleared, and tested.
The booleans permit impossible multi-picker states and spread reset logic over
the picker paths.

Replacement: extend the existing target enum for the variant picker and store
one active target.

Confidence: high.

### PT-13 - one-use forwarding helpers

`delete: remove test-only Agents.ToolsForRole and subagent.IsTaskTool, plus the single-caller MergeMemory, timeNow, touchSessionTx, and isStepLimitErr forwarding helpers; call the underlying functions directly. [internal/settings/settings.go:377-380; internal/subagent/host.go:45-47; internal/recap/memory.go:292-295; internal/recap/memory_run.go:135,248-250; internal/db/queries.go:215,247-249; internal/subagent/runner.go:146,186-188]`

Evidence: each helper adds no behavior. `ToolsForRole` and
`subagent.IsTaskTool` have only test callers, `MergeMemory` has only a test
caller, and the remaining wrappers each have one caller. The underlying
functions already provide the required seams.

Replacement: call `roles.Tools`, `task.IsTaskTool`, `MergeMemoryWithSkills`,
`time.Now`, `touchSession`, and `errors.Is` directly, updating the small unit
tests with them.

Confidence: high.

### PT-14 - unused model-cache helpers

`delete: remove Path, ContextOf, EndpointOf, and HasVariant from modelscache; inline the one test-only endpoint lookup. [internal/modelscache/modelscache.go:52-90,150-162; internal/modelscache/modelscache_test.go:33-34]`

Evidence: no production caller exists for these helpers. `EndpointOf` is used
only by one unit assertion, while current callers build the cache path with
`filepath.Join` and use `InfoOf` for model records.

Replacement: direct `filepath.Join`, `InfoOf`, and field access at the few
callers that need them.

Confidence: high.

### PT-15 - unused runtime fields

`delete: remove Config.Endpoint, Agent.projectInstructionsPath, and Env.DBPath when their declarations and initializers are updated together. [internal/subagent/config.go:32-49,91-93; internal/agent/agent.go:125-128,184; internal/workspace/workspace.go:21-25,47]`

Evidence: the sub-agent endpoint is trimmed but never selected by the manager,
the agent instruction path is assigned but never read, and the workspace DB
path is assigned but callers use the store or construct paths directly.

Replacement: use the runtime endpoint and store owners already selected by
their respective boundaries. Keep `Job.Description`, `Job.ParentPartID`, and
`Job.Timeout`, which support manager naming, persistence, recovery, and timeout
behavior.

Confidence: high.

### PT-16 - duplicate base-tool registries

`shrink: combine allBaseToolSpecs and baseToolRunners into one internal tool registration record containing the spec and executor. [internal/agent/tools_registry.go:41; internal/agent/tools_exec.go:1-80,238]`

Evidence: the same base-tool names are maintained in separate maps, and a
validation function exists only to detect drift between them. One record can
advertise and execute each tool without repeating the names.

Replacement: one internal registration table, with derived spec and runner
maps only if a caller truly needs map lookup.

Confidence: medium. Measure the resulting diff before accepting it because the
registry is a larger structural change.

### PT-17 - provider interface forces no-op methods

`yagni: provider.Client requires FreeModelInfos and Usage from CLI and OpenAI-compatible clients that return nil or zero values. Move those capabilities behind optional narrow interfaces. [internal/provider/provider.go:17-28; internal/provider/openai/client.go:166-173; internal/provider/subscription/client.go:218-230]`

Evidence: both non-OpenCode client families implement no-op methods only to
satisfy the broad shared interface. The UI requests those capabilities only in
catalog and usage paths.

Replacement: keep a small chat contract, then use narrow catalog and usage
interfaces at the views that need them. Do not add a provider registry.

Confidence: medium. Keep the current interface if the split adds more code
than it removes.

### PT-18 - custom helpers replaceable by Go stdlib

`stdlib: replace cloneStrings, indexOfString, maxInt64, minInt, and maxInt with slices.Clone, slices.Index, min, and max. [internal/agent/agent.go:166-171; internal/ui/chat/settings.go:1204-1211; internal/agent/stream.go:60-65; internal/recap/snapshot.go:349-354; internal/recap/memory.go:390-395]`

Evidence: each helper implements a direct standard-library or language-built-in
operation. The module targets Go 1.26.4, so `slices.Clone`, `slices.Index`, and
the built-in `min` and `max` are available.

Replacement: use the stdlib calls directly and preserve the nil behavior of
`slices.Clone`.

Confidence: high.

## Phase-wise checklist

## Implementation evidence, 2026-08-24

The completed rows were rechecked against their live callers before editing.
The focused package tests passed during each change. The final repository gates
were `go test ./...`, `go vet . ./internal/...`, `go build . ./internal/...`,
and `make lint`, all with exit code 0.

PT-10 is deferred. The current settings card has distinct click behavior for
toggle rows, chevrons, numeric values, picker rows, and text forms. Moving its
large switches into a table without a real-terminal geometry pass would add
indirection and risk changing those behaviors. It should be a focused UI task
with keyboard, mouse, compact-card, and conditional-skill coverage.

### Phase 1: remove dead code and wrappers

- [x] PT-01: deleted `DefaultChildTools` after a fresh caller scan.
- [x] PT-02: removed the inactive nested-host branch and factory call while
      preserving persisted MaxDepth compatibility.
- [x] PT-04: removed `RouteForCatalogProvider` and reran provider tests.
- [x] PT-05: removed `ChatURLForModel` and updated its tests.
- [x] PT-13: inlined the one-use forwarding helpers and updated unit tests.

### Phase 2: collapse duplicate ownership

- [x] PT-06: routed agent bash workdir and grep path checks through
      `workspace.Resolve` without weakening the existing boundary tests.
- [x] PT-07: made settings the only source of sub-agent product defaults and
      bounds, then test `ConfigFromSettings` and `NewManager`.
- [x] PT-08: shared recap snapshot loading and sorting while preserving anchor
      selection semantics.
- [x] PT-09: shared recap related-search setup without merging fallback rules.
- [x] PT-14: removed model-cache helpers after a fresh caller scan.
- [x] PT-15: removed unused runtime fields with their declarations and all
      initializers updated together.

### Phase 3: simplify larger maintenance surfaces

- [ ] PT-10: deferred to a focused UI change. It still needs keyboard, mouse,
      compact-card, and conditional-skill behavior proof in a real terminal.
- [x] PT-11: derived slash-palette grouping from canonical slash command records.
- [x] PT-12: replaced the picker booleans with one active picker target.
- [x] PT-16: combined the base-tool spec and runner maps into one registration
      table, with a test that each entry has its matching spec and runner.
- [x] PT-17: narrowed free-model and usage capabilities behind optional
      interfaces, then removed the OpenAI and subscription no-op methods.
- [x] PT-18: replaced the custom collection and integer helpers with stdlib
      operations and rerun the affected package tests.

### Phase 4: closure gates after any implementation

- [x] `go test ./...` passed. All listed packages reported `ok` or had no test
      files.
- [x] `make lint` passed after test fixture permissions and the nil-context
      contract test were made explicit.
- [x] No real-terminal settings inspection was claimed because PT-10 was not
      implemented in this pass.
- [x] The final commands were `go test ./...`, `go vet . ./internal/...`,
      `go build . ./internal/...`, and `make lint`; each exited 0.

## Rejected lookalikes

- `workspace.Resolve`, policy classification, and browser proxy validation are
  boundary checks. They are not bloat.
- The concrete OpenCode client and subscription adapter are required by the
  current provider product scope. This review does not propose a provider
  plugin registry.
- Recap and durable-memory workers have separate triggers, records, and file
  contracts. Their similar names are not enough evidence to merge them.
- Test seams such as `bash.Runner`, `webfetch.resolver`, and the provider
  runner functions have test callers. They are not single-use production
  abstractions.
- Versioned tests and migration code protect stored sessions and earlier
  behavior. The audit did not recommend deleting them from age alone.
- `settings.LoadFile` is retained as a compatibility entry point. Its parity
  test and the v0.0.9 plan both protect the alias, so it is not a cleanup row.
- `Job.Description`, `Job.ParentPartID`, and `Job.Timeout` remain part of the
  manager's naming, persistence, recovery, and timeout contract even when the
  low-level runner does not read every field directly.
- Ignored or generated artifacts such as `opencode_session_logs.json`,
  `temp/*.go2`, `chat.test`, `bin/lk`, and the rendered improve-codebase HTML
  have no runtime callers, but they may be local context or regeneration
  outputs. They are owner-review cleanup candidates, not automatic deletions,
  and are excluded from the net metric.
- `docs/plans.md:41-42` repeats one v0.0.9 phase row. That is a documentation
  typo, not an architectural cut, so it is excluded from the metric.

## Review closure

- [x] Read the ponytail-audit and ponytail-review rules before scanning.
- [x] Inspected live source and tests across all seven review slices.
- [x] Rejected correctness, security, and performance-only findings.
- [x] Wrote and re-read this canonical Markdown ledger.
- [x] Applied the completed cleanup rows and updated their focused tests.
- [x] Passed `go test ./...`, `go vet . ./internal/...`, `go build . ./internal/...`,
      and `make lint`.
- [x] Kept PT-10 open with an explicit scope and verification requirement.

No dependencies were added or removed. The original line estimate was a review
forecast, not a measured implementation result, so it is not reported as a
final metric.
