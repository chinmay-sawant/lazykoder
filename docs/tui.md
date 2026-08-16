# TUI

The interface is a Bubble Tea v2 program (alt screen). The chat screen stays
visible under confirm, ask, slash, and help overlays. The model and session
pickers are centered cards.

## Chat view

- Header: `lazykoder`, the session title (or `new session`), and the project
  directory basename.
- Transcript: user turns labeled `you`, assistant turns labeled `assistant`,
  each with a clock timestamp (`15:32:05`) on the far right of the row.
  Reasoning starts collapsed as `▸ thinking` with the same clock on the
  far right; `t` expands it when the prompt is empty. Tool runs are
  full-width cards that start collapsed (`◆  ▸  bash  title` on the left,
  `15:32:05` on the far right). The diamond is the only status mark: white
  while pending or running, green on success, red on error or deny. `e`
  expands the last run. Clicks on a
  thinking or tool header toggle that item.
- Composer: a rounded input box on the solid black layer. Typed text uses
  the same black background as the rest of the screen (no cursor-line
  highlight). The footer left side stays idle (`enter send`). Live thinking
  and commands stay in the transcript. The right side shows
  `model  used/window  $cost  tps` (click the model to switch). Context
  windows and list prices come from GET /models, filled from the OpenCode
  Go catalog when the API omits them, and are stored in
  `.lazykoder/models.json`. Reopening a session restores used tokens from
  stored step-finish usage, or estimates them from the transcript. Session
  token totals never drop when a later step reports smaller usage. `/refresh`
  always rewrites that cache. Live text is painted instantly.
- Session list (`/sessions` or `ctrl+s`) groups runs as `just now`,
  `recently`, and `older`.
- Session picker: `/sessions` or `ctrl+s` lists sessions for the project
  directory (newest first). Enter loads one; esc keeps the current session.
- Empty session: a short hint in the transcript, not a blank pane.
- `/help` or `?` (empty prompt) opens a key overlay. `esc` closes it.
- `@` in the prompt opens a project file picker. Enter inserts `@path`.
- Dragging across transcript rows selects and copies the range on mouse
  release; a temporary `text copied` notice appears above the prompt. A
  click on a tool card or reasoning header expands or collapses that item
  instead of starting a selection. Clicks on the model status and scrollbar
  keep their existing navigation behavior. A click on a slash-menu row runs
  that command.
- Assistant Markdown formats headings, emphasis, lists, inline code, and fenced
  code blocks. Code sits on the same solid black layer as the rest of the
  screen, with no extra card border. The TUI paints a full-size black
  background so the host terminal color cannot show through.

| Key | Action |
| --- | --- |
| `enter` | send the prompt |
| `shift+enter` | insert a newline |
| `q` | type the letter `q` (the prompt is focused) |
| `esc` | cancel an in-flight turn; when idle, twice clears the prompt |
| `t` | expand or collapse reasoning (empty prompt) |
| `e` | expand or collapse the last tool card (empty prompt) |
| `ctrl+s` | open the session picker (idle only) |
| `/sessions` | same as `ctrl+s` |
| click model | open the model picker |
| `ctrl+c` | two-step quit (press twice) |

Busy turns, provider errors and the missing-key message render as red status
text; the user text stays in the transcript.

## Confirm overlay

A centered card over the dimmed chat (the y/n delete layout). The transcript
stays underneath.

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

While this overlay is up, no keys leak to the prompt or transcript.

## Question overlay

The `question` tool opens an option list over the chat: the question text,
optional header, then numbered options. `j`/`k` or arrows move, `1`-`9` or
enter selects, `esc` cancels.

## Model picker

`m` or `/model` opens a centered settings card sized to about 80% of the
terminal width. The left rail labels the setting and shows the selected model;
the right pane contains the fetched model list. Navigation: `j`/`k` or arrows;
`enter` selects, `esc`/`q` cancels. `/` enters model filtering and `r` refreshes
the model list.

Typing `/` in the chat prompt opens a full-width command popover above the
prompt. Each row shows the command name on the left and its description after
it. `↑`/`↓` select, `enter` or a click runs the command, `esc` closes and
leaves `/` in the prompt.

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
