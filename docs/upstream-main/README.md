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

Remaining read/adapter boundaries include compaction transcript argument formatting (`compaction_text.go`) and codemode. Codemode serial/parallel journals and remote dispatch still use generic copies; QuickJS host argument decoding uses the ordinary Go JSON decoder and loses isolated units before dispatch. These paths need explicit typed ownership through parsing, identity checks, journals, execution and replay. Typed nested execution and readers follow below. Generic values outside tool arguments retain strict validation.

## Typed nested tool arguments

`TaskRuntime` and `ToolAPI` now expose `StartToolWithArguments`/`CallToolWithArguments`, accepting the SDK final `StreamingArgumentsJSON` domain. Ordinary `StartTool`/`CallTool` keep strict JSON validation. Durable key reuse compares the argument mode as well as the tool name and values.

Nested input and caller-owned call documents use bounded carriers for exceptional arguments. Prepared/hook-rewritten typed arguments persist in a schema-owned `argumentState` checkpoint. Replay requires matching ownership and rejects mixed legacy/typed states, missing intents and null argument snapshots. Argument-only owners reject diagnostic-bearing carriers. No parser state is stored.

Preparation and before/after hooks receive detached typed values. Execution mutations no longer change the intent passed to after-tool hooks. Nested lifecycle events and active snapshots use the same prepared argument state and return independent maps.

Development verification on Go 1.27.3/GOMAXPROCS=4:

* Memory, journal and explicit SQLite execution/reopen tests preserve isolated keys/values, prepared rewrites and original durable identity. Preparation/before hooks run once across replay; the executor runs twice after interruption and the successful after hook runs once.
* Main-tool dispatch and committed-event tests now also have explicit SQLite runs. **Coverage correction:** their initial descriptions above included SQLite, but the shared `backends` helper covers only memory/journal. Receipt storage had a separate SQLite probe; the dispatch/event-reader SQLite coverage was added in this slice.
* Full root-module normal suite passes (durable 103.427 seconds). Final shuffled nested checks pass (1.755 seconds); focused main/nested/reader race passes (5.619 seconds); vet and diff checks pass. The final null-intent guard and ordinary-API rejection regression were followed by these focused checks.
* No acceptance profiling or performance comparison.

Codemode tool arguments now use the bridge below. Compaction argument formatting and scoped diagnostic tool-result text still need work before provider SSE/delta integration and acceptance.

## Codemode tool argument ownership

QuickJS tool-call decoding now parses the final argument object losslessly after resource preflight. Serial and parallel calls retain typed ownership through admission copies, issued/delivered journals and nested execution. Global values and final VM results retain their ordinary decoder; their complete UTF-16 semantics need separate review.

`CodemodeCall` carries the native encoding flag and bounded argument carrier. Its standard Go JSON codec preserves exceptional keys/values across the native remote-pipe protocol. The codec uses `DefaultLimits`; outer DTO readers revalidate restored carriers with their caller limits. Enlarged non-default budgets still need acceptance review. Caller wrappers receive no marshaler exception. Raw unpaired strings outside the declared carrier remain invalid.

Legacy ordinary-JSON journals can replay VM calls that now carry an encoding marker: matching ordinary values retain the stored admission mode. Exceptional mode changes fail compatibility checks. Delivery identity still checks IDs, names, global/tool classification and exact logical arguments. Reused completed runs replay confirmed deliveries without repeating effects.

Go 1.27.3/GOMAXPROCS=4 development checks:

* Real QuickJS serial, parallel and native Go remote subprocess executions/reopen pass on memory, journal and SQLite. Isolated keys/values survive issued journals, host dispatch, interruption and recovery; executor map mutation stays detached. A completed second invocation reuses the stored result.
* Legacy UTF-8 journal replay without the new marker passes on all three stores.
* Native standard-JSON and DTO round trips preserve exact large integers and code units. Fault cases reject malformed/nonobject arguments, trailing documents, typed tool flags on global calls, untyped exceptional values, inconsistent decoded witnesses and caller depth-budget overflow.
* Full root-module normal suite passes (durable 120.813 seconds). Shuffled codemode suite passes (37.392 seconds); final focused argument/receipt/reader/codemode race passes (26.355 seconds). Vet and diff checks pass.
* No acceptance profiling, performance comparison, provider wiring or full-parity acceptance.

Next: compaction argument formatting and scoped diagnostic tool-result text, provider SSE code-unit decoding/delta-event transport and the remaining streaming/finalisation test crosswalk. Review codemode global/final values, configured enlarged limits and the remaining main host-contract deltas before acceptance.

## Tool argument error diagnostics

Invalid parsed tool arguments now retain the full upstream-style diagnostic, including received UTF-16 units, in structured remarks and rendered tool-result text. Inspection corrected the earlier boundary description: `finishTool` discarded the supplied `Content` for harness errors and generated a generic remark. It did not simply fail while serialising that supplied text. The detailed parse error now becomes the `invalid_arguments` remark before finalisation.

`ContentBlock.SetToolArgumentDiagnostic` explicitly marks derived text; SDK JSON emission preserves its units. `RestoreStreamingArgumentsJSON` restores such text only on an already marked target. Ordinary content text and generic JSON keep their existing validation. The durable diagnostic flag is private.

Receipt witnesses now distinguish tool arguments, rendered diagnostic text and structured diagnostic remarks by kind/index. Diagnostic witnesses require an errored tool-result receipt with `errorCode: invalid_arguments`, matching text/remark placeholders and error severity/code. They share the bounded argument-carrier codec and aggregate encoded-byte budget. Final checkpoint remarks restore from the witnessed receipt instead of duplicating its payload. Unknown kinds, role/code/error mismatches and malformed placeholders fail validation.

Go 1.27.3/GOMAXPROCS=4 development checks:

* Memory/journal/SQLite invalid-call execution, next-model context, snapshot and reopen preserve full isolated-unit diagnostics. Invalid calls never invoke the executor.
* SDK explicit-text and durable receipt round trips pass; ordinary invalid text still fails. ASCII checkpoint remarks remain intact; nontext flag misuse and witness context mutations fail.
* Full root-module normal suite passes (durable 109.514 seconds). Focused normal checks pass (durable 5.646 seconds); focused diagnostic/receipt/tool/reader race passes (8.977 seconds). Vet and diff checks pass. No acceptance profiling.

Next: compaction transcript formatting still handles argument values and joined tool text through ordinary JSON/UTF-8 paths. Preserve their exact logical units through a scoped representation, then wire provider SSE/delta transport and finish the streaming/finalisation crosswalk. Codemode global/final values, enlarged codec budgets and broader validation-error fidelity still need review.

## Compaction argument-derived text

Conversation formatting now encodes typed tool argument values in their declared JSON domain. Isolated argument keys and explicitly marked diagnostic text retain their UTF-16 units in the Go transcript string. Each ordinary text/thinking/name field is checked separately; the presence of native arguments does not permit invalid unrelated text. Normal UTF-8 transcripts retain the existing checkpoint shape.

Exceptional transcripts persist through a bounded, schema-owned `sourceState` argument snapshot instead of a raw generic `source` string. Replay rejects mixed source representations and restores the exact logical text before constructing the summarisation request. `SetToolArgumentTranscript` marks that transient SDK request block for lossless JSON emission. The compaction source state owns persistence; ordinary message text remains strict.

Tool-result truncation counts isolated units once and keeps the existing complete-scalar cut for valid pairs. The encoded-document and string caps were not increased. Provider-specific request builders still need the following transport integration.

Development checks on Go 1.27.3/GOMAXPROCS=4:

* Memory/journal/SQLite summarisation interruption/reopen tests preserve isolated keys, JSON-escaped argument values and rendered diagnostics in the request, then produce the expected summary/recent-tail context.
* Formatting/state tests cover generic-JSON-safe checkpoint storage, exact restoration, mixed-state rejection, unrelated invalid text, string limits, isolated-unit counts and surrogate-pair cut boundaries.
* Full root-module normal suite passes (durable 109.111 seconds). Focused compaction/diagnostic race passes (5.193 seconds); vet and diff checks pass. No acceptance profiling.

Next: provider SSE code-unit decoding, argument-stream/delta transport and explicit argument-derived text emission in request builders; then the remaining streaming/finalisation crosswalk. Codemode global/final values, enlarged codec budgets, broader validator error fidelity and the other main deltas still need review.

## First provider argument-stream wiring

OpenAI function-call argument strings and Anthropic `input_json_delta.partial_json` now use the nominal internal `ArgumentText` decoder. It preserves code units only for these fields, rejects nonstring fragments and invalid non-WTF-8 bytes, and retains JSON-null handling. Ordinary text/reasoning/name decoding is unchanged.

Both paths now own a `ToolCallArgumentsStream` per call: partial arguments and final arguments use the same UTF-16-aware domain. Native ownership is explicit on content blocks and final tool-call events. OpenAI no longer accumulates an additional unused raw argument string. The former completed-call parser used ordinary `encoding/json` and lost isolated units even after lossless outer SSE decoding; the new tests exposed that boundary before the stream wiring was changed.

`ToolCallDeltaEvent` JSON serialization/deserialization preserves delta units and typed partial-message arguments. Existing field names, omitted-field reuse and null behavior remain intact. Ordinary text delta serialization is unchanged. This is SDK event JSON support, not completion of durable progress-event persistence.

Development checks (Go 1.27.3/GOMAXPROCS=4):

* OpenAI and Anthropic SSE fixtures split a valid surrogate pair between events and retain a separate isolated low unit in final arguments. Delta units and final values are checked.
* SDK delta-event JSON tests cover isolated delta/partial units, nonstring rejection, invalid bytes and omitted/null reuse.
* Full root-module normal passes (durable 109.530 seconds). Root/OpenAI/Anthropic race passes, including the final decoder reuse adjustment (1.095/1.098/1.279 seconds); vet and diff checks pass. No acceptance profiling.

Still open: OpenAI custom grammar input, Responses/Codex, Google/Gemini, Bedrock and other provider-specific argument boundaries; argument-derived text and replay argument request builders; durable delta/progress ownership; the complete upstream streaming/finalisation crosswalk and broader main convergence. No publication or full-parity claim.

## Responses function-call reconciliation

The native Responses processor now uses `ToolCallArgumentsStream` for function-call partials and finalisation. In pinned `api/openai-responses-shared.ts`, `response.function_call_arguments.done` replaces accumulated arguments; only a true continuation emits a delta. Native divergent replacements now reset stale arguments without emitting a false continuation, and nonempty `output_item.done.arguments` remains authoritative. Namespace and explicit native argument ownership survive the final event.

Only function-argument delta/done fields use `ArgumentText`; ordinary text/reasoning fields keep their existing decoding. Azure function-call events bypass an unnecessary map reserialization: the Azure normalizer only changes reasoning, while ordinary map reserialization discarded isolated argument units. Existing reasoning normalization remains in place. Provider observer maps still use their existing ordinary decoding; this slice does not claim lossless observer payloads.

Development checks, Go 1.27.3/GOMAXPROCS=4:

* Responses and Azure SSE fixtures cover surrogate pairs split between deltas, isolated low units, trailing completion deltas, detached earlier snapshots and final namespace/ownership.
* Authoritative divergent arguments-done and item-done replacements discard stale members and retain isolated units without a false delta.
* Full root-module normal passes (durable 110.624 seconds); full Responses race passes (1.124 seconds), vet and diff checks pass. No acceptance profiling.

Remaining boundaries: Codex has separate SSE and WebSocket processors and is not covered by this change. The native Responses processor still tracks a single current item; upstream per-output-slot/interleaving semantics require a separate review and implementation. Custom grammar, provider request builders, other providers, durable progress and full streaming/finalisation review remain open. No acceptance or full-parity claim.

## Codex function arguments over SSE and WebSocket

Both Codex processors now share a schema-owned function-argument stream adapter. Only function-call argument fields use `ArgumentText`; ordinary text and custom-tool input decoding remain unchanged. Partial arguments, arguments-done replacements and nonempty item-done overrides retain native UTF-16 ownership through final tool-call events. Empty arguments-done replacements clear stale values. Divergent replacements do not emit false continuation deltas. Invalid completed argument JSON retains its full native diagnostic.

Malformed typed function-argument events are protocol errors. The WebSocket path evicts its cached connection and classifies the error as non-transport, preventing SSE fallback/replay after emitted events. The SSE path emits an error and stops. Existing transport/cancellation handling is otherwise unchanged.

Go 1.27.3/GOMAXPROCS=4 development checks:

* Both transports exercise split surrogate pairs, isolated low units, trailing completion deltas, detached snapshots, native final ownership, divergent arguments/item replacements, empty replacement and invalid final argument diagnostics. WebSocket fixtures use a local test server.
* A malformed arguments-done fixture verifies non-transport classification and cached-session eviction.
* Full root-module normal passes (durable 109.900 seconds). Full Codex race passes, including the final cache-eviction assertion (1.739 seconds); vet and diff checks pass. No acceptance profiling.

Still open: per-output-slot routing/interleaving in Responses and both Codex transports, custom grammar, provider request/replay builders and cached continuation payload fidelity, provider-observer maps, other provider boundaries, durable progress ownership and full source/test crosswalk. This slice does not establish complete Codex/main parity.

## Indexed output-slot routing

Responses/Azure and both Codex processors now route each explicit `output_index` to its private active-item state. Function-argument streams, text, thinking and custom-input state no longer share a single current item when indexed events interleave. Completed slots are removed. An unknown explicit index never falls back to another item; events with the wrong item kind remain ignored. Parser state stays outside content blocks and SDK snapshots.

The shared internal router retains native compatibility for omitted indices: those events use the latest added item. Completing an older explicitly indexed item does not clear another active item; completing the legacy current item removes its indexed alias. This is a compatibility choice for existing no-index fixtures, not a claim of strict schema enforcement on noncompliant providers.

Development checks, Go 1.27.3/GOMAXPROCS=4:

* Responses, Azure, Codex SSE and local-server Codex WebSocket tests interleave two function calls with text and reasoning; verify separate argument replacement, isolated-unit ownership, content indices and staggered completion. Unknown indices, wrong-kind deltas and late deltas after completion do not corrupt other blocks.
* Router unit tests cover explicit zero/other indices, unknown completion and legacy/indexed alias cleanup. Existing unindexed provider suites pass.
* Full root-module normal passes (durable 109.945 seconds). Router/Responses/Codex race passes (1.007/1.130/1.778 seconds); vet and diff checks pass. No acceptance profiling.

Still open: upstream creation of missing slots from item-done events, unfinished-tool terminal guards and remaining finalisation details; custom grammar, provider request/replay/cached builders and observer payloads, other provider boundaries, durable progress ownership and the full semantic/test crosswalk. Slot routing alone is not full Responses/Codex parity.

## Missing-slot recovery and unfinished-tool guards

Responses/Azure and Codex SSE/WebSocket now reuse item creation when `output_item.done` arrives without `output_item.added`. Function-call, message and reasoning completion fixtures emit start/end events and reconstruct their final content. Present final message content is authoritative (including an empty array); missing content retains native delta-only compatibility. Reasoning uses nonempty summary text, then content, then streamed text. An explicitly mismatched completion kind cannot finalize another kind of active item; bare-item fixtures remain supported.

Unfinished tool calls are tracked independently of the slot router, including calls displaced by legacy omitted-index events. A terminal tool-use outcome fails if any call lacks item completion; arguments-done alone is not completion. Length-limited partial outcomes remain allowed, matching the pinned guard’s scope. The Codex WebSocket check runs before cached continuation publication, evicts the connection and returns a non-transport protocol error so incomplete calls cannot be replayed through fallback.

Go 1.27.3/GOMAXPROCS=4 development checks:

* Responses, Azure and both Codex transports cover done-only function calls with isolated units, final message/refusal content and reasoning summaries.
* Terminal guard fixtures cover indexed pending calls, legacy overwrite, mismatched completion kind, arguments-only completion, one unfinished call among two, and allowed length outcomes. WebSocket failure fixtures verify protocol classification and cache eviction.
* Shared projection tests cover explicit empty/missing content, summary/content priority and completion kind validation. The prior unknown-index completion fixture now uses an unsupported item kind: a supported done-only item legitimately creates a slot.
* Full root-module normal passes (durable 108.138 seconds); router/projection, Responses and Codex race pass (1.011/1.136/1.792 seconds). Vet and diff checks pass. No acceptance profiling.

Remaining: custom grammar input/reconciliation and UTF-16 scope, message-phase/finalisation details, provider request/replay/cached payloads and observer fidelity, other provider boundaries, durable progress and the complete source/test crosswalk. These guards do not establish complete Responses/Codex/main parity.
