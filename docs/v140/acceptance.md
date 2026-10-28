# Native 1.4.0 local verification

Local verification is complete on Go 1.27.3. No push, hosted CI, tag, release or publication was authorised or performed.

Runtime and test candidate: `0852445ca9db68e13013f64fa5dd77ec1e10cb80`. Later evidence-only commits record these results. Rollback baseline: `16cc898de506eb413254e303b0b4f36d77596314` (`origin/main` at the start of this audit). Historical release refs and assets are unchanged.

## Exact upstream scope

- Target: official `v1.4.0`, `4a080252f06caf63a525d7873c5d94d3c23887e0`.
- Prior accepted upstream: `v1.3.0`, `71023aec1546d6d677a65669c5665c0927ce0dd0`.
- [Full changed-path status inventory](changed-paths.tsv): 343 files. The [relevant inventory](changed-paths.json) contains 178 AI/durable files: 68 production paths, 109 test/fixture paths and one additional configuration path under its recorded filter.
- [Per-file disposition and native test crosswalk](crosswalk.json): all 178 relevant paths map to 31 explicit native production/test groups. Group lists identify checked-in tests and production files; they do not assert line-for-line TypeScript translations.
- [Final upstream test corpus](upstream-test-corpus.tsv): 312 files, 109 changed, including fixtures, examples and runner support. `=` marks an unchanged file.
- [Catalogue changes](catalog-delta.json) retain full-record additions/removals/metadata changes for the published-provider subset. Complete native generated catalogues contain 1599 chat, 63 image and 51 classifier models. These are different counting scopes.

## Implementations and adaptations

The crosswalk covers identity-aware request clients, provider wire/schema changes, classifier image inputs and compatibility, transcript tool changes, retry/overflow rules, scoped observational telemetry and generated catalogue changes. Provider tests exercise native HTTP/SSE and serialization paths with deterministic transports. Live paid-provider requests and hosted Codex cache-affinity probes were not run.

Durable changes include explicit document checkpoint policies, model-specific compaction settings, restart counts/parking/progress resets, graph blocked reasons, failed-run inbox policies, interrupted abort ordering, timers/sleep recovery, scoped reports, process-local tracing and paid-model propagation. Task checkpoints, cancellation, storage adoption and publication failure gates remain enforced. Graceful close requeues joined running hosts without fabricating crash outcomes or incrementing restart counts.

Queue/batch input span links use original submit roots despite dedup resubmission. Capture precedes placeholder cleanup; delivery follows successful adoption outside the mutation line. Span handles and ancestry are never serialized. Compaction and deferred cancellation inherit phase telemetry and host/pinned identity; explicit compaction telemetry remains authoritative. Callback panic/reentry tests preserve outcomes and privacy.

Native SQLite uses the existing Go frame/checkpoint model with reopen, sleep and recovery tests. Upstream status-column migrations are not copied into a different schema. IndexedDB and Cloudflare worker deployment have no Go host backend; native storage and wake-hook contracts are tested. Browser/Cloudflare deployment is outside this validation. The crosswalk identifies those files individually.

## Official worker and provenance

Exact `@earendil-works/pi-codemode@1.4.0` is installed under the project cache with installation scripts disabled. `GOAI_WORKER_PACKAGE_ROOT` enabled the official worker, portable batching, resource caps, computing-abort, source and property integration tests. The exact-version admission check now requires 1.4.0. All `TestCodemode120Official*` tests passed under race detection, count 3 (16.161 seconds), and were enabled in final full suites.

Upstream codemode v1.3.0→v1.4.0 changes only its changelog and package version; runtime source is unchanged. The deterministic VM fingerprint therefore remains unchanged. The earlier 1.3.0 worker receipt is historical; final validation uses 1.4.0.

[Provenance](provenance.json) records SHA-256 and cryptographic verification for pi-ai, pi-durable and pi-codemode 1.4.0. Each has two verified registry signatures and two verified attestations. SLSA certificate identity is restricted to `earendil-works/pi/.github/workflows/build-binaries.yml@refs/tags/v1.4.0`; issuer is GitHub Actions; resolved source revision and tarball subject digest match. Modified payloads are rejected. Verification used npm-bundled Sigstore 3.1.0 with project-scoped TUF storage and registry public keys.

## Profiled local gates

All runs used only Go 1.27.3, Linux amd64, with cgo enabled and the exact official worker environment. Module minimum remains Go 1.26.0. CPU cumulative, allocation space and allocation objects were captured and inspected; raw profiles, matching binaries and disposable logs were removed after analysis.

| Gate | Result |
| --- | --- |
| Full ordinary suite | Pass; durable 89.546 s, QuickJS 8.672 s, public-consumer tests 3.703 s |
| Full race suite | Pass; durable 508.229 s, QuickJS 39.826 s, public-consumer tests 19.236 s |
| Full shuffled suite | Pass; durable 91.313 s, QuickJS 8.656 s, public-consumer tests 3.692 s |
| `go vet ./...`, build, Staticcheck 0.8.2, logging checks | Pass |
| Exact chat/image/classifier regeneration | Pass |
| Deliberate metadata corruption and diagnostic/diff fault gates | Pass |
| SBOM generation/schema/checksum and validator/normalizer negative tests | Pass |
| Publisher/native tag policy self-tests | Pass; no publication attempted |
| Govulncheck 1.7.0 and vulnerability-policy self-tests | Pass; no reachable findings and no exceptions |
| Licence scan | Pass; scanner reports its existing inability to inspect dependency assembly for additional dependencies |

`make test-repro` was run as the integrated entry point. Its final ordinary suite, vet/build/static/logging/regeneration and SBOM generation completed before the session interrupted an SBOM validator self-test. Remaining SBOM/publisher/security/licence prerequisites were resumed successfully without repeating completed suites; the final full race suite had already passed separately on the same candidate. Every constituent gate passed. There is no claim that the interrupted invocation itself returned zero.

An earlier race run found a fixture completion race: the final graph frame reached its waiter before the callback finished reentrant reads. The waiter could close the harness too early. Completion is now published after those reads and mutation checks. The regression passed 100 race-enabled repetitions (17.183 seconds), followed by the passing final full race suite. Production reporting and failure gates were unchanged.

## Profiling decisions

Equivalent benchmark fixtures were compared before and after each change; CPU and both allocation measures were inspected. Earlier JSON/QuickJS comparisons used Go 1.27.2 before security remediation; SSE used Go 1.27.3. Toolchain changes are not attributed as optimisation gains.

| Workload | Before | After |
| --- | --- | --- |
| Checkpoint JSON, count 3, 2 s | 60.620–63.369 µs; 10560 B; 405 allocations | 38.219–38.649 µs; 4096 B; 1 allocation |
| Isolated QuickJS engine startup, count 3, 2 s | 215.24–226.04 ms; 36.249 MB; 78745–78746 allocations | 6.367–6.430 ms; 1.851 MB; 21723 allocations |
| SSE 128-byte event stream, count 3, 1 s | 11.324–13.107 µs; 67346 B; 11 allocations | 4.629–4.949 µs; 10000 B; 11 allocations |
| SSE 128-KiB event stream, count 3, 1 s | 117.422–146.580 µs; about 599690 B; 13 allocations | 118.805–125.401 µs; about 657036 B; 16 allocations |

Plain JSON maps avoid reflection iterator slots. Named maps retain the generic path, and nested values retain marshal-authority, cycle, node, depth, string, member and byte-budget checks. Differential generic-path regressions pass. The checkpoint fixture improves about 38% in time, 61% in bytes and 99.75% in allocations.

QuickJS shares immutable compiled code for the fixed embedded module through wazero's process-local compilation cache. Separate runtimes retain guest memory, host state, lifecycle and cancellation ownership. Sibling-close, guest/store isolation, interpreter and cancellation tests pass. Repeated startup improves about 97.1% in time, 94.9% in bytes and 72.4% in allocations; remaining startup work includes WASM decoding/validation.

SSE starts with an 8-KiB scanner buffer instead of 64 KiB, preserving its 4-MiB line limit. Small streams save 57346 bytes (85.2%) and roughly 60% of parse time. Large initial events pay 57346 additional bytes (9.6%) and three growth allocations; timing ranges overlap. This bounded trade-off reduces the measured small-stream cost while retaining large-event support. Buffer-growth and oversize rejection tests pass under race detection. There is no pooled content retention or change to cancellation/channel semantics.

Final durable profiling samples total 63.82 CPU seconds, about 19.96 GB of allocations and 120.86 million allocation objects across the entire test workload. The dominant remaining application paths are commit preparation and bounded encode/decode. Caller/storage detachment, validation and independent adopted-state copies enforce ownership and failure isolation. Removing those boundaries would weaken contracts; the measured plain-map fast path removes avoidable work without doing so. Remaining QuickJS CPU is primarily execution; shared compilation removed repeated machine-code generation. No further material contract-preserving change was identified within this release scope.

## Security and SBOM

Rui approved Go 1.27.3 on 28 October 2026 to fix reachable `GO-2026-6809` / CVE-2026-111943 in Go 1.27.2's certificate verification. Local/CI/publisher toolchain pins now use 1.27.3. Govulncheck reports no reachable vulnerabilities; `allowedFindings` stays empty.

Final local SBOM uses `cyclonedx-gomod@v1.12.0`, CycloneDX 1.6, with 24 dependency components and recorded guest assets. Root version is the local candidate abbreviation `0852445ca9db`, with exact VCS revision `0852445ca9db68e13013f64fa5dd77ec1e10cb80`. Canonical local files are `artifacts/sbom.cdx.json` and `artifacts/sbom.cdx.json.sha256`; SHA-256 is `2fe221797eceb9cc3e81d48e15f98515d69f2a5a62b7ccfe20063b51c2a1d1f9`. These are local candidate assets, not published native-version release assets.

Hosted CI, synchronising `origin/main`, annotated tagging and publication require separate explicit authority. A future publisher must generate/verify native-version SBOM assets for the accepted release target and preserve earlier refs/assets.
