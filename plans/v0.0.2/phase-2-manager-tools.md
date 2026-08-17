# Phase 2 - Manager and task tools

> **Parent:** `plans/v0.0.2/README.md`
> **Status:** done

## 2.1 tools/task

- [x] Specs + arg/result JSON for `task`, `task_list`, `task_status`, `task_wait`, `task_cancel`
- [x] Pure package tests (`go test ./internal/tools/task -count=1` exit 0)

## 2.2 subagent.Manager

- [x] Config, Spec, Snapshot, Result, Runner interface
- [x] Semaphore max concurrent (hard 20)
- [x] Spawn wait vs background, Wait, List, Status, Cancel, CancelAll
- [x] Writer mutex for general role when parallel writers off
- [x] Fake Runner unit tests (`go test ./internal/subagent -count=1` exit 0)

## 2.3 Agent Host seam

- [x] `agent.SubagentHost` interface
- [x] Parent advertises task tools when Host set
- [x] Children get Host=nil (no nested spawn at depth 1)
