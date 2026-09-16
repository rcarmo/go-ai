# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v0.85.1`
- Upstream tag/SHA: `d981de1229ef899957bbe968bc8dcda02a21f477`
- Previous accepted upstream baseline: `v0.85.0` / `107d79f11072bbc8a3a757ed7fd69596bee7d68c`
- Previous accepted Go runtime baseline before this audit: `90d17907b2ce26ffe5f46cd061edc8209e357bed`
- Current repository baseline before this audit: `dfdb68e4e734d730f89e34a4b77f098d68cd8b74`
- Official npm artifact: `/workspace/tmp/pi-ai-0851/earendil-works-pi-ai-0.85.1.tgz`
- Official npm artifact SHA-256: `af7d11986179445ce6fe88b37d57de22f823c0ffd3a65cae31c555b7f5e99253`
- Official npm raw SHA-512: `f958152090e40ced9e7d824a104aaf3d31f8ce69c8697740a6919b3bebca140f6acb93dd8458807a7b8502453ea220927ce0b874c1c3cab8dd41e2f86680b909`
- Detailed path matrix: `docs/v0851-release-ledger.md`
- Whole-corpus upstream test crosswalk: `docs/v0851-142-test-manifest.md`

## Scope evidence

- Changed paths: `9` canonical rows, committed at `docs/v0851/changed-paths.txt`, SHA-256 `ee26f669d92dc77b265731165a2ff69ccb67defba92517cbbd5f97a186e187d2`.
- Changed tests: `3` rows, committed at `docs/v0851/changed-tests.txt`, SHA-256 `f7e274bf229c90fc22ba22384c5b89f71a5c6801f77067d099525a9cdc537610`.
- Whole upstream test corpus: `142` rows, committed at `docs/v0851/test-corpus-142.txt`, SHA-256 `56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065`.
- Source delta: 9 files, `+128/-23`.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.85.1:

- Responses explicit prompt-cache retention: explicit-cache models now emit `prompt_cache_options:{mode:"explicit"}` for `none` and `prompt_cache_options:{ttl:"30m"}` for supported `long`, while avoiding legacy `prompt_cache_retention`; older compatible models retain `prompt_cache_retention:"24h"`.
- Exact generated text catalog refresh to `1354` models across `39` providers and `9` APIs.
- Exact generated image catalog refresh to `52` image models, adding MAI Image 2.6 and MAI Image 2.6 Flash.
- GPT-6 Astra generated metadata/compat across direct OpenAI, Azure OpenAI, OpenAI Codex, and generated provider wrappers, including context/max tokens, text+image input, costs, long-context tier where present, tool search/additional tools, explicit prompt-cache compat, and xhigh/max thinking support.
- Generator support for `ModelCost.Tiers` emission.
- v0.85.1 committed inventory, whole-corpus manifest, full-record catalog delta validator, and model-regeneration negative self-test.

Historical v0.85.0 runtime/SBOM/README evidence remains in `docs/v0850-release-ledger.md` and the git history; v0.85.1 README/SBOM release publication is authorized post-runtime.

## Validation evidence

Current local evidence captured so far during the v0.85.1 audit:

- Focused Responses cache-retention tests — passed.
- Focused GPT-6 Astra/catalog/thinking tests — passed.
- v0.85.1 inventory validator and negative self-test — passed.
- v0.85.1 full-record catalog delta validator and negative self-test — passed (`text +20/-2/18`, `images +2/-0/0`).
- Model regeneration comparator and generated-source negative self-test — passed.
- Full local gates passed: `make check`, shuffle, race, vet, staticcheck, logging, repro, SBOM, vuln, and license. Clean-checkout validation passed with `go test ./...`, v0.85.1 inventory/catalog/manifest validators and self-tests, model-regeneration negative self-test, exact regeneration comparators, and `git diff --check`. Accepted runtime `9c32e1d77bb01bac4574c6ecf260ce07bac9a351` passed hosted CI `35154293042`; SBOM artifact `10470163191` validates with inner SBOM SHA-256 `7f551d8c93a67cf40e32bdd793d5b56c9e43c2b0fd6c55cad47e1bc2e0d063f3`, checksum-file SHA `723487ea76b0b8e244e2e2b510cd1ce43e36bbd9c90bfc6aeb661ca5d5004c93`, archive SHA `47ee0b0a9d947f525cd6f58b9aa05ac8d1fecbc54406a7d1b82a40ad4622f7e4`, and root version `9c32e1d77bb0`.

## Durable SBOM release assets

Accepted runtime `9c32e1d77bb01bac4574c6ecf260ce07bac9a351` has README-visible durable, version-pinned SBOM links for tag `upstream-v0.85.1`. The guarded manual publisher workflow publishes `sbom.cdx.json` and `sbom.cdx.json.sha256` for that exact runtime ref; v0.85.0 release assets remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation/fix/adaptation/N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
