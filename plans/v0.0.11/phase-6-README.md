# Phase 6 usage and validation

This guide covers the pluggable providers, tools, and child roles added in
[the phase 6 checklist](phase-6-pluggable-providers-tools-roles.md). It shows
how to create the project files, add declarative entries, use the TUI pickers,
and run the validation gates.

For the complete field reference, see [providers](../../docs/providers.md),
[tools](../../docs/tools.md), and [roles](../../docs/roles.md).

## Build and initialize a test workspace

Build the current binary from the repository root.

```sh
repo_dir="$(pwd)"
make build
test_dir="$(mktemp -d)"
(cd "$test_dir" && "$repo_dir/bin/lk" init)
```

The `init` command creates these files when they do not exist:

```text
$test_dir/.lazykoder/settings.json
$test_dir/.lazykoder/providers.json
$test_dir/.lazykoder/tools.json
$test_dir/.lazykoder/roles.json
```

The three catalog files contain `[]`. The four JSON files use mode `0600`.
The command also creates the normal project database and `.gitignore` entry.
It does not start the TUI.

Check the bootstrap result before adding catalog entries.

```sh
for name in settings providers tools roles; do
  test -f "$test_dir/.lazykoder/$name.json"
  test "$(stat -c '%a' "$test_dir/.lazykoder/$name.json")" = 600
done

for name in providers tools roles; do
  test "$(tr -d '\n' < "$test_dir/.lazykoder/$name.json")" = '[]'
done
```

`bin/lk init` and the normal application startup use the same bootstrap path.
Run the application from a real terminal when you need to inspect the TUI.

```sh
cd "$test_dir"
"$repo_dir/bin/lk"
```

## Configure the catalog locations

Project catalogs live under `<workdir>/.lazykoder/`:

| File | Contents |
| --- | --- |
| `providers.json` | OpenAI-compatible provider descriptors |
| `tools.json` | Shell-backed tool descriptors |
| `roles.json` | Child role descriptors |

The global catalog directory is the operating system user config directory
under `lazykoder`. On Linux, the default is `~/.config/lazykoder/`. Set
`LAZYKODER_GLOBAL_CONFIG_DIR` to use another directory during tests or local
development.

Each catalog merges its project and global entries. A project entry with the
same provider ID, tool name, or role ID replaces the global entry. Missing
files are treated as empty catalogs. Invalid entries produce diagnostics while
valid entries from the same file remain available.

## Add a provider

Use a declarative provider for an OpenAI-compatible HTTP endpoint. Edit
`$test_dir/.lazykoder/providers.json` and add a JSON array like this one:

```json
[
  {
    "id": "together",
    "label": "Together",
    "auth_method": "api_key",
    "env_key": "TOGETHER_API_KEY",
    "base_url": "https://api.together.xyz/v1",
    "model": "meta-llama/Llama-3.3-70B-Instruct",
    "supported": true
  }
]
```

Set the key in the environment before starting the application.

```sh
export TOGETHER_API_KEY="your-key"
```

The descriptor supports these fields:

- `id` and `label` identify the provider.
- `auth_method` is `api_key`, `codex`, or `grok`.
- `env_key` or `env_keys` names the environment variable that supplies an API key.
- `base_url` and `model` configure an OpenAI-compatible API route.
- `cli` names the provider CLI for the `codex` and `grok` auth methods.
- `supported`, `display_order`, and `aliases` control catalog display and migration.

Select the provider in the TUI:

1. Start `bin/lk` from the test workspace.
2. Enter `/provider`.
3. Filter for `Together`.
4. Press Enter to select it.
5. Enter `/model` to inspect model rows for the selected provider.

The provider picker reports the key state. The application never writes the
API key to `settings.json`, a catalog file, logs, SQLite, or the TUI.

A provider with a custom wire protocol must be compiled into a Go package. It
registers one descriptor during package initialization and supplies a factory:

```go
func init() {
	_ = provider.Register(provider.Descriptor{
		ID: "company", Label: "Company",
		AuthMethod: provider.AuthMethodAPIKey,
		EnvKey: "COMPANY_API_KEY",
		Factory: newCompanyClient,
	})
}
```

The compiled provider uses the shared `provider.Client` contract. It does not
need a change to the provider catalog or factory switch.

## Add a discovered tool

First enable discovered tools in
`$test_dir/.lazykoder/settings.json`:

```json
{
  "tools": {
    "allow_discovered": true,
    "max_discovered": 32
  }
}
```

Merge these fields into the existing settings object. Do not replace unrelated
settings that you already configured.

Then add a descriptor to `$test_dir/.lazykoder/tools.json`:

```json
[
  {
    "name": "format-go",
    "description": "Format one Go file",
    "parameters": {
      "type": "object",
      "properties": {
        "file": {"type": "string"}
      },
      "required": ["file"]
    },
    "command": "gofmt -w {file}",
    "binaries": ["gofmt"]
  }
]
```

Restart the application after changing the settings file. Then use the picker:

1. Enter `/tools`.
2. Filter for `format-go`.
3. Press Enter to toggle the tool on.
4. Press Escape to close the picker.

The eight built-in tool IDs are `bash`, `read`, `write`, `edit`, `grep`,
`webfetch`, `question`, and `todowrite`. The `tools.enabled` map controls which
registered tools the model can see. A discovered descriptor is not executed
while the catalog loads.

At execution time, lazykoder shell-quotes descriptor arguments, checks the
command against the descriptor's `binaries` allowlist, applies the normal
policy decision, and confines the working directory to the workspace. A
missing binary or failed command returns an error tool result.

A compiled tool implements `toolplugin.Tool` and registers through
`tools.Register`. The registration name must match `Spec().Name`:

```go
func init() {
	_ = tools.Register("company-check", companyCheck{})
}
```

## Add a child role

Add a role descriptor to `$test_dir/.lazykoder/roles.json`:

```json
[
  {
    "id": "reviewer",
    "label": "Reviewer",
    "tools": ["read", "grep"],
    "single_writer": true,
    "model_class": "flash",
    "prompt": "Review the requested code and report evidence."
  }
]
```

Use the role picker:

1. Enter `/roles`.
2. Filter for `reviewer`.
3. Press Enter to set it as the default child role.
4. Press Escape to close the picker.

`tools` becomes the child agent's allowlist. `single_writer` serializes jobs
for that role when parallel writers are disabled. `model_class` supplies the
fallback model class when the task does not choose a model. Set the same value
through the `agents.default_role` setting if you prefer to edit JSON.

The built-in roles are `explore`, `plan`, and `general`. An unknown default
role falls back to `explore`. Task arguments that name an unknown role fail with
the registered role IDs in the error.

A compiled role registers one descriptor:

```go
func init() {
	_ = roles.Register(roles.Role{
		ID: "reviewer", Label: "Reviewer",
		Tools: []string{"read", "grep"},
		SingleWriter: true, DefaultModelClass: "flash",
	})
}
```

## Run the phase 6 validation

Run the full gate from the repository root:

```sh
./scripts/verify-phase6.sh
```

The script runs these checks in order:

1. `go build ./...`
2. `go test ./... -count=1`
3. The workspace catalog bootstrap test
4. `go test -race ./...`
5. `make lint`
6. `make vet`

For a catalog-focused pass, run the package tests before the full gate:

```sh
go test ./internal/catalog ./internal/provider ./internal/tools ./internal/roles ./internal/agent/toolplugin ./internal/subagent ./internal/ui/chat -count=1
```

## Check the TUI at both supported sizes

Use a real terminal session. Do not pipe the binary, redirect its input, or
run it under `timeout`; Bubble Tea needs a TTY.

Build the binary, start a 120 by 36 tmux session, and inspect the tools picker.

```sh
repo_dir="$(pwd)"
make build
tmux new-session -d -s lazykoder-phase6 -x 120 -y 36 "cd '$test_dir' && '$repo_dir/bin/lk'"
sleep 3
tmux send-keys -t lazykoder-phase6 '/tools' Enter
sleep 1
tmux capture-pane -p -t lazykoder-phase6
```

The captured pane must show the tools picker and the enabled built-in tools.
Repeat the check for roles, then close the session:

```sh
tmux send-keys -t lazykoder-phase6 Escape
tmux send-keys -t lazykoder-phase6 '/roles' Enter
sleep 1
tmux capture-pane -p -t lazykoder-phase6
tmux kill-session -t lazykoder-phase6
```

Repeat both picker checks with an 80 by 24 session:

```sh
tmux new-session -d -s lazykoder-phase6-small -x 80 -y 24 "cd '$test_dir' && '$repo_dir/bin/lk'"
sleep 3
tmux send-keys -t lazykoder-phase6-small '/tools' Enter
sleep 1
tmux capture-pane -p -t lazykoder-phase6-small
tmux send-keys -t lazykoder-phase6-small Escape
tmux send-keys -t lazykoder-phase6-small '/roles' Enter
sleep 1
tmux capture-pane -p -t lazykoder-phase6-small
tmux kill-session -t lazykoder-phase6-small
```

Check that the picker title, entries, filter hint, and footer remain readable
at both sizes. A catalog diagnostic must not prevent the chat screen from
opening.

## Troubleshoot a missing entry

- If a provider reports a missing key, export the variable named by `env_key` or `env_keys` before starting the application.
- If a discovered tool does not appear, set `tools.allow_discovered` to `true`, check the JSON array, and run `/tools` again.
- If a role does not appear, check its `id`, restart the application, and run `/roles` again.
- If one entry is malformed, read the diagnostic shown by the picker and check the remaining valid entries. A bad entry does not hide its valid siblings.
- If a catalog file is a symlink, move the descriptor into the approved project or global directory as a regular file.

The catalog loaders do not scan top-level `providers`, `tools`, or `roles`
directories. Declarative JSON can describe an OpenAI-compatible provider or a
shell-backed tool. It cannot add a new wire protocol without compiled Go code.
