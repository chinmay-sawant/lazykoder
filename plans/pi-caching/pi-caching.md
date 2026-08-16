# pi-caching - How pi hits ~99.5% prompt-cache hit rate (and how to replicate it in lazyKoder)

> **Parent:** `plans/pi-caching/` - findings folder
> **Status:** research writeup, 2026-08-16 (source-verified against pi 0.x dist and pi-ai; not yet implemented in lazyKoder)
> **Estimated effort to implement in lazyKoder:** 1-2 days
> **Gate:** after implementation, verify per-turn `cacheRead/(input+cacheRead)` >= 95% on turns 2+ of a session against the OpenCode Go endpoint

---

## TL;DR

pi hits 99.5% cache hit rate because the per-turn request delta is tiny (0.5% of the prompt) and the other 99.5% is **byte-identical to the previous request**. That comes from discipline, not cleverness:

1. System prompt built **once** per session, no timestamps, reused verbatim.
2. Tool definitions built **once**, fixed order, fixed schemas.
3. Append-only conversation: full history resent identically every turn, only the tail grows.
4. Three rolling `cache_control: {"type":"ephemeral"}` breakpoints (system prompt, last tool, last message).
5. **Session-affinity headers** (`x-session-affinity`, `x-client-request-id`, `session_id`, `prompt_cache_key`) with a stable per-session ID - the piece most homegrown harnesses miss on OpenAI-compatible endpoints.

The "trick" is mostly *not doing things*: no per-turn regeneration, no volatile prefix, no map-order JSON churn, plus the affinity headers that keep the provider's KV cache warm.

---

## 1. Evidence from real session logs

Usage numbers from `~/.pi/agent/sessions/--home-chinmay-ChinmayPersonalProjects-lazyKoder--/2026-08-16T10-05-40-120Z_01a00a08-bd98-73fa-84f7-180e628f437c.jsonl` (model `opencode-go/deepseek-v4-flash`, api `openai-completions`):

| turn | input (new) | cacheRead | cacheWrite | total | hit rate |
|---|---|---|---|---|---|
| 1 (cold) | 13,552 | 0 | 0 | 13,552 | 0% (full write) |
| 2 | 203 | 13,696 | 0 | 13,899 | 98.5% |
| 3 | 66 | 14,208 | 0 | 14,274 | **99.5%** |
| 4 | 1,346 | 14,208 | 0 | 15,554 | 91.3% |
| 5 | 372 | 16,128 | 0 | 16,500 | 97.7% |
| 6 | 4,711 | 16,640 | 0 | 21,351 | 77.9% |
| 7 | 1,476 | 21,504 | 0 | 22,980 | 93.6% |
| 8 | 14,052 | 23,168 | 0 | 37,220 | 62.2% |

Reading the table correctly:

- Turn 3 pays for **66 new tokens out of 14,274** - that is the 99.5% figure.
- The dips (turns 6 and 8) are the turns where the agent appended a giant `find`/`ls` tool output. That is genuinely new content, **not lost cache**.
- `cacheWrite` stays 0 after turn 1: nothing is ever re-written. The entire ~14K-23K prefix was served from cache on every turn 2-8. The cache itself never missed.

Conclusion: hit rate is high because the harness made the prompt grow slowly and kept the prefix byte-stable.

---

## 2. What pi does (verified in source)

### 2.1 System prompt built once (`system-prompt.js`)

`buildSystemPrompt()` runs at session start; the result is stored (`_baseSystemPrompt`) and reused verbatim on every request. What is *not* in it: no `Date.now()`, no timestamp, no per-turn state. The only dynamic values are `cwd` (constant per session) and project context files (constant). Extension-modified prompts are cached too (`_systemPromptOverride`), rebuilt only when the extension output changes.

The #1 cache killer in naive harnesses is a per-turn timestamp in the system prompt ("Current time: ...") which invalidates everything after it. pi never does this.

### 2.2 Tool definitions stable (`agent-session.js`, ~line 628)

Tools and their JSON schemas are built once, in the same order. The system prompt is rebuilt only when the tool set changes - a deliberate, rare invalidation event. No map-iteration-order churn, no regenerated descriptions per turn.

### 2.3 Append-only conversation

Every request resends the full history in the same order; only the tail grows. Tool results are appended as proper messages matching their `tool_call` IDs. Sessions persist as typed entries in `session.jsonl`; `convertToLlm()` is a pure deterministic function (role + content array, fixed field order). Timestamps live in the session model but are **never serialized into LLM content**.

### 2.4 Three rolling cache breakpoints (`pi-ai` `openai-completions.js`, `applyAnthropicCacheControl`)

```js
function applyAnthropicCacheControl(messages, tools, cacheControl) {
    addCacheControlToSystemPrompt(messages, cacheControl);            // 1. system prompt
    addCacheControlToLastTool(tools, cacheControl);                   // 2. last tool definition
    addCacheControlToLastConversationMessage(messages, cacheControl); // 3. last message
}
```

with `cache_control = { type: "ephemeral" }` (default "short" retention), or `{ type: "ephemeral", ttl: "1h" }` when `PI_CACHE_RETENTION=long`.

The third breakpoint is **rolling**: it always lands on the last user/assistant/tool message, so as the conversation grows the cache boundary moves forward with it. The whole stable prefix (system + tools + history) is always one cache block.

### 2.5 Session affinity headers (what makes OpenAI-compatible caching work at all)

`openai-completions.js` (~lines 482-530): pi generates a **stable session ID once per session** and sends it on every request:

```
x-session-affinity: <id>
x-client-request-id: <id>
session_id: <id>
```

plus `prompt_cache_key` (clamped session ID) in the body where the endpoint supports it. Without these, an OpenAI-compatible provider cannot associate requests into one cache session and would re-write the prompt every turn.

### 2.6 Cache miss telemetry (`cache-stats.js`)

pi computes per-turn missed tokens, the wasted cost (missed tokens billed at input rate instead of cacheRead rate), and flags misses above a 1,024-token noise floor. It knows the failure modes: compaction legitimately resets the cache, and idle gaps over the TTL (5 min short / 1h long) cause a re-write. The harness has a feedback loop, so cache discipline is observable, not aspirational.

---

## 3. How to get the same in lazyKoder

lazyKoder spawns OpenCode Go and talks to an OpenAI-completions-compatible endpoint (`opencode-go`, `deepseek-v4-*` per the session log). The recipe:

### A. Message construction (the 90%)

1. Build the system prompt **once** at session/model init. Store it in a struct field, reuse verbatim. Never put `time.Now()` in it. If the model needs a date, put it in the first user message, not the system prompt.
2. Build the tools array **once**, in fixed order, with fixed schemas. In Go, marshal tool schemas from a fixed struct or pre-built JSON, not from a fresh `map[string]any` per request. (`encoding/json` sorts map keys alphabetically, which is stable, but explicit structs are clearer and immune to drift.)
3. Append-only history: keep `[]llm.Message`, resend all of it every request, append at the tail. Keep timestamps in the session model, never in message content.
4. When a tool runs, append its result as a `tool` message with the matching `tool_call_id`, in the exact order the model issued the calls.

### B. Cache breakpoints (Anthropic-style, if the endpoint accepts them)

Put `{"type":"ephemeral"}` on:
1. the system prompt content block,
2. the last tool definition,
3. the last user/assistant/tool message (the rolling one - this is what keeps the whole prefix cached as the conversation grows).

### C. Session affinity (for OpenAI-compatible endpoints - this is lazyKoder's case)

1. Generate one session ID per session (UUID), reuse it for **every** request in that session. A fresh ID per request defeats affinity.
2. Send it as `x-session-affinity`, `x-client-request-id`, and `session_id` headers on every call.
3. Send `prompt_cache_key` (clamped session ID) in the body if the endpoint supports it.
4. Do not rebuild the HTTP client or headers per turn.

### D. Avoid the known cache killers

- No timestamps or randomness in the prefix.
- No reordering of tools or messages on resume - persist history in canonical order and replay it identically.
- Compaction resets the cache: do it rarely, and when you do, use a real summary (pi wraps it in `<summary>` blocks) rather than silently dropping history.
- Idle gaps over the provider TTL cause a re-write. If sessions idle a lot, use long retention (`prompt_cache_retention: "24h"` / `ttl: "1h"`).

### E. Measure it (make the discipline observable)

Read `usage.prompt_tokens_details.cached_tokens` (or `prompt_cache_hit_tokens`) from each response, compute `cacheRead/(input+cacheRead)` per turn, and log a warning when a turn that should have hit misses above ~1K tokens. This mirrors pi's `cache-stats.js` and catches regressions the day they are introduced.

---

## 4. Failure-mode checklist (what to grep for in lazyKoder before calling it done)

- [ ] No `time.Now()` / date string in the system prompt path
- [ ] System prompt built once and cached on the model/session struct, not per request
- [ ] Tools built once with deterministic (struct) serialization
- [ ] History resent in canonical order; append-only
- [ ] Stable session ID reused across all requests in a session
- [ ] Affinity headers + `prompt_cache_key` present on OpenAI-completions calls
- [ ] Cache breakpoints on system prompt, last tool, last message (if Anthropic-style supported)
- [ ] Per-turn hit-rate logging in place

---

## 5. References

- pi source: `node_modules/@earendil-works/pi-coding-agent/dist/core/system-prompt.js`, `dist/core/agent-session.js`, `dist/core/cache-stats.js`
- pi-ai source: `node_modules/@earendil-works/pi-ai/dist/api/openai-completions.js` (lines ~475-810), `dist/providers/data/opencode-go.json`
- pi docs: `docs/models.md` (`cacheControlFormat`, `sessionAffinityFormat`, cache retention), `docs/settings.md` (`showCacheMissNotices`)
- Real usage data: `~/.pi/agent/sessions/--home-chinmay-ChinmayPersonalProjects-lazyKoder--/2026-08-16T10-05-40-120Z_01a00a08-bd98-73fa-84f7-180e628f437c.jsonl`
