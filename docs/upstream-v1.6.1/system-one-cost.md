# System One preserves response IDs and qualified OpenRouter billing

All 18 unchanged pinned TypeSafe System One originals passed offline on Node 26.13.0 and locked Vitest 4.1.12. The test, adapter, shared classifier and accounting sources, config and lock matched the pinned Git bytes. The focused unchanged native classifier tests ran before the new comparisons.

The old Go adapter dropped `id` and retained estimated totals even when OpenRouter supplied enough billing data. The public Classify comparisons now check the exact request, typed boolean answer, response ID and token counts. Go uses the same billing rule as its chat adapters: `is_byok: false` trusts the reported cost, `is_byok: true` also needs valid upstream cost, and missing accounting keeps the catalog estimate. A direct TypeSafe endpoint ignores OpenRouter billing fields. The input cost component remains the catalog estimate. A provider-ID URL override is a supplemental control.

The unchanged full OpenAI and Anthropic provider scopes passed with the shared arithmetic, and scoped classifier and cost tests passed twice without skips. Compiled overlays rejected dropping the response ID, trusting an unqualified bill and omitting the BYOK upstream cost. Vet and Staticcheck passed. This covers the TypeSafe transport; Cloudflare System One accounting and live traffic were outside the selection.
