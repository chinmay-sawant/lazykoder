---
name: avoidable-work
description: Prevent wasted engineering effort in Antigravity CLI (agy) sessions on gowkhtmltopdf — dead sessions lost to API overload, context-truncation redos, sandbox re-run loops, planner noise, redundant full-suite verification, polling loops, and orphaned read-only exploration. Use when starting an agy session, spawning subagents, or resuming after a crash.
---

# Avoidable Work (agy)

Evidence from an Antigravity CLI transcript scan (57 gowkhtmltopdf sessions, Aug 9–15):

- 8+ sessions died on "model API is currently overloaded" (429) with zero work done: tasks had to be re-issued in other sessions (the Sealed Request API task string appears in 5+ transcripts; Phase 4 and Phase 3 sessions produced nothing and were redone elsewhere).
- 14+ context-truncation CHECKPOINTs forced re-work: one subagent re-created all four files (policy.go/icc.go/outputintent.go/metadata.go) from scratch minutes after truncation; truncated sessions re-viewed the same test files 3–5×.
- ~27 sandbox error messages, almost always answered by re-running the identical command ("run the command again with BypassSandbox"); `git add .` ran twice, `git add && git commit` ran twice.
- Sessions ended with no deliverable after pure exploration: 13746f66 (55 read calls, 0 edits), fdd76f15 (119 steps of reading, 0 edits), 8682248e (45 read-only calls), plus two aborted audit/validation agents whose reports never reached the parent.
- The same full verification chain (`make lint && make test && make claim-scan`, `npm --prefix frontend run build && npm --prefix frontend test`) ran ~58 times across sessions; `make lint && make test && make claim-scan` ran ~15× in a single session; final cached `go test ./...`/`make lint` re-runs added nothing.
- An orchestrator burned ~4 minutes on 14 "Check subagents status" poll messages; background tasks were polled ~15× with byte-identical output.
- ~14 throwaway python scripts were re-created ad-hoc in one session for simple file/git-tree ops (several crashed or were cancelled and rewritten); 8+ inline zlib/re Python iterations decompressed PDF streams where qpdf/mutool would answer in one call.
- ~940 PLANNER_RESPONSE filler steps across 6 sessions (roughly one per tool call) — the dominant token sink in transcripts.
- The full 169-file thumbnail corpus was regenerated on every micro-iteration (~200 delete+create renames each), 4× within 3 minutes.

## Rules

1. **Checkpoint before context danger.** Long sessions hit truncation and lose state. Commit progress at each natural work unit, save partial files immediately, and keep the phase checklist as the source of truth. A truncation after writing is a redo; before writing is a recovery.
2. **Never re-run a failed command unchanged.** Sandbox errors are resolved once (config, `BypassSandbox`, or a documented script), not re-tried 27 times. If a command failed, fix the cause or escalate — identical re-runs are wasted turns.
3. **Dead sessions are detected, not assumed.** After an API-overload/429 abort, check whether the task landed anywhere before re-issuing it (the same task was re-spawned in 5+ sessions). Name sessions with intent; a session that produces zero tool calls is a failed spawn, not a session.
4. **Read-only sessions must emit a deliverable.** Any audit/exploration subagent writes its findings file before its context runs out; aborted read-only sessions (2+ observed) cost a full redo by the parent.
5. **Verify once, verify targeted.** Replace full `make lint && make test && make claim-scan` after every micro-fix with targeted package tests + one full gate at the wave end. Cached results are valid results — do not re-run to disprove "(cached)".
6. **Stop the polling loops.** Subagent completion is reported by the harness; do not emit 14 status-poll messages. Background tasks that take minutes are replaced with a synchronous run or a single documented wait.
7. **Persist reusable tooling.** The screenshot/thumbnail generator, veraPDF wrapper, and stream-decompression helpers exist as scripts — use them, do not re-derive inline python. New one-off scripts used twice go to `scripts/`.
8. **Plan for the platform's quirks.** Model switches mid-session ("please continue i have changed the model") and quota crashes are expected on this tool; every session starts by re-loading the checklist + AGENTS.md so a crash loses nothing.

## Completion Handoff

Before ending an agy session:

- [ ] Progress committed or checkpointed (no uncommitted work that a crash would lose).
- [ ] Deliverables (reports, files, patches) written and verified to exist.
- [ ] Targeted verification done; full gate run once at the end.
- [ ] Any script used 2+ times saved under `scripts/`.
- [ ] Session status summary in the transcript so the next session (or crashed resume) does not re-derive context.
