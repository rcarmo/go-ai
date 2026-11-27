# v1.7.0 AI changed tests: group 3

Pin: `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Native base: `016c041fb016d866273fbbb09bf4d0dfe6830ef5`, verified clean and detached from the latest fetched `origin/main` before changes.

## Exact changed-test audit

These files were checked out at the pin and verified by Git object ID and SHA-256:

| Source | Git blob | SHA-256 | Exact v1.7.0 change |
| --- | --- | --- | --- |
| `packages/ai/test/tool-call-without-result.test.ts` | `2ef9b6a0179c4b5fe34f1a2717be07b00a20c21e` | `f531e126ba16db2c748eb463f06cb7b8b5f49ab79600453c356424d1d8c6bdd5` | Cloudflare Workers/Gateway IDs at lines 170 and 178 |
| `packages/ai/test/total-tokens.test.ts` | `4f0a5c42c5b2162e46a3c1d849be10709cbde612` | `aba56689255451d9f299a1c22946636141b4441ddf6f8b859cec07ff8cce2ac0` | Four display/model ID references at lines 313, 316, 338 and 341 |
| `packages/ai/test/unicode-surrogate.test.ts` | `3664f06ceaa366ef9774e8eab5b2a240a44495e2` | `c6e48008aa73f717a7805fc9efec5d81b3d603e1c591459a6463b8c6602213b8` | Cloudflare Workers/Gateway IDs at lines 501 and 517 |

The release diff changes only the model references above: Kimi 2.6 became GLM 5.3 for the two Cloudflare adapters. It changes no assertion, helper, prompt, fixture text or provider contract. The existing generated Go catalogue already contains both exact GLM IDs; no catalogue or production runtime change was justified.

## Original execution and credentials

The three unchanged originals ran with Vitest 4.1.12 on Node 26.13.0. The stage used the immutable `@earendil-works/pi-ai@1.7.0` data archive (`d0dd546e7c…8a12`) because generated provider data is not a tracked Git input. All three test source files still matched their pinned blobs after staging.

The isolated run had no provider environment variables, and used an empty HOME rather than reading OAuth storage. Result: **160 explicitly skipped live cases** — 31 tool-history, 36 usage and 93 Unicode. No credential or live-provider call was attempted, suppressed or claimed as passing. These cases require separate authorised credentials.

## Native comparison

The unchanged native base passed the existing relevant simulations and provider parsing controls. New production-path controls then established:

- Missing tool results become an errored `No result provided` synthetic tool message before the next user turn, on the actual OpenAI Completions wire serializer.
- A terminal OpenAI SSE usage object preserves input 5, output 1, cache read 3, cache write 2 and total 11.
- The version-neutral fixture contains the exact emoji and LinkedIn strings from the pinned source. The JavaScript 0xD83D input is represented by its exact lossless WTF-8 sequence, then verified absent from valid provider JSON while paired emoji remain unchanged.
- A non-finite payload-hook result is rejected before transport, retaining strict finite JSON.
- Existing catalogue, Anthropic, Google, Mistral, Responses and Bedrock usage tests cover their native parsing paths.
- Existing QuickJS bridge-string and aggregate output controls remain in place, including the 1 MiB bridge bound and 16 MiB aggregate cap. This lane changes neither image/output admission nor their tests.

No implementation difference was found. The only necessary additions are neutral production-path controls, the exact-text fixture, source locators and this audit.

## Locator policy

The [group-3 locator ledger](assertions/ai-changed-group3.jsonl) records exact source hashes, changed lines, helper assertion ranges, native controls and the credential remainder. The shared helper assertions are invoked by every `it` case and therefore cannot be represented honestly as independent direct `it` assertions by the current ledger validator; they are supplemental shared-contract locators instead of duplicated false cases. Run:

```sh
bun run docs/upstream-v1.7.0/assertions/validate.ts "$PI_ORACLE" "$PI_MIRROR"
bun run docs/upstream-v1.7.0/test-crosswalk.ts --check
```

The locator ledger checks source bytes without pretending that skipped live cases are deterministic native coverage.
