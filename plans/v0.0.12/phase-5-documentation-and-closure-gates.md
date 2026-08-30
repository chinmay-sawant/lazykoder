# v0.0.12 / Phase 5 - Documentation and closure gates

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** planned; closure waits for Phases 1-4 evidence
> **Estimated effort:** 1-2 working days plus manual verification time
> **Priority:** P1

---

## Overview

Synchronize the shipped behavior into the committed docs and local
knowledge-base, then run the complete verification set. This phase owns
closure evidence. It does not turn an implementation intention or a skipped
browser check into a passing row.

## 5.1 Documentation

- [ ] Update `docs/tools.md`, `docs/architecture.md`, `docs/tui.md`, and the
      relevant knowledge-base pages with only the behavior that shipped. The
      browser page must state the public-network boundary and the difference
      between local request cancellation and provider-side cancellation.
- [ ] Update `docs/plans.md` and the v0.0.10 Phase 11 pointer when the active
      browser rows move to this ledger. Keep completed historical rows intact
      and mark only moved unfinished rows as deferred with this plan path.
- [ ] Add the final source paths, settings, error categories, and test
      commands to the knowledge-base narrative in the same implementation
      session. Do not describe screenshot-dependent behavior as shipped before
      the live terminal gate passes.

## 5.2 Automated gates

- [ ] Run focused webfetch, provider, agent, sub-agent, and chat tests after
      each implementation slice.
- [ ] Run `make lint` and `make test` after all non-documentation changes.
      Record the exact exit result beside this closure gate. Leave every
      dependent row open if either command fails.
- [ ] Run `make vet`, `go build ./...`, and the relevant race tests. Record
      cancellation timing, goroutine cleanup, browser fixture outcomes, and
      any skipped real-browser check.

## 5.3 Manual closure

- [ ] In a real terminal at `120x36`, start a parent turn with multiple child
      jobs, cancel it, and confirm that the parent request, every child,
      browser or tool work, live indicators, and persisted statuses reach a
      terminal state without a stale spinner.
- [ ] Repeat the cancellation scenario at `80x24` and confirm the composer,
      drawer, error state, and stop affordance remain readable and usable.
- [ ] Read a static local fixture through HTTP mode and a JavaScript fixture
      through browser mode. Confirm metadata, final URL, cleanup, egress
      blocking, and cancellation from the actual result.
- [ ] Recheck the Medium URL only when access is available, record the actual
      response, and confirm that no email is sent and no extracted link is
      followed automatically.

## Dependencies

- Phases 1-4 implementation and evidence
- Current `docs/` reference pages
- Current `knowledge-base/` narrative pages
- Real terminal access for `120x36` and `80x24` checks
- Chrome or Chromium for the opt-in browser check

## Closure gate

- [ ] Local cancellation stops the parent request and every owned child or
      tool operation, releases all slots, and persists terminal statuses.
- [ ] Provider-side cancellation is reported only when a documented provider
      cancellation endpoint accepts a request identifier. Otherwise the result
      states that local transport cancellation was performed.
- [ ] Public HTTP and browser reads preserve SSRF protection across redirects,
      subresources, CONNECT, and DNS rebinding.
- [ ] Browser reads return bounded rendered content and actual final URL data,
      or a precise capability error when that cannot be observed safely.
- [ ] Automated tests, lint, vet, build, race checks, and real-terminal checks
      pass with evidence. No screenshot-dependent row is closed before the
      supplied screenshots and live terminal proof exist.

## Out of scope

- Unbounded crawling, search indexing, or automatic link following
- Login, cookies, authenticated browsing, form submission, downloads, or
  outbound email
- A guarantee that a remote provider stopped work after the local connection
  was closed when that provider exposes no cancellation protocol
- Nested sub-agent trees beyond the existing depth-one rule
- A browser dependency added without explicit approval
