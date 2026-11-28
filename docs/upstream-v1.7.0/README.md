# pi v1.7.0 native port

**Active target:** [official pi v1.7.0](https://github.com/earendil-works/pi/releases/tag/v1.7.0), `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The official Git tag resolves to this commit. The AI, Codemode, Durable and Schema source manifests all declare `1.7.0` and Node `>=22.19.0`.

**Comparison base:** official v1.6.1, `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`, is an ancestor of v1.7.0. The verified native v1.6.1 checkpoint is `3c59c42f10dbc0ecfb159c8d3f5d2a5302c5100d`. Its [verification log](../upstream-v1.6.1/README.md), assertion ledger and crosswalk remain historical evidence.

## Verified inputs

A clean detached source checkout was staged at the exact pin. Its four package manifests and `package-lock.json` match the Git objects byte for byte. [Input fingerprints](inputs.json) record the manifest and lock SHA-256 values and the four published npm tarballs. Each tarball matched the registry's SHA-512 integrity value; its embedded package name and version matched the source manifest. No packages were installed or tested during the retarget. The results below record the later release, dependency and runtime checks separately.

## Change inventory

- [Changed paths](upstream-changes.tsv): 1,400 entries across the release (473 additions, 80 deletions, 845 modifications and two renames).
- [Changed-path dispositions](upstream-disposition.tsv): structural roles for those entries. There are 19 runtime-source paths and 12 catalog inputs in the four target packages. The role `outside-four-package-comparison` does not exclude a path from later dependency or release review.
- [Test inventory](test-inventory.tsv): 324 pinned test files and 23 changed test-file paths under `test/` and `test-workers/`. The checked-out bytes of all 23 changed tests match their pinned Git objects. This seeds the v1.7.0 crosswalk without carrying forward v1.6.1 assertion coverage.

```sh
source scripts/project-env.sh
bun run docs/upstream-v1.7.0/inventory.ts "$PI_MIRROR" --check
```

The mirror must contain both official tags at the recorded commits. Use `--write` to reproduce the inventory. The checker verifies the Git inputs and tracked inventory bytes. It does not download npm archives or run tests.

## Scoped comparisons

Before retargeting, four focused native tests passed on `3c59c42f` with Go 1.27.3 and `CGO_ENABLED=0`. Each comparison below has its own unchanged source run, native evidence and limits.

| Area | Evidence |
| --- | --- |
| AI inputs | [Locked AI test stage](ai-originals.md): 59 originals in five files. [OpenRouter](openrouter-images.md) adds eight image and 46 models-runtime originals. [Azure](azure-originals.md) adds five unchanged files and 37 cases, including the native v1 Responses wire-path correction. The [unchanged Google raw/shared batch](ai-unchanged-google1.md) adds five exact originals and public-path controls without a production fix. |
| Images and pricing | [Pi Image](pi-image.md) and [OpenRouter](openrouter-images.md): canonical requests, output, errors, authoritative cost and separate image-output rates. |
| Completions and retry errors | [Opaque replay](openai-completions-opaque.md), [stored signatures](openai-completions-stored-reasoning.md), [Cloudflare Gateway](openai-cloudflare-compat.md) and [Ant Ling thinking](openai-completions-ant-ling.md), plus [tool-history empty tools](openai-completions-empty-tools-history.md). [Changed-test group 3](ai-changed-group3.md) audits orphaned tool calls, total usage and Unicode with exact source fixtures. [Anthropic retry/error responses](anthropic-provider-error-response.md) maps repeated 429 response hooks and header observation through the public Messages transport. |
| Bedrock | [Reasoning replay and effort](bedrock-reasoning.md), with detached copies of both optional compatibility fields. |
| Catalog | [Readiness audit](catalog-readiness.md) and [regeneration](catalog-regeneration.md): all 1,858 published records matched using the production Go types, without an overlay. Retired rows were not restored; wire tests use matching historical fixtures. |
| Codemode | [Published portable batching](codemode-portable-batching.md): 5,000 ordered console items, six exchanges, same published distribution and QuickJS WASM as v1.6.1. [Worker admission and exit](codemode-worker-lifetime.md) covers refusal and lifetime-hook ordering; [construction failure](codemode-worker-construction.md) separately proves native permit return after WASM setup failure without claiming Node/Bun process semantics. [Source parsing](codemode-source.md) translates all five unchanged original cases and preserves the typed error, line-number and option contracts. |
| Dependencies and release | [Exact closure and release receipt](dependencies/README.md): 305 npm slots, six links, 547 edges, 122 installer placements, ten publisher packages and eight release artifacts independently verified. Both Linux arm64 binaries passed an offline `--version` and Codemode smoke. |
| Durable | [Unchanged SQLite originals and storage boundaries](durable-sqlite-originals-boundaries.md), [grouped SQLite visibility and reopened heads](durable-storage.md), [pre-callback usage fencing](durable-tool-usage-validation.md), [nearest-owner cascade reason](durable-scheduler-reason.md), [blocked recovery and restart controls](durable-blocked-recovery.md), [unchanged turn-limit originals](durable-turn-limit.md), [unchanged session originals, batch 1](durable-unchanged-batch1.md), [environment originals](durable-environment.md), and [harness close lifecycle](durable-harness-close.md). The blocked packet also proves failed-open active wake retention and 30 direct assertion locators. The turn-limit packet maps all 11 unchanged cases to nine neutral SQLite groups without a production fix. The six environment originals ran unchanged with 117 passing cases; public file, line-scan, spill and SQLite writer behavior needed no production fix. The session batch has a disclosed unrelated callback-matrix follow-up; the lifecycle lane excludes a timing-sensitive post-reopen assertion from its exact selector. Chord-only assertions remain unmatched. |
| Schema | [Shared prepared checks](schema-shared.md): the unchanged two-test core with explicit JavaScript cache-identity limits; [prepared absolute IRI](schema-prepared-iri.md): 26 vectors; [prepared IDN-email](schema-prepared-idn-email.md): 24 vectors; [prepared IDN-hostname](schema-prepared-idn-hostname.md): 101 vectors. Legacy and tool annotation behavior is unchanged. |
| Radius | [Typed runtime receipt](radius-typed-runtime-receipt.md): chat, image and classifier models now refresh, replace, retain validators and restore from cache with credential-owned detached snapshots. |

## Unchanged-source batches

Round 3 adds bounded unchanged-source comparisons beyond the 23 changed tests: two Anthropic batches, a five-file Google raw/shared batch, Durable session/view, turn-limit, blocked and environment packets, and Codemode source parsing. The unchanged originals, native controls and source-level limitations are recorded in `unchanged-ai-batch1.md`, `ai-unchanged-batch2.md`, `ai-unchanged-google1.md`, `durable-turn-limit.md`, `durable-unchanged-batch1.md`, `durable-unchanged-views.md`, `durable-blocked-recovery.md`, `durable-environment.md` and `codemode-source.md`. Separate receipts cover Anthropic [preferred strict-tool fallback](anthropic-strict-tool-fallback.md) and [strict schemas](anthropic-strict-tool-schema.md).

## Integrated checkpoint

The exact candidate `5f724c1e3cc6423ec697088dbb339e176b342243` passed 97 focused Go tests twice across six packages without skips, with vet and Staticcheck v0.8.2. A fresh full-history clone reproduced the round-1 checks. The later integrated rounds, through `e115bf61`, passed the combined touched-slice selector twice with vet and Staticcheck; separate focused selections covered the 101 prepared IDN-hostname and 24 IDN-email vectors. The [v1.7.0 crosswalk](test-crosswalk.tsv) records 1437 scoped sites across 103 of 324 files: 1059 mapped, 352 partial and 26 unmatched. It counts scoped assertions, not unrun source tests.

The Go image fixtures, OpenAI reasoning replay and SSE helpers, Durable turn limits, usage and SQLite grouping, and the current Bedrock, Schema and Codemode cases use behaviour names. Source pins and published comparison inputs remain in the ledgers and fixtures. The turn-limit migration preserved all nine native scenarios and revalidated 1,354 historical locators.

## Open gaps

- Wider provider, Durable, Codemode and Schema coverage remains open. The remaining crosswalk gaps separate nonportable Chord, JavaScript parser/writer, and alternate-engine observations from native Go parity. Live replay still needs the authorised credentials. Dependency and installer provenance is closed by the [exact release receipt](dependencies/README.md); full acceptance and acceptance profiling were not run.

```sh
bun run docs/upstream-v1.7.0/assertions/validate.ts "$PI_ORACLE" "$PI_MIRROR"
bun run docs/upstream-v1.7.0/test-crosswalk.ts --check
```
