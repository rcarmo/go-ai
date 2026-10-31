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

## Shared grammar-input stream and OpenAI custom calls

`GrammarToolInputStream` owns literal tool input and incremental JSON encoding. It counts/comparisons UTF-16 units for monotonic final-input reconciliation, emits valid JSON escapes (including NUL and isolated units), preserves valid pairs split between deltas, and enforces append/close/idempotence rules from pinned `constrained-sampling.ts`. Arguments expose strings or the native isolated-unit scalar; ordinary text decoding is unchanged.

OpenAI custom input now uses the nominal fragment decoder and this stream. Custom partial/final calls carry native ownership, including empty closing-delta snapshots. The old `strconv.Quote` fragment helper was removed: it could produce Go-only escapes rather than JSON. Native scalar grammar replay requires explicit block ownership, then retains existing native custom-replay sanitization; function fallback preserves the native JSON value. Full provider request/replay parity is not claimed.

Active OpenAI calls now live behind pointers. Growing a value slice could copy a live `strings.Builder` inside an argument/input stream and panic on later writes. Interleaved function/custom fixtures exercise creation of additional calls followed by writes to earlier streams. This is a correctness fix, not a measured performance claim.

Development checks, Go 1.27.3/GOMAXPROCS=4:

* Common stream tests cover UTF-16 prefix equivalence across split pairs, isolated units, JSON controls, empty input, idempotent closure, changed/invalid input rejection and non-mutating failed reconciliation.
* OpenAI fixtures cover interleaved function/custom calls, native ownership, valid generated JSON, isolated custom input, custom replay sanitization, function fallback and empty closing snapshots.
* Full root normal passed (durable 110.310 seconds) before the final empty-closing-snapshot adjustment. Final parser/OpenAI race suites pass (1.165/1.092 seconds); vet and diff checks pass. No acceptance profiling.

Next: wire the common grammar stream into Responses/Azure and Codex SSE/WebSocket custom inputs, including initial input buffering and final reconciliation. Message-phase/finalisation, remaining provider/request/replay/cached/observer payloads, durable progress and full semantic/test review remain open.

## Responses/Codex custom-input stream wiring

Responses/Azure and both Codex processors now use a shared `CustomInput` adapter around the grammar stream. Initial custom input is owned immediately but its JSON opening is buffered until the first delta or closure. Argument values, generated JSON deltas and final tool calls preserve native UTF-16 units. Argument state stays outside content blocks; blocks and events carry only owned argument projections.

Input-done closes the literal input with code-unit-aware monotonic reconciliation. Explicit empty finals are distinguished from omitted input: they cannot silently override nonempty input. Repeated identical/omitted final input closes idempotently; changes or appends after closure fail. Initial malformed fields and reconciliation failures use the existing stream error paths; Codex WebSocket treats them as non-transport protocol errors and evicts cached connections.

Azure now preserves original custom-tool JSON as well as function-call JSON when reasoning normalization is unnecessary. Responses native scalar replay extraction retains the existing custom-input sanitization policy rather than formatting the scalar as a Go struct. Full request/replay/observer fidelity is not claimed.

Go 1.27.3/GOMAXPROCS=4 development checks:

* Responses, Azure and Codex SSE/WebSocket fixtures interleave custom calls with initial high units, continuation low units, NUL, isolated input, duplicate closure and omitted item-final input; emitted JSON reconstructs final arguments.
* Empty and item-only completion succeed; explicit empty divergence, changed closure and append-after-close fail. WebSocket failures verify protocol classification/cache eviction. Shared adapter tests check buffered openings, schema property mapping, invalid field types and nonmutating failed reconciliation. Responses scalar replay retains native sanitization.
* Full root normal passes (durable 107.714 seconds); shared/Responses/Codex race passes (1.008/1.131/1.770 seconds), vet and diff checks pass. No acceptance profiling.

At fc194ad Codex still defaulted to the legacy `input` property; the following slice aligns schema-specific mapping and replay. Message-phase/finalisation, remaining provider/request/replay/cached/observer payloads, durable progress and full semantic/test review also remain open. No full custom-tool or main parity claim.

## Codex schema properties and custom replay

Codex now builds a per-request grammar property view from declared tools and active context tools. The latest transcript declaration wins; removed definitions still support replay. SSE, WebSocket, history conversion and cached response items use that view. Known custom calls use their schema property; unknown calls retain the `input` fallback. A schema may require an empty property name: the shared grammar adapter now distinguishes that explicit key from its zero-value default.

Replay emits `custom_tool_call` and extracts the selected property, including owned native scalars. Custom input keeps the existing surrogate sanitisation. Unsupported grammar falls back to a function call; native argument keys and values retain their units in its JSON arguments. Cross-model replay clears custom item IDs using the existing function-ID rule.

Malformed selected active schemas and non-string custom replay arguments fail before either transport connects. WebSocket reports a non-transport protocol error. Builders retain their existing internal prevalidated-context contract. Variant selection/definition validation, broader request validation, other providers’ empty-property handling and complete request/observer fidelity still need review.

Development checks on Go 1.27.3/GOMAXPROCS=4: full `go test ./... -count=1 -timeout=240s`, focused JSON/grammar/Responses/Codex race tests, `go vet ./...` and `git diff --check` pass. Tests cover schema-mapped UTF-16 stream/replay, cached view sharing, declaration replacement/removal, native fallback keys/values, malformed preflight and empty property names on both transports. These checks do not include acceptance profiling.

Next: message-phase/finalisation and the remaining source/test/dependency crosswalk, provider/request/replay/observer payloads and durable progress contracts.

## Empty custom properties and Codex variants

OpenAI and Responses replay now distinguish a selected empty property name from an unknown tool. OpenAI streaming explicitly selects that key for both nonempty input and empty closure. Responses/Azure use the same selection flag for custom replay type, item-ID prefix and input extraction.

Codex rejects active grammar configurations whose supported variants are missing or blank before connecting. Its request builder checks trimmed text for variant selection but sends the original definition, preserving leading/trailing whitespace. Lark takes precedence over regex; models without grammar support retain function fallback. OpenAI and Responses already validate these variants and preserve definitions.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full root normal tests, focused OpenAI/Responses/Codex/JSON/grammar race tests, vet and diff checks. New fixtures cover empty property streaming/closure/replay in OpenAI and Responses/Azure, Codex variant precedence/whitespace, blank-variant WebSocket preflight and unsupported-model fallback. No acceptance profiling ran.

Next: malformed historical declaration handling (Responses/Codex views still skip invalid historical grammar definitions), phase/finalisation, broader request/replay/observer validation and the remaining source/test/dependency crosswalk.

## Historical grammar validation

Responses and Codex now reject invalid latest historical grammar declarations before sending a request. Tool removal keeps declarations available for replay; a later same-name declaration replaces an earlier invalid definition. Models without grammar support retain function fallback. Active request definitions keep their existing separate validation.

Responses also validates custom replay input as a string, accepting native UTF-16 scalars only on argument-owned blocks. Missing, null and numeric inputs fail before the payload hook. This supersedes the older v1.0 missing/null-to-empty test: pinned main `getGrammarToolInput` throws for every non-string input. Rendering uses the same typed extraction as validation, removing implicit `fmt.Sprint` coercion.

Go 1.27.3/GOMAXPROCS=4 checks pass: focused packages, full root normal tests (durable 136.010 seconds), Responses/Codex race tests, vet and diff checks. Fixtures exercise invalid historical schema/variants after removal, valid replacement, unsupported fallback, request suppression, missing/null/numeric replay and owned/unowned native scalars. No acceptance profiling ran.

Next: message-phase/finalisation, remaining provider/request/replay/observer payloads, durable progress and the complete source/test/dependency review.

## Message phase and text signatures

Codex SSE/WebSocket now records version-one text signatures at message completion. Responses and Codex share signature parsing: version-one IDs retain `commentary`/`final_answer`; unknown phases are excluded from replay, and legacy plain IDs remain readable. Final item content stays authoritative. A `final_answer` item sets the partial stop state before text-start delivery; terminal response processing still determines the final stop reason.

Codex SSE now tracks terminal events independently of partial stop state, so final phase cannot turn a truncated stream into success. WebSocket and Responses retain their independent terminal guards. Azure commentary still normalises to reasoning; its final-answer message keeps the phase/signature.

A negative WebSocket fixture exposed a handler-lifetime race after intentional client rejection. The harness now waits for its hijacked handler and closes its test connection without waiting for an idle peer handshake. Frame writes may fail when rejection closes the peer; client protocol classification, cache eviction and event assertions remain the test gates.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full root normal, shared/Responses/Codex race, ten repeated Codex custom-reconciliation/message-phase runs, vet and diff checks. Fixtures cover interleaved/out-of-order message finalisation, authoritative final text, end-snapshot signatures, phase replay, Azure normalisation, truncated final phase, signature versions and legacy IDs. No acceptance profiling ran.

Next: Codex incomplete-status/reason mapping and reasoning finalisation, remaining provider/request/replay/observer payloads, durable progress and the full source/test/dependency crosswalk.

## Terminal status, usage and reasoning recovery

Responses and Codex now share pinned-main status mapping. Only `incomplete.max_output_tokens` yields length. Other incomplete reasons, including an absent reason, yield error with a provider diagnostic; unknown statuses fail as protocol errors. Raw stop reasons include the incomplete reason. Native terminal error/event policy still needs comparison with the upstream outer stream wrappers.

Both Codex transports use one terminal finaliser for response identity, end-turn presence, usage and reasoning recovery. Cached/cache-write tokens are removed from ordinary input with a zero floor; this fixes SSE double counting. Reasoning token counters preserve explicit zero presence. Provider total tokens remain unchanged. Broader billed-cost and service-tier accounting remains under review.

Shared reasoning recovery matches terminal output to completed stored signatures by ID and fills absent/null/empty encryption fields. It preserves existing encryption, summary text and opaque JSON fields. This also fixes the Responses substring guard, which previously blocked recovery when summaries mentioned `encrypted_content` or the field was empty. Malformed/non-string existing encryption is retained conservatively. Replay and cached Codex output receive the recovered signature. Duplicate/reused reasoning IDs and outer-wrapper/cache error policy still need review.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal suite (durable 111.155 seconds), shared/Responses/Codex race (1.018/1.142/1.902 seconds), vet and diff checks. Tests cover complete/incomplete/unknown statuses across transports, missing/filter/length reasons, cache-token floor, explicit zero reasoning, end-turn false, terminal encryption recovery, replay/cache propagation and preservation of stored summaries/opaque values. No acceptance profiling ran.

Next: outer stream error-event/cache policy, duplicate reasoning IDs, remaining request/replay/observer and provider accounting semantics, durable progress, and the complete source/test/dependency crosswalk.

## Terminal error delivery and continuation safety

Responses/Azure and Codex now emit a terminal error event for unsuccessful output instead of a done event with an error stop reason. The event retains the partial message, usage, raw reason, response ID and provider diagnostic. Successful stop/tool-use/length outputs keep done delivery; later successful Responses terminal events still clear earlier incomplete diagnostics. Older v0.84/v0.85 error-as-done tests now assert pinned-main error delivery. Unfinished-tool length fixtures explicitly supply `max_output_tokens`.

Codex API errors after startup retain the partial message. Read failures and cancellation also preserve their partial state. A normal close before completion can fall back only before any provider events: after startup it emits one error and cannot replay the request through SSE. Auto-transport tests count zero HTTP fallback requests for both semantic failure and started normal-close truncation.

Native safety choice: semantic terminal failures clear cached continuation and evict the WebSocket session. Upstream inner WebSocket processing can construct continuation state before its outer successful-output assertion; native Go does not retain failed response state. API error paths also evict the failed session. Broader debug counters/transport telemetry and protocol-error partial propagation still need review.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full root normal, shared/Responses/Codex race, ten repeated auto-transport failure/truncation runs, vet and diff checks. Fixtures verify error-only delivery with partial metadata, cached-session eviction, no fallback after startup, max-token length success and stale-error clearing. No acceptance profiling ran.

Next: duplicate/reused reasoning IDs, remaining error/transport observer telemetry, provider accounting/request/replay payloads, durable progress and the complete source/test/dependency crosswalk.

## Reasoning identity ownership

Responses/Azure and both Codex processors now track the latest reasoning block for each provider item ID at slot creation, matching pinned main. Terminal encryption recovery uses that stream-owned index. An unfinished newer block prevents backfill into an older completed block with the same ID; completed newer blocks receive the encryption without changing older signatures. The index never enters content blocks, events or persistence.

Recovery no longer reconstructs identity by scanning completed signatures. A legacy Azure fixture used different IDs for added and done items; it now keeps the identity consistent with the provider protocol. Existing summary/encryption/opaque-field preservation tests still pass.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full root normal (durable 109.503 seconds), shared/Responses/Codex race (1.020/1.162/1.945 seconds), vet and diff checks. New shared and four-transport fixtures verify duplicate-ID ownership with completed and unfinished latest blocks. No acceptance profiling ran.

Next: protocol-error partial propagation and error/transport telemetry, remaining provider accounting/request/replay/observer payloads, durable progress and the full source/test/dependency crosswalk.

## Protocol partials and started transport diagnostics

Codex protocol errors now carry their stream-owned partial to the outer error event. The partial retains response identity and native argument ownership; error conversion sets terminal state without mutating detached progress snapshots. Preflight failures still have no started partial. Protocol errors remain non-transport failures, evict failed cached sessions and do not activate SSE fallback.

Started WebSocket read/normal-close failures now append `provider_transport_failure` diagnostics with configured transport, emitted-event state, after-start phase and original serialised request byte count. They increment failure statistics and activate the session policy for future requests, but do not count or advertise an SSE fallback for the failed current request. Semantic terminal failures and protocol failures do not acquire transport diagnostics/counters. Cancellation keeps its separate aborted path.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full root normal (durable 108.385 seconds), Codex race (1.937 seconds), ten repeated protocol/native-partial and auto-transport failure/truncation fixtures, vet and diff checks. Tests verify typed UTF-16 error-event persistence, detached progress, cache eviction, transport/semantic counter separation and after-start diagnostic fields. No acceptance profiling ran.

Next: payload/observer ordering and cancellation, pre-start request-byte accounting, remaining provider accounting/request/replay/observer payloads, durable progress and the full source/test/dependency crosswalk.

## Codex payload preparation

Codex now validates, invokes payload hooks and serialises one prepared object before opening either transport. WebSocket retries and SSE fallback reuse those bytes and the same tool view; telemetry-aware and ordinary payload hooks each run once per request. This fixes repeated hook execution on transport fallback and prevents a failing local hook from opening a socket. SSE now handles serialisation errors instead of ignoring them.

Already-cancelled requests skip hooks and network activity. Cancellation during a hook stops before connection. Local hook/schema/serialisation errors remain non-transport failures with their causes intact. Native payload replacements must encode JSON objects; null/array/scalar replacements are rejected before I/O. Pre-start transport diagnostics now include the prepared request byte count.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 112.786 seconds), Codex race (1.901 seconds), ten repeated preparation/fallback fixture runs, vet and diff checks. Fixtures cover hook failures, non-finite/non-object payloads, pre/during-hook cancellation, no connection or network counters on local failures, one hook invocation across fallback, preserved replacement fields and UTF-8 byte accounting. No acceptance profiling ran.

Next: Codex raw provider-event observer wiring (currently absent), observer mutation/fidelity and cancellation contracts, cache-retention/identity details, remaining accounting/request/replay payloads, durable progress and the full source/test/dependency crosswalk.

## Codex raw provider observers

SSE and WebSocket now invoke `OnProviderStreamEvent` before Codex event processing. The callback sees the original event type and status; its JSON-compatible mutations feed processing. Declared function-argument/custom-input fields, including terminal output items, expose exceptional code units as native `UTF16String` values and survive re-encoding. Ordinary metadata retains ordinary Go JSON decoding.

Observer errors preserve their cause and typed partial, terminate processing, invalidate cached WebSocket continuation/session state and cannot trigger transport retry or SSE fallback. Cancellation during a callback terminates with an aborted partial; cancellation also takes precedence when the callback returns an error. Neither path increments network failure/fallback counters.

The adapter review found an earlier mapping error: upstream `mapCodexEvents`/`normalizeCodexStatus` at the pinned source drops unrecognised or non-string statuses before shared terminal handling. Codex now does the same, superseding the earlier unknown-status rejection recorded above. Shared Responses/provider status validation is unchanged. Raw observers still see the status before this adapter step.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 109.869 seconds), Codex race (1.934 seconds), twenty repeated observer/status fixture runs, vet and diff checks. Independent production-transport fixtures cover callback order/mutation, first/later failures, cancellation with/without callback errors, typed partials, cache eviction and zero fallback/network counters. Field fixtures cover function/custom deltas, done items and terminal outputs. These do not complete the upstream-test crosswalk or acceptance profiling.

Next: event-envelope/missing-type filtering against the Codex adapter, cache-retention/identity details, other provider observer/request/replay/accounting paths, durable progress and the full source/test/dependency review.

## Codex event-envelope filtering

Both transports now filter valid non-object or untyped envelopes before observers and stream startup, matching `mapCodexEvents`. A callback can subsequently replace the event type with a non-string value; processing ignores that mutated type after startup. Malformed JSON now raises a partial-preserving protocol error on SSE and on WebSocket both before and after startup, without fallback or network-failure counters. SSE trims data and ignores whitespace/`[DONE]` records instead of treating the sentinel as terminal.

Upstream handles a JSON `null` frame differently: property access throws an ordinary adapter exception, outside its non-transport error classes. Native Codex preserves that distinction: before startup it records failure and falls back; after startup it records failure/diagnostics and returns the partial without fallback. Null frames never reach the observer. Production fixtures cover both policies.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 108.878 seconds), Codex race (2.158 seconds), twenty repeated envelope/protocol/mutated-type/null/sentinel fixture runs, vet and diff checks. No acceptance profiling ran.

Next: investigate SSE producer cancellation/ownership on early return, then cache-retention/identity details and remaining provider request/replay/accounting/observer paths. Typed-schema projection edge cases, durable progress and the complete source/test/dependency crosswalk remain open.

## Codex SSE producer ownership

The shared SSE parser can strand its producer on a full channel when a consumer returns early. New additive `sse.ParseContext` makes sends and between-read work cancellation-aware, preserving `Parse` compatibility. It never closes caller-owned readers or promises to interrupt arbitrary blocking reads; HTTP callers must also close response bodies. Buffered events may remain after cancellation.

Codex now derives and cancels a parser context on every processing exit. It checks request cancellation before processing queued events and at EOF, retaining an aborted partial instead of reporting a missing terminal event. The pinned adapter also ends at the first terminal response; native SSE now stops there, discards subsequent frames and releases the HTTP body without waiting for EOF.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 112.250 seconds), SSE/Codex race (1.041/2.296 seconds), fifty repeated parser-cancellation runs and thirty repeated Codex EOF/terminal runs, vet and diff checks. Deterministic parser fixtures cover full-channel cancellation, no reads after pre-cancellation, caller ownership and caller-unblocked reads. A production HTTP fixture holds the connection open after terminal plus malformed trailing frames; Codex completes and closes the body without observing the tail. No acceptance profiling ran.

Next: cache-retention/identity details. Other providers still using compatibility `Parse` require their own early-exit/cancellation ownership review; typed-schema projection edges, remaining request/replay/accounting/observer paths, durable progress and the full source/test/dependency crosswalk remain open.

## Codex cache retention and request identity

`cacheRetention: none` now suppresses generated prompt-cache/SSE affinity and disables WebSocket connection reuse. WebSocket requests without an effective cache session use UUIDv7 identities, generated once in the prepared request and retained across retries. Independent requests get separate ephemeral IDs. Explicit `websocket` transport now caches when a session is supplied, as the pinned source does; the native `websocket-cached` spelling remains supported.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 110.475 seconds), Codex race (2.361 seconds), twenty repeated retention/identity fixtures, vet and diff checks. Production fixtures cover SSE/WebSocket/cached/auto, default/none/missing session, both identity headers, prompt-cache fields, connection reuse and stable ephemeral identity across a connection-limit retry. No acceptance profiling ran.

Blocking follow-up: the native WebSocket registry currently keys sockets only by session, whereas upstream partitions each session by account. A repeated session ID with a different account can therefore select the wrong cached socket. The next slice must partition the registry, continuation ownership, eviction/close and concurrent acquisition correctly and add A/B/A account-switch fixtures before any convergence/acceptance claim. This is unresolved; current cache-identity tests use one account. Busy-session pooling, origin/credential boundaries and other provider ownership remain under review.

## Codex account-partitioned socket ownership

The blocking session-only registry issue recorded above is fixed. Registry keys now contain logical session and account, so switching A/B/A preserves independent sockets and continuation IDs. Removal and idle-close scheduling use the entry-owned key plus pointer identity; stale releases cannot delete a replacement or another account. Public session close detaches every matching account partition, stops its timers and closes outside the registry lock. Idle socket closure also runs outside that lock. Aggregate stats/fallback policy remain session-scoped as upstream does.

Native keys additionally include the resolved WebSocket URL to prevent routing a request onto another configured backend. Upstream groups by session/account without that endpoint component. This extra native endpoint fence is explicitly tracked for the final semantic/adaptation review; documentation does not establish acceptance of a divergence. Credential rotation for one account and busy same-account pooling remain unreviewed.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 108.926 seconds), Codex race (2.350 seconds), thirty repeated account/endpoint/concurrency fixture runs, vet and diff checks. Production fixtures cover A/B/A socket and continuation isolation, account-specific eviction, stale-entry removal/timer scheduling, replacement ownership, logical-session close, endpoint switching and overlapping A/B requests. Existing single-account assertions now inspect logical session membership under the registry lock. No acceptance profiling ran.

Next: busy same-account acquisition/spare-socket/release/cancellation semantics and credential/endpoint adaptation review. Other provider SSE ownership/typed-schema/request/replay/accounting/observer paths, durable progress and the full source/test/dependency crosswalk remain open.

## Codex busy-session spare sockets

Acquisition now claims cached entries under the registry lock. A busy entry produces an uncached spare instead of returning the same entry and serialising behind its stream mutex. Spares send full context without the cached previous-response ID, close on every exit and cannot publish a continuation or replace the cached owner. Successful cached owners release to idle only at function exit; failed owners are evicted through a shared deferred cleanup.

Idle expiry checks busy state, entry identity and timer identity. The timer guard prevents an already-queued old callback from closing a newly released/rearmed owner. Network close remains outside the registry lock. Concurrent cold dials retain one registry owner and treat losing dials as transient spares; this race-adoption choice still requires comparison/adaptation review against upstream’s asynchronous insertion.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 108.944 seconds), Codex race (2.382 seconds), thirty repeated spare/idle-ownership fixture runs, vet and diff checks. Warm-contention production fixtures cover spare success, cancellation, observer/protocol errors, cached-owner cancellation, independent closure, full-context/accounting fields, retained owner continuation and no spare promotion or network-failure counters. Deterministic timer fixtures cover stale callbacks, busy owners and entry replacement. No acceptance profiling ran; the disposable pinned-source review copy was removed after use.

Next: cached socket liveness/reusability and credential rotation; cold-dial adoption and native endpoint partitioning remain explicit adaptation-review items. Other provider ownership/typed-schema/request/replay/accounting/observer paths, durable progress and the full source/test/dependency crosswalk remain open.

## Codex cached-socket liveness and credential rotation

Cached sockets now have a connection-lifetime reader, because the Go WebSocket API has no synchronous ready-state inspection and processes peer-close/control frames only while reading. Per-request leases carry bounded raw event queues; queued data cannot cross lease boundaries, frames read while idle are discarded and cancellation unblocks backed-up queues. Uncached spares retain direct request-owned reads. Cache close, eviction and expiry cancel the reader; closed sockets are discarded before reuse or during release without activating transport fallback for a completed response. Stream-owned lease/reader state never enters messages or persistence.

Credential rotation within one account retains the already authenticated cached socket, matching upstream’s account grouping. A new connection uses the current token. Production fixtures cover idle peer-close/redial without stale continuation or fallback, original authentication on same-account reuse, idle malformed data plus ping/pong processing, reader shutdown and full-channel cancellation. Cached continuation fixtures now keep their server socket live until test cleanup rather than depending on a stale closed entry.

The first full run exposed an unrelated existing durable fault-fixture race: public `AbortTask` resumes scheduling, allowing the cutoff to be decided before append rejection was injected. Local `c11a56c` seeds the marked cutoff atomically without enabling scheduling and retains the real reservation/adoption-rejection checks. The failure reproduced in repeated isolated runs before the fix; 1,000 isolated runs and thirty race runs passed afterward. Temporary diagnostic logging was removed.

Go 1.27.3/GOMAXPROCS=4 development checks pass after that fixture repair: full root normal, Codex race (2.346 seconds), thirty repeated liveness/backpressure/busy-ownership fixtures, durable targeted race (2.087 seconds), vet and diff checks. No acceptance profiling ran.

Next: concurrent cold-dial adoption and endpoint-key adaptation review, remaining Codex continuation recovery/request/accounting/schema edge cases, other provider ownership and the full source/test/dependency crosswalk. Account-scoped credential reuse is covered; this does not establish full provider parity.

## Codex concurrent cold-dial adoption

Cold dials now adopt on completion as the pinned `acquireWebSocket` does: a later completed dial becomes the registry owner even when an earlier cold dial has already installed an entry. This supersedes the earlier first-owner/loser-spare policy. Busy-cache acquisitions still use independent uncached spares. Stream completion order cannot replace the last adopted socket or overwrite its continuation.

Superseded busy owners finish independently. Their release or error cleanup closes their own reader/socket without deleting the newer entry; superseded idle owners are retired immediately. Fixtures force handshake completion separately from stream completion and cover old-first/new-first success, old protocol failure, old cancellation and an older already-idle owner. The next request must reuse the last adopted continuation.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 111.088 seconds), Codex race (2.398 seconds), forty repeated cold-adoption fixture runs, vet and diff checks. No acceptance profiling ran.

Pending Rui decision: retain (1) native endpoint URL partitioning in addition to upstream session/account grouping, preventing wrong-backend reuse, and (2) prompt retirement of superseded untracked sockets/readers instead of preserving their upstream resource-lifetime behaviour. These safety adaptations remain explicit differences, not accepted parity. Other source-aligned work can continue while approval is pending. Next implementation slice: missing-previous-response continuation recovery and remaining request/accounting/schema crosswalks.

## Codex missing continuation before startup

A pre-start `previous_response_not_found` API error now retries WebSocket once. The failed cached entry/continuation is evicted and the prepared full request is reused on a fresh socket, preserving payload-hook invocation count and request identity. Repeated missing-continuation errors terminate without fallback or transport counters. Missing-continuation and connection-limit retry budgets are independent, matching the pinned outer retry loop. Fresh requests also receive the one code-based retry, even without a previous-response field.

Retry classification now requires a top-level Codex API error. An observer error whose cause happens to be a missing-continuation/connection-limit API error remains an observer failure, as upstream’s callback-error wrapper does; it must not retry. Cancellation takes precedence. Fixtures cover direct/nested/error-response codes, repeated rejection, fresh requests, combined retry types, callback cancellation and observer-wrapped API causes.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 108.855 seconds), Codex race (2.385 seconds), forty repeated pre-start recovery fixtures, vet and diff checks. No acceptance profiling ran.

Known remaining difference: upstream’s missing-continuation branch can retry after startup too. Native handling still terminates that started attempt; the next slice must preserve already emitted output and single-start ordering across retry while investigating partial typed-tool completion/reconciliation. This feature is not fully aligned yet. Endpoint isolation and superseded-resource cleanup still await explicit Rui acceptance; automatic continuation did not grant it. Other provider/durable/codemode/source/test/dependency work remains open.

## Codex missing continuation after startup

Missing-continuation recovery now retains a stream-owned output across attempts and emits the start event once. A failed attempt is evicted before retry; the retry uses the prepared full request without rerunning payload hooks. Text from the first attempt keeps its content index, and subsequent output appends new slots. Completed tool arguments retain isolated UTF-16 units and typed ownership through recovery and the explicit lossless persistence restore path. Repeated rejection and cancellation retain the existing partial output.

`recovery_started_main_test.go` covers created-only startup, text, completed and unfinished tools, exhausted retry, response.failed, retry dial failure and cancellation on the retry. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 125.378 seconds), Codex race (2.432 seconds), forty repeated after-start/pre-start/cold-adoption fixture groups, vet and diff checks. No acceptance profiling ran.

Two native safety fences also span attempts. The unfinished-tool ledger survives a missing-continuation retry, so an earlier unfinished call still prevents tool-use completion. Once any attempt has emitted a start event, a later transport failure retains the partial output and reports a transport diagnostic without replaying the stream through SSE. At pinned upstream 4b535d57, `processResponsesStream` creates a new unfinished-call set on each invocation, and the Codex outer loop resets `websocketStarted` on each attempt. The native cross-attempt fences differ from those paths. Rui has not accepted these differences or the earlier endpoint-isolation and superseded-resource-retirement adaptations. The fences stay intact during convergence.

Next: review continuation selection for explicit transport options versus the effective default. The pinned cache-context guard checks raw `options.transport` for auto/websocket-cached, independently of socket acquisition; plain websocket and omitted transport need their own crosswalk. Remaining request/schema/accounting, provider, durable, codemode and full source/test/dependency reviews and acceptance are unfinished.

## Codex transport-mode continuation selection

Socket reuse and continuation selection now have separate guards. A retained session can reuse its socket in any WebSocket mode. Only raw transport auto or websocket-cached selects cached context and updates the continuation. Omitted transport still selects WebSocket through the effective auto default, but sends full context like plain websocket. Plain/omitted requests leave an existing continuation unchanged. A successful cached-context response without a response ID also preserves the earlier anchor, matching the pinned conditional update. Error cleanup uses socket ownership independently of continuation mode.

`transport_mode_main_test.go` covers omitted/plain/auto/cached modes, auto/plain/auto and auto/omitted/auto transitions, plain/auto/auto seeding, missing response IDs and plain-mode eviction. It checks socket identity, previous_response_id, full input, completion and context/reuse/failure counters. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 111.409 seconds), Codex race (2.379 seconds), forty repeated transport fixture groups, vet and diff checks. No acceptance profiling ran.

Next: CacheRetentionNone session fallback/debug scope. Upstream derives cacheSessionId without a session under retention none; native acquisition already bypasses caching, but fallback lookup and debug/failure accounting still need comparison against that effective session ID. Request/schema/accounting, other providers, durable/codemode, full source/test/dependency review and acceptance are unfinished. The four recorded safety adaptations still need Rui’s explicit decision.

## Codex retention-none transport scope

Retention none now removes the effective transport session from fallback lookup, request counters and transport-failure accounting as well as socket acquisition. Such requests ignore an existing session downgrade and cannot create a sticky downgrade. A pre-start transport failure can still fall back to SSE for that request; a later retention-none request tries WebSocket again. Started failures retain the existing partial-error/diagnostic path without changing a retained session.

The transport helper returns the raw session ID; the wire-identity helper clamps that value separately. This follows upstream’s distinct cacheSessionId/codexSessionId handling and keeps long raw session ownership/counters separate from clamped header/prompt-cache identity. The no-retention path leaves a warmed socket and its continuation untouched.

`retention_scope_main_test.go` covers success, pre-existing session fallback, per-request SSE fallback, started failure and long raw session keys. It checks unchanged session counters/owner, fresh uncached sockets, continuation reuse on the original socket and absence of new sticky fallback. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 109.008 seconds), Codex race (2.405 seconds), forty repeated retention fixture groups, vet and diff checks. No acceptance profiling ran.

Next: custom grammar tool-result wire types and continuation-item filtering. Native tool results currently use function_call_output while upstream also distinguishes custom_tool_call_output. Remaining request/schema/accounting, provider, durable/codemode, full source/test/dependency review and acceptance are unfinished. Rui has not accepted the four documented safety differences.

## Codex grammar tool-result wire types

Codex tool results now select custom_tool_call_output when their tool name belongs to the resolved grammar-tool view. Other results keep function_call_output. Selection uses the same active/historical definitions and model capability guard as assistant-call replay. Call IDs retain the first pipe-delimited component. Continuation snapshots filter both output types, including synthetic missing-tool results inserted when transforming a completed assistant call.

`tool_result_main_test.go` covers active/function/removed/replaced definitions, unsupported models, unknown/empty names, and live function/custom WebSocket tool rounds. The cached round checks that the stored response contains only the assistant call and that the next request sends only the real result delta with the matching wire type. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 107.281 seconds), Codex race (2.424 seconds), forty repeated tool-result fixture groups, vet and diff checks. No acceptance profiling ran.

Next: tool-result content conversion. Pinned shared conversion joins text blocks with newlines, supplies empty/image-only fallback text, and sends image arrays when supported. Native Codex currently concatenates text and drops image blocks; the shared native TransformMessages image downgrade also needs review before changing this path. Remaining provider/request/schema/accounting, durable/codemode, full source/test/dependency review and acceptance are unfinished. The four documented safety decisions still need Rui’s acceptance.

## Responses tool-result text and image content

Codex and Responses now share the pinned tool-result text/image conversion. Text blocks join with newlines; ECMAScript whitespace detection includes BOM and excludes NEL. Empty output gets (no tool output). Supported images become ordered input_image entries after optional joined text; image data stays in data URLs. Unsupported images still pass through the shared capability downgrade before serialization. Codex user messages now also retain supported image blocks.

The shared transform deduplicates unsupported images beside matching omission placeholders, including already transformed content. Sanitization preserves literal U+FFFD and removes malformed UTF-8/WTF-8 surrogate bytes; the former implementation erased legitimate replacement characters. Supporting comparisons use pinned packages/ai/src/api/transform-messages.ts and packages/ai/src/utils/sanitize-unicode.ts, even though those unchanged support paths have no row in the changed-file inventory.

Tests cover the shared output union, ECMAScript whitespace, malformed versus literal replacement characters, placeholder idempotence and caller immutability. Live request fixtures exercise Responses SSE and Codex SSE/WebSocket with supported/unsupported images, function/custom outputs, empty/text/image results and compressed Codex request bodies. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal, race across responsestream/Codex/Responses/public tests, twenty repeated public content/transform/sanitization fixture groups, vet and diff checks. No acceptance profiling ran.

Next: Codex instruction construction and ordered system-message history. Native instructions currently take SystemPrompt directly and the input converter lacks a system-role branch; pinned Codex uses initial system-message text with a default and the shared converter handles later system messages. Other request/schema/accounting, providers, durable/codemode, full source/test/dependency review and acceptance are unfinished. The four native safety adaptations still need Rui’s decision.

## Codex instructions and system-message history

Codex now projects the normalized transcript before payload construction. Without mid-conversation support, system content and section patches fold into instructions, and request tools use the current declaration snapshot. With support enabled, only the initial system message supplies instructions; later system updates keep their conversation positions and use developer role for reasoning models, system otherwise. Initial sections render as their values; later section updates/removals use the pinned wording. Empty instructions use You are a helpful assistant. Whitespace and empty text-block separators are preserved. Empty input encodes as an array.

The context guard budgets the effective instructions. In the existing 5000-token context fixture, the 4096 reserve, two user tokens and eight default-instruction tokens leave 894 output tokens instead of 902. The guard is stricter because the request now includes that instruction.

`transcript_main_test.go` exercises compressed SSE and WebSocket payloads, defaults, legacy and transcript context, folded and ordered updates, sections, whitespace, tool snapshots, hook count and caller immutability. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 109.456 seconds), Codex race (2.496 seconds), thirty repeated transcript/budget fixture groups, vet and diff checks. No acceptance profiling ran.

Representation limitation: section fields are Go maps. Projection keeps deterministic sorted keys; original JavaScript object insertion order is unavailable. This limitation belongs in the final semantic review; multi-section byte-order parity is unverified. The four recorded safety decisions also lack Rui acceptance.

Next: Codex additional-tools/tool-search transcript plans, anchored declarations and removal notices. Current projection uses a snapshot; enabled dynamic-tool capabilities still need their own request/conversation plan. Remaining request/schema/accounting, providers, durable/codemode, full source/test/dependency review and acceptance are unfinished.

## Codex anchored additional tools

Codex now selects anchored declarations only when both mid-conversation system messages and additional_tools are supported, matching the pinned changes mode. Request-level tools are omitted in that mode; the initial and later declarations become developer-role additional_tools items in conversation order. Other capability combinations retain a current request snapshot.

Later declaration batches carry the pinned incremental-update notice. True removals emit the prohibition notice; same-name replacements exclude that name from removals. The ordered unavailable set survives replacements, loses names on re-addition and supplies Still unavailable notices. Function and grammar declarations share the request converter; grammar descriptions now survive conversion. The native context guard budgets serialized anchored declarations/notices conservatively, including historical definitions absent from the final snapshot.

`additional_tools_main_test.go` verifies compressed SSE/WebSocket wire order, instruction roles, capability gating, removals/replacements/re-additions, grammar/function definitions, context budgeting, hook count and caller immutability. Tool-result continuation fixtures also run with anchored declarations. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 109.778 seconds), Codex race (2.526 seconds), thirty repeated additional-tools/continuation fixture groups, vet and diff checks. No acceptance profiling ran.

Next: tool-search/additions mode, including initial fold boundaries and request versus result declarations. Responses still uses its older AddedToolNames deferred planner and needs comparison with the pinned capability guards and transcript plans too. Tool namespaces, remaining schema/request/accounting, providers, durable/codemode and full source/test/dependency review/acceptance are unfinished. Section-map ordering and the four recorded safety adaptations retain their earlier review/decision requirements.

## Codex tool-search additions

With mid-conversation system support and tool-search enabled, Codex now folds declarations through the last removal or repeated declaration. The leading system declaration also belongs to that request prefix. Later additions become completed client-side tool_search_call/tool_search_output pairs with query/limit arguments and defer_loading result definitions. Changes mode still takes precedence when additional_tools is enabled. Context budgeting includes both the request prefix and serialized search items.

Load IDs use the pinned UTF-16, uint32/base-36 protocol hash. Direct pinned TypeScript vectors cover ASCII, astral and isolated-surrogate names. The native SDK ShortHash contract is unchanged. Fold fixtures cover initial/no-initial declarations, identical redefinitions, removal/re-addition and prefix order. Compressed SSE/WebSocket fixtures cover function/grammar results, paired IDs, deferral flags, hooks, budgets and caller immutability. A cached WebSocket fixture adds a later tool as a two-item delta without invalidating the earlier prefix.

A direct pinned transcript-utility probe found a source mismatch when tool-search is enabled but mid-system support is disabled: for initial tool a and later tool b, the request plan contains only a and the collapsed conversion produces no b anchor. With mid-system support enabled, b is anchored. Native retains its existing full-snapshot path in the disabled combination to avoid silently dropping available tools. Rui’s decision is required before accepting this additional native/source difference; automatic continuation grants no acceptance. This probe exercises the pinned planners and caller sequencing, not a live provider. Temporary extracted source files were deleted immediately after use.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal, responsestream/Codex race, thirty repeated protocol-hash/fold/wire/cached-addition groups, vet and diff checks. No acceptance profiling ran. Next: compare Responses legacy deferred planning against pinned transcript capability guards and declaration plans, then namespaces/schema/request/accounting. Section ordering, the five recorded native/source decisions, provider/durable/codemode work and full source/test/dependency review/acceptance are unfinished.

## Responses legacy deferred-tool capability guards

The Responses AddedToolNames compatibility path now selects additional_tools only when both mid-system and additional-tools capabilities are enabled. Otherwise it uses tool-search when supported, or a complete immediate snapshot. Search calls include query/limit arguments. Existing native CRC-based load IDs remain unchanged; they are distinct from the pinned transcript IDs implemented for Codex.

The compatibility planner already owns the initial immediate/deferred declarations. The normalized initial header no longer emits them again, preventing a deferred tool from being advertised before its marker and avoiding duplicate announcements when all marked tools fall back to an immediate snapshot. The existing additional-tools fixture now explicitly enables both required capabilities without weakening its declaration checks.

`deferred_capability_main_test.go` exercises all eight capability combinations over live SSE, checking request tools, marker placement, load counts, arguments, legacy IDs, deferral flags, hooks and caller immutability. It also covers all-marked fallback. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal (durable 109.572 seconds), Responses/Codex race (1.165/2.546 seconds), forty repeated compatibility fixture groups, vet and diff checks. No acceptance profiling ran.

At that legacy-only checkpoint, transcript-native anchoring remained unfinished. The following section records its implementation and development checks.

## Responses transcript-native anchoring

Responses now uses a transcript-native plan except when AddedToolNames selects the existing compatibility path. With mid-system/additional-tools support, declarations are anchored as additional_tools rather than duplicated at request level. Tool-search folds through the last removal or repeated declaration and loads later additions with pinned IDs and query/limit arguments. Without mid-system support, the existing complete snapshot remains in place pending Rui’s decision on the pinned late-tool-loss difference.

Codex and Responses share declaration-state notices, section rendering and fold planning. Responses retains the initial system message; subsequent sections use update/removal notices. Request budgeting includes serialized anchored declarations and section content while preserving nil-options omission and the existing ordinary context-clamp fixture. Selected declarations are validated before payload construction; malformed anchored JSON fails before hooks or HTTP rather than becoming a null input.

Eight live SSE capability combinations cover initial placement, removal/redefinition folds, request prefixes, search identities/arguments, hooks and caller immutability. Additional fixtures cover wire/section budgets, nil options, malformed schemas and anchored grammar replay. Go 1.27.3/GOMAXPROCS=4 development checks pass after the final error guard: full normal suite, Responses/Codex/responsestream race, thirty repeated Transcript/ToolSearch/AdditionalTools/LegacyDeferred groups, vet and diff checks. No acceptance profiling ran.

Next: review namespace declarations/replay and remaining schema/request/accounting paths against the pin, then continue other providers, durable/codemode and complete source/test/dependency review. Go section maps still cannot recover original JavaScript insertion order. Five native/source decisions await Rui; broader semantic convergence and acceptance remain incomplete.

## Responses and Codex namespace presence

Pinned shared conversion captures namespace when defined and replays it only for identical model/provider/API provenance. Pinned Tool and ToolReference declarations have no namespace field; this metadata belongs to calls, not declaration grouping. Responses previously lost explicit empty namespaces; Codex omitted namespace capture/replay altogether. Both now preserve valid string presence, including empty completion overrides and absent completion retention. Tool-call end events carry the same metadata as final content blocks. Manually constructed nonempty native namespaces remain compatible without requiring the presence flag.

Namespace helpers do not turn malformed/non-string metadata into an explicit empty string. This preserves the typed string boundary; it does not claim equivalence for malformed provider values outside that contract. Source review: packages/ai/src/api/openai-responses-shared.ts capture/replay and convertResponsesTools, plus packages/ai/src/types.ts Tool/ToolCall.

Forty-two function/custom stream cases cover Responses SSE and Codex SSE/WebSocket: absent/empty/nonempty values, completion clearing/replacement, late values and done-only creation. Each checks start/end/final metadata and four replay provenance combinations. Shared fixtures cover manually constructed native values and absent/malformed updates. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal suite, Responses/Codex/responsestream race, thirty repeated namespace groups, vet and diff checks. No acceptance profiling ran.

Next: strict/schema capability defaults and remaining request/accounting review, then other providers, durable/codemode and the full source/test/dependency crosswalk. Five earlier decisions and section insertion ordering remain open. No push, hosted dispatch, publication or release-ref changes occurred.

## Strict capability defaults and Codex required sampling

Pinned Responses defaults strict support off; Azure Responses defaults it on; Codex defaults it on. The Cloudflare gateway uses the ordinary Responses API, and its pinned provider/catalog does not add a strict-support override. Native Responses now selects the default by API rather than enabling it for the Cloudflare provider. Explicit compatibility overrides still win.

Codex previously swallowed errors from required JSON-schema constrained sampling and could send a non-strict declaration. A shared Codex declaration converter now returns those errors, and request preparation validates exactly the projected request/anchored declarations before hooks or transport. Old definitions folded out of a snapshot or tool-search prefix do not reject a later valid replacement. Unsupported preferred schemas still fall back; existing non-strict payload-hook repair behavior is unchanged.

Fixtures cover eighteen Responses API/provider/override combinations, the Codex support/prefer/require/schema matrix, pre-transport rejection in SSE/WebSocket/auto modes, and snapshot/change/search projection boundaries. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal suite, Responses/Codex/responsestream race, thirty repeated strict-default/sampling groups, vet and diff checks. No acceptance profiling ran.

Next: compare strict schema transformations with the pinned utility, then remaining request/accounting and provider paths. Durable/codemode, full source/test/dependency dispositions, section ordering and five earlier decisions remain open. This checkpoint does not establish acceptance or authorize publication.

## Pinned strict-schema transformation vectors

A direct Bun import of the pinned constrained-sampling.ts generated 33 maintained regression vectors and 132 supported/prefer/require sampling outcomes in testdata/strict_schema_main.json. Inputs cover nullable primitives, nested objects/arrays, const/enum/anyOf nullability, malformed schema nodes and unsupported constructs. Temporary extracted TypeScript was removed immediately after comparison; the retained JSON is intentional test source, not a raw run archive.

Native conversion now classifies non-object roots as unsupported strict schemas, allowing preferred-mode fallback instead of accidental decoding failures. Array items must be a schema object: boolean/null/scalar items no longer pass silently. Malformed anyOf members now use the pinned unsupported-node classification. Required mode continues to reject unsupported schemas. Malformed JSON remains an error, and caller bytes remain unchanged.

The native deterministic required-key order remains intact. Comparison treats JSON Schema required arrays as sets; all other output structure and strict-conversion rejection messages are checked directly. Existing native required-sampling error wrappers remain unchanged. This closes the strict-transform subset, not the whole constrained-sampling utility or provider review.

Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal suite; tests/Responses/Codex/Anthropic/Google/Bedrock race; thirty repeated strict/schema groups; vet and diff checks. No acceptance profiling ran. Next: remaining request/accounting review, including unresolved constrained-sampling/provider helper paths in the wider crosswalk. Durable/codemode, complete source/test/dependency dispositions, five native/source decisions and section insertion ordering remain open.

## Responses and Codex service-tier accounting

Codex now applies the pinned flex/priority rates in SSE and WebSocket completion accounting. A response tier of default retains requested flex/priority pricing; otherwise the response tier wins, falling back only for missing/null metadata. Ordinary Responses does not use that default-tier exception and now preserves explicit empty response tiers. Its fast tier aliases priority; Codex deliberately does not alias fast, matching the pinned API-specific helpers. Priority uses 2.5x only for the exact gpt-5.5 ID, otherwise 2x; flex uses 0.5x.

Both providers share component scaling and total recomputation. Responses now also copies supplied reasoning-token counts, including explicit zero, with the native presence flag. Token counts are not repriced or changed by tier selection. Native optional omission for absent reasoning metadata remains unchanged. Source review covered openai-responses.ts, openai-codex-responses.ts and shared completion usage mapping at the pin.

Eighty-one live-wire cases cover three model IDs, missing/null/empty/default/unknown tiers, response overrides and both transports where supported. Each checks request passthrough/immutability, all counters and four component costs plus total against independently calculated rates. A sixteen-ULP allowance covers double rounding and four-component summation for this workload; no output-hash comparison is used. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal, tests/Responses/Codex/responsestream race, thirty repeated billing groups, vet and diff checks. No acceptance profiling ran.

Next: remaining request options/sampling merge and reasoning compatibility review, followed by other provider, constrained-sampling utility, durable/codemode and full source/test/dependency work. Five earlier native/source decisions and section insertion ordering remain open. No publication or release-ref changes occurred.

## Responses reasoning gates and sampling precedence

Pinned ordinary and Azure Responses gate typed reasoning settings on model.reasoning. Native no longer emits them for non-reasoning models. Empty native reasoning pointers are treated as omission, including summary-only requests. Ordinary Responses keeps its Copilot omitted-off and xAI encrypted-include exceptions; those provider-name exceptions no longer leak into Azure, whose pinned builder has neither exception. Native abstract-level clamping remains unchanged.

Sampling merge required no implementation change: ordinary/Azure Responses resolve model defaults, clamped thinking-level defaults and request overrides, then assign them after named request fields. New fixtures check nested and null overrides, typed temperature precedence, explicit stream/store choices and caller immutability. The pinned Codex body does not perform this sampling merge; its existing behavior remains unchanged rather than gaining an unsupported merge.

One hundred forty-four API/provider/capability/off-map/option cases check reasoning and include projection; four additional merge cases check both APIs with reasoning enabled/disabled. Go 1.27.3/GOMAXPROCS=4 development checks pass: full normal, tests/Responses/Codex race, thirty repeated reasoning/sampling groups, vet and diff checks. No acceptance profiling ran.

Next: remaining header/cache and structured-option presence review, then other providers/constrained-sampling utility, durable/codemode and complete source/test/dependency work. Five native/source decisions and section insertion ordering remain open. This is not release acceptance; no push, hosted dispatch, publication or release-ref change occurred.
