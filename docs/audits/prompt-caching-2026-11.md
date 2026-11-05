# Prompt caching audit (local data, 2026-11)

## Scope and limits
- Data: 7 retained Kintsugi durable journals in `/kintsugi/session-share/builtin-durable` (6,364 assistant calls, 20 conversations, 2026-10-10 → 2026-11-05). Only per-call `usage`, provider/model, timestamps and entry kinds were read; no prompt text is reproduced here.
- These journals were produced by the Kintsugi agent runtime (stack traces name `pi-ai` 1.2.0/1.3.0/1.5.0). go-ai appears in them only as file paths. **The data measures that runtime, not go-ai's providers.**
- Journals contain no request payloads, so cache *causes* below are inferences from usage shape, not observed requests.
- Costs are catalog-derived (`totalFromProvider:false`); for subscription providers (GitHub Copilot, Codex) they are notional, not billed.

## Findings from local data
1. **Caching worked well until late October.** Through 2026-10-21, consecutive same-model calls <5 min apart (previous prompt ≥30k tokens) reused essentially the whole previous prompt: 0–1% broken per day across Copilot GPT, Copilot Claude and Codex.
2. **Then a step regression, per runtime/provider, not per idle time:**

   | Date | Provider/model | Broken pairs* |
   |---|---|---|
   | 10-23 → 10-28 | openai-codex gpt-6.1-sol | 93–100% |
   | 11-04 → 11-05 | github-copilot gpt-6.1-sol | 83–100% (one workspace 0% on 11-04) |
   | 11-05 | github-copilot gpt-6.1-terra | 100% |
   | 11-05 | azure-claude claude-opus-5-5 | 64–96% |
   | 11-05 | github-copilot mai-code-1.1-flash, gpt-6-luna | 0–6% |

   \* next call reused <50% of the previous prompt, gap <5 min.
3. **Broken calls reuse a fixed prefix only.** `cacheRead` freezes at a constant per conversation (Codex 10,368/10,496; Copilot 10,682/10,981/10,999; Azure Claude 18,787/18,814) while the prompt grows to 100k–900k. The constant matches each conversation's first-request size, i.e. roughly system prompt + tools. So something early in the message history changes on every request (or history is re-serialized non-deterministically), invalidating everything after the static prefix. Entry kinds between broken pairs are the same as between good pairs (tool results, timing), so it is not caused by a new entry type being inserted.
4. **Impact.** About 540M prompt tokens were re-read/re-written instead of cache-hit after the regression. On Copilot gpt-6.1-sol, cache writes are $1,488 of a $1,741 notional total; gpt-6.1-terra is 97.5% cache write. Every call in a broken conversation pays full write cost on the whole context.
5. **Expected misses, correctly small.** Idle gaps >1h nearly always miss (TTL expiry: 30/30 Copilot, 6/8 Codex); 5–60 min gaps miss ~13%. 63 zero-usage calls are errors/aborts. Model switches inside a conversation always start cold (expected: caches are per model).
6. **Coincident change (not proven cause):** the regression dates line up with new assistant fields `thinkingLevel` / `providerThinkingLevel` appearing in records, i.e. a runtime release that changed reasoning/thinking handling. Thinking-level or reasoning-item replay that varies per request would produce exactly finding 3.

## go-ai implementation check
- Mechanisms exist and mirror pinned upstream pi-ai: `CacheRetention` none/short/long + `PI_CACHE_RETENTION` (`env.go`); OpenAI Responses/Codex `prompt_cache_key` (clamped to 64 code points), `prompt_cache_retention: 24h`, `prompt_cache_options`; Completions key gating (api.openai.com or long+compat) and Anthropic-format `cache_control`; Anthropic `cache_control` with 1h TTL when supported; Bedrock cache points; Mistral `prompt_cache_key` + `x-affinity`; session-affinity headers; 5m/1h cache-write accounting (`CacheWrite1h`) and cost math; faux provider prefix-cache simulation.
- Focused tests (`-run '(?i)cache|session|usage'`) pass for root, openairesponses, openaicodex, anthropic, openai, faux.
- **Not done:** line-by-line upstream diff of every provider's breakpoint placement; a request-determinism test (same history serialized twice → byte-identical prefix, including reasoning items and thinking signatures); live provider runs. go-ai has no production journal data to measure.

## Recommendations
1. Capture two consecutive raw request bodies from the affected runtime for one broken conversation and diff them; the first differing byte after ~10k tokens is the cause.
2. Add a go-ai regression test: build request N and N+1 for a growing tool-loop history (with reasoning/thinking enabled, each provider) and assert request N is a byte prefix of N+1 up to the last cache breakpoint.
3. Add cache-health telemetry: per conversation, alert when `cacheRead` < 50% of the previous prompt with a gap under the TTL.
