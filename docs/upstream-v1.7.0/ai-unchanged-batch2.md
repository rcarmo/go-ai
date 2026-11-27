# v1.7.0 AI unchanged originals batch 2

This receipt owns eight unchanged Anthropic originals at `fb0fe823539f6d04850f6e34748c01f01f212e1f`, alphabetically after batch 1 and disjoint from its ownership packet:

| Upstream test | SHA-256 | Pinned blob |
| --- | --- | --- |
| `packages/ai/test/anthropic-federation.test.ts` | `d1c8c53f9288ee9fa2e3caff6d7545f3ac721dd6ff532f84f6e01d766443a98b` | `d404fa0edc12cdb98fba9c605372e8a6cc1ac118` |
| `packages/ai/test/anthropic-force-adaptive-thinking.test.ts` | `ce72cc4021a30beb6a86428c2db0b0ace9a8406aad72f0087dcc22dfa45f1c92` | `6629782f2b616499aab84d56db70f8645aebd345` |
| `packages/ai/test/anthropic-long-cache-retention-e2e.test.ts` | `21e093fb3d734a1b19656b4820cc39f02d3ce3720cbd283b5e7c9e13e57668f9` | `7855c872df9491ffc60f91ed41bf28015e65d68d` |
| `packages/ai/test/anthropic-message-threads.test.ts` | `dab78091c5641dbffd93cc3ff50d7ce99723b4bfa47af92973f24d4545a0ca97` | `51e67cab882da608952fd5bf043dd76c7d640688` |
| `packages/ai/test/anthropic-mid-conversation-effort.test.ts` | `cd0b170dc16d3c48302483d9fcde1ebba3035781e467407293991c072bbf2fd8` | `4db36bff429c3a9001d9d09e89a5e6082398cf07` |
| `packages/ai/test/anthropic-oauth.test.ts` | `ba29fb5a43485901f73e1e9410f8ccebc3f3b92bb4ec092a448557cbe96bd9f8` | `e6cd4d604453a31489f337e318654d45ba0df79a` |
| `packages/ai/test/anthropic-opus-4-8-smoke.test.ts` | `8a35c1127e5ee30e0f1b3ad868bf7244cc6815373b840d7beac154b0a04b7844` | `44fa5aebb2cf133213d8a01f2d0e12bc0bf88b57` |
| `packages/ai/test/anthropic-sse-parsing.test.ts` | `591ec97b9c5ab0ad494a56624aad56e4ebf42091e7371da54efb070d19a44f6c` | `9f65f831a3e0ec8479fe07a6918496fe351d343d` |

The original files matched both the SHA-256 values above and `git show fb0fe823539f6d04850f6e34748c01f01f212e1f:<path>`. Vitest 4.1.12 ran under `/tmp/go-ai/v170-shared-verification.lock` with one worker and a 400 MiB Node old-space limit. Seven files passed and the credential-gated Opus file skipped at file level: 85 tests passed and 11 skipped. Ten forced long-cache provider probes and the Opus live smoke made no credentialed requests.

## Native comparison

The unchanged native Anthropic provider package and OAuth package passed before edits under `CGO_ENABLED=0` and `GOMEMLIMIT=650MiB`. That baseline established no broad regression, but did not exercise the provider raw-event observer exposed by the selected SSE original. Two translated production-path controls then failed on the unchanged implementation:

- `TestAnthropicProviderStreamEventsForwardInOrder`: the Anthropic adapter never invoked `OnProviderStreamEvent`; it observed zero of six events.
- `TestAnthropicReturnedModelIsSeparateFromRequestedModel`: a proxy model replaced the requested `model` field and left `responseModel` empty.

The native fix forwards each parsed Anthropic SSE object to the observer before normalization, terminating on observer error, while preserving the requested model in `model` and assigning a differing serving model to `responseModel`. Allowed fallback costs still use the serving model. The two legacy fallback expectations were reconciled with that upstream contract. Federation's optional service-account omission now has a direct token-body control; long-cache behavior, message-thread continuity/recovery, mid-conversation effort, and OAuth already had passing native production controls.

No production change was made for force-adaptive thinking, federation exchange/caching, cache retention, threads, effort markers, OAuth, or the live-only Opus answer. Native SDK-specific constructor details are represented by equivalent observable wire/auth behavior; the live model answer remains credential-gated.

## Verification

Under the shared lock, `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB` and `GOMAXPROCS=2`:

- focused translated controls: first red, then green;
- `go test ./inference/provider/anthropic -count=2`: pass;
- `go test ./oauth -count=2`: pass;
- selected catalog controls: pass twice;
- `make vet` and Staticcheck `v0.8.2`: recorded separately for this lane;
- `git diff --check`: pass.

This is a bounded local receipt, not full v1.7.0 acceptance or release authorization.
