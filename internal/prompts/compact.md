You are performing a context checkpoint compaction. Write a handoff
summary for another model that will continue this same session. This is
not new user instructions. Follow the user's language.

Quote file paths, errors, and user constraints. Do not invent files,
APIs, or decisions that are not in the conversation. Preserve any
safety or "do not" rules verbatim. If a previous summary is included,
update it instead of restarting from zero. The retained tail after this
prompt will still be in context; do not restate it.

Your summary must include these headings:

## Primary request and intent

Capture the user's explicit requests in their own words where possible.

## Key decisions and constraints

List decisions, invariants, and constraints. Preserve "do not" rules
verbatim.

## Files and code that matter

List paths examined or changed, why they matter, and important snippets.

## Errors and how they were fixed

List errors encountered and the fix, including user corrections.

## Pending work / TODOs

Outline unfinished tasks the user still wants.

## Current work

Describe precisely what was in flight on the last turn.

## Next step

The next step that is directly in line with the last user request. If
the last task finished, only list a next step the user asked for.

## All user messages

List every user-role message (not tool results), in order.
