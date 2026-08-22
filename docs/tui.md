# TUI

The interface is a Bubble Tea v2 program (alt screen). The chat screen stays
visible under confirm, ask, slash, and help overlays. The model and session
pickers are centered cards.

## Chat view

- Header: `lazykoder`, the session title (or `new session`), and the project
  directory basename.
- Transcript: user turns labeled `you`, assistant turns labeled `assistant`,
  each with a clock timestamp (`15:32:05`) on the far right of the row.
  Reasoning streams live as `▾ thinking` with the growing body under
  the header. It collapses to `▸ thinking` as soon as the assistant
  reply, a tool card, or the end of the turn arrives. The same clock
  sits on the far right. Tool runs are
  full-width cards that start collapsed (`◆  ▸  bash  title` on the left,
  `15:32:05` on the far right). The diamond is the only status mark: white
  while pending or running, green on success, red on error or deny. `ctrl+e`
  expands all tools when they are all closed, or collapses all tools otherwise.
  `ctrl+p` applies the same rule to all thinking blocks. Plain `t` and `e`
  always type into the composer. Tool-card header clicks toggle their body;
  thinking headers select rows without toggling them.
- Composer: a rounded input box on the near-black graphite canvas. Its charcoal input
  surface fills the complete box, including the footer labels and status chips, and
  receives a muted sage focus border while editing. Long prompts
  grow up to six rows and scroll inside the box. Up/down move the
  cursor through that text first; at the top of a multi-line draft, up
  stays there instead of jumping to a previous message. After browsing
  history, down restores the draft you were typing. While a turn is
  running, a thinking/loading line sits just above the box with a blank
  row on each side. The sent prompt sits in one square-bracket wrap,
  even when it spans several lines. A light vertical line then runs
  from thinking through the assistant reply. It throbs while that
  turn is running and stays as a static line after the turn ends and
  when the session is reopened. Assistant replies fill a muted sky-blue panel
  across every rendered row, including Markdown code. Typed text stays on the
  composer surface without a cursor-line highlight. The footer left side stays
  idle (`enter send`).
  The right side
  stays compact as a clickable `status ▾` control. Use `/status` to open the
  drawer with the current model, variant, token window, cache hit/miss, cost,
  tokens/sec, sub-agent count, model count, scroll state, and prompt hint.
  Cache hit is cached input tokens (`cache_read` /
  `prompt_cache_hit`); miss is uncached input (`prompt - hit`, or the
  full prompt when the API reports input and cache separately). Each
  count is summed since the last compact (or session start). A compact
  resets hit and miss to 0 because those old prefixes are no longer
  in the live request. Over many turns the sums can exceed the model
  window; that is expected (it is not a single-request fill). Each
  count carries its share of hit+miss. tps uses provider-reported output
  tokens divided by the provider step duration when usage is available.
  During streaming, a `~` value is a provisional estimate from recent
  streamed text; if no sample exists yet the drawer says `measuring`.
  Values above 99 are shown numerically, not as a capped greater-than
  label. It is never the session total.
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
  API. Reopening a session restores the **current model fill** from the
  latest step-finish (input tokens when present) or, after a compact,
  from the checkpoint's stored `tokens_after` (summary + kept tail).
  The meter is not a lifetime peak: a later smaller request, or a
  compact, updates the number you see. Cost is priced with the model
  that ran each step (from `messages.model_id` and
  `.lazykoder/models.json` list prices, including cache read/write).
  Sub-agent child sessions keep their own step-finish usage; `/agents`
  shows each child's cost and cache hit/miss, and the status cost line
  adds those children as `subs $X`. Cache hit/miss on the status bar
  is parent-since-compact plus every child. Auto-compact runs
  when used tokens exceed **80% of the live model's context window**
  (`used > window * percent / 100`). That percent is
  `compaction.percent` in `.lazykoder/settings.json` and the
  **compact at** row in `/settings` (default 80, range 5-99, steps of
  5). Turn same-model auto-compact off with **auto-compact**. `/compact`,
  a mid-session shrink, and one overflow retry still run when auto is
  off. The recent tail kept beside the summary is 15,000 tokens
  (`compaction.keep_tokens` in settings.json; not a `/settings` row).
  After a compact the transcript adds a notice `context compacted` or
  `context compacted (1000k -> 256k)`. The painted history stays
  complete; only the next provider request shrinks.

  Examples at the default 80%:

  | Model window | Compact when used exceeds |
  | --- | ---: |
  | 1,000,000 (DeepSeek / luna) | 800,000 |
  | 256,000 | 204,800 |
  | 200,000 (free Zen) | 160,000 |

  A session at 145k on a 1.05M model is about 14% full, so it will not
  auto-compact. Set **compact at** to 50% if you want that 1M model to
  compact at 500k. Switching to a
  smaller-window model while the session is already over that window
  shows `next send will compact (window X -> Y)` above the composer.
  The next send (or `/compact`) writes a checkpoint and continues.
  `/compact` can also run under budget; optional notes after the
  command steer the summary. While a compact call is in flight the
  status prompt segment says `compacting`. `/refresh` always rewrites
  that cache. Live reasoning and
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
- `/help` or `?` (empty prompt) opens a centered key card (two columns
  at 100+ width) listing send, slash commands including `/compact`,
  `/settings`, `/continue`, and `/status`, copy/quit, and undo. `/usage`
  is on the slash palette (Project group), not on this card. `esc`, `?`,
  or `[x]` closes it.
- `/usage` opens a centered OpenCode Go usage card. It fetches the rolling,
  weekly, and monthly plan windows, showing percentage used, rate-limit
  status, and reset times. Press `r` to refresh and `esc` or `x` to close.
- `@` in the prompt opens a project file picker. Enter inserts `@path`.
- Dragging across transcript rows selects and copies the range on mouse
  release; a temporary `text copied` notice appears above the prompt. The
  left work rail and user-frame curls stay on screen for layout, but are
  stripped from the clipboard so the paste is plain message text. A
  click on a tool card or reasoning header selects that row without changing
  its collapsed state. Clicks on the model status and scrollbar
  keep their existing navigation behavior. A click on a slash-menu row runs
  that command.
- Assistant Markdown formats headings, emphasis, lists, inline code, and fenced
  code blocks. Code sits on the same solid black layer as the rest of the
  screen, with no extra card border. The TUI paints a full-size black
  background so the host terminal color cannot show through.

| Key | Action |
| --- | --- |
| `enter` | send the prompt; while busy with a draft, **send now** (interrupts) |
| `shift+enter` | insert a newline |
| `q` | type the letter `q` (the prompt is focused) |
| `esc` | cancel an in-flight turn (and live sub-agents); when idle, twice clears the prompt |

While a turn is running, the status strip shows **working** plus the live
activity (thinking / tool). With the sub-agent drawer open, this strip is the
first row inside the drawer, above the sub-agent list. You can still type a
draft in the input box (**edit**). Actions:

| While busy | Action |
| --- | --- |
| type | edit a draft without waiting for the turn to finish |
| `enter` (with draft) | cancel the current turn and send the draft immediately |
| `esc` | cancel the current turn only (no new send) |
| `ctrl+e` | expand or collapse all tool cards |
| `ctrl+p` | expand or collapse all thinking blocks |
| click a collapsed tool header | expand that tool card (click again to collapse) |
| `ctrl+s` | open the session picker (idle only) |
| `/resume` | same as `ctrl+s` |
| `/model` | open the model picker |
| `/agents` | open the sub-agent list and logs (aliases `/subs`) |
| `/status` | open the status details and visibility drawer |
| click `subs:N` | same as `/agents` when sub-agents exist for this session |
| `ctrl+c` | two-step quit (press twice); after exit prints `lk <session_id>` |

## Sub-agent drawer and logs

After the parent spawns sub-agents (`task` tools), a **drawer above the
prompt** opens (same layout family as `/model`): one row per sub-agent.

| Diamond | Meaning |
| --- | --- |
| Throbbing `◆` | running / queued (pulses with the work rail) |
| Green `◆` | completed |
| Red `◆` | failed, cancelled, or timed out |

The right side of each row includes the resolved model and, when it fits, a
one-liner for the latest tool activity (for example `bash  go test ./...` or
`read  path.go`). Past sub-agents
for the session stay in the list after they finish.

Each row also shows `model: <id>` and `thinking: <variant>` on the right. The
values are resolved for that child job, including explicit child overrides and
configured defaults, and are read from the job snapshot or child session record. An
empty variant is shown as `thinking: default`, meaning the provider default.
The UI does not infer a vendor or substitute a hard-coded model.
When the child session has step-finish usage, the row and the child log
header also show cost and cache hit/miss (`$0.18  ·  12k/800`). Status
cost adds those children as `subs $X`; footer cost uses `+$X`.

- `/agents` (aliases `/subs`) focuses the drawer; it also opens when a
  `task` tool runs.
- Footer chip `subs:live/total` (or `subs:total`) stays next to the model
  stats; click it to open the drawer.
- `↑`/`↓` or `j`/`k` selects a row. `→` or **enter** opens its full-screen
  (100% terminal) log
  for that child, using the same design as the main chat: `you` / `assistant`
  roles, collapsible **thinking** (expanded by default), tool cards with
  status diamonds, and the vertical work rail (`│`).
- In the log view: `↑`/`↓` scrolls, `→` opens the next agent's log, and `←`
  returns to the drawer. `ctrl+p` expands or collapses all thinking, `ctrl+e`
  expands or collapses all tools, and `enter` toggles the selected block;
  `esc` / `[x]` also returns to the drawer; `d` closes. Header clicks only
  select a row.
- A log opens at its latest output and follows the live tail as new child
  events arrive. After scrolling up, the transparent `▼` row above the
  footer jumps back to the latest output.
- `d` on a live drawer row cancels it; `esc` closes the drawer.

When recaps are enabled, the drawer includes a separate selectable `recaps`
row with a semantic success rail for completed work and a danger rail for
failures. It shows the newest record's status and source message range. Press
`enter` or `right`, or click the row, to open the generated summary, questions,
and things-to-avoid context. Press `left`, `esc`, or `[x]` to return. `up` and
`down` move between the recap row and child-agent rows. If the hidden worker
fails, the drawer shows `failed` and the recorded error. The failure stays in
SQLite even though no recap files are created, so the next debugging step is
visible without adding worker output to the chat transcript.

The recap context uses the same Markdown renderer as assistant messages. The
summary has a green panel, questions use the assistant-blue panel, and
things-to-avoid uses the danger panel. Code blocks keep the Markdown renderer's
command formatting inside the summary panel.

Child sessions stay in SQLite (`kind=subagent`) so completed agents still
appear after the turn ends.

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
optional header, then numbered options. Text wraps at word boundaries inside
a dark dialog card; unusually long words are split only when necessary.
`j`/`k` or arrows move, `1`-`9` or enter selects, `esc` cancels, and clicking
any wrapped option row selects it. Clicks outside the card do not reach the
chat underneath.

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

## Project settings

`/settings` (alias `/slot`) opens a **centered full-screen card** in the
same family as `/resume` and `/help`: a bordered panel over the chat with
a `SETTINGS` header and a clickable `[x]` on the top right. Changes
persist in `<cwd>/.lazykoder/settings.json`. Card content, selected controls,
and keyboard hint rows keep the card's opaque neutral-charcoal background.

| Row | What it controls |
| --- | --- |
| theme | `dark` or `light` application palette. Switching it redraws the current chat and keeps the card open. Dark is the default. |
| new-session model | model for new sessions (default `deepseek-v4-flash`) |
| new-session variant | default reasoning effort (`default` / low / medium / high / max) |
| child model override | model every child inherits (empty = inherit parent) |
| explore model | model for explore-role children (empty = inherit) |
| step limit | on/off for the per-turn tool-step budget |
| parent max steps | tool-calling rounds per user turn when the limit is on (1-1000, default 16) |
| auto-compact | on/off for same-model percent preflight (default on) |
| compact at | fire when used tokens exceed this % of the live window (5-99, steps of 5, default 80). Dimmed when auto is off, still editable |
| recaps enabled | on/off for hidden recap generation and first-request local recall (default off) |
| recap model | model for hidden recap generation (default `deepseek-v4-flash`) |
| recap after chats | successful main-chat turns before scheduling (1-20, default 2) |
| skills enabled | on/off for discovery, activation, and request-time injection (default on) |
| skill settings | discovery toggles and automatic-match limit; body/context caps remain JSON controls |
| sub-agents | on/off for parent `task` tools |
| default role | `explore` / `plan` / `general` when `task` omits role |
| max concurrent | concurrent child agents (1-20, default 4) |
| max queued | spawn queue size (default 40, cap 100) |
| child max steps | step budget for each child agent (default 1000) |
| child timeout | wall-clock timeout (`10m` at 600s; 0 = off) |
| parallel writers | allow more than one general-role writer |
| child bash confirms | `ask parent` or `deny` |
| parent bash allowlist | on/off; parent-only, children are not filtered |
| allowed executables | chip/count editor for the parent allowlist |

On terminals at least 42 rows high, the settings card also displays the
latest OpenCode Go rolling, weekly, and monthly usage percentages. On a
shorter terminal, use `/usage` for the same information. Opening `/settings`
loads usage when it has not already been fetched; `/usage` can refresh it.

At 24 rows, the settings card keeps its header, footer, and focused row on
screen. Use `j` and `k` to move through the remaining rows.

When sub-agents are running, the footer may show `subs:N/M` (active / max
concurrent). Cancelling the parent turn also cancels child jobs.

| Control | Action |
| --- | --- |
| `j`/`k` or arrows | move between rows |
| `←`/`→` or `h`/`l` | adjust the focused control (cycle theme/model/variant, toggle limit, nudge steps) |
| enter | open the model or variant picker for that row; toggle/bump for slot rows |
| space | toggle limit or cycle model/variant |
| click `[x]` | close |
| click a row / `◂` / `▸` | adjust that control |
| `esc` / `x` / `q` | close |

`keep_tokens` (recent tail beside the summary, default 15,000) is only
in `.lazykoder/settings.json`. `/settings` does not edit it.

When recaps are enabled, a completed main-chat turn may create files under
`knowledge-base/recaps/sessions/`, `questions/`, and `things-to-avoid/`.
The worker is silent in the transcript and child-agent logs. `/agents` shows
the newest recap row and opens its context on `enter`, `right`, or a click. A
later parent turn performs one bounded local grep before its first provider
request and sends matching lines as untrusted historical hints.

The same `recap.enabled` and `recap.model` settings also control the hidden
memory worker. It runs after each successful parent turn, stays out of the
transcript and drawer, and updates the project-scoped
`knowledge-base/memories.md` aggregate. The worker receives only the bounded
recent snapshot, the previous memory document, and related local knowledge
evidence. Explicit user instructions are decoded into their durable section.
While local memory patterns are being searched or the hidden update is
running, the status line shows a separate animated memory marker.

`/skills` and `/skill` open the same drawer family as `/model`. The drawer
rescans approved project and configured global roots, labels each descriptor
as local or global, and lets Enter activate one bounded skill for the next
ordinary parent request. `/skills <query>` filters the catalog without a
provider call. The prompt status shows `scanning approved skills` during the
request-time lookup. Skill bodies are untrusted, wire-only context and do not
appear in the visible transcript, SQLite history, recap artifacts, or memory.
The persisted `skills` settings group contains `enabled`, `auto_detect`,
`include_local`, `include_global`, `remember`, `max_auto_matches`,
`max_body_bytes`, and `max_context_bytes`.

When the step limit is off the agent still has a large safety bound so a
runaway loop cannot run forever. `/model` and `/variant` still change only
the **current session**; the settings card is the project default.

## Continue

`/continue` keeps the session going after a step-limit stop: it resumes
the agent loop on the existing history for another MaxSteps budget and
does not write a new user message. When the last turn did not hit the
limit, `/continue` sends a normal user message of `continue` instead.
After a step-limit error the status line also hints `/continue to keep
going`.

## Compact

`/compact` summarizes older context now and stops after the checkpoint
(no follow-up chat turn). It needs an existing session; otherwise the
status says `nothing to compact`. Optional notes after the command
(`/compact focus on the auth bug`) are appended to the summarizer
prompt. Auto-compact uses the same backend when used tokens exceed
`compaction.percent` of the live window.

Switching to a smaller-window model while the session is already over
that percent shows `next send will compact (window X -> Y)` above the
composer. The next send (or `/compact`) writes the checkpoint. There is
no y/n confirm. A switch during a busy turn is cosmetic until the next
send: the in-flight agent keeps the model it was built with.

While a compact call is in flight the status prompt segment says
`compacting` and the activity line says `working  compacting`. Enter
while busy does not run `/compact`; it force-sends the draft.

Typing `/` in the chat prompt opens a full-width command popover above the
prompt, grouped as Session / Model / Project / Help. Each row shows the
command name on the left and its description after it (on narrow terminals
the description sits on the footer). `↑`/`↓` select, `enter` or a click
runs the command, `esc` closes and leaves `/` in the prompt.

Selecting a model:

1. updates the current chat model (shown in the `/status` drawer),
2. persists to `sessions.model` via `db.UpdateSessionModel` (when a session
   exists), and
3. sends `ChatRequest.Model` and the stored `endpoint` on every
   subsequent turn.

The chosen model is stored on the session row. After a fresh launch, pick that
session again with `/resume` or `ctrl+s` to get the same model back.

## Status drawer and todos

The footer keeps only a compact `status ▾` control so detailed metrics do not
compete with the prompt. Type `/status` or click that control to open the
agent-style drawer. All rows are enabled by default. The drawer owns the
visibility state for `model`, `variant`, `tokens`, `cache`, `cost`, `tps`,
`subs`, `models`, `scroll`, and `prompt`; `↑`/`↓` select, `enter` toggles,
and `←`/`esc` closes. Visibility is stored in the current session's
`status_segments` JSON column and restored on replay.

The model can call `todowrite` with the complete `{todos:[...]}` list. The
tracker under the session title replaces the stored list atomically and shows
pending, in-progress, completed, and cancelled marks. Reopening a session
loads the tracker from SQLite without a provider request and expands it so the
stored task bodies are visible immediately. The expanded body has six visible
rows and its own viewport; use the mouse wheel over the checklist to scroll
longer lists. A scrollbar marks overflow, and the header click collapses or
reopens the body. While work is active, the viewport follows the first
in-progress row so later tasks remain visible and highlighted. When resuming a
completed list, it opens at the newest page instead of the first six rows.
There is no hidden `+N` summary row.

## Session replay

Every launch opens a fresh session (blank transcript). Past runs stay in
SQLite. `/resume` or `ctrl+s` lists them and rebuilds the chosen transcript
from the store (messages + parts) without a network call. New turns on a
loaded session append to that same row.

If the workdir has `AGENTS.md`, the empty state and alert row show
`project instructions: AGENTS.md`. That file is sent as a system message on
each model call; it is not a transcript row.

## Quit banner

Confirmed quit (`ctrl+c` twice when the prompt is empty, or other paths that
return `tea.Quit`) leaves the alt screen, then prints the lazykoder ASCII
wordmark plus session lines on the normal console before the process exits:

```text
  █    █▀▀█ ▀▀▀█ █  █ █ ▄▀ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█
  █    █▄▄█  ▄▀   ▀▀█ █▀▄  █  █ █  █ █▀▀▀ █▄▄▀
  ▀▀▀▀ ▀  ▀ ▀▀▀▀  ▀▀▀ ▀  ▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀  ▀

lk ses_<id>
session name: session title here
resume with /resume or ctrl+s
```

If you quit before the first send (no session row yet), the same logo prints
with `lk (no session)` and `resume older runs with /resume or ctrl+s`. An
empty title prints as `session name: untitled`.

`ctrl+c` with unselected text in the composer clears the prompt; `ctrl+a` then `ctrl+c` copies the draft.
