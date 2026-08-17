# Safety

Safety is a hard invariant: the executor never sees a raw model command.
Every bash call passes through `internal/policy` first, and the confirm view
is the only path to execution for destructive commands.

## Classifier

`policy.Classify(command) Decision` tokenizes the full command string
(splitting on whitespace and the shell operators `| & ;`) and scans every
token:

| Class | Rule |
| --- | --- |
| `Ask` | any delete-class token: `rm` (incl. `/bin/rm`, `sudo rm`, `env rm`, `command rm`, `xargs rm`, `find -exec rm`), `git rm`, `rmdir`, `unlink`, `shred`, `find -delete` |
| `Ask` | empty or unparseable command (never `Allow`) |
| `Allow` | everything else (`ls`, `go test`, `echo room`, `chmod`, ...) |

`echo rm` is `Allow` (the token is an argument to `echo`, not a delete
invocation). Recursive/force flags (`-r`, `-rf`, `-fr`, `-R` forms,
`--recursive`) set `Decision.Destructive = true`.

Each call is classified fresh. There is no sticky "remember this path"
approval.

Project settings can optionally enable a **parent** bash allowlist
(`agents.bash_allowlist_enabled` plus `agents.bash_allowlist`). When on,
the parent agent only runs executables on that list (plus the usual
policy gate). Child agents do **not** inherit this list.

Child destructive bash follows `agents.bash_confirm`: `parent` routes the
y/n card to the user; `deny` refuses ask-class bash with no prompt.

## Executor gate

```
model tool-call (bash)
        |
        v
 policy.Classify(command)  -->  Allow | Ask | Deny
        |
        +-- Deny         --> no exec, stored as denied
        +-- Allow        --> exec once (rm never returns Allow)
        +-- Ask          --> confirm view
                              +-- y     --> exec once
                              +-- n/esc --> denied, no exec
```

`internal/tools/bash.Run(ctx, command, workdir, decision, confirmed, runner)`
never executes when the decision is Deny or when an Ask was not confirmed;
the runner (a real `exec.CommandContext`, or a fake in tests) is not even
called. Tests prove that a declined `rm` cannot reach the runner.

Real execution: simple single-program commands run via parsed argv; commands
with shell metacharacters fall back to `sh -c`. Context cancellation kills
the process.

## Confirm overlay

Destructive bash (`rm` class) opens a centered y/n card over the dimmed chat:

```
Delete <subject> (<qualifier>)?
 
y confirm  •  n cancel
```

- Subject is highlighted; `Delete ` and the qualifier render in error color.
- `y`/`Y` confirms once, `n`/`N`/`esc`/`q` cancels, `ctrl+c` quits the app
  (never confirms), bare Enter does nothing, every other key is ignored.
- Default is deny: an untouched modal never executes.
- Keys never leak to the prompt or transcript while the modal is up.

Subject mapping: for bash the subject is the command and the qualifier is
`rm` or `rm -rf` (Destructive). The question tool uses a separate option
list overlay, not this y/n card.

## Denied calls

Denied calls are fully persisted: `tool_calls.status = denied` (the model
sees `{"denied":"user denied the command"}`), `exit_code` stays NULL, and no
file is touched. The loop continues with the denial result.
