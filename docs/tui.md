# TUI

The interface is a Bubble Tea v2 program (alt screen). The chat screen stays
visible under confirm, ask, slash, and help overlays. The model and session
pickers are centered cards.

## Chat view

- Header: `lazyKoder`, the session title (or `new session`), and the project
  directory basename.
- Transcript: user turns labeled `you`, assistant turns labeled `assistant`,
  each with a clock timestamp (`15:32:05`) on the far right of the row.
  Reasoning streams live as `▾ thinking` with the growing body under
  the header. It collapses to `▸ thinking` as soon as the assistant
  reply, a tool card, or the end of the turn arrives. The same clock
  sits on the far right; `t` expands a collapsed block when the prompt
  is empty. Tool runs are
  full-width cards that start collapsed (`◆  ▸  bash  title` on the left,
  `15:32:05` on the far right). The diamond is the only status mark: white
  while pending or running, green on success, red on error or deny. `e`
  expands the last run. Clicks on a
  thinking or tool header toggle that item.
- Composer: a rounded input box on the solid black layer. Long prompts
  grow up to six rows and scroll inside the box. Up/down move the
  cursor through that text first; at the top of a multi-line draft, up
  stays there instead of jumping to a previous message. After browsing
  history, down restores the draft you were typing. While a turn is
  running, a thinking/loading line sits just above the box with a blank
  row on each side. The sent prompt sits in one square-bracket wrap,
  even when it spans several lines. A light vertical line then runs
  from thinking through the assistant reply. It throbs while that
  turn is running and stays as a static line after the turn ends and
  when the session is reopened. Typed text uses
  the same black background as the rest of the screen (no cursor-line
  highlight). The footer left side stays idle (`enter send`). The right side shows
  `model  used/window  hit N 93%  miss N 7%  $cost  tps` (click the
  model to switch). hit is cached input tokens (`cache_read` /
  `prompt_cache_hit`); miss is uncached input (`prompt - hit`, or the
  full prompt when the API reports input and cache separately). Each
  count carries its share of hit+miss. tps is this turn's generated tokens (output, or
  reasoning if output is missing) divided by turn wall time. While a
  turn is running it updates from streamed output; after the turn it
  stays as that turn's average. It is never the session total.
  Context
  windows, list prices, cache read/write prices, and reasoning variants
  come from GET /models plus the live models.dev OpenCode catalog, then
  are stored in `.lazykoder/models.json`. That file is the source of
  truth for the picker. `/variant` shows whatever variants are stored
  for the current model, including `max` when the catalog lists it.
  Every model row keeps `cache_write_per_million` (0 when the provider
  has no write price) and an `endpoint` (the chat-completions URL to
  call). Free OpenCode models come from the Zen models list on
  `/refresh` and keep the Zen endpoint so send does not hit the Go
  API. Reopening a session restores used tokens from
  stored step-finish usage, or estimates them from the transcript.
  Session token totals never drop when a later step reports smaller
  usage. `/refresh` always rewrites that cache. Live reasoning and
  assistant text are painted as SSE deltas arrive. A non-streaming JSON
  response is treated as one complete chunk.
- Session list (`/resume` or `ctrl+s`) groups runs as `just now`,
  `recently`, and `older`.
- Session picker: `/resume` (or `/session`) or `ctrl+s` lists sessions for
  the project directory (newest first). The card uses about 80% of the
  terminal height. Enter or a click loads one; esc keeps the current
  session. Rows stay on one line (newlines in a title collapse to spaces);
  long titles truncate with an ellipsis, and the list scrolls with the wheel.
- Empty session: a short hint in the transcript, not a blank pane.
- `/help` or `?` (empty prompt) opens a centered key card with
  shortcuts in two columns. `esc` or `?` closes it.
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
| `/resume` | same as `ctrl+s` |
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

`/model` or a click on the model status opens a full-width drawer above
the prompt, the same place as the `/` command list. The drawer shows
more rows than the slash menu so more models stay visible, including
free OpenCode models. Each row shows the provider on the right
(`opencode go` or `opencode zen`). Navigation: `j`/`k` or arrows;
`enter` or a click selects, `esc`/`q` cancels. `/` enters model
filtering (also by provider) and `r` refreshes the model list.
Typing `/model` (or `/mode` then enter) opens the drawer with a
trailing space so you can type a search right away. `/model ope`
or `/model flash` filters the list live. If the extra text matches
no model, the drawer closes so a normal prompt like `/model and
then this is the thing I want to test` is not blocked by an empty
model box.

`/variant` opens the same card for the current model's reasoning variants
from `models.json`. The choice is stored in `sessions.variant` and sent
as `reasoning_effort` on later turns.

Typing `/` in the chat prompt opens a full-width command popover above the
prompt. Each row shows the command name on the left and its description after
it. `↑`/`↓` select, `enter` or a click runs the command, `esc` closes and
leaves `/` in the prompt.

Selecting a model:

1. updates the current chat model (shown in the status line),
2. persists to `sessions.model` via `db.UpdateSessionModel` (when a session
   exists), and
3. sends `ChatRequest.Model` and the stored `endpoint` on every
   subsequent turn.

The chosen model survives restart because the app resumes the latest session
for the cwd on startup.

## Session replay

On start, the app looks up the latest session for the current directory and
rebuilds the transcript from the store (messages + parts) without any
network call. New turns append to the same session.
