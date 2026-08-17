# v0.0.6 / Phase 3 - Render path (fingerprint and partial rebuild)

> **Parent:** `plans/v0.0.6/README.md` - evidence: 191-message / 187-tool
> sessions; full `buildRenderedItems` + fingerprint of every tool body on
> each `syncTranscript`
> **Status:** planned
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

- [ ] Stop writing full `it.tool.Output` and full `InputJSON` into the FNV
      stream on every `renderFingerprint` call
- [ ] Prefer: `part_id` / `call_id` + `status` + `collapsed` + `len(output)` +
      a cached `uint64` content hash updated only when the string pointer or
      length changes
- [ ] Keep correctness: expanding a tool or growing a streamed assistant
      string must still miss the cache
- [ ] Test: two models identical except one expanded tool → different
      fingerprint; same models → same fingerprint -
      `TestRenderFingerprintCollapse`, exit 0

## 3.2 Per-item render memo (P1)

- [ ] Cache `renderItem` / `railedItem` results per index keyed by that
      item's fingerprint + layout width + rail state needed for that row
- [ ] On `buildRenderedItems`, reuse unchanged rows; only re-render dirty
      indices
- [ ] Invalidate all on width change (or key width into the per-item key)
- [ ] Test: mutate last assistant text only; counter or hook shows older
      tool rows not re-entered (optional internal test hook behind
      `testing` build tag, or benchmark comparison)

## 3.3 Pulse / live rail without full rebuild (P1)

- [ ] Audit whether `pulse` / `pulseOn` belong in the global transcript
      fingerprint; if they force full rebuild, move throb to
      view-time recolor of the live rail only
- [ ] `pulseMsg` must not call a full `syncTranscript` unless content
      actually changed (today it does not; keep that, and do not regress)
- [ ] Test or comment in code documenting the invariant

## 3.4 Stream tail path (P1)

- [ ] When only the last reasoning/assistant item grows, avoid
      `strings.Join` of the entire history if the viewport API allows
      appending (if not, still benefit from per-item memo in 3.2)
- [ ] Document any bubbles viewport limitation if full SetContent remains
      required

## 3.5 Benchmarks (P1)

- [ ] Add `BenchmarkBuildRenderedItems` with fixtures:
      - 100 assistant-only items
      - 100 tools collapsed with 8k output each (strings present, collapsed)
      - 20 tools expanded under phase-2 UI cap
- [ ] Record before/after numbers in this file when closing the phase
      (command + ns/op). Do not mark `[x]` without numbers
- [ ] Existing `BenchmarkDragMotion` stays green

## 3.6 Explicit non-goals (this phase)

- [ ] Full virtualization (only visible window in SetContent) - deferred
- [ ] Lazy SQL load of tool output - deferred
- [ ] Changing SQLite schema

## Dependencies

- Phase 2 should land first so expanded fixtures use capped bodies.
- Phase 1 bulk expand is independent but should be tested together before
  release so `ctrl+e` + memo stay correct.

## Closure

- [ ] `go test ./internal/ui/chat` exit 0
- [ ] `go test ./...` exit 0
- [ ] Benchmark table filled under ## Benchmark results

## Benchmark results

| Case | Before (ns/op or note) | After | Command |
| --- | --- | --- | --- |
| collapsed 100 tools × 8k | _pending_ | _pending_ | `go test -bench=...` |
| expanded 20 tools (capped) | _pending_ | _pending_ | |
| drag motion items=100 | _pending_ | _pending_ | |
