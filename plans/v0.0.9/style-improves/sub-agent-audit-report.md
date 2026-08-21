# Sub-Agent Audit Report

**Project:** lazykoder  
**Overall rating:** **7/10**

## Scope

This report consolidates the six sub-agent sessions associated with the review:

1. `layout-audit`
2. `function-audit`
3. `runtime-tmux`
4. `test-review`
5. `layout-report`
6. `methods-report`

## Executive summary

The application is well structured, visually coherent, and broadly tested. Normal-size terminal layouts work well, and the tmux runtime is healthy. The main risks are safety-policy bypasses, shutdown behavior while an agent is busy, and defensive handling of small terminal dimensions.

## 1. Layout audits

**Rating: 7–8/10**

### Strengths

- Clear header → transcript → status → composer hierarchy.
- Strong wrapping, scrolling, picker, drawer, settings, and mouse tests.
- Reusable overlay architecture and thoughtful render caching.
- Markdown rendering has broad test coverage.
- Normal terminal sizes behave well.

### Issues

- Narrow terminals can overflow horizontally.
- Very short terminals can overflow vertically.
- File-picker paths and settings values may exceed available width.
- Markdown table sizing can fail when the first column is widest.
- Some Unicode width calculations use rune counts instead of display width.
- Overlay centering may look incorrect when the composer expands.
- Systematic tests for very small terminal sizes are missing.

## 2. Function and method audit

**Rating: approximately 7/10**

### High priority

- Quitting while an agent is busy may close the database before the agent fully stops.
- Escape in question prompts may select option 0 instead of cancelling.
- Model refresh can panic if the client is nil.

### Medium priority

- Event channels may block after the UI exits.
- File-walking errors are silently ignored.
- The file picker silently truncates results after 80 entries.
- Stale ask-resolution logic should be removed or consolidated.

### Positive observations

- Broad coverage across TUI, tools, streaming, persistence, settings, and sub-agents.
- Race tests passed.
- Agent dispatch, tool denial, step limits, and persistence are well tested.

## 3. Runtime and tmux audit

The runtime agent did not produce a final text response before stopping, but its database records confirm that repository discovery and runtime inspection started successfully.

Direct verification confirmed:

- `go test ./...`: passed
- Build: passed
- tmux UI rendered correctly at `120×36`

The perceived delay came from waiting on interactive processes and broad inspection, not from tmux startup itself.

## 4. Testing and accessibility audit

**Rating: 7/10**

### Strengths

- Strong automated tests.
- Consistent visual styling in screenshots.
- Width-aware rendering and terminal background handling.
- Documentation covers most controls and layouts.

### Recommendations

- Do not rely on color alone for tool status.
- Test contrast in dark, light, and monochrome terminals.
- Add tests for `40×12`, `60×16`, and other small sizes.
- Add explicit geometry tests for every overlay.
- Add screenshots for errors, streaming, confirmation, settings, and sub-agent views.
- Reconcile discrepancies between `docs/tui.md` and `TASKS.md`.
- Make important keyboard actions visible in the help overlay.

## 5. Security and reliability findings

### High priority

- `sh -c`, `bash -c`, and `zsh -c` may bypass destructive-command detection.
  - Relevant area: `internal/policy/policy.go`

### Medium priority

- `webfetch` permits localhost and private-network requests, creating SSRF risk.
- Malformed provider stream chunks are silently ignored.
- Filesystem containment has symlink TOCTOU risks.

### Low priority

- Database updates do not verify affected rows.
- Grep fallback behavior differs from ripgrep.
- Dotenv parsing is less capable than standard dotenv behavior.

## Recommended priority order

1. Fix the shell-wrapper safety bypass.
2. Ensure busy-agent shutdown cancels cleanly.
3. Fix Escape question cancellation.
4. Add SSRF/private-network protection.
5. Add narrow and short terminal clipping plus tests.
6. Improve accessibility and status readability.
7. Surface malformed stream errors.
8. Harden filesystem operations against symlink races.
9. Validate database update row counts.

## Final assessment

**Overall rating: 7/10.**

Lazykoder has a strong foundation and unusually good coverage for a terminal UI. It is ready for continued development, but the safety-policy bypass and shutdown race should be addressed before calling the safety layer production-ready.
