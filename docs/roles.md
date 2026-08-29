# Roles

Roles describe the tools, model class, and writer policy for a child agent.
The built-in roles are `explore`, `plan`, and `general`. The role registry also
accepts compiled registrations and project or global JSON descriptors.

## Declarative roles

Add a JSON array to `<workdir>/.lazykoder/roles.json`:

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

The global mirror is `~/.config/lazykoder/roles.json`; a local ID wins over a
global ID. Files and entries are bounded, symlinks are rejected, and malformed
entries become diagnostics while valid siblings remain available. Role prompt
text is metadata supplied to the request boundary. It is not executable.

`single_writer` serializes children with that role when parallel writers are
disabled. `model_class` is the fallback class used when the task does not name
a model. The `tools` list becomes the child agent's explicit allowlist.

## Selection and settings

`/roles` opens the shared picker. Type a filter, press Enter to select the
default child role, or press Escape to close it. The `/settings` default-role
row uses the same picker and cycles the registered IDs with the arrow keys.
`agents.default_role` is normalized against the live role registry and falls
back to `explore` when it names an unavailable role.

## Compiled roles

A Go extension registers one descriptor from `init`:

```go
func init() {
    _ = roles.Register(roles.Role{
        ID: "reviewer", Label: "Reviewer", Tools: []string{"read", "grep"},
        SingleWriter: true, DefaultModelClass: "flash",
    })
}
```

No switch in `internal/tools/task`, `internal/subagent`, or the role catalog is
needed for a new role.
