# Phase 3 - Real runner and storage

> **Parent:** `plans/v0.0.2/README.md`
> **Status:** done

## 3.1 Child sessions

- [x] Migration: `sessions.parent_session_id`, `sessions.kind` (schema v4)
- [x] `ListSessionsByDir` hides `kind=subagent`
- [x] Delete parent cleans up children
- [x] `messages.agent` set for child messages via `AgentName`

## 3.2 AgentRunner

- [x] Child `agent.New` + `Send` with role tool allowlist
- [x] Final summary folded into parent task tool result
- [x] Integration test with fake provider (`TestAgentRunnerCreatesChildSession`, exit 0)
