# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v0.99.1`
- Upstream tag/SHA: official npm `@earendil-works/pi-ai` `0.99.1` package artifact; upstream git tag SHA not embedded in the npm package metadata.
- Previous accepted upstream baseline: `v0.87.1` / `f07218c4d4bbc12bef056a7058c3dd49dfe41abe`
- Previous accepted Go runtime baseline before this audit: `c2d0231d8bef63a920e1663143e6c1c39ef0679d`
- Current repository baseline before this audit: `459c0e2` (`Record v0.87.1 acceptance [skip ci]`)
- Official npm artifact: `/workspace/tmp/pi-ai-0991/earendil-works-pi-ai-0.99.1.tgz`
- Official npm artifact SHA-256: `f9f44692157d0bf5679c4a17304a310028231d7daaeaaea3b73252f4b7a264d3`
- Detailed path matrix: `docs/v0991/changed-paths-crosswalk.md`
- Changed-test crosswalk: `docs/v0991/changed-tests-crosswalk.md`
- Whole-corpus upstream test crosswalk: `docs/v0991/test-corpus-160-crosswalk.md`

## Scope evidence

- Changed paths: `169` canonical rows, committed at `docs/v0991/changed-paths.txt`, SHA-256 `086beb5b751f144f9e45034bfbb2b0f4e8a0d3a00f9e17ec1b073b3a8397221e`.
- Changed tests: `58` rows, committed at `docs/v0991/changed-tests.txt`, SHA-256 `d8eefa94ed87c03de0545965351de4d800cf76ce1db33b0f62fab0f9ab3c7acc`.
- Whole upstream test corpus: `160` basename-only rows, committed at `docs/v0991/test-corpus-160.txt`; SHA-256 `7ad5f140edc5bc49a348b7b7e36ea266dd82a3075c23ad9997b6e8bc21992b06`.
- Schema-v6 manifest: `docs/v0991/schema-v6-manifest.json`, schema version `6`, structure hash `58511a57fb2db5e984ee62857d8079aec6ff800e19226c327c118e7f57ea916b`.
- Source/test delta spans typed model lookup, classifier runtime transports, llama.cpp classification, OpenAI/Responses raw stream and ChatGPT sign-in behavior, ChatGPT OAuth/callback handling, `thinkingLevel`/`nestedCalls` transcript fields, and generated chat/image/classifier catalog metadata.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.99.1:

- Exact generated chat catalog refresh to `1523` models across `41` providers and `10` chat APIs, preserving schema-v6 `Type: "chat"` metadata.
- Exact generated image catalog refresh to `57` image models and classifier catalog generation to `12` classifier models across `5` providers.
- Unified typed model lookup/listing for chat/image/classifier model references and type-aware compatibility checks.
- TypeSafe/System One and Cloudflare Workers AI classifier transports with deterministic request/response, usage/cost, retry, timeout, cancellation, and malformed-answer behavior.
- llama.cpp classifier transport with deterministic question ordering, label token cache, `/tokenize`, `/apply-template`, `/completion`, `n_probs` escalation, temperature probability handling, hooks, and error/cancel/timeout paths.
- OpenAI Completions and OpenAI Responses raw provider stream event hooks before normalization, with callback failure terminating the stream.
- OpenAI Responses direct ChatGPT-token field suppression for non-`sk-` credentials against official `api.openai.com/v1`, while empty credentials retain legacy prompt-cache serialization.
- HTTP/SSE ChatGPT usage-limit guidance for `subscription_sharing_usage_limit_exceeded`.
- ChatGPT OAuth authorization-code/callback provider with direct-token scope validation, issued client ID persistence, refresh, and deterministic callback-server coverage.
- Transcript wire support for assistant `thinkingLevel` and tool-result `nestedCalls`, with provider payload exclusion coverage.
- v0.99.1 inventory and crosswalk validator now fails closed on missing/duplicate/exact-set/order mismatches and unresolved/pending markers; negative self-test covers inventory, crosswalk, and schema corruption.
- Regeneration comparator updated to the v0.99.1 unified schema-v6 source for chat/image/classifier catalogs. The negative self-test requires typed filename diagnostics, the exact v0.99.1 mismatch phrase, corruption marker, and diff evidence for each typed output.

Historical v0.85.1, v0.87.0, and v0.87.1 runtime/SBOM/README release evidence remains in this file and git history. README and current public SBOM links intentionally remain on the previously published v0.87.1 release until the guarded v0.99.1 publisher completes.

## Validation evidence

Current v0.99.1 acceptance evidence:

- `docs/v0991/changed-paths.txt`, `changed-tests.txt`, and basename-only `test-corpus-160.txt` match pinned hashes.
- `scripts/validate-v0991-inventory.py` and `--self-test` — passed; corruption self-tests cover all three manifests, crosswalk pending/missing/duplicate rows, and schema manifest corruption.
- Crosswalk row counts are exact and zero-pending: `changed-paths-crosswalk.md` `169`, `changed-tests-crosswalk.md` `58`, `test-corpus-160-crosswalk.md` `160`.
- Deterministic generation twice comparison — passed byte-identically from `/workspace/tmp/pi-ai-0991/package/dist/models.generated.js`:
  - `models_generated.go` (`1523` chat): SHA-256 `e9510a5adb1705bd9fb0c84abf368b18b94bd4d0f0a628e8852a707c55538fbc`.
  - `image_models_generated.go` (`57` image): SHA-256 `d4e5bf7fb0499081045569c66d5448ae2ef40d5627309f52717e675326daeb6e`.
  - `classifier_models_generated.go` (`12` classifier): SHA-256 `863f3037f48c0c359f80cd4b2c676c5d3a25d445ee78156ad79cd4e18b81da23`.
- `scripts/check-model-regeneration.sh` — passed for chat/image/classifier exact regeneration.
- `scripts/test-check-model-regeneration.py` — passed locally and with `GO_TMPDIR=/tmp TMPDIR=/tmp`; it faults all three typed generated outputs and verifies v0.99.1 diagnostics and diff evidence.
- Focused auth/runtime gate passed under `nice -n 10`: 8 top-level tests / 10 subtests covering provider stream hook failure, Responses direct-token suppression, HTTP/SSE usage guidance, `thinkingLevel`/`nestedCalls`, and ChatGPT OAuth/callback.
- Full local gates passed under `nice -n 10`: `go test ./...`, deterministic `go test ./... -count=3`, shuffle, `go vet ./...`, `make staticcheck`, `make check-logging`, `make check`, `make test-repro`, race, fuzz, SBOM check/self-test, vulnerability check/self-test, license check, and clean copied-worktree `make check`.
- Accepted runtime `d3ac443f6f553a9f079501c3954eb2c65d051c24` passed hosted CI `36638377814` with both main and fuzz jobs green.
- Hosted CI SBOM artifact `11065019247`: CycloneDX 1.6, 18 components, 19 dependencies, root version/ref `d3ac443f6f55`; SBOM SHA-256 `b37c6dc82e692c7b9de56bf135b922dea75c834d1b81cf891b285bbf3f1fe8b2`; root dependency edge valid.
- Final accepted runtime SHA: `d3ac443f6f553a9f079501c3954eb2c65d051c24`.
- Rollback SHA before v0.99.1 runtime candidate: `459c0e2`.

## Durable SBOM release assets

Accepted runtime `d3ac443f6f553a9f079501c3954eb2c65d051c24` is the current v0.99.1 runtime baseline. The guarded manual publisher workflow is authorized to publish `sbom.cdx.json` and `sbom.cdx.json.sha256` for tag `upstream-v0.99.1` against that exact runtime ref. Historical v0.87.1 durable SBOM links for `c2d0231d8bef63a920e1663143e6c1c39ef0679d`, v0.87.0 links for `c51fb076ad9f0207ba128af750d94fc40de9a121`, and historical v0.85.1/v0.85.0 release assets remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation, fix, adaptation, N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
