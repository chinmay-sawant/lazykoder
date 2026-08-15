# v0.0.1 / Phase 2 - Destructive-command gate and bash tool

> **Parent:** `plans/v0.0.1/README.md` - safety invariant
> **Status:** implemented 2026-08-15 (automated gates green; 2.6 manual UI proof left for a live terminal)
> **Estimated effort:** 2-3 days
> **Priority:** P0 (must land before any live bash)
> **Gate:** every `rm` shows the employee-style y/n confirm; decline never executes

---

## Overview

Wire the first real tool (`bash`) through the policy package from Phase 1. The TUI switches to the same y/n confirm view the employee app used (highlight the subject, `y confirm  •  n cancel`). `rm`, including `rm` of a file in the current directory and `rm -rf` anywhere, cannot reach `exec.Command` unless the user confirms that one call. The same view is reused when a sub-agent is stopped or dismissed.

## Executive Summary

- Tool loop is still OpenCode-only. The chat request now advertises a `bash` tool.
- Classifier stays the single chokepoint. The executor takes a `Decision` plus, for `Ask`, a confirm result.
- Modal default is Deny. No sticky allow. Denied calls are stored as `tool_calls.status = denied`.
- `step-start`, `tool`, `step-finish` parts are written for every bash attempt, including denied ones.

Do not implement edit/read/write/question/webfetch in this phase.

## 2.1 Confirm view (P0) - same design as employee delete

Reuse the removed prototype's `renderDelete` / `updateDelete` layout. Do not add a boxed modal or "allow once" wording.

```
Delete <subject> (<qualifier>)?
 
y confirm  •  n cancel
```

- [x] Add `internal/ui/confirm` as a dedicated view (full switch, like `viewDelete`) - 2026-08-15; `go test ./internal/ui/confirm` exit 0 (8 tests)
- [x] Line 1: error style for `Delete ` and ` (<qualifier>)?`; focused/bold style for `<subject>` - error=Color("1"), subject=Color("212") bold
- [x] Hint line exactly: `y confirm  •  n cancel` - faint style, exact text (ANSI-stripped assertions in tests)
- [x] For bash `rm`: subject is the command (or target path); qualifier is `rm` or `rm -rf` - chat wires subject=command, qualifier from Decision (rm / rm -rf)
- [x] For a sub-agent stop/dismiss: subject is the sub-agent name (same role as the employee name); qualifier is `sub-agent` - same view reused (no sub-agent UI in 0.0.1; layout ready)
- [x] Recursive deletes still use this layout; qualifier becomes `rm -rf`. No second design.
- [x] Keys do not leak to the prompt, transcript, or any sub-agent list - full view switch; chat routes keys in confirm mode (tested in chat)
- [x] `y` / `Y` emits `confirm.Result{Allow: true}` once
- [x] `n` / `N` / `esc` / `q` emits `confirm.Result{Allow: false}`
- [x] Bare Enter does nothing (not a confirm)
- [x] `ctrl+c` still quits the app and must not exec the pending command
- [x] Test: model in confirm mode ignores transcript / list navigation keys - j/k/up/down/space/enter ignored, View unchanged
- [x] Test: Decline result never accompanies an exec cmd - `go test ./internal/tools/bash` gate tests, exit 0
- [x] Test: View string for subject `lint-fix` contains `Delete ` + `lint-fix` + `y confirm`

## 2.2 Policy to executor seam (P0)

- [x] `internal/tools/bash` accepts `(command, workdir, Decision, Confirm)` - `bash.Run(ctx, command, workdir, dec, confirmed, runner)`
- [x] If `Decision` is `Deny` -> no exec, status `denied` - ErrDenied, runner never called (tested)
- [x] If `Decision` is `Ask` and confirm is not Allow -> no exec, status `denied` - ErrDenied, runner never called (tested)
- [x] If `Decision` is `Ask` and confirm is Allow -> exec once - runner called exactly once (tested)
- [x] If `Decision` is `Allow` -> exec (non-rm only; tests must show `Classify("rm x") != Allow`) - policy table test proves rm is Ask
- [x] Workdir defaults to session `directory` (cwd). Policy does not care that the target is inside cwd - agent passes workdir; "" inherits process cwd
- [x] Exec uses a cancelled context; no shell interpretation beyond `sh -c` if we need pipelines. Prefer `exec.CommandContext` with a parsed argv when the command is a single program - Exec: argv parse for simple commands, sh -c only on metacharacters
- [x] Hypothesis (validate in implementation): `sh -c` is required for pipelines the model emits; the classifier must run on the full string before the shell sees it - validated: policy runs on the raw string in agent.executeTool before bash.Run; Exec falls back to sh -c for pipelines
- [x] Capture stdout+stderr, exit code, start/end timestamps - separate Stdout/Stderr buffers, UnixMilli Start/End
- [x] Test: `Classify("rm -rf /tmp/lazy-x")` + declined confirm -> `exec` helper is not called (inject a fake runner) - `go test ./internal/tools/bash` exit 0
- [x] Test: `Classify("rm ./README.md")` + declined confirm -> same
- [x] Test: `Classify("rm -rf .")` + accepted confirm -> fake runner called exactly once
- [x] Test: `Classify("ls")` -> runner called, no modal

## 2.3 Persist tool parts (P0)

- [x] On bash tool-call: insert `parts` row `type=tool`, `tool_name=bash`, `tool_status=pending` - agent.runTool
- [x] Insert matching `tool_calls` row (`input_json` has `command` and `workdir`) - `db.InsertToolCall`
- [x] After exec or deny, update `tool_status` / `status`, `output`, `exit_code`, `time_start`, `time_end` - `db.UpdateToolCall` (upsert + part status)
- [x] Denied calls store a short output such as `denied by user` and `exit_code` NULL - stores `{"denied":"user denied the command"}`, ExitCode nil
- [x] Wrap a model step as `step-start` ... tool parts ... `step-finish` when usage is available - agent.writeResponse writes step-start/reasoning/text/tool/step-finish
- [x] Test: declined `rm` leaves one tool part + one tool_calls row with `status=denied` and empty-or-reason output, no file deleted - agent DeniedBash test, exit 0
- [x] Test: allowed `echo hi` leaves `status=completed`, `exit_code=0`, output contains `hi` - agent AllowedBash test, exit 0

## 2.4 Provider tool advertisement (P0)

- [x] OpenCode chat request includes a `bash` tool schema (`command` required, `workdir` optional) - `bashToolSpec` sent every call; opencode client test asserts wire shape
- [x] Agent loop: if the response has a bash tool-call, classify -> maybe modal -> exec/deny -> send tool result back for the next model step - agent.Send loop, store-driven history rebuild
- [x] Loop bound: hard max steps per user turn (start at 8) so a runaway model cannot spam `rm` prompts - MaxSteps default 8, step-limit error + EventError
- [x] Each `rm` in that loop gets its own modal. Approval of call N does not approve call N+1 - Confirm hook invoked per tool call, no sticky state
- [x] Test: fake provider returns a bash `rm -rf /tmp/x` then a text reply; after decline, second provider call receives a denied tool result and no runner exec - agent DeniedBash test asserts `{"denied":"user denied the command"}` in the tool message; bash gate tests prove no exec

## 2.5 Extra destructive forms (P1, same modal)

- [x] `rmdir`, `unlink`, `shred`, `find ... -delete` classify as `Ask` - policy table tests, exit 0
- [x] `git rm` classifies as `Ask`
- [x] `sudo rm`, `env rm`, `command rm` classify as `Ask` + Destructive when `-r`/`-rf` present
- [x] Table-test additions live in `internal/policy` (extend Phase 1 table, do not fork a second classifier) - single Classify, 33-case table

## 2.6 UI proof (P0)

- [ ] Manual: type a prompt that would cause bash `rm` (or inject via a debug key if the model will not cooperate) and see the employee-style confirm
- [ ] Manual: press `n` -> transcript shows a denied tool card, files untouched
- [ ] Manual: a non-rm bash such as `go version` runs without a confirm view
- [ ] Full-screen check: subject is highlighted, hint is `y confirm  •  n cancel`, colors stay readable

## Dependencies

- Needs: Phase 1 workspace, db, provider, chat model, policy stub
- Blocks: Phase 3 remaining tools (they reuse confirm only if they shell out; file tools do not)

## Closure gates

- [ ] `go test ./internal/policy ./internal/tools/bash ./internal/ui/confirm ./internal/agent` pass
- [ ] `go vet ./...` pass
- [ ] Proof that `rm` cannot reach the runner without `confirm.Allow == true` (fake runner test, exit 0)
- [ ] Proof that declined `rm -rf` and declined `rm ./file` leave the filesystem unchanged
- [ ] No "always allow" control exists in the UI or store
