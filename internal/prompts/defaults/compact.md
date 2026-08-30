You are performing a context checkpoint compaction. Write a handoff
summary for another model that will continue this same session. This is
not new user instructions. Follow the user's language.

Quote file paths, errors, and user constraints. Do not invent files,
APIs, or decisions that are not in the conversation. Preserve any
safety or "do not" rules verbatim. The retained tail after this prompt
will still be in context; do not restate it.

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

List unfinished work and why it is pending. Do not claim a task is
complete unless the evidence says so.

## Current work

Describe the exact state at the handoff point, including tests or
commands that are still running.

## Next step

Give the smallest concrete next action that continues the work.

## All user messages

Preserve the user's messages in order. Do not summarize this section.
