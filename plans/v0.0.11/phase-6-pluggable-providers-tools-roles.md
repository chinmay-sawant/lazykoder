# v0.0.11 / Phase 6 - Pluggable providers, tools, and roles

> **Parent:** `plans/v0.0.11/README.md`
> **Status:** draft - not started
> **Estimated effort:** 7-10 days
> **Priority:** P1
> **Gate:** `/provider`, `/model`, skill-style pickers for tools and roles, and agent turns prove that a new provider, tool, or role can be added without editing the hardcoded catalog, registry, or role switches

---

## Overview

Today only `skills` is pluggable. Discovery scans approved local and global roots for `SKILL.md` descriptors, ranks them, and injects bounded wire-only context at the first-request boundary. Providers, tools, and roles are the opposite: five providers hardcoded in `internal/provider/catalog.go:30` plus a switch in `internal/provider/factory.go:16`, eight tools hardcoded in `internal/agent/tools_registry.go:39` plus switches in `internal/agent/tools_exec.go:63`, and three roles hardcoded in `internal/roles/capabilities.go:6` plus switches in `internal/subagent`, `internal/tools/task`, and the settings drawer.

This phase makes providers, tools, and roles pluggable with the same shape as skills: a typed catalog, bounded loading with diagnostics, a registry that supports both compiled-in registration and declarative descriptors persisted under `<workdir>/.lazykoder/*.json`, the existing picker/drawer family for selection, an agent seam that never trusts descriptor content, and persisted settings that controls scope and caps. Adding a new OpenAI-compatible provider, a new bash-safe tool, or a new role should touch one JSON entry under `.lazykoder/` or a single `Register` call, not eight files.

## Executive summary

Codex extensibility means a contributor can ship a new provider, tool, or role without forking the three central switches. Concretely:

- **Providers:** `internal/provider/catalog.go` and `internal/provider/factory.go` become a registry. Built-in providers register themselves from `init()` via `provider.Register`. Declarative OpenAI-compatible providers are also loaded from `<workdir>/.lazykoder/providers.json` (and the global mirror `~/.config/lazykoder/providers.json` when present), bounded and validated like `skills.Opts`. No top-level `providers/` directory is scanned for config.
- **Tools:** `internal/agent/tools_registry.go` becomes a registry. Each package under `internal/tools/*` exposes a `Tool` interface and calls `tools.Register` in `init()`. Declarative local tools (shell-backed) are loaded from `<workdir>/.lazykoder/tools.json` (and `~/.config/lazykoder/tools.json` globally) when explicitly allowed by settings, with the same path-containment and policy gate as built-ins.
- **Roles:** `internal/roles/capabilities.go` becomes a registry backed by `<workdir>/.lazykoder/roles.json` (and `~/.config/lazykoder/roles.json` globally). Each entry defines the allowlist, single-writer flag, and default model class mapping. `Settings.Agents.DefaultRole` and `subagent.Config` validate against the merged catalog instead of a three-way switch.
- **UX:** `/provider` stays, and two new commands `/tools` and `/roles` use the same `internal/ui/chat/picker.go` kind as `/skills` and `/provider` (filter, enter to select, esc to cancel). `/model` grouping uses `Info.Provider` directly instead of `providerIDForModelInfo` heuristics.
- **Safety:** Descriptor discovery rejects symlinked roots and files, caps depth/size/count, and returns partial catalogs with `Diagnostics` so one bad global entry never hides local entries. Bodies and descriptions are treated as untrusted display text. Tool bodies are never executed at discovery time.

## Phase 6.1: Define the shared pluggable contract

### 6.1.1 Extract the discovery primitive that skills already proved

- [ ] Add `internal/catalog` or `internal/discovery` helpers for approved-root resolution: `ResolveRoots(workdir, includeLocal, includeGlobal, explicitGlobal []string) ([]Root, []Diagnostic)` patterned on `internal/skills/catalog.go:130`. Shares the `priority`, `seen[abs]`, `Lstat` symlink rejection, and stable sort helpers. Keep env lookups at the boundary. No behavior change yet - covered by existing `TestDiscover*` clones.

### 6.1.2 Define the registry shape reused by all three domains

- [ ] Introduce a generic `Registry[T]` or three typed registries with the same contract: `Register(Descriptor) error`, `Descriptors() []Descriptor`, `DescriptorFor(id string) (Descriptor, bool)`, `IDs() []string`, and a `ResetForTest` helper. Registry is a `sync.RWMutex` map keyed by normalized ID. Duplicate registration returns an error with a `Diagnostic` instead of panicking. Unknown ID lookups return `false`, never silent fallback to `IDOpenCode`.

### 6.1.3 Establish descriptor validation and diagnostics

- [ ] Define bounds shared with skills: `DefaultMaxDepth=4`, `DefaultMaxDescriptors=256`, `DefaultMaxDescriptorSize=256*1024`, `DefaultMaxBody=48*1024` per domain, tunable via `Options`. Every load pass returns `(Catalog, error)` where `Catalog.Diagnostics []Diagnostic` explains missing files, unreadable JSON, oversize payloads, duplicate IDs, and parse failures. A diagnostic never hides sibling entries; it is reported alongside the partial catalog like `skills.scanRoot:403` does.

### 6.1.4 Guarantee .lazykoder bootstrap via init and auto-create

- [ ] Extend `internal/workspace/workspace.go:28 Init(cwd)` to be the single bootstrap for all project-local config under `<workdir>/.lazykoder/`. After `MkdirAll` and `ensureGitignore`, ensure four JSON files exist with mode `0600`, never overwriting user edits: `settings.json` (write `settings.Default()` via `settings.Save` when absent), `providers.json`, `tools.json`, `roles.json` (each write `[]\n` when absent, bounded reads later). Creation is idempotent: a second `Init` leaves timestamps and contents alone. Add `TestInitCreatesEmptyCatalogFiles` and `TestInitDoesNotOverwriteExistingProviders` in `workspace_test.go`.
- [ ] Support an explicit init command `bin/lk init` (and `go run main.go init`, alias `lz init`) that calls the same `workspace.Init` and prints the created paths to stdout, then exits 0 without starting the TUI. `main.go` parses `os.Args[1]=="init"` before the bubbletea program and reuses the same bootstrap helper so the explicit and implicit paths cannot drift.
- [ ] Make TUI startup auto-create through the same path: `main.go:26` already calls `workspace.Init(cwd)` on every launch. After the bootstrap, missing catalog files are treated as empty arrays by loaders (`os.IsNotExist` -> empty catalog, not an error), so a user who never ran `lk init` still gets a working app on first `make run` or `bin/lk` launch. No duplicate bootstrap code in `settings.LoadFile` or catalog loaders.

## Phase 6.2: Make providers pluggable

### 6.2.1 Replace the hardcoded catalog with a registry

- [ ] Refactor `internal/provider/catalog.go:30` - keep the five built-in descriptors but move them into `init()` registrations via `provider.Register`. Remove `const IDOpenCode` hardcoding from logic (keep constants for compatibility, but `Normalize` and `canonicalID` use the registry map). `Descriptors()` and `IDs()` read the registry. `Normalize` returns the input when unknown instead of silently returning `IDOpenCode` so callers can surface an error. Adds test `TestProviderRegistryRetainsBuiltins` that asserts 5 IDs without touching the switch.

### 6.2.2 Replace the factory switch with registry dispatch

- [ ] Refactor `internal/provider/factory.go:16` - `NewClient(id)` becomes `registry[Normalize(id)].Factory(descriptor)` lookup. Each provider package (`internal/provider/opencode`, `openai`, `subscription/codex`, `subscription/grok`) registers a factory closure in `init()`. `IDCodex` and `IDGrok` registration supplies the `Runner` CLI path variant. `IDXAI` becomes a declarative declaration of the openai factory with a different `BaseURL` and `EnvKey`, proving that aliases do not need code. Test covers unknown ID returns `unknown provider` error.

### 6.2.3 Support declarative OpenAI-compatible providers from .lazykoder config

- [ ] Add provider descriptor schema `ProviderDescriptor` with fields `id`, `label`, `auth_method` (`api_key|codex|grok`), `env_key` or `env_keys[]`, `cli`, `base_url`, `model`, `supported`. The local source is `<workdir>/.lazykoder/providers.json` (JSON array of descriptors); the optional global mirror is `~/.config/lazykoder/providers.json`. Both are bounded (file size, entry count) and validated like `skills.Opts`. Missing files are ignored; symlink files are rejected. `LoadProviders(workdir)` merges built-in registry entries with local and global file entries (local wins on duplicate `id`), caps entries, and returns `(Catalog, Diagnostics)` without executing anything. An `openai`-compatible descriptor with `base_url=https://api.openai.com/v1` must be creatable without touching Go.

### 6.2.4 Make auth and credentials generic

- [ ] Refactor `internal/provider/auth.go:69` and `internal/provider/credentials.go:11`. Drop the `AuthMethodAPIKey/Codex/Grok` const switch in `CheckAuth` and `LoginCommand`. Dispatch via registry-provided `AuthChecker` and `LoginCommandFactory` per descriptor. `CredentialSource` returns `descriptor.EnvKey` (or first set entry of `EnvKeys`) generically; `IDOpenCode` dual `OPENCODE_API_KEY`/`OPENCODE_ZEN_API_KEY` becomes `EnvKeys: ["OPENCODE_API_KEY","OPENCODE_ZEN_API_KEY"]` in the descriptor. Update `InitialAuthStatus` to use descriptor labels.

### 6.2.5 Make model cache and routing provider-agnostic

- [ ] Refactor `internal/modelscache/provider.go` helpers: `ProviderFromEndpoint`, `ProviderOf`, `providerIDForModelInfo` in `picker.go:291`, `opencode.RouteForCatalogModel:150`, and `ParseModelsDev:83` loop. Instead of switching on `opencode.ai` or `cli://codex`, use `info.Provider` field stored directly from the registry descriptor. Cache `Info.Provider` is the canonical registry ID. Migration path keeps reading legacy endpoint heuristics but writes the canonical ID on next `Save`.

### 6.2.6 Update the TUI provider grouping

- [ ] Refactor `internal/ui/chat/picker.go:132` and `internal/ui/chat/chat.go:723`. Replace `modelCatalogProviderIDs = [codex, grok, opencode]` and `modelPickerProviderIDs` hardcoded arrays with `provider.IDs()` iteration in registry order plus a `DisplayOrder` field on the descriptor. Remove the `providerIDForModelInfo` heuristic once cache stores canonical provider. `refreshModels()` loops registry IDs, reuses `m.client` only when `providerID == EffectiveProvider()`, otherwise `m.newProviderClient(id)`. Drawer status continues to show `selected` and auth label via generic `providerStatus`.

## Phase 6.3: Make tools pluggable

### 6.3.1 Define the tool plugin interface

- [ ] Add `internal/tools.Tool` interface in a small package (`internal/tools/registry` or `internal/agent/toolplugin`): `Spec() opencode.ToolSpec`, `Run(ctx context.Context, argsJSON string, c ToolContext) (output string, metaJSON string, status string, err error)`, `Title(argsJSON string) string` for the drawer header. `ToolContext` carries `Workdir`, `Store`, `Events chan<- agent.Event`, `Ask func`, `Confirm func(policy.Decision, string)(bool,error)`. Keep `policy` and `workspace.Resolve` calls inside `Run` so the gate stays at the tool boundary.

### 6.3.2 Replace the hardcoded map with a registry

- [ ] Refactor `internal/agent/tools_registry.go:39` - `baseToolRegistry` literal becomes a `sync.Map` populated by `Register(name string, t Tool)`. Each `internal/tools/{bash,read,write,edit,grep,webfetch,question,todo}` package registers itself in `init()`. `task` family stays behind `SubagentHost` but also implements `IsHosted() bool` so `toolSpecsFor` can treat it uniformly. `DefaultParentTools` becomes the ordered list of registered builtin IDs at startup. Existing `TestToolRegistry*` tests assert `Spec().Name == key` and runner non-nil via the new registry.

### 6.3.3 Generalize execution and rendering

- [ ] Refactor `internal/agent/tools_exec.go:218` - collapse `execBash/execRead/...` per-tool functions into `registry[tc.Name].Run`. `toolAllowed` supplements registry membership; unknown names still produce `unknown tool: ...` denied rows. `toolTitle` delegates to `Tool.Title`. In `internal/ui/chat/transcript.go`, replace the `switch name` in `renderToolMode` and `toolCommand` with generic rendering driven by `Tool.Title` plus a metadata flag `metadata.diff` for the edit panel. Keep `edit.UnifiedDiff` as a shared renderer but trigger it from metadata rather than name equality.

### 6.3.4 Add file-loaded shell tools from .lazykoder config

- [ ] When `settings.tools.allow_discovered` is true, load `ToolDescriptor` entries from `<workdir>/.lazykoder/tools.json` (and `~/.config/lazykoder/tools.json` globally) - each entry holds `name`, `description`, `parameters` JSON schema, and `command` template with binary allowlist. Loading is bounded (file size, entry count), rejects symlinked files, and returns diagnostics. Discovered tools appear in `toolSpecsFor` when allowed, run via a sandboxed `bash.Run` with the same `policy.ClassifyWithAllowlist` and `workspace.Resolve` checks as built-ins. A missing executable becomes `status=error`, never a panic. No scan of top-level `tools/` directories.

### 6.3.5 Persist tool enablement

- [ ] Extend `internal/settings/settings.go` with `Tools{Enabled map[string]bool, AllowDiscovered bool, MaxDiscovered int}` under `.lazykoder/settings.json`. Default map contains the eight built-ins set to `true`. `EffectiveTools()` normalizes unknown names against the current registry and caps counts. `agentOptions()` maps `Options.ToolNames` from the sorted enabled keys. Settings card rows list each registered tool as a toggle, plus an `allow discovered` row.

## Phase 6.4: Make roles pluggable

### 6.4.1 Replace the closed enum with a registry and .lazykoder descriptors

- [ ] Refactor `internal/roles/capabilities.go:6` - keep `Explore/Plan/General` as built-ins but load them via `roles.Register(Role{ID, Label, Tools []string, SingleWriter bool, DefaultModelClass string})` in `init()`. Add file loading from `<workdir>/.lazykoder/roles.json` (and `~/.config/lazykoder/roles.json` globally) - each entry holds `id`, `label`, `tools` allowlist, `single_writer bool`, `model_class string` (`flash|pro|general`). Loading caps entry count and file size and returns diagnostics like skills. `Roles()` returns registry plus loaded entries (local wins on duplicate `id`); `DescriptorFor` validates callers. No scan of top-level `roles/` directories.

### 6.4.2 Generalize role normalization and tool mapping

- [ ] Refactor `roles.Normalize:12`, `task.NormalizeRole:255`, `subagent/config.go:90 normalizeRole`, and `orchestrator/plan.go:126` switch. All four call `roles.IsKnown(id)` against the registry. `roles.Tools(role)` returns the registered `Tools` slice instead of a two-branch switch. `task.ParseTaskArgs:284` validates against the registry and returns `invalid role` with the registry IDs in the error message.

### 6.4.3 Make subagent policy and model class data-driven

- [ ] Refactor `internal/subagent/manager.go:474` writer lock: replace `if role==RoleGeneral && !AllowParallelWriters` with `if roles.DescriptorFor(role).SingleWriter && !AllowParallelWriters`. Replace `manager.go:352` per-role model override `if role==RoleExplore && ExploreModel!=""` with `descriptor.ModelClass` lookup and `ConfigFromSettings` mapping that indexes `ModelClassByRole map[string]string` instead of three hardcoded fields. `subagent.Config` stores `Roles []roles.Role` so tests can assert custom roles without touching production code.

### 6.4.4 Rewire settings and prompts

- [ ] Refactor `internal/settings/settings.go:520` - `Agents.DefaultRole` validation uses `roles.IsKnown` instead of a string switch, defaulting to `Explore` when unknown. `Orchestrator` field changes from `ExploreClass/PlanClass/GeneralClass string` to `ModelClassByRole map[string]string` (keep deprecated JSON keys as aliases for one version via custom `UnmarshalJSON`). Refactor `internal/orchestrator/plan.go:70` prompt template to enumerate `roles.IDs()` instead of hardcoding `"explore|plan|general"`. Settings drawer `cycleAgentsRole:1130` cycles `roles.IDs()` and shows `label + tools count` rather than three names.

## Phase 6.5: Extend the picker and slash menus generically

### 6.5.1 Add generic picker support for new kinds

- [ ] Extend `internal/ui/chat/picker.go` `pickerKindTool = "tool"` and `pickerKindRole = "role"`. Add `openToolsPicker()` and `openRolesPicker()` mirroring `openSkillsPicker:735` - each calls a discovery function under `Effective*` options and returns a `tea.Cmd` that yields `toolCatalogMsg` / `roleCatalogMsg`. `pickerView:49`, `pickerRow:330`, `applyFilter:647`, `pickerSource:790`, `selectPickerItem:522`, `pickerSelectedValue/Label:832` gain two branches that delegate to `toolsCatalog` / `rolesCatalog` filtered lists. Keep the existing string-kind comparisons to avoid a larger picker redesign in this phase.

### 6.5.2 Add slash commands for tools and roles

- [ ] Add `slashCommands` entries `"/tools"` (aliases `["tool"]`) and `"/roles"` (aliases `["role","roles"]`) with `group: "Project"` in `internal/ui/chat/chat.go:430`. Wire `runSlashArg` in `slash.go:168` to `openToolsPicker` / `openRolesPicker`. When the corresponding settings group is disabled, show `"tools disabled in settings"` like the existing `"/skills"` guard.

### 6.5.3 Decouple settings picker target from model-only

- [ ] Extend `settingsPickerTarget` in `settings.go` to support `settingsPickerTool` and `settingsPickerRole` so default-role and per-tool enablement can pick from the same model/variant-style drawer. `selectPickerItem` switch already has a `default: goto normalSelection` branch; new cases map cleanly.

## Phase 6.6: Wire first-request boundaries and carry context to children

### 6.6.1 Add tool and role providers alongside the skill provider

- [ ] Extend `internal/agent/agent.go:151` with `ToolProvider func(ctx, sessionID, userText string)([]ToolContext, error)` and `RoleProvider` analog when roles carry bodies. For this phase, tool and role descriptors are lightweight metadata, so injection may be metadata-only. Keep the same invariant as skills: run once per `Send` after the user row is persisted, before `callModel`, and never for `Continue`, `SendHidden`, compaction, or child sessions.

### 6.6.2 Pass explicit selection into child jobs

- [ ] Extend `subagent/types.go:111 Job.Tools` is already dynamic, so a `/tools`-selected allowlist can be passed via `Job.Tools`. For roles, extend `Job.Role` validation to registry IDs and pass `skills.Context`-style explicit role context when a role descriptor carries prompt text. Child `AgentRunner` receives the explicit contexts and does not rescan global roots, matching `skills:112` behavior.

## Phase 6.7: Tests, docs, knowledge-base, and gates

### 6.7.1 Unit and integration tests

- [ ] Add `internal/provider/catalog_test.go` - `TestProviderRegistryDiscoverMerge` (local wins, global visible, diagnostics for bad JSON), `TestProviderFactoryDelegatesToRegistry`.
- [ ] Add `internal/agent/tools_registry_test.go` - `TestToolRegistryRejectsDuplicate`, `TestToolRegistryEnablesOnlyAllowed`, `TestDiscoveredToolSandboxedByPolicy`.
- [ ] Add `internal/roles/capabilities_test.go` - `TestRoleRegistryAllowsCustomSingleWriter`, `TestRoleNormalizeAgainstRegistry`.
- [ ] Add `internal/ui/chat/picker_test.go` - filtering and cursor stability for `tool` and `role` kinds.
- [ ] Add `internal/ui/chat/slash_test.go` - `/tools` and `/roles` entries appear in palette, disabled guard when settings off.
- [ ] Add `internal/subagent/manager_test.go` - spawning with a custom discovered role uses its `Tools` and `SingleWriter` without touching `roles.Explore`.

### 6.7.2 Documentation

- [ ] Update `docs/architecture.md` - add a "Pluggable catalogs" section describing the shared `Registry + Discover + Diagnostics` pattern, approval roots per domain, and the invariant that discovery never executes descriptor content.
- [ ] Update `docs/providers.md` - document `<workdir>/.lazykoder/providers.json` (and `~/.config/lazykoder/providers.json`) descriptor format and `provider.Register` for compiled providers.
- [ ] Add `docs/tools.md` - document `<workdir>/.lazykoder/tools.json` schema, the `Tool` interface for Go plugins, `AllowDiscovered`, and the policy gate that still applies.
- [ ] Add or update `docs/roles.md` - document `<workdir>/.lazykoder/roles.json` schema, `single_writer`, `model_class`, and how `agents.default_role` now validates against the catalog.
- [ ] Update `docs/tui.md` - document `/tools`, `/roles`, and the filter/enter/esc behavior shared with `/skills` and `/provider`.
- [ ] Update `knowledge-base/` in the same session: `wiki/concepts/tools.md`, new `wiki/concepts/providers.md` or the existing providers concept, new `wiki/concepts/roles.md`, `wiki/architecture/data-flow.md` first-request injection, `wiki/architecture/component-map.md`, `wiki/overview/glossary.md`, and `INDEX.md`.

### 6.7.3 Automated gates

- [ ] Run `go build ./...` (exit 0), `go test ./... -count=1` (exit 0) including `go test ./internal/workspace -run TestInitCreatesEmptyCatalogFiles` (exit 0), `go test -race ./...` for provider/tool/role registries (exit 0), `make lint` (exit 0), `make vet` (exit 0). Record outcomes in this file beside the rows when they run. Keep live TTY evidence (120x36 and 80x24) as a manual follow-up: remove `.lazykoder/*.json`, confirm that `bin/lk init` and then a plain `bin/lk` launch without prior init both recreate `settings.json`, `providers.json`, `tools.json`, `roles.json` with `0600` and `[]` defaults, then add one entry in each catalog file and verify `/provider`, `/tools`, `/roles` filtering, selection, diagnostics, and that `/model` groups by the new provider ID.

## Dependencies

- Existing skills discovery in `internal/skills/catalog.go` and its first-request seam in `internal/agent/agent.go` and `internal/ui/chat/runtime.go` - reuse the bounded-catalog and `Diagnostics` pattern; for providers/tools/roles the local source is `<workdir>/.lazykoder/*.json` with a global mirror under `~/.config/lazykoder/*.json`, not a directory walk.
- Existing TUI drawer family in `internal/ui/chat/picker.go`, `slash.go`, `focus.go`, and `settings.go`.
- Existing settings normalization and persistence in `internal/settings/settings.go` (already owns `.lazykoder/settings.json` and `models.json` handling).
- No new third-party dependency. Use the standard library, existing Bubble Tea components, and `go:embed` only where descriptor defaults are needed. Any new module requires explicit announcement and sign-off per `AGENTS.md`.

## Closure gate

- [ ] A new OpenAI-compatible provider can be added via a declarative entry in `<workdir>/.lazykoder/providers.json` (or the global `~/.config/lazykoder/providers.json`) without editing `internal/provider/catalog.go` or `internal/provider/factory.go`; a full provider with custom wire protocol can be added via `provider.Register` in `init()` with no factory switch change.
- [ ] A new tool can be added via `tools.Register` in `init()` or via a bounded entry in `<workdir>/.lazykoder/tools.json` (or the global mirror) without editing `internal/agent/tools_registry.go` or `internal/agent/tools_exec.go`; its spec is advertised exactly when allowed, and its execution honors `workspace.Resolve` and `policy.ClassifyWithAllowlist` where applicable.
- [ ] A new role can be added via an entry in `<workdir>/.lazykoder/roles.json` (or the global mirror) without editing `internal/roles/capabilities.go`, `internal/tools/task/task.go`, or `internal/subagent/config.go`; its allowlist, single-writer flag, and model class are honored in `Manager.Spawn` and validated at task-parse time.
- [ ] `/provider`, `/tools`, `/roles`, and `/skills` share discovery diagnostics that never block chat, and `/model` groups by `Info.Provider` rather than endpoint heuristics after the cache migration.
- [ ] Automated provider, orchestration, memory, UI, database, build, lint, and test gates pass. Live TTY checks remain explicit human checks.
