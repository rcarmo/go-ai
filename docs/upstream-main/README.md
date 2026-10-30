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

`CostBreakdown.totalFromProvider` is optional, estimated calculations return false, and provider overrides set true. The existing native `CalculateCost` return-value API is retained; providers assign its result. OpenAI now uses that shared calculator, including tiered rates, instead of a duplicate flat-rate calculation. Context/event snapshots detach the optional provenance pointer. The follow-up below adds Anthropic's production integration and ledger provenance. The presence follow-up below closes optional zero-counter JSON handling. Other provider deltas still need review.

Tests translate every case in upstream `packages/ai/test/openrouter-cost.test.ts`, add invalid/incomplete/overflow and missing-versus-null cases, exercise native raw SSE and choice-level usage, reset estimates on a later usage update, and verify snapshot isolation and optional JSON fields semantically. A full-package run caught a missing provenance field in the calculator's returned value; this was corrected before the passing runs below. Benchmark fixtures are prepared, but no profiling/benchmark acceptance run has been started.

Go 1.27.3/GOMAXPROCS=4 development checks:

* `go test ./internal/openroutercost ./inference/provider/openai ./tests -count=1 -timeout=180s`: pass (0.003 / 0.056 / 4.200 seconds).
* `go test -race ./internal/openroutercost ./inference/provider/openai -count=1 -timeout=180s`: pass (1.018 / 1.094 seconds).
* `go vet . ./internal/openroutercost ./inference/provider/openai`: pass.
* `git diff --check`: pass.

## Accounting follow-up: Anthropic and ledger provenance

Anthropic now computes usage costs at message_start, including initial output tokens, so an early/truncated stream retains its token/cost state. Deltas replace only present, non-null numeric fields (zero is a real update), including input/cache counts, one-hour cache writes and reasoning. Omitted/null usage retains counters but recalculates the catalogue estimate, matching the pinned source. OpenRouter billed accounting applies only on delta usage; completion no longer overwrites its result with a fresh estimate. Native raw SSE tests cover partial/zero/null updates, billed BYOK and free totals, invalid accounting types, endpoint/provider detection and early failure. Existing fallback/pricing tests also pass.

The durable reducer now retains the first record's optional cost provenance while summing only numeric counters, exactly as upstream recordUsage/addUsage do. A later billed or estimated record does not relabel the bucket. Models and tools (including a `__proto__` tool key) retain billed totals independently of the sum of catalogue components, with input mutation detached from stored state.

Development verification on Go 1.27.3/GOMAXPROCS=4:

* Full Anthropic and cost-helper packages: pass (0.120 / 0.003 seconds).
* `go test ./durable -run Usage -count=1 -timeout=180s`: pass (0.985 seconds).
* Full Anthropic/helper race tests: pass (1.272 / 1.014 seconds).
* `go test -race ./durable -run Usage -count=1 -timeout=180s`: pass (6.446 seconds).
* `go vet ./inference/provider/anthropic ./durable` and `git diff --check`: pass.

No profiling was run during these development regressions. The complete upstream accounting/test crosswalk and acceptance are incomplete.

## Optional counter presence and OpenAI usage

`Usage` retains integer fields and adds JSON-hidden presence flags for explicit zero reasoning and one-hour cache-write counts. Public JSON decoding still tolerates unknown fields; strict durable DTO decoding rejects unknown usage and nested cost fields. The bounded encoder handles the exact native Usage type directly without invoking its marshaler. Caller wrappers retain the marshal-authority rejection; object-only, byte, node, depth and member limits still apply. Models/tools retain optional zero counters after a later omitted update.

Anthropic initial usage always reports one-hour cache writes, including zero, as upstream does. Delta reasoning zero survives later null/omitted updates. OpenAI Completions now uses nullish cache precedence (details, hit count, top-level Kimi count), keeps reads separate from writes, clamps uncached input at zero, reports reasoning including zero, and derives totals from counters. The older response-model/cache regression expected inclusive read/write accounting; its expectation now follows pinned main and retains the metadata assertions.

Development verification on Go 1.27.3/GOMAXPROCS=4:

* OpenAI/Anthropic/helper/SDK tests: pass (0.027 / 0.127 / 0.006 / 3.728 seconds).
* Durable usage/JSON/marshal/limit tests: pass (3.238 seconds).
* Corresponding provider/helper/SDK race tests: pass (1.138 / 1.316 / 1.017 / 18.708 seconds); durable focused race tests: pass (23.313 seconds).
* `FuzzUsagePresenceMainNativeEncoding`, 10 seconds/two workers: 80,948 executions; pass. Parsed JSON objects and exact integer values are compared, including values above 2^53.
* Full durable ordinary tests: pass (87.248 seconds). Other root-module packages passed a subsequent ordinary test invocation excluding packages already tested above.
* `go vet . ./inference/provider/openai ./inference/provider/anthropic ./durable` and diff checks: pass.

CPU/allocation impact of the usage codec and strict decoded-target walk is unmeasured; include both in acceptance profiling. Nested-module acceptance, complete upstream test coverage and full semantic review are still open. Next: the streaming-JSON parser and provider integration.

## Streaming parser core: partial

`internal/jsonparse/streaming.go` adds incremental append/snapshot state, ordered set/append edits, immutable snapshot histories, buffered strings, copy credit, 32-level change paths and the 64-character pending-scalar bound. Existing production providers still use the old parser. ToolCallArgumentsStream, provider replacement/finalisation and the complete upstream streaming test file have not been ported.

The committed 33 KB fixture contains 66 curated append/snapshot cases generated with Bun from pinned streaming-json.ts. Its finish import was replaced by a throwing stub; finish was never exercised. Comparisons use parsed objects, ordered edits and old-snapshot stability. Fixture inputs cover incomplete scalars/containers/strings, control characters, malformed escapes, missing separators, prototype-like keys and Unicode scalar values. They exclude lone UTF-16 surrogates and numeric overflow; Go JSON decoding would conceal those differences in a reference fixture.

Checkpoint `040018c` had two representation gaps, resolved for the core by the follow-up below:

* After `append('"\\ud83d')`, upstream snapshot is a one-unit string containing 55357 (0xD83D). Go strings/standard JSON cannot represent and round-trip that unit faithfully. That checkpoint held high surrogates until paired/completed and replaced isolated units with U+FFFD.
* After `append("1e400")`, upstream snapshot is numeric Infinity. That checkpoint omitted nonfinite scalars. Ordinary persisted JSON still rejects them.

The native preview adapters and argument-stream wrapper below close those core gaps. Bounded provider/DTO integration and the remaining upstream tests are still open.

Development checks on Go 1.27.3/GOMAXPROCS=4: core package tests pass (0.012 seconds), race tests pass (1.085 seconds), snapshot-history fuzzing passes (38,270 executions, two workers, 6.029 seconds), vet and diff checks pass. These checks cover the partial core only. No profiling or performance comparison was run; acceptance CPU/alloc_space/alloc_objects analysis is still required.

## Lossless previews and argument-stream wrapper

The core now represents isolated UTF-16 units with an immutable native `UTF16String`; ordinary strings remain Go strings. Open strings retain a pending high unit separately from shared prefix storage, then join a following low unit. `AppendUTF16` handles raw code-unit splits. Exceptional object keys use WTF-8 internally. `SnapshotJSON` and ordered-change serialisation escape those units losslessly; they apply JS JSON.stringify semantics to nonfinite numbers and signed zero. These are preview adapters, separate from persisted JSON validation.

`ToolCallArgumentsStream` owns raw text privately, reuses unchanged argument objects, distinguishes empty continuation deltas from replacement resets, compares UTF-16 prefixes, and performs exact final structural parsing with string repairs. Truncated prefixes and nonobject results fail finalisation. Diagnostics retain original text; syntax wording uses Go errors. The existing production final-argument helper and provider call sites are unchanged.

Reference coverage now includes the original 66 cases, 60 tagged UTF-16/IEEE-number cases and 14 argument-stream cases. The new tags preserve code units, infinities and signed zero before JSON fixture serialisation; they do not normalise exceptional values away. The wrapper oracle uses the pinned real finish/repair implementation. Its unused partial-json import is a throwing stub; partial-json parsing was not exercised.

Development verification, Go 1.27.3/GOMAXPROCS=4:

* Core/wrapper normal and race tests: pass (0.024 / 1.154 seconds).
* Focused durable boundary/usage/authority normal and race tests: pass (0.314 / 3.269 seconds). Persisted JSON still rejects preview overflow, raw non-UTF-8 keys and unadapted UTF-16 values; the authority checks remain intact.
* UTF-16 history fuzzing: 85,405 executions, pass (6.012 seconds). General snapshot-history fuzzing: 2,970 executions, pass (6.083 seconds). Two workers per run.
* Vet and diff checks: pass. No profiling or performance comparison.

The typed SDK/DTO slice below follows this checkpoint. Production persistence, provider delta transport and finalisation still need integration.

## Typed SDK and DTO argument boundaries

`SetStreamingArguments` opts native tool calls/blocks into lossless argument-key/value and diagnostic serialisation. Private mode flags survive context/event copies and never become JSON fields. `UnmarshalStreamingMessage` provides an explicit SDK decoder with JS argument numbers. Ordinary Go decoding keeps its previous behaviour. The DTO decoder retains `json.Number` spellings, including integers above 2^53; the SDK decoder applies JS rounding. Native preview overflow serialises as null in the SDK and still fails the durable finite-number gate.

The durable scanner permits isolated units only in argument/diagnostic spans selected from a typed tool-call schema. Generic JSON, control fields, IDs, ordinary text, unknown fields and caller-defined marshalers keep their existing checks. A resource preflight runs before allocating schema projections; the final schema-scoped scan authorises the strings. Duplicate keys compare lossless code units. UTF-16 values outside typed argument contexts still fail validation. Escaped output remains subject to the encoded-byte cap.

Development checks on Go 1.27.3/GOMAXPROCS=4:

* Full root-module normal suite: pass; durable 98.160 seconds.
* Final focused streaming/JSON/usage/authority race: pass; durable 14.888, SDK tests 1.118, parser 1.161 seconds. Full normal testing preceded the final nil-map/compatibility fixes; focused normal/race and vet passed afterwards.
* Final argument-replay fuzz probe: 26,243 executions, two workers, pass (6.015 seconds). An earlier probe exposed the existing omission of empty arguments on malformed non-tool blocks. A dedicated compatibility test preserves that behaviour; the fuzz property now checks typed tool-call arguments and diagnostics.
* Additional regressions cover exact stored integers, native string node/string/byte budgets, raw UTF-8 rejection, range isolation, distinct/duplicate surrogate keys, schema cycles, unknown fields, caller marshalers and clone isolation. No profiling or acceptance validation.

At checkpoint `8cc7fa1`, generic receipt projection still rejected isolated units. The receipt adaptation below follows that typed-message slice.

## Receipt storage and replay

Contribution and generation-terminal receipt construction now detach streamed arguments through their typed JSON domain and preserve diagnostics and private encoding mode. Exceptional values survive generic DTO projection, record storage and typed receipt replay. Ordinary JSON keeps its Unicode, finite-number, caller-marshaler and resource checks.

The native receipt wire format uses `utf16Arguments` slots: each identifies a tool-call content index and contains bounded base64 chunks of validated final arguments/diagnostics. The stored content slot holds an empty-object placeholder. Typed receipt decoding validates the role, index, uniqueness, placeholder and carrier fields before restoring the values. IDs, names, namespaces and signatures stay outside the carrier; nonempty identity fields inside it fail validation. These chunks contain final structured values, not incremental parser state or its raw source. Raw storage readers see this native adaptation; receipt/context projection restores the logical model values. The remaining API/codemode crosswalk must cover that boundary.

Logical argument budgets apply before encoding. Physical budgets apply to the native wire format, including its overhead; no limit was increased. Payload/chunk budgets are aggregate across slots, allocations are bounded before base64 encoding, and the final projected document is scanned again. Canonical ordering inside carriers keeps unchanged-receipt deduplication stable. Caller-supplied carrier metadata does not override logical Go receipt arguments when writing.

Development verification, Go 1.27.3/GOMAXPROCS=4:

* Contribution and production `drainInvocation` terminal paths pass; replay after memory, journal and SQLite reopen preserves isolated key/value units, diagnostics and exact integers above 2^53. Empty arguments and diagnostic-only carriers pass.
* Full root-module normal suite passes (durable 97.610 seconds); focused durable race passes (25.678 seconds). Final JSON/receipt normal checks pass (4.096 seconds), receipt race (1.124 seconds), vet and diff checks pass.
* Final receipt projection fuzz probe: 41,391 executions, two workers, pass (6.014 seconds); earlier probe 74,692 executions.
* Fault cases reject duplicate/out-of-range slots, role/placeholder mismatches, malformed base64, unknown fields, identity injection, ordinary-value carriers, nonfinite floats and caller marshalers. Tight byte/chunk budgets and detached caller maps pass their regressions.
* No profiling, performance claim, acceptance or provider integration.

Tool checkpoint and dispatch support follows below. Reader/codemode projection, diagnostic tool-result text, SSE/delta transport, providers and the complete streaming/finalisation crosswalk are unfinished.

## Tool checkpoint and dispatch arguments

Model tool checkpoints preserve the argument encoding mode in a native `streamingArguments` flag, including ordinary arguments that need no carrier. Exceptional checkpoint arguments and diagnostics use `utf16Arguments` chunks and the same bounded carrier codec as receipts. Typed replay requires the ownership flag and empty argument/diagnostic placeholders. Generic JSON keeps its existing validation.

Preparation, validators, before-tool rewriting, execution and after-tool callbacks receive detached copies of the typed argument values. Replayed started intents retain the prepared/rewritten arguments without rerunning preparation or before-tool hooks. Native string-schema checks count valid surrogate pairs as one code point and isolated units as one character. A rune reader passes isolated units directly to the existing Go regexp engine; literal U+FFFD patterns cannot match them through replacement. The broader source/native schema crosswalk is unfinished.

Development checks on Go 1.27.3/GOMAXPROCS=4:

* Production execution/reopen across memory, journal and SQLite passes. Preparation and before-tool rewriting run once; validators run twice before execution; replayed execution sees the stored rewrite. After-tool callbacks retain argument ownership, host mutation stays detached, and the next model request keeps the original model arguments.
* Checkpoint round trips cover isolated keys/values, diagnostic-only carriers, exact integers and ordinary-mode ownership. Fault checks reject missing ownership, inconsistent placeholders, malformed base64, unknown fields, caller marshalers and nonfinite values. Byte limits remain enforced.
* Full root-module normal suite passes (durable 100.213 seconds). Focused checkpoint/receipt/restart race passes (3.782 seconds); final tool-argument normal/race passes (0.243/2.448 seconds). Vet and diff checks pass. No acceptance profiling.

Next: inspect readers and codemode adapters, which still copy tool arguments through generic JSON. Tool argument diagnostics also reach ordinary tool-result text on the invalid-arguments path; isolated diagnostic units need a scoped representation there without relaxing ordinary text validation. Finish those boundaries before provider transport/wiring and full acceptance.

## Committed event reader arguments

Tool-start event projection now uses the checkpoint argument mode when detaching arguments. Previously its generic copy rejected exceptional units after successful checkpoint adoption and stopped the reader. A production watcher regression across memory, journal and SQLite verifies isolated keys/values in tool-start events and active conversation snapshots; listener/snapshot mutations leave stored arguments and later snapshots unchanged.

Go 1.27.3/GOMAXPROCS=4 development checks: new reader normal test 0.135 seconds, focused reader/event race 2.748 seconds, shuffled event/snapshot/reader/context suite 3.723 seconds; vet and diff checks pass. The preceding tool checkpoint commit passed the full root-module suite. No acceptance profiling.

Remaining read/adapter boundaries include compaction transcript argument formatting (`compaction_text.go`), nested-tool events and codemode. Codemode serial/parallel journals, remote dispatch and nested-tool preparation/hooks still use generic copies; QuickJS host argument decoding uses the ordinary Go JSON decoder and loses isolated units before dispatch. These paths need explicit typed ownership through parsing, identity checks, journals, execution and replay. Generic values outside tool arguments retain strict validation.
