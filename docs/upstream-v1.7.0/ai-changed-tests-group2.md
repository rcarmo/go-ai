# AI changed-test group 2

## Pinned originals

The isolated source checkout at `fb0fe823539f6d04850f6e34748c01f01f212e1f` supplied these byte-verified originals:

| Test | SHA-256 |
| --- | --- |
| `packages/ai/test/qwen-token-plan-models.test.ts` | `def12b77206be50df46dc8f6dcb7340bab18425f16d2b1aafb0c7383c14339ea` |
| `packages/ai/test/stream.test.ts` | `213184d2d8a487de8a988fd8470520ff0e6a86706c464607d5b59b5d74d431e2` |
| `packages/ai/test/together-models.test.ts` | `6d3afbce1a00f3d41e47b5b43c066804a21f3754bed1ee3b7adb32c0825522bf` |
| `packages/ai/test/tokens.test.ts` | `3f493a40d9a5e6f031d7ec9d183470f24e10537eb4a01fea4ef461839b1b7ba7` |

The private stage also matched the locked `package-lock.json` (`2a2523fdeacf04ccc5f707132d62ba6a091df196d0b56ff07da1d527f1ff7502`) and `packages/ai/vitest.config.ts` (`628ade6e9960f0378ef8ddfd1c06f3a12cf375199725f3f6b41de0c8cf5c0313`). The four files ran unchanged with Vitest 4.1.12 and Node 26.13.0. Qwen and Together completed 123 assertions; `stream` and `tokens` were credential-gated (270 skipped tests across their two files). The skips are original upstream behavior, not native skips.

## Disposition

The version delta is a catalog-fixture change, not a new executable behavior:

- Qwen Token Plan replaces the retired `qwen4-max-preview` catalog fixture with `qwen4-max` in both Individual endpoint lists.
- Together replaces retired GPT-OSS/DeepSeek controls with two current catalog entries whose compatibility is the provider toggle (`thinkingFormat: together`, explicit `supportsReasoningEffort: false`).
- `stream` and `tokens` rotate Cloudflare fixtures from Kimi K2.6 to GLM 5.3 and the Ant Ling streaming fixture from Ling 2.6/Ring 2.6 to Ling 3.1. Their shared executable helpers did not change.

Native catalog and production-route checks pass on the unchanged implementation. There was no proven provider-runtime mismatch to fix. The version-neutral Go tests now exercise the four live Qwen plans, current Together toggle models and their captured Chat Completions payloads. Existing native coverage supplies the Cloudflare direct/gateway route and Ant Ling thinking/stream compatibility.

## Retired fixtures

The detached v1.6.0 Qwen preview and Together reasoning fixtures were deleted from active tests. They only reproduced IDs removed by the pinned v1.7.0 catalog and would preserve obsolete product expectations as live test inputs. The catalog tests still assert that `qwen4-max-preview`, the retired Together IDs, and the other removed Qwen IDs are absent. No retired ID was restored to generated catalogs or registries.

Two mutation controls deliberately changed test expectations back to a retired Qwen preview and retired Together DeepSeek ID. Both controls failed at the intended catalog-lookup assertions. Production and generated data were not mutated.

## Native verification

Focused native tests ran twice with shuffle under one bounded lock. The current Qwen, Together, Cloudflare direct/gateway and Ant Ling production routes passed. This receipt does not claim live credential tests: Cloudflare and Ant Ling network scenarios remain unavailable without their upstream credentials.
