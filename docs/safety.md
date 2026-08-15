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

There is no allow-list, no sticky approval, no "remember this path". Each
call is classified fresh.

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

## Confirm view

The only confirm design in the app is the y/n delete layout:

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
`rm` or `rm -rf` (Destructive). For the question tool the subject is the
question text and the qualifier is the optional header.

## Denied calls

Denied calls are fully persisted: `tool_calls.status = denied` (the model
sees `{"denied":"user denied the command"}`), `exit_code` stays NULL, and no
file is touched. The loop continues with the denial result.
