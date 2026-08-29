# Phase 2 - OpenAI provider

> **Parent:** `plans/v0.0.11/README.md` - v0.0.11 plan
> **Status:** complete
> **Estimated effort:** 3-4 days

---

## Overview

Add OpenAI as a second provider package behind the Provider seam so a GPT
main agent can run with Kimi/DeepSeek/GLM children over the OpenCode client,
without any aggregator. Independent of Phase 1; unblocks cross-provider
fan-out used by Phase 3.

## 2.1 Provider package

- [x] Create `internal/provider/openai` implementing the same Provider
      interface as `internal/provider/opencode`; chat-completions wire format
      at `https://api.openai.com/v1/chat/completions`; no new dependencies.
      The shared provider contract delegates the compatible wire format.
- [x] Unit tests against a fake HTTP server covering tool calls, streaming,
      and error mapping, matching opencode client test coverage shape.
      `internal/provider/openai/client_test.go` covers these paths.

## 2.2 Settings and keys

- [x] Add `provider.active` setting with values `opencode` (default) and
      `openai`; key resolution via `OPENAI_API_KEY` env or `.env`, mirroring
      `OPENCODE_API_KEY` handling including error text when missing.
- [x] Model catalog: static curated list (or cached `/v1/models`) written to
      `.lazykoder/models.json` cache path with the same 15 minute TTL
      semantics; `/model` picker and `r` refresh work unchanged. The shared
      catalog seam stamps OpenAI endpoints and uses the existing cache.

## 2.3 Cross-provider wiring

- [x] Verify the parent can run on OpenAI while `task` children resolve their
      models through the OpenCode client (per-role overrides unchanged);
      policy gate, persistence, and compaction behave identically. `main.go`
      wires the OpenCode child client when its key is available.
- [x] Gate: `make lint` PASS, `make test` PASS, then live TTY check by user:
      GPT main agent + Kimi/DeepSeek explore child in one session, drawer
      shows child jobs, final answer cites child output. Record outcomes
      beside these rows when they run. Automated gates pass; the live
      provider-key scenario remains a human check.
