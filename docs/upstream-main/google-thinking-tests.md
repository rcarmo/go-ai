## Google thinking-map tests

Pinned source: `4b535d57ea8863dbba44b89872146523da271786`, `packages/ai/test/google-thinking-level-map.test.ts`. All 15 expanded cases are translated in `inference/provider/google/thinking_map_upstream_main_test.go`.

| Upstream case | Expanded cases | Native test / subcase |
| --- | ---: | --- |
| Exhaustive logical and mapped resolver values | 1 (30 checks) | `TestUpstreamMainGoogleThinkingMapResolver` |
| Lowest supported level when reasoning is omitted, both adapters | 2 | `TestUpstreamMainGoogleThinkingMapAdapters/{api}/lowest_when_reasoning_omitted` |
| Gemini 3.1 Pro native medium effort, both adapters | 2 | `.../{api}/preserves_3.1_Pro_medium` |
| Gemini 2.5 disabled when reasoning is omitted, both adapters | 2 | `.../{api}/disables_2.5_when_omitted` |
| Gemini 4 discrete levels, both adapters | 2 | `.../{api}/Gemini_4_uses_levels` |
| Google xhigh and max mapped to high | 2 | `.../Google_extended_{xhigh,max}` |
| Google uppercase provider mapping | 1 | `.../Google_uppercase_mapping` |
| Google mapped custom token budget | 1 | `.../Google_mapped_budget` |
| Vertex extended level | 1 | `.../Vertex_extended` |
| Vertex mapped custom token budget | 1 | `.../Vertex_mapped_budget` |

`{api}` is `google-generative-ai` or `google-vertex`. Tests call each registered adapter's production `StreamSimple` function. A payload hook captures the request and returns the same `payload captured` sentinel used upstream; the emitted error must contain it. No HTTP call or live credential is used. SDK `config.thinkingConfig` is compared with native REST `generationConfig.thinkingConfig`; complete-object versus subset assertions follow the upstream cases. The resolver checks all four standard levels, all eight lower/uppercase mapped values at high/xhigh/max, and both diagnostics including provider/model context.

Against unchanged native `65734fc80f597556caa0f1e1ea35a6d4049b6441`, six adapter cases failed: omitted reasoning chose MINIMAL instead of the lowest supported LOW, Gemini 3.1 Pro medium became HIGH, and mapped custom budgets were ignored, on both adapters. Both resolver diagnostic checks also failed. The remaining assertions passed. These tests ran before production changes.

The fix uses capability-based clamping, preserves resolved discrete levels, honours custom mapped budgets (including zero), and propagates invalid mappings instead of silently choosing high. Existing disable fixtures now include the capability maps their expected levels require; their wire assertions are unchanged. The older raw-helper null-low rejection expectation is replaced with the pinned raw resolver result plus production upward-clamping and extended-null rejection checks. A transient catalogue lookup fixture failed under race validation; self-contained capability maps removed that harness dependency.

## Source-derived control-flow probes

`thinking_map_source_main_test.go`, `TestMainSourceGoogleThinkingControlFlow`, adds seven cases per adapter from pinned `google-shared.ts#getDisabledGoogleThinkingConfig` and the adapters' `streamSimple`/`getGoogleBudget` functions. These 14 cases are additional probes, separate from the 15 upstream test cases: omitted reasoning with off supported; explicit off; upward clamping of unsupported low; explicit zero custom budget; case-insensitive Gemini 4 wire-format detection; and invalid mapped values in both enabled and disabled paths. Invalid maps must emit one error before invoking the payload hook.

The first 12 probes fail against the exact baseline production `google.go` supplied through Go's `-overlay`, and pass with the fix. The two case-insensitive probes then failed against the intermediate implementation before lowercasing Gemini 4+ detection. All 14 pass with the final fix. The overlay, copied baseline file and diagnostic log were removed after comparison.

## Development validation and limits

With `scripts/project-env.sh`, Go 1.27.3 and `GOMAXPROCS=4`: full root-module normal tests, Google race tests, root `go vet ./...`, scoped Google staticcheck v0.8.2 and `git diff --check` pass. Root `make staticcheck` reports 23 findings in unchanged files outside Google: four unused declarations, 17 capitalised error strings, one HTTP-status literal and one unexported-field JSON warning. Those findings require their own review; the full staticcheck gate is open.

No live thinking-disable/signature tests, Vertex credential/ADC integration, remaining Google test-file crosswalks, full acceptance or profiling are covered by this slice. Provider-only thinking options and other source behaviours also need review. One identified follow-up is token-budget family matching: native lowercases model IDs, while pinned `getGoogleBudget` uses case-sensitive `includes`; add source-derived tests before changing that path. Generated catalogues and native `v1.4.0` are unchanged.
