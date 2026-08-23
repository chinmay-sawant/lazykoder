# Improve-codebase review: v0.0.10

**Date:** 2026-08-24
**Repository:** `github.com/chinmay-sawant/lazykoder`
**Scope:** current Go source under `internal/`, with the knowledge base used as a map and live code used as proof.
**Status:** review and implementation complete; not committed or pushed
**Git metadata:** intentionally not collected because the repository rules prohibit Git commands without explicit publication authorization.

## Review boundary

This review used three read-only lenses:

- Architecture deepening: find a real ownership or depth problem, not a generic large-package complaint.
- Extension seams: find places where a new model, control, or tool requires edits in multiple registries.
- Go practices: find cancellation, error, resource, or contract issues that can be proved from current source.

The product ceiling remains a local Bubble Tea TUI agent with provider adapters,
durable SQLite history, recap workers, and guarded web fetching. The review did
not treat browser page execution as a defect by itself. The inspected webfetch
path validates public HTTP(S) destinations, revalidates redirects, and places
Chrome traffic behind a validating local proxy.

The five implementation rows were completed after the review. Focused tests,
`go test . ./internal/...`, `go vet . ./internal/...`, and `go build . ./internal/...`
pass. No Git command was run.

## Summary

| Measure | Result |
| --- | --- |
| Active findings | 5 |
| P0 findings | 0 |
| P1 findings | 5 |
| P2 findings | 0 |
| Refused lookalikes | 2 |
| Implementation rows | 5, all `[x]` |

### P1 findings

1. **EXT-01 - Catalog model metadata does not own protocol routing.**
2. **EXT-02 - Settings controls have parallel paint, navigation, activation, and hit-test registries.**
3. **EXT-03 - Settings form validation disagrees with settings-owned bounds.**
4. **EXT-04 - Tool advertisement and tool execution use separate registries.**
5. **PRAC-01 - Recap cancellation-aware entries replace nil contexts with `context.Background()`.**

## Finding records

### EXT-01 · P1 · defect

**Title:** Catalog model metadata does not own protocol routing

**Location:** `internal/provider/opencode/client.go:77-88`, `internal/provider/opencode/client.go:119-129`, `internal/modelscache/catalog.go:101-105`

**Evidence:**

```go
func RouteForModel(base, id string) Route {
    if isFreeModelID(id) { ... }
    if isResponsesModel(id) {
        return Route{Endpoint: ResponsesURL(base), Provider: ProviderGo}
    }
    return Route{Endpoint: ChatURL(base), Provider: ProviderGo}
}

var responsesModels = map[string]struct{}{
    "gpt-5.6-luna": {},
}
```

`liveInfo` still calls `RouteForCatalogModel`, which falls back to this
model-ID route table when constructing catalog information. A current or
future Responses-capable model therefore needs a source edit before it can
use `/responses` unless another path has already stored an endpoint.

**Cost:** Provider and model-catalog maintainers must edit a hard-coded list
for each protocol change. Users see a model in the catalog but can receive
the wrong request shape or a failed request when the model is not in the list.

**Change:** Make the catalog endpoint or protocol capability the owner of route
selection. Preserve protocol metadata from the live catalog and cache. Keep a
small route fallback only for records that truly lack protocol metadata.

**Proof:** Add a catalog test with a new synthetic model ID and Responses
protocol metadata. Assert that `ModelInfos` selects `/responses` without a
model-ID source edit. Retain a fallback test for a catalog record with no
protocol metadata.

**Depends-on:** none

**Not:** A larger provider plugin registry or another model-ID heuristic table.

### EXT-02 · P1 · friction

**Title:** Settings controls have parallel paint, navigation, activation, and hit-test registries

**Location:** `internal/ui/chat/settings.go:97-130`, `internal/ui/chat/settings.go:239-252`, `internal/ui/chat/settings.go:345-433`, `internal/ui/chat/settings.go:506-570`, `internal/ui/chat/settings.go:670-785`, `internal/ui/chat/settings.go:1489-1505`

**Evidence:**

```go
var settingsNavigationOrder = [...]int{
    settingsRowTheme,
    settingsRowModel,
    settingsRowVariant,
    // ... every selectable row is repeated here
}

func (m Model) settingsPaintLines(innerW int) []settingsPaintLine {
    // ... rows are appended in a second hand-maintained order
}
```

The same row identity is also repeated in `settingsRowLabel`,
`activateSettingsRow`, the adjustment switch, and mouse hit testing. The
navigation code first scans painted rows, then applies the separate order
array. A new control can therefore be visible while missing from keyboard
activation, left and right adjustment, or mouse mapping.

**Cost:** Every settings control change requires synchronized edits across
several switches and arrays. The recent keyboard-navigation repair is evidence
that the split is already user-visible, not only a future maintenance concern.

**Change:** Introduce one internal settings-row definition table containing
the row identity, label, visibility, value rendering, activation, adjustment,
and hit-test behavior. Derive paint and navigation from that table. Keep the
existing settings screen and controls; centralize ownership rather than adding
a second UI framework.

**Proof:** Add a registry coverage test that enumerates every selectable row
and asserts it has a label, paint entry, keyboard activation path, adjustment
path where applicable, and mouse hit-test path. Add one regression test for a
conditionally visible skill row.

**Depends-on:** none

**Not:** A generic UI framework or a second settings system.

### EXT-03 · P1 · defect

**Title:** Settings form validation disagrees with settings-owned bounds

**Location:** `internal/settings/settings.go:56-59`, `internal/ui/chat/settings.go:736-801`, `internal/ui/chat/settings.go:1331-1339`

**Evidence:**

```go
// internal/settings/settings.go
MinCompactPercent = 5
MaxCompactPercent = 99

// internal/ui/chat/settings.go
"Percentage of context window (10-90)"
if err != nil || v < 10 || v > 90 {
    return fmt.Errorf("must be between 10 and 90")
}
```

The arrow adjustment clamps to the settings package range of 5 through 99,
while the input form rejects 5 through 9 and 91 through 99. The settings
package also documents zero agent timeout as no timeout, while the shared UI
positive-integer validator rejects zero for the agent timeout form.

**Cost:** Users can reach values with arrow controls that the form refuses to
save. A persisted no-timeout value cannot be entered through the corresponding
form. Product rules are duplicated as literals in the UI and can drift again.

**Change:** Keep bounds and sentinel semantics in `internal/settings`. Expose
typed validators or field descriptors to the UI and derive form help text from
the same values. Preserve zero as the documented no-timeout sentinel.

**Proof:** Add edge tests for compact percentages 5, 10, 90, and 99, plus an
agent timeout of zero. Assert form validation and arrow adjustment accept the
same settings contract.

**Depends-on:** EXT-02

**Not:** A second validation layer with duplicated numeric literals.

### EXT-04 · P1 · friction

**Title:** Tool advertisement and tool execution use separate registries

**Location:** `internal/agent/tools_registry.go:39-45`, `internal/agent/tools_registry.go:178-209`, `internal/agent/tools_exec.go:34-45`, `internal/agent/tools_exec.go:231-243`

**Evidence:**

```go
var allBaseToolSpecs = map[string]opencode.ToolSpec{ ... }

var baseToolRunners = map[string]baseToolRunner{
    toolBash: (*Agent).execBash,
    toolRead: (*Agent).execRead,
    // ... a second hand-maintained map
}

if run, ok := baseToolRunners[tc.Name]; ok {
    return run(a, ctx, events, partID, title, tc)
}
out := "unknown tool: " + tc.Name
```

The provider-visible schema map and the runtime executor map are independent.
An advertised tool without a runner is visible to the model but always returns
`unknown tool`. A runner without a spec is not advertised. The spec builder
currently skips unknown names instead of making the mismatch fail during
development or startup.

**Cost:** Adding or renaming a tool requires synchronized edits in separate
maps. A partial edit creates a runtime failure that the compiler cannot catch.

**Change:** Use one internal registration record owning the tool name, spec,
and runner, then derive both advertisement and dispatch from it. If the maps
must remain separate for a short migration, add a parity invariant test that
fails when either side is missing.

**Proof:** Add a registry parity test and a dispatch test using every base tool
name. Assert that each advertised name has a runner and each runner has a
provider-visible spec unless it is explicitly host-owned.

**Depends-on:** none

**Not:** Reflection or a plugin system for the current fixed tool set.

### PRAC-01 · P1 · defect

**Title:** Recap cancellation-aware entries replace nil contexts with `context.Background()`

**Location:** `internal/recap/snapshot.go:87-97`, `internal/recap/worker.go:53-66`, `internal/recap/memory_run.go:32-39`, `internal/recap/memory.go:1061-1066`

**Evidence:**

```go
func BuildSnapshot(ctx context.Context, store *db.Store, sessionID string, opts SnapshotOptions) (Snapshot, error) {
    // ... input validation
    if ctx == nil {
        ctx = context.Background()
    }
    sess, err := store.GetSession(ctx, sessionID)
```

The same fallback appears in snapshot builders, recap workers, memory workers,
memory update entry points, and the memory document writer. A nil context is
therefore treated as an uncancellable request at multiple exported or
boundary-like entries. There is no canonical nil-context sentinel in the
recap package.

**Cost:** A malformed caller can silently lose cancellation and allow database,
file, or provider work to continue after its owner believes the operation was
cancelled. Hidden recap and memory work is especially difficult to observe
when it outlives the parent turn.

**Change:** Define one recap package sentinel for nil contexts and reject nil
at cancellation-aware entry points. Keep explicit `context.Background()` at
the few deliberate root call sites in the UI, where ownership is clear.

**Proof:** Add `errors.Is` tests for nil contexts on snapshot, recap worker,
memory worker, and memory update entry points. Add a cancellation test that
proves the provider or store operation is not started after cancellation.

**Depends-on:** none

**Not:** Storing context on worker structs or silently substituting
`context.Background()` again.

## Phase-wise checklist

The rows below are the canonical implementation ledger. All five findings are
implemented. The final real-terminal visual check remains open for a human
operator because Bubble Tea uses an alternate screen.

### P0 - Integrity

No active P0 finding. The inspected webfetch and Chrome paths were not filed
as an execution or SSRF defect because their public-destination validation and
redirect/proxy checks are explicit in the current source.

### P1 - Seams and contracts

- [x] **EXT-01:** Make catalog protocol metadata own model route selection and add a future-model routing test.
- [x] **EXT-02:** Replace parallel settings row maps with one row definition table and add coverage tests.
- [x] **EXT-03:** Make settings form validation use settings-owned bounds and sentinel semantics.
- [x] **EXT-04:** Unify tool specs and runners or add a failing parity invariant during the transition.
- [x] **PRAC-01:** Reject nil contexts in recap and memory entry points with one sentinel and cancellation tests.

### P2 - Shared forks

No separate P2 row. The findings above are the smallest shared ownership seams
that currently justify a change.

### P3 - Locality

No separate P3 row. The largest production files remain below the repository's
rough 2,000-line ceiling, and line count alone was not treated as a defect.

### P4 - Closure

- [x] Re-read each changed seam and update the matching knowledge-base narrative in the same implementation session.
- [x] Run the repository's available build, test, and vet gates, plus focused tests named in each finding.
- [ ] Re-run the settings interaction path in a real terminal and inspect the full screen before closing EXT-02 and EXT-03.

The final real-terminal visual check remains a human-in-the-loop step. The
settings package tests cover painted order, mouse hit testing, and keyboard
navigation, but this session did not start the alternate-screen TUI.

## Refused lookalikes

These were considered and intentionally not filed as active findings:

- **ARC-R01 - Split `internal/ui/chat` solely because it is large:** the package is the intentional Bubble Tea coordinator and the current evidence does not show a failed alternate adapter or a specific ownership defect. Splitting it without a concrete seam would add another coordinator and reduce locality.
- **ARC-R02 - Add a provider plugin registry:** provider descriptors, factory selection, and auth routing already form a concrete seam for the fixed provider set. A plugin framework would add indirection without a current second implementation boundary.

## Source index

- `knowledge-base/wiki/architecture/component-map.md` - package ownership map used to choose review boundaries.
- `knowledge-base/wiki/architecture/data-flow.md` - request, tool, persistence, and recap flow used to trace seams.
- `internal/provider/opencode/client.go` - route selection and hard-coded Responses capability.
- `internal/modelscache/catalog.go` - live model metadata assembly.
- `internal/ui/chat/settings.go` - settings paint, navigation, activation, adjustment, and hit testing.
- `internal/settings/settings.go` - settings bounds and timeout sentinel semantics.
- `internal/agent/tools_registry.go` and `internal/agent/tools_exec.go` - tool specs and runners.
- `internal/recap/` - context-aware snapshot, worker, memory, and artifact entry points.
