# Kintsugi runtime: prompt caching report (2026-11)

Audience: Kintsugi runtime maintainers. Source: 7 retained built-in durable journals (`/kintsugi/session-share/builtin-durable`), 6,364 assistant calls, 2026-10-10 → 2026-11-05. Only usage counts, provider/model, timestamps, entry kinds and diagnostic stack paths were read. No request bodies exist in the journals, so causes are inferred.

## Summary
A cache-reuse regression started **after the runtime's pi-ai 1.2.0 → 1.3.0 upgrade (Codex, from 2026-10-23)** and spread to **GitHub Copilot GPT and Azure Claude from 2026-11-04/05 (runtime pi-ai 1.4.0/1.5.0, when `thinkingLevel`/`providerThinkingLevel` first appear on assistant records)**. In affected conversations, each call reuses only the static prefix (system prompt + tools) and rewrites the entire history to cache.

## Runtime versions observed
Taken from `/app/node_modules/.bun/@earendil-works+pi-ai@…` paths in transport-failure diagnostics (sparse evidence):

| First seen (UTC) | Runtime pi-ai |
|---|---|
| ≤ 2026-10-21 00:21 | 1.2.0 |
| 2026-10-21 19:34 | 1.3.0 |
| 2026-11-04 08:02 | 1.4.0 (journals of 11-04/05 also reference 1.5.0 / pi-coding-agent 1.5.0 / pi-durable 1.5.0) |

## Cache health by period
Pairs = consecutive same-model calls in one conversation, gap < 5 min, previous prompt ≥ 30k tokens. Broken = next call reused < 50% of previous prompt.

| Period | Provider / model | Broken |
|---|---|---|
| 10-12 → 10-21 | all (Copilot GPT/Claude, Codex) | 0–1% |
| 10-23, 10-24, 10-28 | openai-codex gpt-6.1-sol | 93%, 100%, 99% |
| 11-04 | Copilot gpt-6.1-sol / Codex (main workspace) | 100% / 100% |
| 11-05 | Copilot gpt-6.1-sol | 83–100% |
| 11-05 | Copilot gpt-6.1-terra | 100% |
| 11-05 | azure-claude claude-opus-5-5 | 64–96% (fast variant 82%) |
| 11-05 | Copilot mai-code-1.1-flash, gpt-6-luna | 0–6% |

## Failure signature
- `cacheRead` is pinned at a constant per conversation (Codex 10,368/10,496; Copilot 10,682/10,981/10,999; Azure Claude 18,787/18,814) while prompts grow to 100k–900k. The constant equals the first request's size ≈ system prompt + tool definitions.
- Therefore the first divergence between consecutive requests is at the start of message history, every request. Candidates, in order of fit:
  1. Replayed reasoning/thinking items (encrypted reasoning, thinking signatures, or thinking-level–dependent conversion) serialized differently per request — fits the 11-04 onset with `thinkingLevel` fields and the unaffected non-reasoning/medium models.
  2. A per-request value injected early in history (timestamps, IDs, timing/metadata, reordering).
  3. Changed session/cache key per request (would usually yield `cacheRead=0`, not a stable prefix — less likely).
- Entry kinds between broken pairs equal those in healthy pairs (`pi.tool-result`, `kintsugi.timing`), so no new entry type is being inserted into history.

## Impact (catalog-estimated; Copilot/Codex are subscriptions, not billed)
- ~540M prompt tokens rewritten or uncached instead of read after the onset.
- Copilot gpt-6.1-sol: cache writes $1,488 of $1,741 total estimate; gpt-6.1-terra 97.5% of prompt tokens are writes.
- Codex: 810 partial-reuse calls, median ~214k tokens not reused per call.
- Also expect higher latency and rate-limit pressure on large contexts.

## Normal behaviour (no action)
- Idle > 1h: ~all miss (TTL). 5–60 min: ~13% miss.
- Model switch in a conversation always starts cold.
- 63 zero-usage calls are errors/aborts (52 on Azure Claude).

## Recommended actions
1. **Confirm the cause:** in a current workspace, log the serialized provider request for two consecutive tool-loop calls (Copilot gpt-6.1-sol, thinking `high`) and diff; report the first differing JSON path after the tools/system section.
2. **Bisect** runtime pi-ai/pi-coding-agent 1.2.0 → 1.3.0 (Codex) and 1.3.0 → 1.5.0 (Copilot/Azure Claude) for changes to history conversion, reasoning replay and `thinkingLevel` handling.
3. **Regression test:** request N must be a byte prefix of request N+1 (up to the last cache breakpoint) for each provider with reasoning enabled.
4. **Monitoring:** Kintsugi already exports cache counts and weighted cache %; add an alert when per-conversation reuse < 50% with gap < 5 min, e.g. over 3 consecutive calls.
5. **Mitigation until fixed:** prefer models not affected (gpt-6-luna, mai-code-1.1-flash showed normal reuse), and compact long conversations more aggressively since every call pays full-context writes.

## Limits
- Version-to-date mapping relies on a handful of error stack traces.
- No raw requests; root cause unconfirmed.
- Two workspaces only cover the post-onset period, so per-model comparisons on 11-05 are from few conversations.
