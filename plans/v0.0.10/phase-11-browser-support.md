# v0.0.10 / Phase 11 - Browser-backed URL understanding

> **Parent:** `plans/v0.0.10/README.md`
> **Status:** implementation landed; unfinished browser closure moved to `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`
> **Estimated effort:** 5-8 days
> **Priority:** P1
> **Gate:** a user-supplied public HTTP(S) URL returns bounded readable page
> content, title, final URL, ordinary links, and email links when available;
> JavaScript-rendered pages can use an isolated browser when the HTTP path is
> insufficient; no email is sent and no private destination is reached.

## Overview

The existing `webfetch` tool is an HTTP-only reader. It validates the initial
host and every redirect, performs one GET, caps the response at 5 MiB, and
returns the response body. It does not run JavaScript, inspect a DOM, extract
links, or read `mailto:` targets. Its `markdown` option currently adds a query
parameter to the request; it is not a local HTML-to-Markdown renderer.

Add browser-backed reading as an optional capability of the same URL-reading
workflow. A user who gives the agent a public article, documentation page, or
other URL should receive the page's useful rendered content, followed by
bounded link metadata that the agent can use for a deliberate next request.
The first slice reads one page per tool call. It does not crawl a site or
follow every link automatically.

The requested Medium article is the reference acceptance case:
[Andrej Karpathy's LLM Knowledge Bases explained](https://medium.com/data-science-in-your-pocket/andrej-karpathys-llm-knowledge-bases-explained-2d9fd3435707).
The current direct HTTP request returned 403, while indexed content exposed an
article body, a YouTube link, and the visible contact address
`datasciencepocket@gmail.com`. The implementation must handle both outcomes:
use the browser when it can render the page, or report the access failure
clearly without pretending that the page was read.

## Product decisions

- Keep `webfetch` as the model-facing tool so existing parent and child role
  allowlists remain valid. Add an optional `mode` with `auto`, `http`, and
  `browser` values. Existing calls without `mode` keep their current HTTP
  behavior until the auto path has passed its safety and compatibility gates.
- `auto` first uses the guarded HTTP reader. It may use the browser after a
  JavaScript shell, an unusable HTML response, a browser-required status, or an
  explicit user request to browse the page. `http` is deterministic and never
  starts a browser. `browser` requests the browser path and returns a clear
  capability error when no supported browser is installed.
- Prefer Google Chrome as the system browser when it is available. Accept
  Chromium as the compatible fallback. Detect both through a fixed executable
  allowlist, never through page content or a shell command supplied by the
  model. Do not bundle a browser in the application.
- Do not add a new Go dependency in the first design. Use the detected Chrome
  or Chromium process through a narrow internal runner. If reliable DOM control
  and request interception cannot be built with the standard library and
  already-present module dependencies, stop at a decision gate and request
  explicit approval for one browser library. Do not silently add Playwright,
  chromedp, Puppeteer, or another dependency.
- Browser support is read-only. Extracting a `mailto:` link or visible email
  address never sends mail, opens a mail client, or changes the page. A later
  outbound-email feature would need its own tool, policy, and confirmation.
- The agent reads the initial page and reports links. It follows a link only
  after a later model request supplies that URL. This keeps navigation bounded
  and prevents an article from turning into an uncontrolled crawl.
- No login, user cookies, extensions, downloads, file URLs, form submission,
  popup interaction, or authenticated browsing belongs in this phase.

## Architecture

### 11.1 Preserve the existing HTTP path

- Keep the current `internal/tools/webfetch` public `Run` seam and its response
  cap, timeout, redirect checks, copied client behavior, and DNS revalidation.
- Extract shared destination validation into a reusable internal egress seam
  only if the browser runner needs it. The browser path must not duplicate a
  weaker version of the current private-host checks.
- Add a bounded HTML normalization step for static responses. Prefer semantic
  article or main content, then headings and paragraphs, while excluding
  scripts, styles, hidden elements, navigation chrome, and repeated boilerplate.
  Preserve the raw response only when normalization cannot produce useful
  content, and record that outcome in metadata.
- Resolve relative links against the final response URL. Deduplicate links and
  cap the number, text length, URL length, and total metadata size.

### 11.2 Add a browser runner seam

- Add a focused internal browser package with a testable runner interface. The
  agent should depend on the interface, not on process details or a concrete
  Chrome command.
- Detect Google Chrome first, then Chromium, from a fixed executable allowlist.
  Never accept a browser command from page content or interpolate user input
  into a shell command. Start a fixed argv process and kill it on context
  cancellation.
- Launch each request with a temporary isolated profile, no extensions, no
  existing cookies, no downloads, no persisted credentials, and a bounded
  lifetime. Clean the temporary profile after the request.
- Prefer a browser control path that can wait for page readiness, inspect the
  rendered DOM, observe the final URL, and intercept every network request.
  A command-line DOM dump is acceptable only if it meets those safety and
  extraction requirements; otherwise it is a diagnostic fallback, not the
  shipped browser path.
- Detect and report missing browser binaries, startup failures, navigation
  timeouts, renderer crashes, blocked pages, and pages that remain empty after
  the wait budget. These errors must remain distinct from an empty valid page.

### 11.3 Enforce network safety inside the browser

- Validate the initial URL before launch with the same public-destination
  policy used by HTTP fetches.
- Enforce the policy for redirects and subresource requests as well. The
  initial URL check is not sufficient because a page can redirect, load an
  iframe, or issue a request to a private address from JavaScript.
- Ship the browser path only after request interception or an equivalent
  validated egress proxy has been proved to block loopback, private, link-local,
  multicast, metadata, file, and other unsupported destinations. Test DNS
  rebinding and redirect cases for both HTTP and browser modes.
- Do not disable browser sandboxing as a convenience. If the installed browser
  cannot run safely with the required isolation, return a capability error and
  document the prerequisite instead of weakening the launch flags.

### 11.4 Extract a bounded document result

Return the existing readable body as the primary tool output and add bounded
metadata for:

- page title and final URL;
- content type and the selected mode;
- whether browser rendering was used and why the HTTP path was bypassed;
- truncation and navigation timing status;
- ordinary links with visible text and resolved URLs;
- `mailto:` links with decoded address, subject, and body fields when present;
- visible email addresses that were not represented as `mailto:` links.

Keep the metadata code-owned and JSON-safe. Page text, link labels, email
subjects, and email bodies are untrusted page data. Do not treat them as
instructions, tool arguments, system prompts, or permission to contact anyone.

For the Medium acceptance case, the result should make the article readable,
include the visible contact address and relevant page links, and state when a
link is only an extracted reference. It must not send an email or automatically
open the YouTube link.

### 11.5 Wire the capability through the agent

- Extend the `webfetch` schema and argument parser with the optional mode while
  preserving existing calls and outputs.
- Keep tool lifecycle persistence unchanged: pending, completed, denied, and
  error states must continue to store bounded output and metadata in the same
  `parts` and `tool_calls` rows.
- Preserve role capabilities. Explore, Plan, and General may retain their
  existing `webfetch` access; browser mode is a behavior of that tool rather
  than a second unreviewed tool.
- Expose enough metadata for the model to decide whether it needs a second
  request for a specific extracted link. Do not inject the entire link list
  into the transcript outside the existing bounded tool output.
- Keep user-visible status meaningful while a browser is running. The tool
  title should distinguish HTTP fetch from browser navigation without leaking
  page content into progress messages.

## Security and failure boundaries

- Only public `http` and `https` initial URLs are accepted. `mailto:`, `file:`,
  `javascript:`, local paths, and browser-internal URLs are extracted only as
  data when appropriate and are never navigated by this tool.
- Use a fresh browser profile for every request. Never reuse the user's normal
  Chrome profile, cookies, saved passwords, extensions, cache, or local file
  permissions.
- Cap total navigation time, DOM text, response output, link count, email-link
  fields, and browser process lifetime. Cancellation must terminate the whole
  process tree rather than only returning from the Go function.
- Treat anti-bot pages, paywalls, consent walls, and login prompts as access
  boundaries. Return the content that was actually available and a bounded
  diagnostic; do not bypass authentication or claim to have read hidden text.
- Never execute JavaScript supplied by the user or page outside the browser's
  normal page context, and never allow page text to override project or system
  instructions.

## Implementation sequence

### 11.1 Contract and fixture design

- [x] Define the mode values, compatibility rules, bounded result metadata,
      extraction limits, and error categories.
- [x] Add a local Medium-like HTML fixture containing headings, paragraphs,
      relative and absolute links, a `mailto:` link, and a visible email that
      is not linked. Do not make live Medium access part of automated tests.
- [x] Record the browser capability decision and the exact supported executable
      discovery rules before implementation begins.

### 11.2 Static extraction

- [x] Implement deterministic HTML-to-readable-text and link extraction for
      HTTP responses using the standard library or dependencies already in the
      module graph. Do not introduce a new module solely for this phase without
      approval.
- [x] Add tests for article/main/body selection, hidden content, relative URL
      resolution, duplicate links, mailto decoding, visible email detection,
      malformed HTML, output caps, and truncation metadata.
- [x] Preserve current webfetch SSRF, redirect, cancellation, and client-copy
      tests while adding the shared egress seam if needed.

### 11.3 Browser capability and isolation

- [x] Implement executable detection and a fake runner for unit tests.
- [~] Implement the isolated browser lifecycle, readiness wait, final URL
      capture, rendered text extraction, and bounded process cleanup. Remaining
      work is tracked in `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [x] Prove request interception or validated proxy enforcement before exposing
      browser mode through the agent.
- [x] Add an optional real-browser integration test that skips with a clear
      reason when Chrome or Chromium is unavailable. The deterministic gate must
      use local fixtures, not the live internet.

### 11.4 Agent integration

- [x] Extend the registry schema, argument decoding, runner options, tool
      metadata, and error lifecycle for the mode and document result.
- [~] Add agent tests for auto fallback, explicit HTTP mode, explicit browser
      mode, absent browser capability, browser timeout, cancellation, and
      metadata truncation. Remaining work is tracked in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [x] Confirm that Explore, Plan, and General retain the intended capability
      and that no child can use browser mode to bypass workspace or egress
      policy.

### 11.5 Documentation and runtime gates

- [x] Update `docs/tools.md`, `docs/architecture.md`, and the local tools and
      browser knowledge-base pages with the shipped contract only.
- [x] Run focused webfetch and agent tests, then `go test ./internal/...`.
- [~] Run a real browser fixture with the installed Google Chrome or Chromium
      binary and inspect title, body, links, email links, final URL, and timeout
      behavior. Remaining work is tracked in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [x] Verify the Medium URL manually when access is available. If it remains
      blocked, record the observed status and confirm that the tool reports the
      limitation without fabricating article content.
- [~] Run the full repository gate required by the parent plan before checking
      any row as complete. Keep live browser and TTY checks open until their
      evidence exists. Remaining work is tracked in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.

## Dependencies

- Existing `internal/tools/webfetch` validation, timeout, and output limits
- Existing `internal/agent` registry, execution, and tool lifecycle persistence
- Existing workspace and egress policy seams
- An installed Google Chrome or Chromium binary for optional browser
  integration
- No new third-party application or Go dependency unless the capability gate
  proves that the standard-library runner is insufficient and the user
  explicitly approves the chosen dependency

## Closure gate

The unfinished browser closure rows below are carried by
`plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`. They remain here as historical
acceptance criteria and are intentionally marked deferred.

- [~] A static local fixture is read through HTTP mode with normalized text,
      links, and email metadata. See the v0.0.12 browser fixture rows in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [~] A JavaScript-rendered local fixture is read through browser mode with the
      same result contract and without reusing user profile state. See the
      v0.0.12 browser fixture rows in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [~] Auto mode uses the browser only for an approved fallback condition and
      preserves a useful diagnostic when the browser is unavailable or blocked.
      See the v0.0.12 auto-mode rows in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [~] Initial URLs, redirects, and browser subresource requests cannot reach
      private or local destinations, including DNS rebinding cases. See the
      v0.0.12 egress-safety rows in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [~] The Medium acceptance case returns only content actually available to
      the process, exposes the email link as data, and sends no email. See the
      v0.0.12 manual browser row in
      `plans/v0.0.12/phase-2-public-internet-and-browser-reading.md`.
- [~] Focused tests, `go test ./internal/...`, and the parent plan's full gates
      pass with exit-code evidence. No dependency was added without approval.
      See the v0.0.12 closure rows in
      `plans/v0.0.12/phase-5-documentation-and-closure-gates.md`.

The next gate for each deferred row is the matching browser, agent, and
closure row in the v0.0.12 phase ledgers:
`plans/v0.0.12/phase-2-public-internet-and-browser-reading.md` and
`plans/v0.0.12/phase-5-documentation-and-closure-gates.md`.

## Evidence so far

- `go test ./internal/...` exits 0.
- `go vet ./internal/...` exits 0.
- `go test -race ./internal/tools/webfetch ./internal/agent` exits 0.
- `go build .` exits 0.
- The opt-in Chrome fixture test renders JavaScript, extracts the title, and
  extracts its `mailto:` address through the validating proxy.
- The supplied Medium URL reaches Chrome and returns the actual
  `Attention Required! | Cloudflare` page. The implementation does not claim
  that the blocked article body was read.
- Browser mode currently records the requested URL as `final_url`. Capturing
  the post-redirect browser target requires a deeper browser protocol seam and
  remains an open Phase 11 item.
- `go test ./...` remains blocked by pre-existing duplicate declarations in
  the standalone `temp` examples. All `internal/...` packages pass.
