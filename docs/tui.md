# TUI

The interface is a Bubble Tea v2 program (alt screen) with three full views:
chat, confirm, and the model picker.

## Chat view

- Transcript: user and assistant text lines. Reasoning shows the stored text.
  Tool calls render as dark cards with the command or title, status, and
  captured response, for example `bash: completed` with its output below it.
- Prompt: a `textinput` line. Enter sends, empty sends are ignored; while a
  turn is in flight the prompt ignores Enter.
- Status line (idle): current model, hint line with keys, and the model-list
  health line (`models: N available`, or a red error).
- Dragging across transcript rows selects and copies the range on mouse
  release; a temporary `text copied` notice appears above the prompt. Clicks
  on the model status and scrollbar keep their existing navigation behavior.
- Assistant Markdown formats headings, emphasis, lists, inline code, and fenced
  code blocks. Fenced blocks use a dark bordered card with an optional language
  label.

| Key | Action |
| --- | --- |
| `enter` | send the prompt |
| `m` | open the model picker (idle only) |
| `q` | quit |
| `ctrl+c` | quit |

Busy turns, provider errors and the missing-key message render as red status
text; the user text stays in the transcript.

## Confirm view

Full-screen switch (the y/n delete layout):

```
Delete <subject> (<qualifier>)?
 
y confirm  •  n cancel
```

| Key | Action |
| --- | --- |
| `y` / `Y` | confirm once |
| `n` / `N` / `esc` / `q` | cancel (deny) |
| `ctrl+c` | quit the app, never confirm |
| any other key | ignored |

While this view is up, no keys leak to the prompt or transcript.

## Model picker

`m` or `/model` opens a centered settings card sized to about 80% of the
terminal width. The left rail labels the setting and shows the selected model;
the right pane contains the fetched model list. Navigation: `j`/`k` or arrows;
`enter` selects, `esc`/`q` cancels. `/` enters model filtering and `r` refreshes
the model list.

Typing `/` in the chat prompt opens a command popover above the prompt. The
popover keeps the active slash query in an input-like row and shows the
highlighted command description beside the command list. `↑`/`↓` select a
command, `enter` runs it, and `esc` closes the popover.

Selecting a model:

1. updates the current chat model (shown in the status line),
2. persists to `sessions.model` via `db.UpdateSessionModel` (when a session
   exists), and
3. sends `ChatRequest.Model` on every subsequent turn.

The chosen model survives restart because the app resumes the latest session
for the cwd on startup.

## Session replay

On start, the app looks up the latest session for the current directory and
rebuilds the transcript from the store (messages + parts) without any
network call. New turns append to the same session.
