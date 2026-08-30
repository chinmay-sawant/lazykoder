# Prompt customization

lazykoder seeds editable prompt files under `<workdir>/.lazykoder/prompts/`
when the workspace starts. Existing files are never overwritten, so users can
customize the assistant without changing Go source.

The directory contains Markdown instruction files and JSON schemas for the
built-in tools. The main groups are:

| Path | Controls |
| --- | --- |
| `compact.md` | conversation handoff summaries |
| `agent/` | project-instruction, memory, recall, skill, and checkpoint headers |
| `orchestrator/` | decomposition requests and plan guidance |
| `recap/` | recap selection and hidden recap or memory workers |
| `provider/` | the subscription-provider transcript wrapper |
| `subagent/` | child report, resume, and retry guidance |
| `ui/commit-action.md` | the commit-and-push hidden task |
| `tools/*.json` | descriptions and JSON schemas advertised to the model |

Prompt files are resolved per workspace. The file is read from the project
directory first, then the embedded default is used when the file is missing,
empty, or invalid. This fallback keeps a bad customization from removing a
required safety boundary.

Tool JSON files must keep the filename and `name` aligned. They must contain a
non-empty `description` and an object-valued `parameters` schema. Keep schema
properties compatible with the Go handler that executes the tool. Changing a
description is safe; changing required fields or property names can make model
tool calls fail validation or execution.

The planner template uses `{{.RoleIDs}}`, `{{.MaxSubtasks}}`, and `{{.Task}}`.
The subscription wrapper uses `{{.Transcript}}`, and the fallback sub-agent
retry template uses `{{.Error}}`. Other files are plain Markdown. Templates
are rendered with data supplied by lazykoder and have no filesystem or process
access.

The prompt directory is created by both normal startup and the explicit
workspace initializer. It is inside `.lazykoder/`, which is normally ignored
by Git and is not intended to be published as project source.
