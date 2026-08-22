# Skills

LazyKoder can discover local and global skill descriptors and use a bounded,
untrusted copy of a selected skill during the first provider request.

## Discovery roots

The catalog scans these roots when they exist:

- `<workdir>/skills`
- `<workdir>/.agents/skills`
- `LAZYKODER_GLOBAL_SKILLS_DIR` entries
- `$CODEX_HOME/skills`
- `$HOME/.agents/skills`

The scanner does not follow symlink roots or descriptor files. It accepts
`SKILL.md` and the legacy `SKILLS.md`, prefers `SKILL.md` when both exist,
limits directory depth and descriptor size, and returns diagnostics without
blocking a chat when one root is unavailable.

## `/skills`

Type `/skills` or `/skill` to open the same drawer family used by `/model`.
The drawer labels local and global entries with their display paths. `/`
filters names, descriptions, triggers, and paths. Enter activates one skill
for the next ordinary parent request; escape cancels. The body is read only
when the request is prepared.

The settings card exposes the discovery toggles and automatic-match limit.
They are persisted under `skills` in `.lazykoder/settings.json`:

```json
"skills": {
  "enabled": true,
  "auto_detect": true,
  "include_local": true,
  "include_global": true,
  "remember": true,
  "max_auto_matches": 2
}
```

Automatic matching is parent-only. A local skill wins a global duplicate for
automatic use, while the drawer keeps both entries visible. Automatic and
explicit contexts share bounded body and combined-context limits.

## Request boundary and trust

After the user message is persisted, `internal/agent` emits skill scan events
and prepares the selected contexts before the first ordinary provider call.
The wire-only block follows project instructions and historical recall. It is
marked untrusted and never enters SQLite, recap files, tool definitions, or
the normal transcript. Tool follow-ups, `/continue`, compaction, children,
and hidden workers do not rescan.

The TUI uses a distinct `scanning approved skills` activity marker. Discovery
errors are nonfatal. Selected contexts are copied to direct child agents;
children do not perform a global scan themselves.

## Memory references

When `skills.remember` is enabled, successful parent turns pass code-owned
skill metadata to the memory worker. `knowledge-base/memories.md` is migrated
from format version 1 to version 2 and gains a `Skills` section before the
source ledger. Each JSON entry stores the stable ID, name, scope, reusable
path, content hash, use count, last-used time, and source message IDs. Bodies
are never stored there.
