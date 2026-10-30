# Upstream main convergence

Target: `4b535d57ea8863dbba44b89872146523da271786`, observed 30 October 2026. Baseline: upstream v1.4.0 `4a080252f06caf63a525d7873c5d94d3c23887e0`, accepted by native v1.4.0 (`ee3076b`). The published release and its assets remain unchanged. This is unreleased-source convergence, not a new release or a claim of full parity. npm still publishes 1.4.0; no npm artifact is claimed to represent this main snapshot.

Rui authorised convergence for pi-ai, pi-durable and pi-codemode with full semantic alignment. Work proceeds locally in bounded slices; pushes, hosted dispatches and publication are not authorised by this scope. Intentional semantic divergences require explicit acceptance.

## Inventory and dispositions

[changes.json](changes.json) inventories all 354 changed paths inside the three packages: 178 AI, 174 durable and two codemode paths. [upstream-tests.tsv](upstream-tests.tsv) records the complete target test/fixture corpus (352 paths), including unchanged cases, changed markers and native evidence. A pending row is a gap, not an N/A decision or an acceptance result. The earlier accepted baseline crosswalk remains in [../v140/crosswalk.json](../v140/crosswalk.json).

[dependencies.json](dependencies.json) records exact baseline/target package dependencies and 410 changed shared/root/Chord/telemetry support paths for dependency-impact review. This conservative support inventory is not a claim that every Chord feature needs a Go implementation: each applicability decision still needs a concrete contract rationale. Shared dependencies cannot be silently excluded from parity.

Codemode's own runtime and test sources have no diff from 1.4.0. Its package delta updates the upstream development runner Vitest 4.1.11 to 4.1.12; the other change is an empty Unreleased changelog section. This does not establish integration parity with the changed AI/durable contracts. The previously verified actual official worker 1.4.0 remains the baseline, not an invented main package artifact.

## Ordered semantic work

1. Finish per-path contract review and dependency impact, connecting target source and test cases to native production paths. Record a separate disposition for every behaviour within partly implemented files. Recheck the complete upstream test corpus, not only newly added tests.
2. AI usage accounting and metadata: billed versus estimated costs, OpenRouter BYOK/fallback rules, all affected transports and usage aggregation. Refresh catalogues from exact pinned inputs with full-record comparisons; review model/provider additions and response metadata.
3. AI incremental streaming: stateful JSON argument parsing, delta-only grammar input and explicit close, streamed versus completed validation/repair, malformed streams and index validation. Cover every affected provider, not just shared helpers. Review auth confirmation flags, trusted URLs and file permissions through actual native callback/storage paths.
4. Durable storage/publication foundation: counted SQLite writes, exactly-once durable adoption and post-adoption failures, allocator/reopen rules, deferred persistence, flush dependencies/deadlines/close, and commit observer publication matching what Storage actually stores. Review Chord store integration against the native storage architecture rather than assuming SQLite alone is sufficient.
5. Durable watches/observers: tagged calls/endings, address-following retirement/recreation, owner and Session termination, pending operation coalescing and overflow replacement. Distinguish synchronous observer close failures from normal watch endings. Retire removed state/event APIs in favour of the target contracts; verify retention and cancellation.
6. Durable views and lifecycle: bounded transcript windows, resume/paging/forks, live failure state and migrations, progress visible before persistence, restart-limit terminal outcomes, interrupted request/poll retry charging, afterTool on every result, unoffered calls, ordered controls, continue submissions, nothing-to-answer and turn limits. Preserve migration/version fences and exact errors/outcomes.
7. Finish environment/tools, tagged reports and telemetry changes, dependency/security updates, and codemode integration against the changed host contracts. No unchanged package source is sufficient by itself to waive cross-package tests.
8. Acceptance only: complete crosswalk/contract review, full normal/shuffle/race and applicable regeneration/fault/security gates. Capture and analyse Go CPU, alloc_space and alloc_objects; act on material outliers and remeasure matched workloads. Dispose captures/binaries/logs after analysis. Ordinary development regressions below run without profiling. No full-parity or acceptance claim until every applicable gap is resolved or explicitly accepted.

## First slice: development verification, not acceptance

Implemented the pinned OpenRouter accounting helper and the OpenAI Completions production SSE path. Provider or endpoint detection matches upstream; valid non-BYOK reported cost overrides the estimate, including zero. BYOK adds upstream inference cost, using components only for an explicitly null upstream total. Missing/invalid accounting leaves the estimate. Server-tool cost is not double-counted, and component costs remain catalogue estimates. Raw JSON accounting preserves missing/null and wrong-type behaviour without making the SSE decoder discard otherwise valid usage.

`CostBreakdown.totalFromProvider` is optional, estimated calculations return false, and provider overrides set true. The existing native `CalculateCost` return-value API is retained; providers assign its result. OpenAI now uses that shared calculator, including tiered rates, instead of a duplicate flat-rate calculation. Context/event snapshots detach the optional provenance pointer. Anthropic's corresponding billed-cost integration and its broader usage-delta changes are still pending; the shared helper is not evidence that those production paths are done.

Tests translate every case in upstream `packages/ai/test/openrouter-cost.test.ts`, add invalid/incomplete/overflow and missing-versus-null cases, exercise native raw SSE and choice-level usage, reset estimates on a later usage update, and verify snapshot isolation and optional JSON fields semantically. A full-package run caught a missing provenance field in the calculator's returned value; this was corrected before the passing runs below. Benchmark fixtures are prepared, but no profiling/benchmark acceptance run has been started.

Go 1.27.3/GOMAXPROCS=4 development checks:

* `go test ./internal/openroutercost ./inference/provider/openai ./tests -count=1 -timeout=180s`: pass (0.003 / 0.056 / 4.200 seconds).
* `go test -race ./internal/openroutercost ./inference/provider/openai -count=1 -timeout=180s`: pass (1.018 / 1.094 seconds).
* `go vet . ./internal/openroutercost ./inference/provider/openai`: pass.
* `git diff --check`: pass.

Next: complete the accounting slice through Anthropic and aggregation regressions, then the incremental argument-parser/provider crosswalk. Neither full main parity nor acceptance profiling is complete.
