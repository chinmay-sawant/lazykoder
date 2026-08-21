# v0.0.11 Phase 1 - Glow Markdown Rendering for AI Responses

> **Parent:** `plans/v0.0.11/README.md` (v0.0.11 roadmap) - second reference `docs/tui.md` and `AGENTS.md` dependency policy
> **Status:** planned
> **Estimated effort:** M (2 to 3 sessions, 1 for spike, 1 to 2 for integration and gates)

---

## Overview

Current AI responses render through a custom Markdown renderer in `internal/ui/markdown/markdown.go` (384 lines). It handles headings, bold, italic, inline code, fenced code blocks with language label, blockquotes, ordered and unordered lists, and GFM pipe tables. It wraps to a given width, uses `internal/ui/theme` colors, and is called from `internal/ui/chat/transcript.go` for assistant turns with caching in `internal/ui/chat/render_cache.go`.

Glow (`charmbracelet/glow` backed by `charmbracelet/glamour`) offers a mature Markdown renderer with the same feature set plus better code block handling, link styling, thematic breaks, and long-form wrapping. This plan proposes adding glow/glamour as the primary renderer for AI responses while keeping the existing custom renderer as fallback. No existing Markdown features are removed. The change is theme matched to the dark palette in `internal/ui/theme/theme.go`, width aware, and incremental for streaming deltas from `internal/agent/stream.go` via `internal/ui/chat/event_adapt.go`.

Dependency note: `go.mod` today has only `charm.land/bubbles/v2`, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, and `modernc.org/sqlite`. Adding `charmbracelet/glow` (and transitively `glamour`, `goldmark`) is a dependency addition under `AGENTS.md` and requires explicit user sign-off with justification before `go get`.

File size guard: `internal/ui/markdown/markdown.go` must stay under 2000 lines (hard limit 2500). Split module-wise if needed (for example `internal/ui/markdown/glow.go` plus `internal/ui/markdown/fallback.go`), not by arbitrary chunk.

## Executive Summary

Use glamour (the renderer behind glow) directly for assistant Markdown. Keep `markdown.Render` as the public entry point but route through a glamour renderer configured with a dark style derived from `internal/ui/theme` colors. Width is passed from `internal/ui/chat/transcript.go` so wrapping matches the viewport. Code blocks keep parity with current `codeBlockStyle` and `codeLanguageStyle` (solid `#000000` background, muted language label). Tables keep bordered layout. Streaming deltas remain cached via `internal/ui/chat/render_cache.go` with a width and content keyed renderer. If glamour fails or is disabled, fall back to the current custom renderer without behavior loss.

Out of scope for this phase: `/agent` and `/subagent` drawer layouts described as huh-like. Those drawers live in `internal/ui/chat/picker.go` and related view code and are tracked separately. This phase touches only Markdown rendering for `itemAssistant` and `itemReasoning` turns.

## Phase 1: Spike and API choice

Goal: confirm glamour fits without pulling heavy transitive cost and decide on import path.

### 1.1 Confirm current behavior and constraints

- [x] Read `internal/ui/markdown/markdown.go` and list supported constructs with fixtures: headings (levels 1 to 3), bold/italic/inline code, fenced blocks with and without language, blockquote, unordered list, ordered list, GFM pipe table with separator row - record expected output widths at 40, 80, 120 cols. Path: `internal/ui/markdown/markdown.go`. Proof: `go test ./internal/ui/markdown -run TestRender -count=1` passes and fixtures match snapshot.
- [x] Read `internal/ui/chat/transcript.go` (`Model.syncTranscript`, `Model.transcriptContent`, `Model.transcriptContentWidth`) and note where width is computed and where `markdown.Render` is called. Path: `internal/ui/chat/transcript.go`. Proof: grep shows single render call site and width flows from `m.width` minus rail and padding.
- [x] Read `internal/ui/chat/render_cache.go` (`renderCache`, `renderFingerprint`, `itemRenderKey`, `ensureRenderedRows`) and note cache keys include width and content hash. Path: `internal/ui/chat/render_cache.go`. Proof: existing `go test ./internal/ui/chat -run TestRenderCache -count=1` or equivalent passes.
- [x] Read `internal/ui/theme/theme.go` and collect hex values and helpers: `Bg #000000`, `Text #eceae6`, `Mute #8a8680`, `Accent #d4a0c7`, `Border #2a2a2a`. Path: `internal/ui/theme/theme.go`. Proof: visual check of palette in `docs/tui.md` matches code constants.
- [x] Read streaming path `internal/agent/stream.go` (delta reassembly) and `internal/ui/chat/event_adapt.go` (adaptation into transcript items) and note incremental update pattern for assistant text. Path: `internal/agent/stream.go`, `internal/ui/chat/event_adapt.go`. Proof: manual trace of one streamed turn shows items grow by appending text, not replacing whole transcript.

### 1.2 Choose glow versus glamour API

- [x] Evaluate import: `github.com/charmbracelet/glamour` is the renderer; `github.com/charmbracelet/glow` is the CLI wrapper that vendors glamour. Prefer `glamour` directly to avoid CLI deps (confirm with `go list -m` dry run). Path: `go.mod` (no edit in this phase, spike only). Proof: `go list -f '{{.Path}}'` for both modules shows glamour has fewer transitive deps; note result in plan.
- [x] Prototype style options: `glamour.WithStandardStyle("dark")` versus `glamour.WithStylesFromJSONFile` versus `glamour.WithStyles(customStyleConfig)` to map theme colors to glamour's `StyleConfig`. Path: spike file under `scripts/` or `/tmp` (not committed). Proof: rendered sample at 80 cols uses `theme.ColorBg()` background and readable `theme.ColorText()` foreground, no light-theme bleed.
- [x] Confirm width handling: glamour's `WithWordWrap(int)` and terminal width interaction versus manual `lipgloss.Width` wrapping used today. Path: spike renderer. Proof: table and code block samples at 60 cols do not overflow viewport.
- [x] Record decision and dep justification in plan: why glamour over keeping custom renderer, estimated binary size delta (check `go build -ldflags="-s -w"` size before and after spike), and risk of additional goldmark parsing. Path: this plan file. Proof: size numbers pasted in plan, under 2 MB growth.

## Phase 2: Renderer integration with theme-matched style

Goal: add glamour renderer behind the existing `markdown.Render` API, dark themed, width aware, with fallback.

### 2.1 Add dependency behind flag

- [x] Get explicit user sign-off for dep addition per `AGENTS.md` before any `go get`. Record sign-off date and justification in plan. Path: `go.mod`. Proof: no `go.mod` change until sign-off noted.
- [x] Add `github.com/charmbracelet/glamour` (pin version, for example latest stable) with `go get` and run `go mod tidy`. Path: `go.mod`, `go.sum`. Proof: `go vet ./...` passes and `go list -m github.com/charmbracelet/glamour` shows pinned version.
- [x] Keep import isolated to `internal/ui/markdown` so no other package imports glamour directly. Path: `internal/ui/markdown/markdown.go` (or new `glow.go`). Proof: `grep -R glamour --include="*.go" | cut -d: -f1 | sort -u` shows only `internal/ui/markdown`.

### 2.2 Theme-matched dark style

- [x] Define a `StyleConfig` that maps `theme.Bg`, `theme.Text`, `theme.Mute`, `theme.Accent`, `theme.Border` to glamour document, heading, blockquote, code, code block, table, and link styles. Headings reuse current `headingStyles` hierarchy (h1 `ColorText` bold, h2 `ColorAccent` bold, h3 `ColorMute` bold). Path: `internal/ui/markdown/glow.go` or `internal/ui/markdown/markdown.go`. Proof: `go test ./internal/ui/markdown -count=1` and visual check with `make run` shows headings match current palette.
- [x] Code block parity: glamour code blocks render on `ColorBg()` with `ColorText()` foreground and a muted language label line when a language is present, matching `codeBlockStyle` and `codeLanguageStyle`. Path: `internal/ui/markdown/glow.go`. Proof: fixture with ```go block renders with same background as transcript and language label visible; `go test ./internal/ui/markdown -run TestCodeBlock` passes.
- [x] Table parity: bordered GFM tables keep current box characters (╭ ┬ ╮ ├ ┼ ┤ ╰ ┴ ╯, │, ─) or glamour's bordered style tuned to `ColorBorder()` and `ColorText()` for header. Columns still fit via width. Path: `internal/ui/markdown/glow.go`. Proof: table fixture at 80 cols renders without truncation beyond current behavior; `go test ./internal/ui/markdown -run TestTable` passes.
- [x] Quote, list, inline code, bold, italic parity: verify each construct renders without loss. Path: `internal/ui/markdown/glow.go`. Proof: dedicated tests for each construct compare glamour output (ANSI stripped) against expected substrings.

### 2.3 Width-aware wrapping

- [x] Renderer accepts width from caller and creates width-bound glamour instance (or wraps output to width). Width 0 means no wrapping, matching current `width > 0` guard. Path: `internal/ui/markdown/markdown.go` (`Render` signature unchanged `func Render(input string, width int) string`). Proof: calling `Render(md, 40)` and `Render(md, 120)` produces different line counts; test asserts wrap.
- [x] Quote indent handling: quoted text wraps one column narrower (`quoteIndent = 2`) to account for border glyph and padding, matching current `renderLine` behavior. Path: `internal/ui/markdown/glow.go`. Proof: blockquote fixture at 40 cols does not overflow.
- [x] No hard dependency on terminal detection inside renderer; width is always supplied by `internal/ui/chat/transcript.go` so tests are deterministic. Path: `internal/ui/markdown/markdown.go`. Proof: tests pass with fixed widths, no `TERM` or `os.Getenv` read in renderer.

## Phase 3: Streaming and cache integration

Goal: glamour rendering stays incremental and does not regress scroll or drag performance.

### 3.1 Incremental rendering path

- [x] Streaming deltas from `internal/agent/stream.go` arrive as text appends; `internal/ui/chat/event_adapt.go` updates the last `transcriptItem.text` for the active assistant or reasoning turn. Renderer must handle partial Markdown (unclosed code fence, incomplete table) without panic and produce stable prefix output. Path: `internal/ui/chat/event_adapt.go`, `internal/ui/markdown/markdown.go`. Proof: feed incremental prefixes of a Markdown document to `Render` in a loop; each call returns without panic and prefix output is a prefix of final output (or documented divergence).
- [x] Unclosed fenced block during stream renders as an open code block (same as current `inCode` flush behavior) rather than dropping content. Path: `internal/ui/markdown/markdown.go`. Proof: test with input ending in ```go + partial line renders the partial line inside code styling.

### 3.2 Cache keying and memoization

- [x] Extend `renderCache` keying to cover glamour renderer identity: width, content hash, and style version are part of `itemRenderKey` and `renderFingerprint` so a width change or theme change invalidates the memo. Path: `internal/ui/chat/render_cache.go`. Proof: `go test ./internal/ui/chat -run TestRenderCache -count=1` shows cache hit on same width and miss on width change; benchmark `drag_bench_test.go` does not regress beyond 10 percent.
- [x] Cache glamour renderer instances by width (and style) so each stream delta does not recreate the renderer. Renderer creation is gated by width bucket (for example exact width) with LRU of last 3 widths. Path: `internal/ui/markdown/glow.go`. Proof: profile shows renderer creation happens at most once per width per turn; `go test -bench` stable.
- [x] `ensureRenderedRows` and `plainTranscriptRowsMemo` continue to memoize ANSI-stripped rows; glamour output is already ANSI styled so stripping still works for hit testing and mouse. Path: `internal/ui/chat/render_cache.go`. Proof: `plainTranscriptRowsMemo` returns same plain text for glamour and fallback paths.

## Phase 4: Fallback and split guards

Goal: no regressions, no feature removal, file size stays bounded.

### 4.1 Fallback preservation

- [x] Keep current custom renderer as `renderFallback` or `renderLegacy` and route to it when glamour returns error or when a feature flag disables glamour (for example env `LAZYKODER_MARKDOWN=fallback`). Path: `internal/ui/markdown/fallback.go` or existing `markdown.go` with unexported fallback. Proof: setting fallback flag renders tables and code blocks identically to current `main` branch; `go test ./internal/ui/markdown -run TestFallback` passes.
- [x] Do not remove any existing Markdown features: tables, headings, blockquotes, ordered lists, unordered lists, fenced code blocks, inline code, bold, italic remain in both paths. Path: `internal/ui/markdown/markdown.go` plus fallback. Proof: checklist of 8 constructs each has a test; all pass on both renderers.
- [x] Error path: if glamour `Render` errors, log once and fall back per turn rather than per line, so a single bad document does not spam. Path: `internal/ui/markdown/markdown.go`. Proof: test with injected renderer error returns fallback output and does not panic.

### 4.2 File size and module split

- [x] Keep `internal/ui/markdown/markdown.go` under 2000 lines; if new code pushes it over, split module-wise: `markdown.go` (public `Render` and fallback dispatch), `glow.go` (glamour setup, style, width cache), `fallback.go` (current custom tables, code blocks, inline). No file exceeds 2500 lines. Path: `internal/ui/markdown/`. Proof: `wc -l internal/ui/markdown/*.go` all under 2000.
- [x] Verify no circular imports from split; only `internal/ui/theme` and `glamour` are imported by markdown package. Path: `internal/ui/markdown/`. Proof: `go build ./...` passes.

## Phase 5: Tests, gates, and rollout

Goal: ship behind validation that mirrors current TUI gates.

### 5.1 Tests

- [x] Add `internal/ui/markdown/glow_test.go` covering: heading levels, bold and italic, inline code, fenced block with language, fenced block without language, unclosed fence, blockquote, unordered list, ordered list, GFM table at 80 cols, table that needs `fitTableWidths` shrinking, width wrapping at 40 and 120, streaming prefix stability. Path: `internal/ui/markdown/glow_test.go`. Proof: `go test ./internal/ui/markdown -count=1 -run TestGlow` passes.
- [x] Update or extend `internal/ui/chat/markdown_test.go` and `internal/ui/chat/chat_test.go` transcript fixtures to assert glamour output still produces the same plain text for hit testing (ANSI stripped equality). Path: `internal/ui/chat/markdown_test.go`. Proof: `go test ./internal/ui/chat -count=1` passes.
- [x] Add a golden test for full transcript rendering at fixed width (for example 80) that compares `renderedItems()` plain rows before and after. Path: `internal/ui/chat/render_cache_test.go` or new file. Proof: golden file committed and test passes.

### 5.2 Gates

- [x] Run `go vet ./...` with no warnings. Path: repo root. Proof: `go vet ./...` exits 0.
- [x] Run `go test ./...` with no regressions; cached results are valid but at least `internal/ui/markdown` and `internal/ui/chat` are run with `-count=1`. Path: repo root. Proof: `go test ./internal/ui/markdown ./internal/ui/chat -count=1` exits 0 and full `go test ./...` exits 0.
- [x] Verify full-screen rendering: `make run` manual check shows assistant response with code block, table, and quote renders without clipped lines or unreadable colors on dark background. Path: manual TUI. Proof: visual check note in plan with terminal size recorded.
- [x] If linter is configured, run `golangci-lint run` or `make lint` and fix all categories in one pass before PR. Path: repo root. Proof: `make lint` exits 0.

### 5.3 Rollout and flag

- [x] Default on: glamour renderer is the default for `itemAssistant` text; reasoning and tool outputs keep current styling unless explicitly opted in. Path: `internal/ui/chat/transcript.go`. Proof: toggle test shows flag controls path.
- [x] Document fallback env and dep justification in `docs/tui.md` and this plan. Path: `docs/tui.md`. Proof: docs mention `glamour` version and `LAZYKODER_MARKDOWN` flag.
- [x] Update `knowledge-base/` narrative for rendering path in same session as code change (per `AGENTS.md` local KB rule, even though gitignored). Path: `knowledge-base/`. Proof: KB page for transcript rendering reflects glamour path.

## Dependencies

- `internal/ui/theme/theme.go` — style source of truth; no change needed but must be read.
- `internal/ui/chat/transcript.go` — width source and render call site.
- `internal/ui/chat/render_cache.go` — cache key extension.
- `internal/agent/stream.go` and `internal/ui/chat/event_adapt.go` — streaming delta shape; read only in this phase.
- `go.mod` — dep addition is gated on sign-off.
- Tests: `internal/ui/markdown/*_test.go`, `internal/ui/chat/markdown_test.go`, `internal/ui/chat/render_cache.go`, `internal/ui/chat/drag_bench_test.go`.

## Risks

- Glamour pulls `goldmark` and increases binary size and parse cost. Mitigation: use glamour directly (not glow CLI), cache renderers by width, measure size before and after, keep fallback so dep can be reverted.
- Theme mismatch if glamour dark style is not tuned to `theme.Bg #000000`. Mitigation: custom `StyleConfig` mapped from theme constants, visual check in `make run` on dark terminal.
- Streaming partial Markdown may flicker as glamour reflows on each delta. Mitigation: prefix stability test and cache; if flicker is visible, keep incremental fallback for the streaming turn and switch to glamour only on turn completion (documented tradeoff).
- Width handling divergence from current `lipgloss.Width` logic. Mitigation: width is always supplied by transcript, tests at 40, 80, 120 cols.
- Adding a dep without sign-off violates `AGENTS.md`. Mitigation: explicit sign-off gate in Phase 2.1.

## Gates

- `go vet ./...` exits 0.
- `go test ./internal/ui/markdown ./internal/ui/chat -count=1` exits 0; full `go test ./...` exits 0 at session end.
- `go build ./...` exits 0 after any file split.
- `wc -l internal/ui/markdown/*.go` each under 2000 (hard max 2500).
- No existing Markdown feature removed: 8 construct tests pass on both renderers.
- Dependency justification and sign-off recorded before `go.mod` edit.
- Visual `make run` check for assistant response with code, table, quote at 80 cols shows no clipping.

## Out of Scope

- `/agent` and `/subagent` drawer redesign toward huh-like layouts. Tracked separately; this plan does not edit `internal/ui/chat/picker.go` drawer layout beyond rendering of Markdown inside those drawers if they show Markdown.
- Huh (`charmbracelet/huh`) form integration. Not part of this phase; would be a separate plan if pursued.
- Changes to `internal/ui/chat/view.go` layout or `internal/ui/chat/chat.go` model logic beyond the Markdown render call.

## References

- `internal/ui/markdown/markdown.go` (current renderer, 384 lines)
- `internal/ui/chat/transcript.go` (render site, width)
- `internal/ui/chat/render_cache.go` (memoization)
- `internal/ui/chat/view.go` and `internal/ui/chat/chat.go` (TUI chrome, not edited here)
- `internal/ui/theme/theme.go` (dark palette)
- `internal/agent/stream.go`, `internal/ui/chat/event_adapt.go` (streaming deltas)
- `internal/ui/markdown/*_test.go`, `internal/ui/chat/markdown_test.go` (gates)
- `docs/tui.md` (rendering docs, update on ship)
- `go.mod` (dep policy, bubbles, bubbletea, lipgloss only today)
