# v1.7.0 AI changed-test audit group 1

Target `fb0fe823539f6d04850f6e34748c01f01f212e1f` is compared with `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. This audit owns the changed tests `cross-provider-handoff`, `empty`, `image-model-data` and `providers`.

## Unchanged original controls

A private copy of the prior pinned v1.7.0 stage was compared byte for byte with `git show` from the pinned upstream mirror:

| Upstream test | SHA-256 |
| --- | --- |
| `cross-provider-handoff.test.ts` | `570fc7da4982321a07461f4ca71a7f2a81792345f060d4827e29cf153ac9ec57` |
| `empty.test.ts` | `6e5e297fee16a627a3ea5beffbab5dc67f0bc45db6e7236d4400089e3d6514e0` |
| `image-model-data.test.ts` | `b265e236f6cd077a5b9532d043a359ec2880b7a54781d4a49cd657ba387ad1bd` |
| `providers.test.ts` | `e274837a32f8cc7ec3609c0bf1867233aa522618f0060c04556138b854008972` |

The unchanged originals ran with Vitest 4.1.12 under one `v170-ai-changed-group1.lock`: five deterministic `image-model-data` tests and 31 deterministic `providers` tests passed. The 2 handoff cases and 124 empty-message cases were credential-gated; their files passed with no failures or executed network calls.

## Delta disposition

- `cross-provider-handoff.test.ts`: replaces the Cloudflare Kimi fixture IDs with `@cf/zai-org/glm-5.3` and its gateway-prefixed form. Existing native production-path catalog coverage already locates both records in `TestV0844CloudflareGatewayMirrorsToolCapableWorkersAIModels`; there is no behavior delta and no production change.
- `empty.test.ts`: makes the same two Cloudflare fixture substitutions. The original assertions are unchanged. Native catalog coverage locates the IDs, while credentialed provider requests remain an explicit live-only remainder rather than an offline implementation failure.
- `image-model-data.test.ts`: adds an image-output-specific OpenRouter catalog rate. Native generation preserves a present optional `imageOutput` independently of `output`, and `TestOptionalMetadataProjection` checks absent, zero and decimal rates. The renamed test is version-neutral; no production change was required.
- `providers.test.ts`: adds GitHub Copilot effort/cache metadata, two verified mid-conversation system models, one negative model, and Anthropic tool-change routing. The generated v1.7.0 catalog already contains each field. `TestBuiltinCatalogPreservesProviderConversationCapabilities` consolidates the native assertions in the version-neutral upstream-catalog suite.

## Native controls

Before any production edit, the focused native controls passed with `CGO_ENABLED=0` and `GOMEMLIMIT=650MiB`:

```text
go test ./tests -run '^(TestBuiltinCatalogPreservesProviderConversationCapabilities|TestImageOutputPrices|TestV0844CloudflareGatewayMirrorsToolCapableWorkersAIModels)$' -count=1
go test ./cmd/generate-models -run '^(TestOptionalMetadataProjection|TestSourceV170OfflineCatalogueChecksReleasedManifest)$' -count=1
```

No demonstrated native semantic difference was found, so the audit adds only consolidated tests/locators and evidence. The credential-gated live handoff and empty-message requests are not claimed as native runtime coverage.

## Control status

Focused native tests passed twice, as did vet, Staticcheck v0.8.2, logging checks, both assertion/crosswalk validators, `git diff --check`, and a clean-status inspection. The full `make test` did not pass: failures are outside this audit's changed files, concentrated in Durable scheduler/storage, QuickJS, one retired OpenCode fixture, and an existing durable consumer expectation. Production was not changed to address those unrelated failures.
