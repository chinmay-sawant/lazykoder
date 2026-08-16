# Tips

The rotating usage hints shown right-aligned on the transparent row above the
input box (the same row as the `▼` jump-to-latest icon). While the app is
idle, the current tip is displayed bottom-right; every 15 seconds it advances
to the next one. Alerts override the tip: the red quit warning wins, then the
green "Text copied" confirmation, then the tip.

The source list lives in `internal/tips/tips.go` (`tips.All`). This page is
the readable reference; keep the two in sync when adding or editing a tip.

## Sending and editing

- `enter` sends the prompt
- `shift+enter` inserts a newline
- `esc` cancels a running turn
- press `esc` twice to clear the prompt

## Commands

- type `/` to list commands
- `/new` starts a fresh session
- `/sessions` or `ctrl+s` opens past sessions
- `/model` switches the chat model
- `/variant` sets the reasoning effort
- `/refresh` reloads the model list
- `/help` or `?` shows every shortcut

## Input box

- type `@` to mention a project file
- `ctrl+a` selects the whole input
- `ctrl+c` copies the input, twice quits
- `up`/`down` browse your previous prompts

## Transcript and mouse

- drag across the transcript to select and copy
- click a tool card to expand it
- `t` expands thinking, `e` expands the last tool
- click the scrollbar to jump the transcript
- the `▼` above the box jumps to the latest output
- click the model label to switch models
- a green diamond means the tool succeeded
- a red diamond means the tool failed

## Sessions and safety

- your session resumes where you left off
- destructive bash commands always ask first
- the footer shows model, tokens, cost and tps
