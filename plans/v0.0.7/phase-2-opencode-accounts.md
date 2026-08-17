# v0.0.7 / Phase 2 - OpenCode account profiles

> **Parent:** `plans/v0.0.7/README.md`
> **Status:** planned
> **Priority:** P1
> **Provider scope:** OpenCode only
> **Reason:** users may have more than one OpenCode account or plan, each with
> a separate environment key, and need an explicit active selection

## Scan findings

The current application has one credential and one client for the whole
process:

| Concern | Current implementation | Consequence |
| --- | --- | --- |
| Key resolution | `main.go` calls `opencode.APIKeyFromEnv()` before loading settings | only the two legacy environment names can be selected |
| Client lifetime | `main.go` constructs one `*opencode.Client`; `chat.Model` and sub-agents share it | changing an account needs an explicit idle-time client rebuild |
| Settings shape | `.lazykoder/settings.json` contains `slot`, `model`, and `agents` | no dynamic account metadata, enabled state, or active selection |
| Settings UI | `internal/ui/chat/settings.go` uses a fixed row enum | account rows need a nested dynamic drawer, not hard-coded rows |
| Session identity | sessions persist model and variant but no OpenCode account id | resume could silently use a different account after a settings change |
| Model cache | `.lazykoder/models.json` is workspace-wide | a model list or price catalog can be stale for the selected account |
| Environment loader | `internal/envfile` loads arbitrary keys with process-env precedence | it can already support profile-specific names without storing secrets |
| Provider requests | `opencode.Client` sends one bearer key to chat, models, free-models, and usage endpoints | all provider calls must resolve from the active profile |

## Product contract

An OpenCode profile is project metadata, not a stored credential. It has:

| Field | Meaning |
| --- | --- |
| `id` | stable local identifier used by settings, sessions, and cache state |
| `name` | short label shown in the settings card and status context |
| `description` | optional user-authored account or plan note |
| `env_var` | environment variable name containing the secret, for example `OPENCODE_PERSONAL_KEY` |
| `enabled` | whether the profile is selectable and eligible to become active |

Settings hold an ordered `opencode.profiles` collection and an
`opencode.active_profile` id. They never hold the value of `env_var`.

The resolver uses the selected profile's environment name. For backwards
compatibility, a configuration with no profiles creates or resolves an
implicit default profile using `OPENCODE_API_KEY`, with
`OPENCODE_ZEN_API_KEY` as its existing fallback. Process environment values
continue to win over `.env` values.

## Settings experience

The existing `/settings` card gets one `opencode accounts` row. Enter opens a
dynamic account drawer with rows showing enabled state, name, description,
environment-variable name, and the active marker.

The drawer supports:

- `enter` to make an enabled profile active;
- `space` to enable or disable a profile;
- `a` to add a profile;
- `e` to edit name, description, or environment-variable name;
- `d` to delete a profile after confirmation;
- arrows or `j`/`k` to move, and `esc` to return to the main settings card.

The active profile label is also shown in the status drawer. Missing or empty
keys are reported by profile name and environment-variable name without
rendering a secret value.

## Runtime design

1. Load `.env` and project settings before constructing the OpenCode client.
2. Resolve one active profile into a short-lived runtime credential and client.
3. Pass the same resolved client to the parent agent and sub-agent runner.
4. Permit profile changes only while the parent is idle and no child job is
   running. Rebuild the client and sub-agent runtime after a change.
5. Refresh model discovery after switching profiles. Do not reuse a model list
   loaded for another profile.
6. Stamp new sessions and child sessions with the active profile id. On resume,
   use the recorded profile when it still exists and is enabled; otherwise
   show a clear selection error instead of silently switching credentials.

The client interface stays small: it still receives an API key and endpoint,
while profile resolution remains in the OpenCode/settings seam. No provider
abstraction is added for this phase because only OpenCode varies today.

## Persistence and migration

- Extend settings normalization with a default legacy profile and an active
  profile fallback.
- Add a database migration for the session profile id, preserving existing
  sessions as legacy-default sessions.
- Scope or invalidate `models.json` by profile id so model, endpoint, pricing,
  and variant metadata cannot cross account switches.
- Do not persist API keys, `.env` contents, or authorization headers.

## Implementation gates

| Gate | Evidence required | Status |
| --- | --- | --- |
| Profile persistence | settings round-trip, normalization, legacy fallback tests | planned |
| Secret handling | tests prove only env-var names are serialized and missing keys are readable | planned |
| Runtime switching | idle switch rebuilds parent and child clients; busy switch is rejected | planned |
| Session safety | resume selects the recorded profile or reports a visible error | planned |
| Model cache safety | cache is profile-scoped or invalidated on switch | planned |
| Settings UX | dynamic add/edit/enable/disable/select flow works at 120x36 and 80x24 | planned |
| Provider coverage | chat, models, free models, and usage use the selected key | planned |
| Regression gate | `go test ./...`, `go build ./...`, `go vet ./...`, and lint findings recorded | planned |

## Out of scope

- OpenAI, Anthropic, Gemini, or other provider account profiles.
- Storing secrets in `.lazykoder/settings.json`.
- Importing or synchronizing OpenCode's global account database.
- Automatic account rotation, quota balancing, or failover.
- Editing `.env` values from the TUI. The TUI edits only profile metadata and
  the environment-variable name.
