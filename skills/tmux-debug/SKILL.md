---
name: tmux-debug
description: Run and inspect Bubble Tea TUIs inside a real tmux pseudo-terminal, capture rendered cell output at multiple terminal sizes, exercise interactive states, and diagnose alignment, clipping, anchoring, and viewport issues. Use when a Bubble Tea layout looks wrong at runtime, when headless execution cannot open /dev/tty, or when source and view tests are insufficient to explain screen geometry.
---

# Bubble Tea tmux Debugging

Use tmux as a reproducible terminal harness for Bubble Tea runtime debugging. Capture the actual cell grid, compare card and prompt rows and columns, test responsive sizes, then clean up the dedicated session.

## Workflow

### 1. Prepare a real pseudo-terminal

Check the tools and read the repository's `AGENTS.md` before launching anything:

```sh
command -v tmux
command -v go
```

Use a unique, dedicated session name. Do not use `tmux kill-server`, attach to a user's existing session, or run the TUI through a pipe, redirect, `head`, or `timeout`.

For lazyKoder, launch from the repository root with a writable task cache:

```sh
tmux new-session -d -s lazykoder-ui-qa -x 120 -y 36 'GOCACHE=/tmp/lazykoder-go-cache go run .'
```

Confirm the pane dimensions:

```sh
tmux display-message -p -t lazykoder-ui-qa '#{pane_width}x#{pane_height}'
```

### 2. Capture the rendered cell grid

Allow startup to finish, then capture the pane without ANSI escape sequences first:

```sh
tmux capture-pane -p -t lazykoder-ui-qa:0.0 -S -
```

Use `-e` only when color or style state is part of the diagnosis. Treat captured output as potentially sensitive: redact API keys, tokens, paths, prompts, and provider responses before sharing it.

The cell capture is the primary visual artifact for a terminal UI. It preserves rows, borders, whitespace, clipping, and scrollbar columns even when a raster screenshot is unavailable.

### 3. Exercise the failing state

Send keys to the dedicated pane and recapture after every meaningful state transition:

```sh
tmux send-keys -t lazykoder-ui-qa:0.0 /
tmux capture-pane -p -t lazykoder-ui-qa:0.0 -S -
```

For the slash model flow, use `/`, then `m`, then Enter. To open the picker directly, start from a clean chat state and send `m`:

```sh
tmux send-keys -t lazykoder-ui-qa:0.0 m Enter
tmux capture-pane -p -t lazykoder-ui-qa:0.0 -S -
```

In the current lazyKoder chat model, Escape closes the slash popover but intentionally leaves `/` in the prompt. Do not interpret a resulting `//` as an application layout bug; reset to a clean state or remove the retained slash before testing another path.

### 4. Test responsive geometry

Resize the same session and recapture at a compact terminal size:

```sh
tmux resize-window -t lazykoder-ui-qa:0 -x 80 -y 24
tmux capture-pane -p -t lazykoder-ui-qa:0.0 -S -
```

Compare:

- the card's left offset against `(pane_width - card_width) / 2`;
- the card's bottom row against the prompt row;
- nested borders against the outer card borders;
- the number of visible viewport rows against the loaded item count;
- scrollbar columns against the right-pane boundary; and
- status or hint text for wrapping instead of clipping.

Prefer at least `120x36` and `80x24`. A layout that passes only at one size is not finished.

### 5. Convert evidence into a fix

Rank falsifiable hypotheses before editing. Typical Bubble Tea causes include:

- dynamic cards inserted without reserving transcript rows;
- viewport height calculated before filtered items are populated;
- selection matched against an empty explicit model instead of the provider fallback;
- nested Lip Gloss widths subtracting borders twice; and
- status or hint strings wider than the terminal.

Use the capture to choose the smallest responsible seam. Add a deterministic view test for the exact geometry, then run targeted UI tests and the full Go suite.

```sh
GOCACHE=/tmp/lazykoder-go-cache go test ./internal/ui/chat
GOCACHE=/tmp/lazykoder-go-cache go test ./...
```

Do not claim visual completion from tests alone. Ask the user to run `make run` in a real terminal for final human inspection.

### 6. Clean up

Kill only the dedicated session after captures and validation finish:

```sh
tmux kill-session -t lazykoder-ui-qa
```

Never kill the tmux server or another user's session. Remove temporary debug instrumentation and redact captured artifacts before reporting findings.
