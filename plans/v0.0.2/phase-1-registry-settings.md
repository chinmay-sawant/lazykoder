# Phase 1 - Tool registry and agents settings

> **Parent:** `plans/v0.0.2/README.md`
> **Status:** done

## 1.1 Tool registry

- [x] Extract tool specs + advertise helper in `internal/agent` (`tools_registry.go`)
- [x] `runSteps` advertises tools from allowlist, not only bash
- [x] `executeTool` still hard-denies unknown names
- [x] Unit test: `TestAdvertiseBaseTools` (`go test ./internal/agent -count=1` exit 0)

## 1.2 Settings

- [x] `settings.Agents` block in `.lazykoder/settings.json`
- [x] Defaults: enabled, max_concurrent=4 (clamp 1-20), max_depth=1, child_max_steps=12, timeout 600s
- [x] Load/save/normalize tests (`go test ./internal/settings -count=1` exit 0)
