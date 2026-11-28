# v1.7.0 AI unrepresented image, message and thinking comparison

Pin: `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Native base: clean `d557b0a191976d8bf0c78e54488cf02162dfe062` on local main. Isolated worktree: `/work/port-lanes/ai-image-json-batch` on `port/v170-ai-image-json-batch`.

## Originals

The pinned detached source checkout was byte-checked against these Git objects. All six files matched `HEAD` before the comparison run; the source checkout remained clean afterward.

| Upstream test | Git blob | SHA-256 |
| --- | --- | --- |
| `packages/ai/test/image-tool-result.test.ts` | `e8c42ad234b7c34e559a096af9dd0fa7c386efd9` | `3aad54caa277960bd8c23cefb1d728dad37af47f590b49185a8bf51d919a473c` |
| `packages/ai/test/images-models.test.ts` | `1993f8619585220a1c84bfd9ca2b5ac4c72febd8` | `8739d53df9b6d50ad1211b52590ab7488917636e43e2b7b12ef751583206f780` |
| `packages/ai/test/images.test.ts` | `8f4a2acae223247f90622e4a5c6cd91712e65f0c` | `fba99a5b5e039f15e47e478243876cfecb28cc2fca6dc045f3157d15e8aa0d45` |
| `packages/ai/test/interleaved-thinking.test.ts` | `b787a2cf765bf4c50d15499bc6312ef1e57191db` | `7434abff7d439beaf80bb3d7e5fd208cfa1397765f2c5ca7b546f14d9a743afe` |
| `packages/ai/test/json-parse.test.ts` | `5783d956849a91c7971f794dc9884feddc126de3` | `ed5137f776ef10f3429d83769dc50eb5d7185ddfc5d260ad82d553d40c8d8581` |
| `packages/ai/test/lax-message-content.test.ts` | `24a7df3cb3bb461e6bb67b75e52c8274c11718f9` | `2a8ee2048658a1a0a2a8928cc9570c7db41ed295ca20b1d23098b70d4d39e196` |

With Node `26.13.0`, Vitest `4.1.12`, one worker, no credential environment or OAuth home, and a 650 MiB old-space cap, all six exact originals ran once: **3 files and 22 deterministic tests passed; 3 files and 52 live-provider tests skipped**. `images-models.test.ts`, `json-parse.test.ts` and `lax-message-content.test.ts` supplied the deterministic originals. The JSON and lax assertions already have native receipts and locators and were not duplicated by this lane.

## Native comparison

`tests/ai_unrepresented_image_json_upstream_test.go` compares the remaining public paths without credential or live traffic:

- typed model reads and dynamic entries expose chat, image and classifier records separately and together;
- image records refresh and persist in the public model store;
- image generation accepts text plus image input through the registered OpenRouter public transport;
- image-only and mixed tool results remain image-bearing for vision models and downgrade as text for text-only models;
- Anthropic public replay sends signed thinking, a tool call and its result into the second request, then accepts thinking plus final text in the second response.

The unchanged runtime lacked `ModelRuntime.GetModelsOfType`, `GetModelOfType` and `GetAllModels`. It also admitted explicitly chat-typed records from an image or classifier catalog. A focused compile/run first demonstrated the missing public typed accessors; commit `15f4c0774d99bf197bcf48ffa6d32f4454202071` adds those accessors and rejects catalog records whose explicit type disagrees with their native collection. No message transform, image transport or thinking-replay production change was required.

The upstream helper-driven live suites assert remote model quality and provider credentials. Their deterministic schema and replay portions are represented here; remote red-circle descriptions and interleaved-thinking quality remain unverified live outcomes, not local parity evidence.

Native model collections do not own provider credential resolution or request transforms as TypeScript's `Models` collection does. Direct native image options, provider transports and public typed catalogs are tested here. The unrepresented availability, provider-auth merge and cross-operation construction assertions are recorded as partial or unmatched rather than presented as complete `Models`-collection parity.

## Verification

Under the shared `/work/port-lanes/.v170-focused-verify.lock`, `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2`, `GOGC=60` and `GOFLAGS=-p=1`:

- the focused five-test selector passed twice with `-count=2`;
- focused `go vet` passed for the root, `./tests` and `./images/...`;
- pinned Staticcheck `v0.8.2` passed for the same focused packages;
- `git diff --check` passed.

This is a bounded offline comparison. It is not a full v1.7.0 acceptance, full test-suite run, acceptance profile, hosted validation, live-credential replay or release authorization.
