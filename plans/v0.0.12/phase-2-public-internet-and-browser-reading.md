# v0.0.12 / Phase 2 - Public internet and browser reading

> **Parent:** `plans/v0.0.12/README.md` - v0.0.12 index
> **Status:** implementation and Phase 2 closure evidence validated
> **Estimated effort:** 4-6 working days
> **Priority:** P1

---

## Overview

Finish the public internet capability behind `webfetch`. HTTP mode remains a
bounded deterministic reader. Browser mode uses an isolated system Chrome or
Chromium process only when rendered content is needed. Every network path
must preserve public-destination validation and the browser must stop with
the request context.

## 2.1 Preserve the HTTP contract

- [x] Keep `internal/tools/webfetch` limited to public `http` and `https`
      destinations, with the existing redirect, DNS, response-size, and
      timeout rules. Add no automatic crawl, login, form submission, file
      navigation, download, email send, or private-network exception.
- [x] Verify that `webfetch` tool arguments, bounded output, metadata, and
      persisted `parts` and `tool_calls` rows remain compatible for callers
      that omit `mode`. `http` must never start a browser.
- [x] Preserve the existing HTML extraction behavior for title, readable
      text, ordinary links, `mailto:` links, visible email addresses, and
      truncation metadata. Page content remains untrusted data and cannot
      change tool permissions or project instructions.

## 2.2 Complete the browser runner

- [x] Replace the requested-URL placeholder in browser metadata with the
      actual final navigation URL, or return an explicit capability error when
      the selected browser control path cannot observe it.
- [x] Keep Chrome-first and Chromium-fallback executable discovery fixed and
      code-owned. Never accept a browser binary, flag, or shell command from
      page content or model output.
- [x] Tie the browser process, temporary profile, local validating proxy, and
      active proxy tunnels to the request context. Cancellation must terminate
      the complete process group and close proxy connections before the tool
      returns.
- [x] Keep the temporary profile isolated and disposable: no user cookies,
      saved credentials, extensions, downloads, normal profile, or persistent
      cache. Cleanup must run after success, timeout, startup failure, and
      cancellation.
- [x] Keep browser failure categories distinct: missing binary, startup
      failure, blocked page, empty valid document, renderer crash, navigation
      timeout, unsafe destination, and cancellation.

## 2.3 Enforce egress safety in the browser

- [x] Test the local browser proxy for initial navigation, redirects,
      subresource requests, CONNECT requests, private and link-local IPs,
      metadata addresses, unsupported schemes, and DNS rebinding.
- [x] Prove that no browser request can bypass the public-destination policy
      through a proxy exception, IPv6 spelling, redirect, iframe, image, or
      JavaScript fetch. Keep the browser sandbox enabled.
- [x] If reliable request observation or interception cannot be proved with
      the standard library and the existing module graph, stop at this gate
      and request explicit approval for one narrowly scoped browser
      dependency. Do not silently add Playwright, chromedp, Puppeteer, or an
      equivalent module.

## 2.4 Agent integration and fixtures

- [x] Add deterministic local HTTP and JavaScript-rendered fixtures covering
      title, final URL, links, email metadata, redirects, delayed content,
      blocked content, and cancellation. Do not make the live internet part
      of the deterministic test gate.
- [x] Add agent tests for explicit `http`, explicit `browser`, `auto` fallback,
      absent browser capability, browser timeout, browser cancellation, and
      bounded metadata persistence.
- [x] Add an opt-in real-browser check that skips with a clear reason when
      Chrome or Chromium is unavailable. Record the binary, fixture URL, and
      observed result when it runs.
- [x] Recheck the Medium acceptance URL only as a manual access case. Report
      the content actually returned, including a blocked or consent page, and
      never claim that hidden article text was read. The observed response was
      a Cloudflare `Attention Required!` page at the requested final URL, with
      no extracted email links.

## Dependencies

- `internal/tools/webfetch` HTTP, extraction, proxy, and process seams
- Phase 1 cancellation contract
- Existing provider and agent tool lifecycle
- An installed Google Chrome or Chromium binary for the opt-in check
- No new dependency unless the capability gate proves it necessary and the
  user explicitly approves it

## Closure gate

- [x] Public HTTP and browser reads preserve SSRF protection across redirects,
      subresources, CONNECT, and DNS rebinding.
- [x] Browser reads return bounded rendered content and actual final URL data,
      or a precise capability error when that cannot be observed safely.
- [x] Local HTTP and JavaScript fixtures pass through the agent, including
      timeout and cancellation cleanup.
- [x] The Medium acceptance case returns only content actually available to
      the process. Any email link found by extraction remains data, and the
      run sends no email. The observed Cloudflare response contained no email
      link.

## Evidence

- `GOCACHE=/tmp/lazykoder-phase2-go-cache go test ./internal/...` passed.
- `GOCACHE=/tmp/lazykoder-phase2-go-cache make test` passed for the full
  module.
- `GOCACHE=/tmp/lazykoder-phase2-go-cache go test -race ./internal/tools/webfetch ./internal/agent` passed.
- `make lint`, `GOCACHE=/tmp/lazykoder-phase2-go-cache make vet`, and
  `GOCACHE=/tmp/lazykoder-phase2-go-cache go build ./...` passed.
- `LAZYKODER_BROWSER_TEST=1 GOCACHE=/tmp/lazykoder-phase2-go-cache go test ./internal/tools/webfetch -run '^TestChromeBrowser' -count=1 -v` passed all local browser cases. The configured live URL test skipped unless `LAZYKODER_BROWSER_URL` is supplied.
- With `LAZYKODER_BROWSER_URL` set to the Medium acceptance URL, the opt-in
  check ran `google-chrome` and returned title `Attention Required! | Cloudflare`, the requested final URL, `output_runes=720`, and `email_links=[]`.
- No browser or network dependency was added.

## Out of scope

- Unbounded crawling, search indexing, or automatic link following
- Login, cookies, authenticated browsing, form submission, downloads, or
  outbound email
- A browser dependency added without explicit approval
