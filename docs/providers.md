# Providers

lazykoder stores the selected parent provider in
`.lazykoder/settings.json`. It never stores API keys, OAuth tokens, device
codes, or refresh tokens. API-key providers read from the process environment
or the project `.env` file, with the process environment taking precedence.

Codex and Grok use their official installed CLIs for subscription sign-in and
inference. Their CLIs retain the persistent session in their own user-level
storage. lazykoder only checks whether that session is usable.

## Authentication and routing

| Provider | Authentication | Inference route | Default model |
| --- | --- | --- | --- |
| OpenCode | `OPENCODE_API_KEY` or `OPENCODE_ZEN_API_KEY` | OpenCode Go chat-completions or Responses API, selected per catalog route | `deepseek-v4-flash` |
| OpenAI | `OPENAI_API_KEY` | OpenAI chat-completions API | `gpt-4.1-mini` |
| Codex | `codex login` with ChatGPT sign-in | authenticated `codex` CLI | `gpt-5.6-luna` with `low` reasoning when the signed-in catalog supports it, otherwise the account default |
| Grok | `grok login --device-auth` | authenticated `grok` CLI | `grok-4.6` |
| xAI | `XAI_API_KEY` | xAI chat-completions API | `grok-4.6` |

OpenCode, OpenAI, and xAI send their configured API key as a Bearer token to
their documented HTTP endpoint. Codex and Grok do not read `OPENAI_API_KEY` or
`XAI_API_KEY` and do not copy CLI-managed credentials into lazykoder.

The `xAI` option is the direct API-key integration. It is separate from the
Grok subscription option, even though both may use Grok models.

Set the keys before starting lazykoder, either in the shell or in `.env`:

```text
OPENCODE_API_KEY=...
OPENAI_API_KEY=...
XAI_API_KEY=...
```

Keys are never written to settings, logs, the database, or the TUI.

## Sign in with a subscription

Open `/provider` and select Codex or Grok.

- Selecting Codex when signed out runs `codex login`. The official Codex CLI
  opens its browser sign-in flow, where you use the ChatGPT account with your
  subscription.
- Selecting Grok when signed out runs `grok login --device-auth`. The CLI
  prints an xAI device URL and a short user code. Open the printed URL in a
  browser, complete the sign-in, then return to lazykoder.

The provider drawer shows `checking sign-in`, `signed in`, `sign in required`,
or `CLI missing` for subscription providers. It does not treat an unverified
local value as an available account.

Install the required official CLI before selecting a subscription provider:

```text
codex --version
grok --version
```

The provider CLI persists the refreshable login session. Restarting lazykoder
does not require another sign-in unless the provider session has expired or
was signed out elsewhere.

At startup, provider selection, and `/refresh`, lazykoder opens the installed
`codex app-server`, completes its initialization handshake, and reads
`model/list` from the signed-in ChatGPT account. If the catalog offers
`gpt-5.6-luna` with `low` reasoning, that is the Codex default. Otherwise
lazykoder uses the account default. The returned catalog drives `/model` and
`/variant`. This reads metadata only and does not start a model turn. Codex
skips the normal 15-minute model-cache fast path so a new launch checks the
current account catalog. A stale cache is used only if that read fails.

`/model` combines available OpenCode and Codex rows in one drawer. The rows
sit under `OpenCode` and `Codex` headings while their detailed provider labels
stay on the right. Selecting a row from the other heading changes the parent
provider and client before the next turn. The selected provider, model, and
reasoning variant are written to the current session. Codex receives the
selected variant as `model_reasoning_effort` for its CLI turn.

If both catalogs advertise the same model ID, both rows remain visible. The
provider and model ID together identify a catalog row, so choosing the
OpenCode row uses the OpenCode route and choosing the Codex row uses the
authenticated Codex route. Normal and keypad arrow events move through the
grouped rows without treating the key as prompt text. Resuming a session also
restores the provider recorded with that session before restoring its model,
so a Codex Luna session cannot resume through the OpenCode client.

## Select the parent provider

Run `/provider` to open the provider drawer. Each row reports both states:

- `selected` or `not selected` for the persisted parent provider.
- `key set` or `key missing` for API-key providers.
- `checking sign-in`, `signed in`, `sign in required`, or `CLI missing` for
  Codex and Grok.

The drawer does not label a provider as available based only on a local value.
Selecting a signed-out subscription provider opens the provider-owned login
flow. Selecting an API-key provider with no key saves the choice and reports
the required variable.

The selected parent provider is saved as `provider.active`. OpenCode is the
default. A provider change updates the parent client, model catalog, memory
workers, and retry policy for the next turn.

## Provider execution boundary

The parent and child agents continue to use lazykoder's transcript, SQLite
store, policy checks, confirmation prompts, and tool loop. The Codex and Grok
adapters send the current transcript to their authenticated CLI with a strict
structured-output contract. Tool arguments are JSON-encoded strings in that
contract, which satisfies Codex's strict schema validator without tying the
request to a model name. The response parser also accepts the older object
shape. A subscription model can request only tools that lazykoder advertised.
lazykoder validates each requested tool and executes it through the existing
Allow, Ask, or Deny policy.

The adapters do not hand the workspace or lazykoder's tools to the provider
CLI. Codex runs with a read-only sandbox. Grok runs with its built-in tools,
subagents, web search, memory, and plan mode disabled. Neither adapter copies
or logs the session credential.

Subscription models currently return the structured result as a completed
response rather than forwarding token-level CLI output. The normal busy state,
tool cards, confirmations, and final transcript entry still behave as usual.

## Parent and sub-agent separation

The parent and sub-agent paths are independent:

- `model.default` is the parent model.
- `agents.model_override` is the common child model override.
- `agents.explore_model` overrides the explore role.
- `orchestrator.provider` selects the child provider and defaults to
  `opencode`.

This lets a parent use a Codex model while children use an OpenCode or Grok
model. The hidden orchestrator plan call stays on the parent client. Child
task calls use the child client and child model settings. The common child
model override wins over a planner-provided `model_class`; the explore model
also wins for explore-role tasks. A planner class is used only when the
settings leave the relevant model unset. When providers expose the same model
ID, child model profiles are filtered by the configured child provider before a
job starts.

`/provider` changes the parent provider. The child provider can be set in
`.lazykoder/settings.json`:

```json
{
  "provider": { "active": "codex" },
  "orchestrator": { "provider": "grok" },
  "agents": { "explore_model": "grok-4.6" }
}
```

Partial settings files are normalized on load. Unknown provider IDs fall back
to OpenCode.

## Adding a provider

Use the existing shared `provider.Client` interface. A new provider needs:

1. A catalog entry in `internal/provider/catalog.go` with its ID, label,
   authentication method, default model, and any API endpoint or CLI name.
2. A branch in `internal/provider/factory.go` that creates the client.
3. A client that implements `provider.Client`. HTTP providers can reuse the
   OpenAI-compatible wire types. Subscription providers must keep credentials
   in the official client and expose no token values.
4. Settings normalization coverage in `internal/settings`.
5. Picker coverage and a request test using a local fake server or injected
   command runner. Do not use a live key or subscription in tests.

Keep authentication, model defaults, endpoints, and usage behavior behind the
provider client. The agent, orchestration, recap, memory, and UI should depend
on the shared interface rather than provider-specific credential details.
