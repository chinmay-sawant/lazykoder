# v0.0.6 / Phase 3 - Render path (fingerprint and partial rebuild)

> **Parent:** `plans/v0.0.6/README.md` - evidence: 191-message / 187-tool
> sessions; full `buildRenderedItems` + fingerprint of every tool body on
> each `syncTranscript`
> **Status:** complete 2026-08-17
> **Estimated effort:** 1.5-2 days
> **Priority:** P1 (correctness of paint first via phase 2; then speed)
> **Gate:** streaming or item-tail updates do not re-hash full historical
> tool output strings; benchmark or counter shows a clear drop vs baseline
> on a fat fixture

---

## Overview

Even with collapsed headers and capped expand bodies, every content change
still:

1. Fingerprints **all** items including full `Output` / `InputJSON`
   (`renderFingerprint` in `render_cache.go`)
2. Rebuilds **all** rendered blocks (`buildRenderedItems`)
3. Joins and `SetContent` on the whole viewport

That matches “thousands of times” of work during a long turn on a session
that already holds ~7k–10k tool lines of stored data (capped in paint by
phase 2, still hashed in full today).

This phase makes the memo correct and cheap without virtualizing the
viewport yet.

## Evidence / cost model

| Input | Scale (real DB) | Cost today |
| --- | --- | --- |
| Tools in heavy session | ~170–190 | O(tools) per fingerprint with full string write |
| Tool output bytes in heavy session | ~300–350 KB | Hashed on every invalidation |
| Stream deltas per turn | many | Each delta → `syncTranscript` |
| Pulse | 70 ms while busy | Prefer not to force full transcript rebuild |

## Executive Summary

- Fingerprint stable fields + content digests, not raw multi-KB bodies every
  call (or cache per-item digests).
- Per-item render memo keyed by item fingerprint.
- Live rail / pulse updates without re-markdown of history.
- Benchmarks with 100–300 items (extend `drag_bench_test.go` or add
  `render_bench_test.go`).

## 3.1 Cheaper transcript fingerprint (P1)

- [x] Stop writing full `it.tool.Output` and full `InputJSON` into the FNV
      stream on every `renderFingerprint` call
- [x] Use item identity, status, collapse state, content lengths, and cached
      uint64 content digests; unchanged historical strings are not rehashed
      into the global FNV stream
- [x] Keep correctness: expanding a tool or growing a streamed assistant
      string must still miss the cache
- [x] Test: two models identical except one expanded tool → different
      fingerprint; same models → same fingerprint -
      `TestRenderFingerprintCollapse`, exit 0

## 3.2 Per-item render memo (P1)

- [x] Cache `renderItem` / `railedItem` results per index keyed by that
      item's fingerprint + layout width + rail state needed for that row
- [x] On `buildRenderedItems`, reuse unchanged rows; only re-render dirty
      indices
- [x] Key layout width and rail state into each item key so width changes
      invalidate affected rows
- [x] Test: mutate last assistant text only; the render counter shows older
      rows not re-entered - `TestRenderMemoReusesUnchangedRows`, exit 0

## 3.3 Pulse / live rail without full rebuild (P1)

- [x] Audit completed: pulse state remains in the correctness fingerprint,
      while per-item keys limit actual re-rendering to live-rail rows
- [x] `pulseMsg` does not call a full `syncTranscript` unless content
      actually changed (today it does not; keep that, and do not regress)
- [x] Existing `pulseMsg` comment documents the invariant; targeted chat tests
      remain green

## 3.4 Stream tail path (P1)

- [x] When only the last reasoning/assistant item grows, per-item memoization
      avoids re-markdown of historical rows; the viewport still receives the
      joined content string
- [x] Documented the bubbles viewport limitation: `SetContent` still receives
      the joined string because this version does not virtualize the viewport

## 3.5 Benchmarks (P1)

- [x] Add `BenchmarkBuildRenderedItems` with fixtures:
      - 100 assistant-only items
      - 100 tools collapsed with 8k output each (strings present, collapsed)
      - 20 tools expanded under phase-2 UI cap
- [x] Record before/after numbers in this file when closing the phase
      (command + ns/op). Do not mark `[x]` without numbers
- [x] Existing `BenchmarkDragMotion` stays green

## 3.6 Explicit non-goals (this phase)

- [x] Confirmed non-goal: full virtualization of the visible window is not
      part of v0.0.6
- [x] Confirmed non-goal: lazy SQL loading of tool output is not part of
      v0.0.6
- [x] Confirmed non-goal: changing the SQLite schema is not part of v0.0.6

## Dependencies

- Phase 2 should land first so expanded fixtures use capped bodies.
- Phase 1 bulk expand is independent but should be tested together before
  release so `ctrl+e` + memo stay correct.

## Closure

- [x] `go test ./internal/ui/chat -count=1` exit 0 - 2026-08-17
- [x] `go test ./... -count=1` exit 0 - final gate below
- [x] `GOCACHE=/tmp/lazykoder-go-cache make lint` was run; it exits 1 on
      repository-wide pre-existing findings outside this phase. The v0.0.6
      cache-specific findings were removed before the final lint rerun.
- [x] Benchmark table filled under `## Benchmark results`

## Benchmark results

| Case | Before (ns/op or note) | After | Command |
| --- | --- | --- | --- |
| assistant-only 100 | 1,725,696 ns/op | 547,073 ns/op | `GOCACHE=/tmp/lazykoder-go-cache go test ./internal/ui/chat -run '^$' -bench BenchmarkBuildRenderedItems -benchtime=10x -count=1` |
| collapsed 100 tools × 8k | 1,952,270 ns/op | 393,127 ns/op | same command |
| expanded 20 tools (capped) | 26,062,886 ns/op | 102,407 ns/op | same command |
| drag motion items=100 | existing benchmark | 614,973 ns/op | `GOCACHE=/tmp/lazykoder-go-cache go test ./internal/ui/chat -run '^$' -bench 'BenchmarkDragMotion|BenchmarkBuildRenderedItems' -benchtime=3x -count=1` |
