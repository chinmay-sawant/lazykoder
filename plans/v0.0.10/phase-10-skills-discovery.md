# v0.0.10 / Phase 10 - Global and local skill discovery

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** implementation landed; automated gates in progress; live TTY evidence open
> **Estimated effort:** 3-5 days
> **Priority:** P1
> **Gate:** `/skills` lists valid project and global skills, a selected or
> automatically detected skill reaches the first ordinary provider request
> within a bounded untrusted context block, and the durable memory document
> records the skill name, scope, and reusable path.

## Overview

Add a read-only skill catalog to lazykoder. The catalog must discover skills
from the current project and from the configured global skills roots. The
`/skills` command exposes the catalog, while ordinary parent requests can
select a small number of relevant skills from the same metadata index.

Skill files are instructions, not executable tools. The scanner never runs a
skill, follows arbitrary paths from a model response, or gives a skill file
permission to bypass the system prompt, project instructions, policy gate, or
user confirmation.

The existing memory worker gains a code-owned `Skills` section. It records
validated references to skills that the user selected or that the runtime
applied. It stores the path needed for a later lookup without storing the full
skill body in the transcript or in `memories.md`.

## Product decisions

- `/skills` is available from the parent TUI and accepts an optional name or
  keyword query. `/skill` is an alias.
- Project-local roots are `<workdir>/skills` and
  `<workdir>/.agents/skills`. The project `skills/` root takes precedence for
  automatic selection when the same skill name appears in both local roots.
- Global roots are selected in this order: `LAZYKODER_GLOBAL_SKILLS_DIR` when
  set, `$CODEX_HOME/skills` when `CODEX_HOME` is set, and
  `$HOME/.agents/skills`. Missing roots are ignored. The catalog labels every
  result as `local` or `global` and keeps duplicate entries visible in the
  manual picker.
- A skill directory is valid when it contains `SKILL.md` or the existing
  plural `SKILLS.md`. The parser reads bounded front matter for `name`,
  `description`, and optional `triggers`; it derives a stable name from the
  directory only when the file has no name.
- Local skills override global skills with the same normalized name for
  automatic selection. `/skills` still shows both entries and their source
  paths so the user can choose the global copy explicitly.
- Discovery and manual listing work without recap artifacts. Skill references
  are persisted through the memory worker when skill remembering is enabled;
  the detailed recap threshold never controls skill selection.
- Auto-detection is bounded and conservative. It uses the existing safe prompt
  tokenization rules against names, descriptions, headings, and explicit
  triggers. It never turns raw user text into a regular expression.
- Explicit selection wins over automatic matches. The runtime injects at most
  two automatically selected skills and at most three explicitly selected
  skills, subject to a total skill-context cap. A later provider step, tool
  follow-up, `/continue`, compaction retry, and child request do not rescan.
- Skill content is sent as a wire-only block labeled as untrusted skill
  guidance. It is not written to `messages`, `parts`, recap artifacts, or the
  memory aggregate. The path and metadata remain code-owned.
- The memory renderer writes the new `Skills` section after `Recent context`
  and before `Source ledger`. Existing format version 1 files parse and
  rewrite as version 2 without losing their current facts.

## Phase 10.1: Define discovery roots and the catalog contract

### 10.1.1 Resolve the roots safely

- [x] Add a focused `internal/skills` package with a root resolver that
      expands the local project roots and the configured global roots above.
      Keep environment lookup at this boundary and pass resolved roots into
      pure catalog functions.
- [~] Reject empty, non-directory, unreadable, and duplicate roots. Do not
      scan the whole home directory, follow directory symlinks, or accept a
      root supplied by the model. Keep the global root allowlist explicit.
- [~] Bound directory depth, descriptor size, total descriptors, and scan
      time. Return a partial catalog plus per-root diagnostics when one root
      fails so an unavailable global directory does not hide local skills.

### 10.1.2 Parse one canonical skill descriptor

- [x] Define `Skill`, `SkillScope`, `SkillRoot`, and `SkillDiagnostic` types
      with a canonical path, display path, name, description, triggers,
      source scope, content hash, and descriptor path.
- [x] Accept `SKILL.md` and `SKILLS.md` at each bounded skill directory.
      Prefer `SKILL.md` when both exist and record a duplicate diagnostic.
- [x] Parse only the front matter and bounded headings needed for discovery.
      Keep the complete body available through an explicit read method with a
      separate byte cap. Reject malformed metadata without executing or
      silently repairing the descriptor.
- [x] Normalize names, triggers, and paths for comparison while preserving a
      human-readable path for `/skills` and the memory file. Use a stable
      identity based on scope, canonical path, and content hash.

### 10.1.3 Index, rank, and deduplicate

- [x] Build a deterministic catalog sorted by normalized name, scope, and
      display path. Preserve local-before-global precedence for automatic
      matching and retain shadowed entries for manual selection.
- [x] Implement bounded query matching for exact name, prefix, trigger phrase,
      description terms, and heading terms. Return scores and reasons so the
      UI and tests can explain why a skill matched.
- [~] Add tests for both descriptor filenames, missing and malformed front
      matter, duplicate names, local-over-global precedence, root failures,
      symlink rejection, path normalization, deterministic order, caps, and
      content hashes.

## Phase 10.2: Add `/skills` discovery and activation

### 10.2.1 Wire the slash command

- [x] Add `/skills` and `/skill` to `slashCommands`, the grouped palette, help
      text, keymap documentation, and slash-menu tests.
- [~] Route `/skills` through the same picker or drawer interaction family as
      `/model` and `/agents`. Rescan the catalog when the view opens and offer
      a bounded refresh action for changed skill files.
- [x] Support `/skills <query>` by filtering the catalog without sending a
      provider request. Show separate local and global labels, the descriptor
      path, the description, a shadowed marker, and scan diagnostics.

### 10.2.2 Make activation explicit and reversible

- [~] Let the user select one or more skills with the existing arrow, enter,
      escape, and mouse behavior. Entering a skill opens a read-only detail
      view; an explicit activate action queues the skill for the next ordinary
      parent request.
- [x] Keep activation state in the chat model only until the next eligible
      request. Clear it after injection, cancellation, session change, or
      disabling skills. Never write activation state to the chat transcript.
- [~] Make malformed or unreadable skills visible in the picker while keeping
      them unavailable for activation. A scan error must not block normal chat.
- [~] Add keyboard, mouse, filtering, duplicate-source, empty-catalog, and
      narrow-terminal tests. Verify the full-screen view at 120x36 and 80x24.

## Phase 10.3: Persist skill settings and enforce context budgets

### 10.3.1 Extend project settings

- [x] Add a `Skills` settings group under `.lazykoder/settings.json` with
      `enabled`, `auto_detect`, `include_local`, `include_global`,
      `remember`, `max_auto_matches`, and bounded content limits. Normalize
      missing or invalid values to safe defaults without changing existing
      recap or agent settings.
- [x] Add settings-card rows for discovery, automatic selection, source
      scopes, and remembering references. Use the same model-drawer-like
      selection behavior and persistence tests as the existing recap rows.
- [x] Keep discovery available when auto-detection is off. Turning skills off
      suppresses `/skills` activation and provider injection but does not
      delete the existing `Skills` memory entries.

### 10.3.2 Bound content before it reaches the provider

- [x] Cap the number of automatic and explicit skills, each descriptor body,
      the combined skill context, and the number of metadata terms. Truncate
      at rune boundaries and include an explicit truncation note.
- [~] Reject binary or secret-like skill content before injection. Treat
      skill paths and descriptions as untrusted data. Keep the model's safety,
      project instructions, and user request higher priority than skill text.
- [~] Add unit tests for total and per-skill caps, invalid UTF-8, secret-like
      content, and a missing descriptor during activation.

## Phase 10.4: Auto-detect skills before the first provider request

### 10.4.1 Share the first-request boundary with recall

- [x] Add a skill provider seam to `internal/agent.Options` alongside recall.
      Prepare it after the parent user message is persisted and before the
      first ordinary `Chat` or `ChatStream` call.
- [x] Keep lookup order deterministic: explicit activated skills first,
      local automatic matches next, and global automatic matches last. Do not
      run the catalog for tool-result follow-ups, `/continue`, compaction,
      child sessions, or hidden recap and memory workers.
- [x] Inject one wire-only block after project instructions and historical
      recall hints, with stable headers, scope labels, display paths, match
      reasons, and bounded bodies. Clear the block after the first ordinary
      response and reuse it for one context-overflow retry.
- [x] Emit scan-start and scan-finished events or equivalent UI messages so
      the TUI shows a distinct `scanning skills` status. Lookup errors remain
      nonfatal and produce no empty or misleading provider block.

### 10.4.2 Test keyword detection and request ordering

- [~] Reuse the recall tokenizer and stop-word policy for skill terms. Add
      tests for exact names, prefixes, trigger phrases, descriptions,
      punctuation, short prompts, no matches, duplicate names, and explicit
      selection overriding a global automatic match.
- [x] Add agent tests proving that skill scanning happens once after the user
      row is persisted, before the first provider request, and never for tool
      follow-ups, `/continue`, compaction, or child agents.
- [~] Add tests proving skill text is absent from SQLite history, recap files,
      memory source text, tool definitions, and task-tool responses.

## Phase 10.5: Add the memory `Skills` section

### 10.5.1 Extend the application-owned document

- [~] Add a typed `MemorySkillReference` with stable ID, state, name, scope,
      display path, descriptor path, description, trigger terms, content hash,
      first-seen UTC, last-detected UTC, last-used UTC, and source message IDs.
      Keep paths and hashes code-owned rather than model-authored.
- [x] Add the `Skills` heading and deterministic entry renderer to
      `internal/recap/memory.go`. Store local paths relative to the workdir
      and global paths relative to a named global root or with a normalized
      home prefix so the file remains portable and useful.
- [x] Bump the memory document format to version 2. Parse version 1 files,
      retain all existing sections, and write the new section only after a
      successful validated update. Keep the 64 KiB aggregate cap and add a
      skill-entry count cap.
- [x] Keep the model envelope unchanged for skill paths. Merge validated
      runtime skill-use records after model facts are validated. Mark removed
      or changed skills superseded instead of allowing a model to delete a
      reference silently.

### 10.5.2 Connect skill use to the memory worker

- [x] Add a bounded skill-use payload to the successful parent turn's memory
      update input. Record only skills that were explicitly activated or
      automatically injected, not every descriptor found during a scan.
- [x] Reuse the existing idempotent `memory_updates` source anchor. A replayed
      completion must not duplicate a skill reference or rewrite the file
      with a different order.
- [x] Define the setting boundary in code and docs: `skills.remember` controls
      skill-reference persistence, while `recap.after_chats` controls detailed
      recap artifacts. If skill remembering is enabled while recaps are off,
      schedule only the code-owned memory merge for turns that used a skill.
- [~] Add tests for version 1 migration, section ordering, path display,
      duplicate use, superseded content hashes, disabled remembering, replayed
      completion, cap pruning, and atomic write recovery.

## Phase 10.6: Carry explicit skills into agent work safely

- [x] Pass explicitly activated skill references and bounded bodies through
      the parent agent options without rescanning global roots in child
      sessions. Child agents receive only the selected context, never the
      entire catalog.
- [x] Keep automatic detection parent-only unless a future child policy
      explicitly opts in. Sub-agent jobs must not write skill references to
      the parent memory file or receive hidden global instructions.
- [~] Add tests for parent-to-child propagation, child exclusion from scans,
      restart and resume behavior, task-tool summaries, and cancellation.

## Phase 10.7: Synchronize documentation and close the gates

### 10.7.1 Update committed documentation and local knowledge

- [x] Update `docs/architecture.md` with the catalog roots, first-request
      injection boundary, precedence, context caps, and trust rules.
- [x] Add `docs/skills.md` with the descriptor format, `/skills` interaction,
      settings, source precedence, and error behavior.
- [x] Update `docs/tui.md`, `docs/plans.md`, and the v0.0.10 parent plan with
      the shipped command, settings rows, scan status, and manual terminal
      evidence.
- [x] Add or update `knowledge-base/03-concepts/skills.md`,
      `knowledge-base/03-concepts/memory.md`, `knowledge-base/02-architecture/data-flow.md`,
      and `knowledge-base/README.md`. State which behavior is shipped and
      which paths are local conventions or configured global roots.
- [~] Run the unslop pass over all new prose. Check that no em dashes,
      unsupported claims, raw absolute secrets, or stale "planned" labels
      remain after implementation.

### 10.7.2 Run automated and live gates

- [x] Run focused `internal/skills`, settings, agent, recap, and chat tests
      (`go test ... -count=1`, exit 0).
- [x] Run `go build ./...` (exit 0), `make test` (exit 0), `make lint`
      (exit 0), and `make vet` (exit 0).
- [x] Run race tests for catalog refresh, first-request injection, memory
      merging, and concurrent skill activation (`go test -race ...`, exit 0).
- [ ] In a real TTY at 120x36 and 80x24, create one local skill and expose one
      global fixture through the configured root. Verify `/skills`, filtering,
      duplicate precedence, activation, auto-detection, scan status, and the
      absence of skill bodies from the visible transcript.
- [ ] Inspect `knowledge-base/memories.md` after an explicit and an automatic
      skill use. Verify the `Skills` heading, scope labels, reusable paths,
      stable ordering, source IDs, bounded size, no secret-like content, and
      no duplicate entry after replay.

## Dependencies

- Existing slash palette and drawer interaction in `internal/ui/chat/slash.go`
  and `internal/ui/chat/picker.go`.
- Existing project settings normalization in `internal/settings/settings.go`
  and settings card rows in `internal/ui/chat/settings.go`.
- Existing first-request recall seam in `internal/agent/agent.go` and
  `internal/ui/chat/runtime.go`.
- Existing path containment, grep limits, project instruction formatting, and
  memory writer in `internal/workspace`, `internal/tools/grep`,
  `internal/agent/project_instructions.go`, and `internal/recap`.
- No new third-party dependency. Use the standard library and existing
  Bubble Tea components.

## Closure gate

- [~] Automated discovery, activation, injection, memory, documentation, and
      race rows have current source or test evidence. Live terminal and
      post-run memory inspection remain open for a human session.
- [ ] `/skills` exposes both local and configured global catalogs without
      executing skill files or allowing a global skill to override local
      project or safety instructions.
- [ ] A successful turn that uses a skill updates the memory `Skills` section
      once with a reusable path, and a replayed completion is idempotent.
