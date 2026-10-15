# Native 1.2.0 implementation status

Active target: official `v1.2.0` at `06a466935536f8c9bcacb2d5efc0fc021be1bdff`, compared only with official1.1.0 `abe508e1b89912adde45528136c3221eb69acdd7`. Native working base/published1.1.0 runtime is `f44bf375763046ebef6f4eb49fea70ea046fc3ed`. User authorised updating both ports; no new commit/push/tag/publication authority. Go1.27.1 only. Durable legacy compatibility is not required.

## Pinned audit

Both npm1.2.0 archives match registry SHA512 integrity. [Provenance](provenance.json) records SHA256, versions, archive URLs and registry attestation references. Decoded publish/SLSA subjects match both archives. DSSE signatures/certificates have not been cryptographically verified for1.2.0. A prior1.1.0 artifact path assumption failed before catalog analysis; using the preserved `npm-ai/package` source corrected it.

[AI inventory](ai-inventory.json):21 changed paths,200 sources,171 suites. [Durable inventory](durable-inventory.json):117 changed paths,82 sources,64 suites. Counts are from exact Git trees/diffs; the GitHub compare API's300-file cap initially omitted durable paths and is not the inventory source.

[Full-record catalog delta](catalog-delta.json), identity type/provider/id with API included: chat1563→1554 (+16/-25/63changed); image61→61 (2changed); classifier26→34 (+8/0/5changed). Native catalogs regenerate to1.2.0 counts from the verified npm archive; generator/comparator pins now use1.2.0 schema-v6 structure hash `2c88c6b129a59cef02b34f4d964d3df80a9bbd79dec2459b5311a3f299a229d2`. Generated output was not hand-edited.

## Implemented

`retry_assistant.go` matches `provider returned an error` as well as the existing `provider returned error`. `tests/retry_120_test.go` tests the official upstream phrase, quota precedence and successful-stop exclusion. New/existing retry focused race count3 pass on Go1.27.1 (1.060s). This is ordinary development validation, without profiling; disposable log removed. `git diff --check` passes.

Mistral now uses a disarmed-after-headers timer and no whole-body http.Client timeout. Default header budget60000ms; caller cancellation survives through streaming. `timeout_120_test.go` covers slow-body success, header timeout and caller abort. Complete package race/shuffle count3 passes1.597s. The old0.84.2 test expected body timeout and hung under the new official contract; its caller-abort assertions stay intact and timeout assertions move to the new header/slow-body test. A fixture initially did not drain request bytes, delaying server cancellation; corrected without deadline changes. Initial compile probe corrected int/int64 options mismatch.

Anthropic400 `invalid_request_error` strict grammar/tool-count/union limits trigger one rebuild with preferred strict tools disabled; mandatory strict tools and input objects remain unchanged. Payload hooks rerun for the rebuilt request; fallback diagnostic persists. `strict_fallback_120_test.go` covers success, repeated rejection, unrelated error and required-only requests. Complete package race/shuffle passes2.844s; final focused count3 after cancellation reason preservation passes1.037s. Provider vet and diff whitespace pass. These are ordinary development checks; pre-release profiling/full validation still required. Disposable diagnostic logs removed.

## Catalog and OpenRouter HTTP slice

`scripts/generate-models.go` emits1554chat/61image/34classifier from the pinned artifact. Strict counts and official changed Kimi/GLM5.2 limits/pricing expectations are updated. Integrated tests/OAuth race shuffle pass23.970s/6.449s. Exact full-record comparators, non-ID metadata corruption gates and reproducible offline hydration with twelve fail-closed atomic faults pass. Helper profiles analysed by the existing wrapper and raw captures disposed.

`openrouter_catalog.go` implements bounded15s `/models/user` HTTP discovery with regional base URLs, key isolation, allowed-chat filtering, top-provider limit precedence and per-million pricing/tier mapping.401/403/404 retain a detached baseline; malformed/server responses reject. `oauth.RuntimeForAPIKeyContext` opts into key-specific discovery without mutating global models; OAuth runtime also personalizes OpenRouter chat models. Classifier catalogs remain separate and unchanged. Tests exercise HTTP/auth/path/pricing/empty/error/cancellation, distinct-key runtimes and global baseline isolation. `ResolveOpenRouterCatalog` now uses caller-owned `ModelsStore` persistence keyed by an opaque credential+normalized-endpoint scope. Offline reads retain restricted models without network; refresh uses the full baseline so expanded permissions can restore models. Failed refresh preserves stored data and returns its error. API-key/OAuth WithCatalog adapters expose regional/offline options without global registry mutation. Cache/key/region/expansion/OAuth tests pass focused race count3 (tests1.075s/oauth2.704s). No automatic durable cache store or silent failed-refresh fallback is supplied.

Repository vet/staticcheck/logging and non-CGO build pass. Static probes found an obsolete timeout-test pointer helper and required an exact-upstream-capitalisation lint note; fixed without gate suppression. Disposable logs removed after use. No new commit/push/tag/publication.

## Shared schemas

`model_schemas_generated.json` contains24 official AI model/compat JSON-schema fragments exported with pinned TypeBox1.3.27 (npm integrity verified). `scripts/generate-model-schemas.ts` validates package/dependency versions and exact official source hashes before generation. Output regeneration matches; no generated schema was hand-edited. `ModelSchema` returns detached fragments; `ValidateModelSchema` validates strict JSON against the fixed keyword set, required/type/enum/union/bounds/properties constraints and exact rational integer boundaries, with bounded depth/bytes/exponents. ProviderCompat intentionally allows extra properties as upstream specifies. Unknown schema names/keywords fail closed.

Schema tests cover thinking levels, null maps, costs/tiers, image limits/JPEG quality, cache lifetimes, compatibility enums, detached reads and oversized exponent/numerical rounding cases. Integrated tests/oauth race shuffle pass24.398s/7.173s; final schema/cache focused race count3 passes1.348s. Repository vet/staticcheck/logging/non-CGO gates pass before the final exponent guard; the final focused tests pass. This is fixed-fragment validation, not a general JSON-schema engine or coding-agent models.json CLI/config port. The exported TypeScript-only type refactor has native Go struct equivalents; coding-agent config schema belongs outside these two package inventories and must receive an explicit final disposition.

## Durable restart and owned-task slice

Tasks/options/canonical views now carry optional `AbandonOnRestart` and `AbortReason:"restart"`. Open captures only existing live disposable tasks; the scheduler durably abort-marks them before reservation/effects. Newly created disposable tasks run normally. Restart-blocked tasks wait for missing/old/migration-failed definitions so cleanup can run; explicit cancellation clears restart-only intent and allows normal orphan handling. Restart-only intent cascades to owned foreground tasks, with explicit parent cancellation taking precedence. Restart policy is immutable on replacement and abort-reason validation rejects invalid unions.

`TaskRuntime.OwnedTasks` exposes detached ID-ordered direct live children; `AbortOwned` checks direct ownership on the Session line, stages cancellation and uses invocation-bound terminal waiting. Tests cover detached checkpoints, self/unrelated/ended authority, terminal exclusion, reopened missing-definition cleanup and explicit-parent override. Focused restart/owned/recovery/inspection race shuffle count3 passes2.835s; broader recovery/ownership/wait/replay race shuffle passes66.192s. Repository vet/staticcheck/logging and non-CGO build pass. These are development checks, not final pre-release/profile acceptance. Initial Open variable-name compile probe corrected; logs disposed after analysis.

## Durable document retention and request errors

`SessionOptions.DocumentRetentionMs` and Harness settings now control migrated-document idle caches (default600000ms, explicit zero allowed). Reads refresh the idle timestamp; timer sweeps discard unobserved expired values and byte accounting. Live document watches pin their incarnation until detach; stopping a watch schedules eviction. Unload/Close stop timers and clear process-local values. Stored document versions/values and migration authority remain unchanged. Focused document retention/definition/settings/context race shuffle count3 passes5.029s; broader document/watch/scan/error race passes6.718s. Repository vet/staticcheck/logging and non-CGO build pass.

`StorageRequestError` now identifies invalid order/cursor requests and unwraps to native `StorageRejected` for existing inspection. Cursor decode/mismatch failure is nonfatal to direct storage reads; regressions verify subsequent Snapshot remains usable. Direct-storage read taxonomy still needs expansion beyond cursor/order failures; Session read guards also classify native `StorageRejected` reads as invalid requests. Initial test-helper signature probes were corrected; no timeout/assertion relaxation. Disposable logs removed after diagnosis/use.

## Durable Session backend failure guard

`SessionFailed` retains and unwraps the first backend cause. Session reads use a call-site guard, preserving backend identity and adapters installed after Open. Invalid storage requests and caller-cancelled reads do not fail the Session. Backend commit and ID-reservation errors latch failure, including definite rejections; local transaction callback/validation failures remain recoverable. A caught reservation failure still rejects the enclosing transaction. `Session.Closed(ctx)` waits for shutdown and returns `closed` or `failed`; cancelling a waiter does not cancel settlement.

Failure ends document watches and commit subscriptions with `session_failed` and the first cause, fences further admission, and starts asynchronous shutdown. Harness-owned sessions join actual retained handlers before closing storage. Tests verify stubborn-host cancellation/join, storage ownership, absence of invented outcomes, rejected-mutation state after reopen, normal shutdown, local validation and cancellation inside backend reads. Session/lifecycle focused race shuffle count3 passes1.409s. Broader Session/watch/document/restart/owned and two wrapper-dependent recovery tests pass count3 in30.818s. Vet/staticcheck/logging and non-CGO build pass before the last test-only additions. No pre-release profile acceptance yet.

The initial permanent Storage wrapper interfered with adapter fault hooks; call-site guards fix this and the affected ordered-continuation/intent-reopen tests pass. Recovery fault tests now require shutdown/reopen after backend rejection. Rejected prefix assertions run against reopened storage, before Open-time running-to-pending recovery when sequence equality is checked. Adopted hold/effect/receipt counters remain checked; uncommitted phases can rerun. Writer-attempt counters count the failed backend only; fresh-backend results are checked separately. Bound/frontier wait tests assert failure causes, detached registrations, no leaked provisional authority and unchanged durable state. Backend failure blocks fallback writes; host-offline preparation still tests its actual fallback failure. The restart-parent cancellation fixture now waits on its blocked child, removing a normal-completion race without changing deadlines.

Broad recovery/Session race shuffle passes57.331s. Final rejection/publication/restart fixture checks pass race shuffle count10 in2.830s. Complete durable shuffled normal suite passes48.083s and race suite passes317.383s withGOMAXPROCS=2. Repository vet/staticcheck/logging and non-CGO build pass after these migrations; diff check is clean. These development gates do not replace final integrated pre-release CPU/alloc_space/alloc_objects analysis. Diagnosed probe scripts and disposable logs are removed after use.

## Direct read validation and shared search

Direct storage now returns `StorageRequestError` for page limits, invalid IDs in legacy/ordered queries, missing entry conversations, legacy entry-cursor mismatches, request identifiers, invalid document scopes/points/addresses, unavailable historical content and document-cursor decode/query failures. Read-boundary conversion is separate from persisted-history materialisation and write admission; malformed stored revisions remain backend failures. The error still unwraps to `StorageRejected` for native error inspection. Memory, journal and SQLite share an18-case invalid-read matrix that checks subsequent valid reads and unchanged sequence/high-water state. Session guards and fatal-mutation tests remain in the affected suite.

`RankDocuments` ports the shared BM25 primitive with upstream stop words, ASCII camelCase splitting, plural stemming, unique query terms, k1=1.2, b=0.75, positive scores and stable input-order ties. Tests check tokenisation, empty/no-match/limit guards and scores against an independently written formula with1e-14 absolute tolerance. The implementation retains query-term counts rather than all document vocabulary. Deferred-tool discovery, schema text extraction and branch-local loading are still unimplemented; the ranker alone does not provide those APIs.

Read/document/history/scan/Session focused race shuffle count3 passes59.851s; final search/read/SQLite/Session focused race count3 passes1.281s. Full durable shuffled normal suite after the final missing-conversation regression passes47.525s. Repository vet/staticcheck/logging and non-CGO build pass before that last test-only addition. The full durable race receipt317.383s predates these read/search changes; final integrated profiling and race acceptance are outstanding. Diagnostic probes and disposable logs are removed after use.

## Deferred-tool discovery and model selection

`ToolRegistration.Deferred` and optional `Callers` separate enabled registrations from default model offers. Nil callers permits model/tools; explicit empty permits neither. `TaskAgent` exposes detached `Deferred` and `Callable` metadata. Registry selection composes extensions/wrappers before filtering; wrappers preserve the new metadata. `AgentChange`/`AgentPatch.ModelTools` adds/removes model offers separately from enabled-tool selection. Successful ordered tool controls persist model additions even when enabled tools use their default nil selection; additions follow branch-local load order. Patch clear/conflict checks include modelTools.

`ToolSearchRegistration` and `NewToolSearchExtension` provide model-only replay-safe `tool_search`, default limit8. Discovery searches only enabled deferred model-callable registrations from the phase snapshot, including names, descriptions and recursively collected schema property/description text. Blank queries and invalid configured limits reject. Positive matches return loaded names plus `AddTools` control; no-match results omit control. Tests verify initially hidden tools, internal-only exclusion, detached schemas, model filters/load order, next HTTP model-call offers, persisted additions on reopen and conversation fork, and absence of discovered-tool execution.

Focused tool-search/agent/filter/control race shuffle count3 passes34.838s. Staticcheck's schema traversal loop suggestion was applied; vet/staticcheck/logging and non-CGO build pass. The first full normal run caught a lost settings-validation error in `Harness.agent`; its original error return is restored. Settings/search/filter race count3 passes9.836s and latest full durable shuffled normal suite passes48.089s. Full race and pre-release profiling must be rerun after this slice. Metadata `Callable` is implemented; nested tool-call execution/authority needs its own upstream disposition and implementation. Compaction persistence and remaining extension/cancellation/error/replay boundaries need final coverage. Probe scripts/logs are deleted after use.

## Tool caller authority and typed model catalogue

Model-issued durable intents now retain their persisted model-caller permission; current captured selection also excludes caller-revoked, model-removed and newly deferred registrations. Recovery tests cover all three revocations and preserve cwd/deselection behaviour. Caller policy participates in implementation comparison; registry snapshots copy caller slices, and wrappers retain metadata without mutating the original registration. Forged model calls to internal-only, deferred/unoffered and explicitly caller-empty tools never execute. Tool-search tests now perform actual manual compaction and verify loaded tools remain offered on the next request, as well as fork/reopen persistence.

`TypedModelCatalog` optionally extends host-scoped catalogues with image/classifier lookup. `ModelAccess.GetModelOfType` and `GetModelsOfType` expose detached DTOs, remove endpoints/headers and reject wrong types/discriminators. Chat-only scoped catalogues return no typed models rather than falling back to global registries. Typed access expires with its invocation. Default unscoped access uses native typed registries. Auth availability queries and paid typed-model calls are not implemented by these lookup methods.

Focused authority/recovery/search/model race shuffle count3 passes33.342s. Latest full durable shuffled normal suite passes49.488s; vet/staticcheck/logging and non-CGO build pass. Full race/profile acceptance predates this slice. Upstream nested calls require keyed durable admissions, caller-owned result documents, call-kind-specific hooks/receipts, abort/restart settlement and bounded waits. That executor remains unimplemented; `Callable` metadata does not grant an execution API. Initial API/type probes were corrected and disposable logs removed after use.

## Keyed nested-tool implementation candidate

`TaskRuntime.StartTool`/`CallTool`, `ToolAPI.StartTool`/`CallTool` and invocation-bound `NestedToolHandle.Wait` now provide keyed native child execution. Admission atomically creates a task and family-keyed caller-owned `pi.tool.nested-call` document. Identical key/name/arguments reuse the same task; changed arguments reject. Enabled `Callable` selection and captured registry caller permission gate admission/execution. Input preparation is checkpointed before dispatch. Safe replay preserves the child ID; unsafe started calls abandon on restart and settle interrupted. Results settle in caller-owned documents alongside native outcomes and allowed application callbacks; they retire with the caller and add no standalone transcript entries. Executor attempts are timed, outputs bounded, returned views detached and escaped APIs sealed. Builtin native nested-task registration cannot be replaced through public registration/extensions.

Tests cover reuse/detachment/model-only exclusion, safe/unsafe restart replay, invalid arguments without dispatch, explicit abort waiting for actual stubborn-handler return, and callback settlement once. Nested/search/authority focused race shuffle count3 passes23.654s; latest complete durable shuffled normal suite passes49.243s. Vet/staticcheck/logging and non-CGO build pass. The initial abort fixture called joining AbortTask synchronously before releasing its handler; it now runs that joining request concurrently and verifies nonterminal state until handler return. No timeout or behavioural deadline was widened. Disposable diagnostic logs are removed after use.

This candidate uses a native task kind (`task.pi.nested-tool`) rather than extending the existing model `pi.tool` receipt envelope. Structured output, parent/caller hook attribution, paid-model adapters, result-document/key integrity under adversarial staging, nested capacity/concurrency tests and complete commit-failure matrices are not complete. Caller-owned durable keyed results/restart semantics are implemented; full upstream nested-tool parity is unverified. Further audit must address these gaps before release acceptance.

## Nested progress, result fidelity and hooks

Nested progress now commits bounded output/details/diagnostics in the native execution checkpoint. Updates read the latest checkpoint on the Session line and reject cancelled/ended/abort-marked invocations. Unsafe restart settlement preserves committed progress; safe executor replay drops previous-attempt progress before dispatch. Nested receipts retain bounded content blocks (including images), diagnostics, arbitrary JSON details and usage; handles return detached copies. Invalid content/diagnostics discard application callbacks. Before hooks can block or rewrite arguments before intent persistence, and after hooks receive detached results and can replace them. Before hooks do not rerun for a started recovery attempt. Hook call IDs currently use the nested key; full parent/caller attribution needs a native contract.

Tests verify durable checkpoint prefixes, unsafe restart preservation/safe reset, bounded text plus image-block fidelity, array/null details, no aliases, invalid-diagnostic callback suppression, before-hook rewrite/block, after-hook replacement and rejected progress leaving no result/partial checkpoint adoption. A rejected progress backend write fails the Session and remains rejected on reopen. The fault fixture initially reentered the ToolAPI mutex through TaskID inside append; it now captures identity before that admission. Behavioural deadlines are unchanged.

Focused nested race shuffle count3 passes5.294s; latest complete durable shuffled normal suite passes48.939s. Vet/staticcheck/logging and non-CGO build pass. Full race and pre-release profiling still predate these nested changes. Diagnostic logs are removed after use. Remaining gaps include schema-bound structured output, parent hook attribution, nested capacity/deep/concurrent joins, adversarial result-key integrity and broader outcome/uncertain-write fault matrices.

## Nested structured output, attribution and first integrity checks

`ToolRegistration.StructuredOutputSchema` accepts the validated native schema subset; unsupported schemas fail registration. Schema/caller metadata survives wrappers and detached snapshots. `ToolResult.StructuredOutput` and `HasStructuredOutput` preserve explicit JSON null and detached nested arrays/objects through hooks and caller-owned receipts. Nested validation runs after hooks/output bounding; mismatches or undeclared structured values produce `invalid_structured_output`, clear structured presence and suppress application callbacks. Schema-less nested calls expose bounded text as structured output. Missing structured values on schema tools remain absent, matching upstream error/no-result paths.

`HookAPI.ToolInvocation(ctx)` reports actual task, parent, caller category, call ID and nested key from committed invocation state. Tests verify nested caller/parent attribution. Final-candidate validation ties native nested task input to exactly one family-keyed caller document, requires result presence to agree with a decided child outcome, rejects early retirement/negative timing, and protects key/task/arguments and already-settled results on replacement. Native nested task input/version is immutable. Tests stage forged result, retarget, input mutation and early retirement attempts and verify normal execution remains usable. These are first integrity checks; coordinated raw authority forgery and further record/lifecycle adversarial cases require review.

Nested/tool-authority/control race shuffle count3 passes40.245s. Latest complete durable shuffled normal suite passes50.049s; vet/staticcheck/logging and non-CGO build pass. Initial nullable-schema snapshot/wrapper probes used an object-only copier; corrected to the nullable copier without relaxing validation. Raw probe logs are disposed after diagnosis/use. Full race/profile acceptance still predates this slice. Model-call structured output currently detaches through hooks but is not preserved in the existing transcript receipt; that upstream model/native disposition remains outstanding. Deep/concurrent capacity tests and uncertain outcome matrices are outstanding.

## Model structured-output disposition and nested capacity/settlement

Pinned upstream `appendToolResult` stores output, diagnostics, details and usage; it omits structured output from model transcript entries. Model structured-output validation now runs after hooks, reports/discards invalid values without changing model-call outcome or application callback semantics, and adds no transcript field. Successful nested schema tools reject missing structured output; explicit error results may omit it. Focused tests verify this distinction and callback behaviour.

Explicit nested keys now reject empty strings, slash segments, `__proto__` and positive-integer segments, matching upstream explicit-key rules. Default sequential key allocation is not implemented. Capacity tests exercise native MaxPage1 join rejection (`InvocationWaitRequiresYield`) without dispatch, then actual child settlement after the parent returns; MaxPage3 tests verify three-level calls with stable keys and one leaf effect. The one-permit test polls public task state rather than registering an extra external waiter that would consume its sole waiter slot.

Settlement fault tests cover rejection, uncertain-before and uncertain-after on memory/journal. Task outcome and caller-result presence agree after reopen; failed writers run once. Safe unconfirmed effects rerun, confirmed journal outcomes do not, and the parent joins the existing keyed result. Nested/model-structured/capacity/fault race shuffle count3 passes15.235s; final missing-structured regression race count3 passes6.995s. Full durable shuffled normal suite passes49.964s before that last test-only case. Vet/staticcheck/logging and non-CGO build pass. Final full race/profiling and concurrent/deep cancellation/sequence/replay integrity coverage are outstanding; no final parity/release acceptance yet. Logs are disposed after analysis.

## Sequential nested keys, concurrency and deep cancellation

`StartToolAuto`/`CallToolAuto` on TaskRuntime and ToolAPI allocate canonical positive-integer keys from an invocation-local atomic sequence. Fresh replay resets the sequence; the same call order reuses adopted caller documents/tasks. Explicit keyed APIs still reject reserved numeric keys. The native input tags sequential keys so final-candidate validation distinguishes them from explicit keys. Tests check ordered IDs across parent replay, distinct concurrent automatic admissions, and concurrent explicit-key coalescence without duplicate effects. Concurrent automatic call order is scheduler-dependent; callers requiring replay-stable association across nondeterministic concurrency must use explicit keys.

A three-permit deep-cancellation test verifies no descendant terminal outcome is invented while a retained leaf handler is returning, and result documents retire after bottom-up settlement. AbortTask joins its target handler, while descendant settlement continues through the scheduler; the fixture now waits for terminal state separately. Cleanup gate registration follows harness registration so failed assertions cannot block teardown. Deadlines were not widened. Nested environments now resolve before execution/timing and cache the invocation's result; failures suppress executor dispatch and produce a tool-error result. Executor arguments are detached from admission/checkpoint data before host use.

All nested race shuffle count3 passes16.927s; latest full durable shuffled normal suite passes50.426s. Vet/staticcheck/logging and non-CGO build pass. Full race/profile acceptance predates these slices. Remaining nested audit includes sequential executionMode, control/diagnostic edge cases, deeper caller/result/raw-state integrity and crosswalk evidence; paid models/images/codemode and auth availability are unimplemented. Disposable logs are removed after use.

## Nested mode/control audit and scoped model authentication

Pinned upstream explicitly runs nested calls immediately regardless of `executionMode`; sequential mode applies to model rounds. A three-permit test verifies two sequential registrations overlap as nested calls. Malformed nested control rejects the result and suppresses callbacks; valid controls remain local and never patch caller model state. Harness errors now omit structured output and carry error diagnostics. Final-candidate checks also validate nested outcome keys, usage and settled structured-result unions. Nested execution/identity integrity still needs further adversarial audit.

Optional `ModelAuthCatalog` exposes host-authenticated `HasAuth`, `GetAvailable` and `GetAvailableOfType` through invocation-bound ModelAccess. Calls link cancellation to the invocation, propagate host failures, strip credentials/endpoints from returned detached DTOs, and reject returned type/provider/catalogue scope widening. Missing auth capability returns ErrUnsupported for known models; catalogue presence is never interpreted as authentication. Out-of-scope references return false without asking host auth. This is a host capability boundary; automatic provider-auth adapters are not implemented.

Nested/model race shuffle count3 passes18.185s; final auth-capability regression race count3 passes1.298s. Full durable shuffled normal suite passes49.337s. Vet/staticcheck/logging and non-CGO build pass before the last test-only auth case. Full race and pre-release profiles remain outstanding. Initial test catalogue API probe was corrected; disposable logs are removed after analysis. Next implementation scope is paid classifier/image adapters and codemode, alongside final nested integrity/crosswalk work.

## Paid classifier/image adapter candidates

Opt-in `Options.PaidModels` installs tools-only non-replay-safe internal classifier/image adapters. `ModelAccess.Classify` and `GenerateImages` execute through keyed nested tasks, store structured provider results and settle usage atomically with caller-owned result documents. Known typed context fields are converted without running caller-defined MarshalJSON authority at durable admission. Context validation rejects empty/malformed questions or image input before paid dispatch. Models resolve within the existing host typed catalogue using provider/ID; returned endpoint/header fields stay stripped. Fresh `ClassifierRequestOptions`/`ImageRequestOptions` callbacks supply process-local credentials and transport options and receive invocation-linked contexts. Headers are cloned and credential options are never serialized.

Native image dispatch uses `images.GenerateImages`; OpenRouter provider registration is installed by its package. Native reserved tool `pi.codemode.models.generate-images` adapts upstream camelCase `generateImages` to durable lowercase identifiers; classify retains `pi.codemode.models.classify`. Private registration authority rejects public reserved-name replacement and repeated Open reuses installed adapters. Tests use synthetic classifier/image providers to verify host credentials, one keyed classifier effect, image data fidelity, usage3/6 tokens, no effect during reopen, input rejection and tools-only/non-replay-safe metadata.

Nested/model race shuffle count3 passes20.685s; final paid adapter checks race count3 pass2.968s. Full durable shuffled normal suite passes53.170s before the final test-only validation case. Vet/staticcheck/logging and non-CGO build pass. Provider cancellation/unsafe restart/error-billing/fault matrices, request-option hooks, catalogue/header/endpoint adaptation and codemode integration remain unverified. Provider tests are synthetic, without paid network calls. Final full race/profiling and crosswalk acceptance are outstanding; raw logs are removed after analysis.

## Paid image lifecycle, billing and request boundary

Synthetic paid-image providers now test shutdown cancellation with actual provider-return joins, unsafe restart without redispatch or fresh credential resolution, billed error result reuse, and rejected/uncertain-before/uncertain-after settlement. Confirmed journal results retain usage; unconfirmed results settle interrupted without repeating paid work or inventing durable billing. A provider may have billed an unconfirmed request; the durable port cannot reconstruct that amount without a confirmed receipt. Reopen preserves billed provider-error usage once.

Request option headers and suppression lists are copied; image metadata is bounded/detached. Payload rewrite hooks reject before dispatch, matching the durable chat boundary. Scope mismatch and invalid context tests dispatch no provider and verify secrets do not enter task/document records. Provider endpoints from catalogue DTOs remain stripped; explicit per-provider endpoint adaptation and real provider credentials are not certified. Tests use synthetic registered image providers, not live paid APIs. Classifier lifecycle/error matrices remain to be expanded independently.

Paid/nested race shuffle count3 passes27.462s; latest complete durable shuffled normal suite passes49.930s. Vet/staticcheck/logging and non-CGO build pass. Initial secret-inspection probe attempted to encode ID-keyed Snapshot maps as JSON; corrected to inspect bounded task/document DTOs. Probe logs are deleted after use. Final full race/pre-release profiling and crosswalk acceptance remain outstanding; codemode/image-processing are unimplemented.

## Classifier lifecycle and native image-preparation candidate

Classifier-specific synthetic tests now cover cancellation with actual-provider-return shutdown joins, unsafe restart without redispatch or fresh credential calls, billed provider errors/key reuse/reopen, and rejected/uncertain-before/uncertain-after settlement. Invalid classifier answer types/values become bounded error results with empty answers while retaining valid provider usage; previously the adapter returned a thrown error and lost that bill. Answer DTO parsing validates the returned public classifier shape. Classifier/typed paid race shuffle count3 passes11.045s. Synthetic provider registrations do not certify real network accounts.

`durable/tools.PrepareImage`, `Env.LoadPreparedImage` and `ReadWithPreparedImages` add an optional native preparation boundary. PNG/JPEG/GIF input fully decodes within existing8MiB/40Mpixel limits, validates MIME, applies JPEG EXIF orientation, preserves eligible original inline data, and otherwise resizes/encodes under width/height/base64-character budgets. PNG/JPEG quality attempts use80/70/55/40 and shrink dimensions by3/4 down to1×1. Native bilinear scaling uses premultiplied-alpha interpolation; row loops check cancellation, while stdlib decode/encode cancellation remains bounded only by input/pixel budgets. Local read paths retain existing Env ownership/security handling. Prepared reads avoid redundant full decoding. Tests cover decode/MIME/limit rejection, budget and dimensions, EXIF6, all orientation dimensions, constant-colour error≤1/255, and real local read/base64 decode.

Image/read focused race shuffle count3 passes1.051s. Latest shuffled normal durable/tools suites pass51.637s/4.151s; vet/staticcheck/logging and non-CGO build pass. This preparation is a candidate, not Photon parity: WebP/BMP decode, model-derived limit wiring, animation/pass-through rules, Lanczos3 quality/numerical comparisons, adversarial EXIF/dimensions and worker/cancellation/profile coverage remain. Defaults are native2048px/4MiB base64 pending full contract audit. Final full race/profile/crosswalk and codemode remain outstanding. Disposable logs are removed after analysis.

## Image formats, EXIF and invocation model limits

Native preparation now decodes WebP/BMP using `golang.org/x/image v0.39.0`; eligible WebP passes through, BMP converts to PNG/JPEG. JPEG and WebP EXIF share a bounded TIFF orientation reader. Pixel-level fixtures check all eight orientation mappings, WebP EXIF, malformed/truncated offsets and BMP colour decoding. A442-byte lossless WebP fixture from x/image v0.39.0 is vendored with its BSD licence. APNG preparation rejects animation chunks. Defaults now match pinned upstream2000×2000/4.5MiB base64. ReadWithImages derives per-field resize budgets from the invocation model and returns a non-attachment note for non-vision models; unresolved models retain upstream vision/default fallback. ToolAPI.Agent exposes the detached selection. Explicit ReadWithPreparedImages host limits override model dimensions.

Adding latest x/image v0.47.0 experimentally raised Go minimum/x/text; selectedv0.39.0 preserves Go1.25 module minimum and x/textv0.41.0. Execution/validation remains Go1.27.1. Only decoder module is a new direct dependency; final vulnerability/licence/SBOM gates are outstanding. Latest image/read model race shuffle count3 passes5.861s; normal durable/tools pass50.653s/4.097s. Vet/staticcheck/logging and non-CGO build pass. Avoided duplicate TIFF parsing; preparation still uses native bilinear interpolation, not upstream Photon Lanczos3. Numerical/quality parity, animated GIF/WebP behaviour, exact model zero-limit presence and full decode resource/cancellation/profile/fuzz boundaries need final review. Codemode is still unimplemented. Probe logs are disposed after analysis.

## Lanczos3 quality, allocation tuning and security blocker

Image resizing now uses separable antialiased Lanczos3 with premultiplied alpha, float32 intermediate scratch, final clamping/rounding and a64MiB resize-scratch budget checked before coefficient allocation. Source/output pixel budgets and cancellation row checks remain. Independent direct2D float64 convolution fixtures measure final channel error; observed max/mean0 at7×5,12×9 and19×13, accepted tolerance≤1channel unit and mean≤0.05. Alpha-edge tests reject dark fringes. Animated GIF pass-through retains two frames; resized GIF adapts its first frame. APNG rejects. Full Photon visual/decode-animation equivalence is not established; explicit-zero model resize presence still needs a native DTO decision.

Targeted profiling on Go1.27.1, i7-12700,512×384→256×192 showed per-tap image.At boxing dominated alloc_objects/alloc_space and repeated colour access CPU. Concrete NRGBA/RGBA access removed that work: equivalent benchmark25.314742ms,6,693,213B,1,174,977allocs →9.626753ms,2,002,116B,2,241allocs. CPU, alloc_space and alloc_objects were inspected before/after; captures/matching binaries/logs deleted immediately after analysis. This targeted benchmark does not replace integrated pre-release profiles.

Security scan found reachable x/image decoder findings fixed throughv0.45.0. Upgraded tov0.47.0/x.textv0.43.0, raising declared module minimum to Go1.26; README updated. The earlier preservation of Go1.25/x.textv0.41.0 is superseded by this security correction. Validation still uses Rui's pinned Go1.27.1. Decoder findings now clear, but vulnerability gate fails on reachable stdlib findingsGO-2026-6599,6600,6603–6605,6607–6613,6617, fixed in Go1.27.2. No exception added. Fresh approval to change the validation toolchain is needed; release security acceptance is blocked until then.

Image/read race count3 passes5.976s after dependency upgrade; numerical/read race3 passes5.421s after fast-path tuning. Latest normal durable/tools passes51.168s/4.095s; vet/staticcheck/logging and non-CGO build pass. Full race/security/licence/SBOM/crosswalk and codemode remain outstanding. Vulnerability JSON/logs are removed after diagnosis.

## Codemode catalogue and declaration primitive

`TaskRuntime.CodemodeTools` exposes detached phase-local program-callable tools with input/output schemas and identifiers, using the same captured selection as nested dispatch. Identifier normalisation follows ASCII JavaScript identifier rules over UTF16 units, including astral-character replacement. Colliding identifiers reject instead of silently granting the wrong tool. Default output declarations are string when no structured schema exists. `CodemodeSection` groups by longest namespace prefix, selects cheapest declarations round-robin within a token budget using TypeScript UTF16 length/4 estimation, and marks omitted groups/tools for discovery. `SearchCodemodeTools` uses the shared BM25 ranker and namespace filtering. Schema rendering currently covers the validated native subset; arbitrary TypeBox/local-ref/declaration-tag parity is not implemented.

Catalogue/caller-detachment/namespace/budget/schema tests race count3 pass1.128s. Latest normal durable/tools passes51.330s/4.124s; static/non-CGO gates pass before the final identifier-only adjustment. Codemode execution remains unimplemented: upstream requires the separate QuickJS sandbox package, clock/random/start snapshots, acknowledged ordered deliveries and replay journal, globals/store semantics, grammar-constrained raw input and sandbox limits. No unsandboxed JS substitute was added, and the catalogue helpers do not execute code. Security still fails for pinned Go1.27.1 stdlib findings fixed1.27.2; toolchain-change approval is outstanding. Disposable logs are removed after analysis.

## Codemode serial sandbox/journal boundary candidate

`CodemodeSandbox`/`CodemodeExecution` define an explicit host-supplied isolated-VM boundary; Go never evaluates source. `TaskRuntime.RunCodemode` persists caller-owned source/version/start-clock/random-seed/callable-tool snapshots, call records and ordered deliveries in family-keyed run/journal documents. Serial host calls commit admission before dispatch and journal each delivery before VM resolution. Replayed call IDs/names/arguments must match; results are detached, reused and retire with their caller. VM start/delivery DTOs are copied so a host sandbox cannot mutate durable snapshots through aliases. Calls use stable nested keys and existing durable paid/replay authority. Tests simulate VM failure at delivery and verify unchanged start data and one host effect, result isolation/source mismatch, caller document retirement and sandbox authority rejection.

This is a serial boundary candidate only. A concrete resource-limited QuickJS engine, parallel idle/issued-count delivery replay, journal cross-record adversarial validation, restart/cancellation/failure matrices, globals/conversation store, model-only raw-input tool/grammar, declarations beyond the native subset and output/spill integration are unimplemented. Hosts must supply isolated execution; no unsandboxed JS replacement or default codemode registration exists. Synthetic tests do not establish actual VM security or full upstream execution parity.

Catalogue/runtime plus sequential-key race count3 passes2.794s; latest normal durable/tools passes51.003s/4.157s. Vet/staticcheck/logging and non-CGO build pass. Security remains blocked by pinned Go1.27.1 stdlib findings; approval to validate on1.27.2 is still outstanding. Full race/pre-release profiling and changed-path/test crosswalks remain incomplete. Probe logs are removed after use.

## Codemode replay integrity and caller restart

Storage final-candidate validation now requires paired run/journal documents, immutable source/start snapshots, callable issued IDs, gap-free matching deliveries, coherent value/error unions, complete deliveries before final results, and caller-terminal retirement. Replacement validation makes issued/delivery prefixes append-only and settled results immutable. Checks cover full-base and delta writes; the fault test exposed native write reduction to deltas, so nested result replacement checks were also added at the delta boundary. Tests stage source/seed/prefix/value/retirement mutations and verify the original run remains recoverable.

Actual caller shutdown/reopen replays the stored clock/seed/journal and returns without repeating a completed host effect. Rejected delivery commits fail the Session before VM resolution; reopen recovers the existing keyed child result and records one delivery without another effect. Synthetic sandbox tests cover cancellation and acknowledgement failure; no actual QuickJS security claim. Codemode/integrity race count3 passes5.108s; latest normal durable/tools passes51.374s/4.174s. Vet/staticcheck/logging and non-CGO build pass. The first fault probe ignored reduced delta writes; corrected without weakening prefix assertions or deadlines. Logs are deleted after use. Concrete VM, parallel protocol, globals/store, raw grammar/output and full acceptance remain incomplete; security blocker still requires Go1.27.2 approval.

## Codemode conversation store candidate

The serial sandbox start snapshot now includes detached conversation store values. Native `pi.codemode.store` uses version1 conversation scope with rewindable/asOf fork policy. Sandbox results can return bounded JSON `StoreWrites`; successful writes merge into the live conversation store atomically with the settled run result. Script-error results discard store writes; already-settled runs reuse their result without starting another VM or reapplying writes. Concurrent other runs can change the live store while this run retains its immutable start snapshot; successful write patches merge at final admission.

Tests verify VM snapshot alias isolation, successful-only writes, idempotent result reuse, fork/reopen persistence and rejected store/result commits adopting neither side. Actual JS `store`/`load` globals still require the concrete sandbox implementation. Codemode race shuffle count3 passes5.817s; latest normal durable/tools passes51.292s/4.106s. Vet/staticcheck/logging and non-CGO build pass before the last test-only fork addition. Full QuickJS/parallel protocol/global callbacks/raw grammar/output and release gates remain unimplemented/unverified. Go1.27.1 stdlib security blocker and1.27.2 approval request remain. Logs are disposed after analysis.

## Codemode options, persisted deadlines and bounded output

`ParseCodemodeSource` validates the optional first-line `// @options:` directive and rejects unknown fields, fractional/out-of-range limits, misplaced directives and missing source before admission. Defaults match upstream300000ms/10000output tokens; max_output_tokens0 is explicit and timeout is bounded1..2147483647ms. The directive is removed with line numbering preserved. Serial start snapshots persist the original deadline and output budget; replay computes remaining time without reset, and clock rollback cannot extend beyond the original timeout. Invocation/caller contexts and timeout cancel sandbox/nested calls. Context health is rechecked after sandbox events before durable effects. A cooperative host sandbox must implement its own hard VM CPU/memory limits; this interface does not enforce those against an uncooperative VM.

Native output projection currently keeps a UTF8-safe prefix using upstream's4UTF16units/token estimate; zero keeps no text. Tests cover directives, Unicode boundaries, timeout and replay not resetting deadline. Full head/tail/full-output spill, image output files, console grouping and raw constrained model tool grammar are not implemented. Codemode race count3 passes6.136s; latest normal durable/tools passes51.422s/4.115s. Vet/staticcheck/logging and non-CGO build pass. Concrete QuickJS/parallel/global callbacks and release gates remain incomplete; pinned Go1.27.1 security blocker unchanged. Diagnostic logs are disposed after analysis.

## Codemode driver admission and sandbox callback containment

Invocation-local per-key admission now rejects a second concurrent VM driver for the same durable run while permitting independent keys. Admission releases on all return paths and settled-result reuse starts no VM. Options parsing rejects duplicate fields and trailing JSON. Sandbox Start/Next/Deliver/Close callbacks run off the Session line behind panic containment; fixed errors/report messages exclude private host panic values. A close failure is reported without replacing an earlier execution result/error. Tests cover blocked concurrent driver, retry after panic, and each callback panic boundary. These wrappers do not enforce VM CPU/memory limits or contain a native engine process crash; concrete isolation remains unimplemented.

Codemode race shuffle count3 passes7.191s. Latest normal durable/tools passes51.863s/4.120s; vet/staticcheck/logging and non-CGO build pass after a test-only staticcheck conversion fix. No final parity/security/release acceptance. Concrete QuickJS/parallel/globals/raw grammar/output and image zero-presence audits remain; pinned Go1.27.1 vulnerability blocker still awaits approval. Disposable logs are removed after use.

## Codemode settlement deadline and numeric boundaries

Final result admission now rechecks both the execution context and persisted deadline inside the Session transaction, before staging store writes or the result. A synthetic clock test advances past the deadline during the sandbox event and verifies neither side settles on each native backend.

Option parsing accepts exact integral decimal/exponent forms (`1.0`, `1e0`, `10e-1`) without float rounding. It rejects fractional values beyond the safe-integer boundary. Numeric literals are limited to 128 characters and exponents to ±400 to bound parser work; this is a native resource limit, not exact acceptance parity for every JavaScript numeric spelling. Invalid UTF-8 output uses a diagnostic string subject to the same output budget, including explicit zero.

Codemode race tests with shuffle/count3 pass in 7.584s. Normal durable/tools suites pass in 51.057s/4.154s. Vet, staticcheck, logging checks and non-CGO build pass after correcting a test-fixture conversion warning. Disposable logs are deleted after analysis. Concrete QuickJS, parallel replay, globals, complete output rendering and final integrated acceptance are still unfinished. Go1.27.1 security findings still require approved toolchain remediation.

## Model image limit presence

Model resize limits now distinguish omitted/null upstream fields from explicit zero. JSON decoding records presence; native `maxWidthSet`, `maxHeightSet` and `maxBytesSet` flags preserve zero through bounded model DTO encoding and cloning. Nonzero values need no flag. Explicit zero or negative dimensions/base64 budgets reject image preparation instead of silently selecting defaults. Omitted fields still use 2000×2000 and 4.5MiB base64 defaults. The host `ImageLimits` API retains its documented zero-means-default behaviour.

Tests cover upstream JSON presence round trips, cloning, omitted limits, explicit zero for each field and negative width through real model/read invocations. The initial custom-Marshaler prototype failed durable preparation because caller-controlled marshal authority is prohibited; it was removed without relaxing that check. Native presence flags are a Go DTO extension. Exact Photon numerical/quality parity, JPEG-quality handling and full image resource acceptance still need work.

Focused root/tools race tests count3 pass in 1.014s/17.200s. Normal root/durable/tools tests pass in 0.004s/51.559s/4.453s. Vet, staticcheck, logging checks, non-CGO build and diff checks pass on Go1.27.1. Disposable logs are deleted after analysis. No publication or final parity acceptance.

## QuickJS-WASI reuse candidate from gi

Rui identified `rcarmo/gi/internal/codemode` as an existing engine. Inspected local revision `9b48b91f29b808a979d2267cb067ac7b5f464ff9` against remote main `7078df0d59bf4e763c9d5c80a38f309860c930de`; engine code differs only in a documentation link. It embeds MIT QuickJS-WASI 3.6.2 and a pi-codemode 1.0.1 prelude, uses wazero1.12.0 with context-close and a 4096-page (256MiB) cap, and configures QuickJS heap/stack limits. No engine was added to this repository yet.

An isolated Go1.27.1 probe ran the gi tests without modifying gi. Nine tests pass under race, including infinite-loop timeout, caller abort, recursion, concurrent tools, store and global/output tests. Its output-limit test hits the fixed20s deadline under race; unchanged, non-race it passes in1.495s. The full race probe therefore fails and is not accepted as a passing gate. The initial probe encountered Go auto-vendoring because guest assets live in `vendor`; explicit `-mod=readonly` fixes that harness issue. Disposable probe source, binaries and logs are removed after analysis.

Reuse needs persisted clock/seed instead of system clocks/randomness, journalled tool admission/delivery instead of direct callbacks, settled host joins, bounded event/output handling, and pinned1.2.0 prelude/provenance. gi store writes include deletions, which the current durable candidate does not yet support. Fresh native hard-memory exhaustion tests are required.

## JPEG quality candidate

Image preparation now uses the configured JPEG quality (host zero defaults to80) followed by the pinned deduplicated85/70/55/40 ladder. Model JPEG quality is propagated; out-of-range values reject. A grayscale fixture compares decoded output with an independent float64 Lanczos reference: quality20/95 RMS error22.651/1.978, encoded sizes854/1790bytes. This checks native quality response, not Photon equivalence or visual acceptance.

Focused tools race tests count3 pass18.986s; root/durable/tools normal tests pass0.006s/51.446s/4.559s. Vet/staticcheck/logging and non-CGO build pass. The model invalid-quality case was added before the normal/static gates, after the focused race run. Logs are disposed after analysis.

## Concrete QuickJS-WASI serial candidate

Added `internal/quickjs` from gi's engine with its MIT notice, QuickJS-WASI3.6.2 asset and official1.2.0 prelude. Asset origin/hash and independent-build limitation are recorded in `internal/quickjs/guest/PROVENANCE.md`. Wazero1.12.0 terminates cancelled execution and caps memory at256MiB; the durable adapter sets a32MiB QuickJS heap cap. WASI has no filesystem mounts, inherited environment, stdio or network access. Start clock and seeded randomness replace ambient inputs before prelude freezing. The current seed implementation uses a32-bit JS stream and the persisted start clock remains fixed; exact upstream random/time progression still needs audit.

`NewQuickJSSandbox` connects tool callbacks to the native serial event/delivery journal. Engine callbacks wait for journal delivery; settled reuse skips VM/effect execution. Concurrent callbacks reject until the parallel journal protocol exists. The adapter joins its engine worker on close. Image output and store deletion return explicit unsupported script errors, so store writes do not silently settle partially. Globals, return-value rendering, bounded bridge payloads, unsettled caller restart and delivery-time clock updates still need work.

New engine race tests pass5.390s for deterministic clock/random/store/tool execution, hard infinite-loop cancellation, caller abort and heap exhaustion. Durable real-VM journal/reuse race test passes4.576s on native backends. Normal engine/durable/tools tests pass0.768s/51.711s/4.613s; vet/staticcheck/logging and non-CGO build pass. No full race/pre-release profiling/security acceptance yet. Go1.27.1 vulnerability remediation still requires approval.

## QuickJS store deletion, return values and delivery clocks

Successful store patches now include atomic deletions alongside sets. Duplicate deletions, conflicting set/delete keys and oversized deletion lists reject before settlement and during journal validation. Real-VM tests distinguish explicit null from deletion and verify script errors cannot adopt deletion patches. QuickJS return values now populate the native `Value`/`HasValue` union, including explicit null; full upstream return-value text rendering is still missing.

WASI wall time reads an invocation-local atomic clock. It starts at the persisted start timestamp and advances to the journalled delivery timestamp before resolving each tool promise, including error delivery. A real-VM test observes1000ms before a call and2000ms after delivery without consulting host wall time. Exact upstream random algorithm and richer clock semantics still need audit.

Focused durable store/QuickJS race tests count3 pass18.570s. Normal engine/durable/tools suites pass0.710s/52.001s/4.551s. Vet/staticcheck/logging and non-CGO build pass. Parallel admission, image output, globals, bridge payload bounds, actual unsettled VM reopen and final profiling/security acceptance are unfinished. Diagnostic logs are deleted after use.

## QuickJS bridge bounds and actual unsettled reopen

QuickJS bridge strings are limited to1MiB before copying guest memory to Go. Output collection stops at4096items or4MiB aggregate payload; tool call collection has a4096-call cap. Oversized arguments reject before host dispatch, oversized output/return values produce fixed sandbox resource errors, and host callback results/errors are bounded before guest settlement. Callback panics become fixed errors without private panic data. These are native resource limits; the aggregate bridge counters do not replace durable record limits or the upstream output renderer.

An actual WASM caller shutdown/reopen test interrupts after a journalled tool delivery but before final result admission. A new VM replays the same seeded arguments, receives the stored delivery and completes; the host effect count stays1 and VM start count is2 across native backends. The wrapper pauses only the result boundary and still joins the real worker on close.

Latest focused engine/durable race tests pass11.037s/9.760s. Normal engine/durable/tools suites pass1.375s/52.468s/4.529s; static/logging gates and non-CGO build pass before the last callback-test-only addition. Final full race, profiling, independent guest provenance, security and upstream crosswalks are incomplete. Parallel protocol, globals, image/return rendering and exact random semantics still need work. Disposable logs are deleted after use.

## QuickJS deterministic seed audit

Replaced the temporary Go random reader and JavaScript LCG with pinned `runtime/vm.ts` behaviour: uint32 seeds, per-byte mulberry32 WASI randomness seeded with `seed ^ 0x9e3779b9`, and realtime `(seed+1)*1000ns` during QuickJS context creation to seed native Math.random. After context creation, clocks use persisted start/delivery timestamps. Seeds outside uint32 reject. WeakRef and FinalizationRegistry are removed before prelude freezing; native disabling of their intrinsics still needs audit.

Tests compare WASI bytes against the independent JavaScript formula, verify chunk-independent reads, same-seed native Math.random reproducibility, different-seed divergence, missing GC-observation globals and actual unsettled caller replay. Native Math.random has not yet been compared with the exact upstream QuickJS runtime artifact; runtime fingerprints/version pinning need further work.

Focused engine/durable race tests pass11.153s/10.434s. Normal engine/durable/tools suites pass1.430s/53.062s/4.599s. Static/logging checks and non-CGO build pass. Parallel protocol, globals, full output rendering, independent WASM provenance and final integrated/security/profiling gates are incomplete. Diagnostic logs are deleted after use.

## QuickJS text output projection

The engine retains console output as a distinct kind. Native text projection labels multiple text items with upstream `==> text N/M <==` markers, groups console lines last in `<console_output>`, and appends successful defined return values (strings directly; other JSON values indented). Explicit null renders as `null`. The existing durable Unicode/token budget applies after formatting. Real-VM tests cover mixed console/text ordering, return strings/objects/null and a one-token formatted output budget.

The reopen fixture now expects its numeric return value in output and handles the owned DTO's json.Number representation. Initial focused runs failed those obsolete fixture expectations; corrected runs preserve effect/start assertions. Focused engine/durable race tests pass11.587s/12.935s. Normal engine/durable/tools suites pass1.406s/54.648s/4.505s; vet/staticcheck/logging and non-CGO build pass. Image files, head/tail spill, failure formatting/status/wall-time annotations and exact JavaScript object-key/number formatting are not implemented. Parallel replay/globals, provenance and final full acceptance gates are incomplete. Disposable logs are deleted after analysis.

## Runtime identity fence

QuickJS now exposes a runtime identity containing embedded WASM/prelude SHA256 hashes, engine versions and native host protocol2. `RunCodemode` persists the optional sandbox identity in its immutable start snapshot and compares it before settled reuse or VM admission. A changed identity rejects without launching a VM or overwriting the original journal. Matching settled reuse still starts no VM. Custom hosts without an identity retain responsibility for stable semantics; wrappers must forward identities. The actual WASM reopen fixture now forwards the concrete identity.

Identity hashes cover intentional engine assets, not generated signal output. Host semantic changes require a protocol bump; the identity does not establish independent guest build provenance or exact upstream runtime equivalence.

Focused engine/durable race tests pass11.357s/12.944s. Normal engine/durable/tools suites pass1.485s/53.458s/4.669s; static/logging checks and non-CGO build pass. Parallel replay, globals, full output files/rendering and final security/profiling/crosswalk gates are unfinished. Disposable logs are deleted after use.

## Serial overlap rejection and fatal VM joins

The durable QuickJS engine now rejects overlapping tool calls on the VM thread before dispatching the second callback. This removes goroutine scheduling from the overlap decision; parallel execution is still unsupported. Host protocol identity is3. The first admitted callback may already have an effect, so rejection never promises rollback.

A real durable Promise.all test exposed a deadlock: the worker waited to publish its fatal result while the durable driver waited to deliver to an abandoned promise. The adapter now buffers one event and permits delivery acknowledgement after the engine worker joins. The durable driver still commits the admitted delivery before consuming the final result. Tests require no second callback, no successful store patch and prompt completion across native backends. Unsupported-parallel race tests count3 pass9.533s after diagnosis; the initial probe timed out and is not passing evidence.

Latest focused engine/durable race tests pass13.107s/14.608s. Normal engine/durable/tools suites pass1.653s/53.651s/4.590s; static/logging checks and non-CGO build pass. Full parallel replay, globals, complete output rendering, independent guest provenance and final integrated/security/profiling gates are unfinished. Disposable logs are deleted after analysis.

## Guest package integrity and ambient isolation

The published `quickjs-wasi@3.6.2` npm tarball passes its registry SHA512 integrity check, and its WASM SHA256 matches the embedded asset. `internal/quickjs/guest/PROVENANCE.md` records the receipt and remaining signature/independent-build limitations. Downloaded archive/metadata/extracted files are disposed after analysis.

Real-VM isolation tests verify missing process/require/network/timer/std/os/GC globals, frozen Math/JSON/Object.prototype, persisted Date constructors/Date.now and UTC timezone, and prompt rejection of `file:///etc/passwd` dynamic imports. These tests exercise guest-visible capabilities; they do not establish absence of all native-engine vulnerabilities. Race count3 passes7.689s.

Fresh shuffled `go test ./... -count=1 -timeout=180s` passes across the root module, including all provider/auth/catalog/test packages and concrete QuickJS integration. Durable/tools/engine times are55.605s/5.645s/2.033s. Vet/staticcheck/logging, non-CGO build and diff checks pass. This full normal receipt does not replace the stale full race receipt or final profiling/security/SBOM/consumer/fuzz gates. Parallel replay, globals and complete output handling are unfinished; Go1.27.1 security remediation still needs approval. Disposable logs are deleted after use.

## Strict QuickJS result and store decoding

Malformed guest store patches now fail atomically: invalid shapes/JSON values, duplicate keys, excessive entries and conflicting deletion/set encodings reject with no partial effects. Explicit JSON null remains a set; a one-element entry is deletion. Host callback outputs must be valid bounded JSON before guest settlement. Successful guest return values are validated before native result admission. Host protocol identity is4.

Strict decoder tests, real-VM store/reopen/output tests and focused engine/durable race suites pass15.318s/14.801s. A5s two-worker store-patch fuzz run passes171980executions; reproducible generated fuzz cache is disposed after use. Fresh full root-module normal tests pass (durable55.237s, tools4.626s, engine1.932s; GOMAXPROCS2). Vet/staticcheck/logging and non-CGO build pass before the fuzz-test-only addition. Full parallel replay, globals/output files and final race/security/SBOM/profiling/crosswalk gates are incomplete. Disposable logs are deleted after analysis.

## Concrete VM resource recovery and profiling

A new serial store/tool benchmark measures fresh QuickJS execution with one cached compiled engine, a32MiB heap cap, deterministic inputs and checked results. Go1.27.1/GOMAXPROCS2,3s benchmark:99iterations,37.105ms/op,5163556B/op,2767allocs/op. CPU, alloc_space and alloc_objects were captured and inspected. CPU is dominated by external WASM execution (61.33% flat) and wazevo callWithStack (21.05% flat); setup is24.03% cumulative. The profile includes compilation before benchmark timer reset, so cumulative profile percentages include startup. Allocation bytes are dominated by WASM Grow (56.34%) and initial memory (19.93%); object counts by constant-expression evaluation (46.43%) and context-close tracking (18.24%).

No measured optimisation is claimed. Compiled code is already reused; mutable-instance pooling would require resetting hidden guest state and could weaken isolation. These measurements are a targeted concrete-engine diagnostic, not whole-port pre-release profiling. Raw captures, matching binary and benchmark log are deleted immediately after analysis.

Engine recovery tests verify successful fresh executions after loop timeout, caller cancellation and heap exhaustion on the same compiled engine. Race count3 passes9.416s; vet/staticcheck/logging and non-CGO build pass. `go mod tidy` now records wazero as a direct dependency. Final whole-port profiling/race/security/SBOM gates and parallel/global/output parity are incomplete.

## QuickJS catalogue aliases and reserved tools

Codemode catalogue construction now excludes reserved `pi.codemode.*` tools before ordinary discovery/identifier checks. QuickJS receives both the original registered tool name and the catalogue's explicit JS identifier. Both raw-name and sanitised-name access route to the same original registered name in the durable journal and nested authority checks. ALL_TOOLS exposes sanitised names and excludes reserved tools. Global-only access to reserved tools is still unfinished.

A real-VM alias test exposed the earlier adapter's identifier-as-call-name bug for dotted tool names. Corrected tests invoke both `tools["mcp.leaf"]` and `tools.mcp_leaf`, assert two effects and reject reserved-name visibility. Host identity is5. Focused catalogue/alias race tests count3 pass10.311s. Fresh full root normal suite passes (durable55.226s, tools5.635s); static/logging and non-CGO build pass. Parallel/globals/full output and final race/security/SBOM/profile/crosswalk gates are incomplete. Disposable logs are deleted after analysis.

## Snapshot discovery globals candidate

QuickJS now offers `searchTools`, `describeTool` and `describeNamespace` using only its detached start catalogue. Search uses native BM25, accepts a bounded positive limit and optional namespace, and returns sanitised names/descriptions. Tool descriptions include native rendered schemas; namespaces support raw/sanitised lookup and longest-prefix membership. Discovery errors reject guest promises and do not invoke host tools. Host identity is6.

Real-VM tests cover namespace-filtered search, descriptions, schema rendering and missing-tool errors. Focused discovery/alias race tests count3 pass17.182s. Full root normal suite passes (durable58.374s/tools5.347s); static/logging checks and non-CGO build pass. Runtime-created starts currently have no namespace configuration source; explicit start namespaces work. Discovery globals are snapshot-pure but not yet journalled as upstream global events; exact option validation/default limit/result formatting and mixed tool/global serial admission need audit. Custom/model globals and parallel replay are unfinished. Final race/security/SBOM/profiling/crosswalk gates are incomplete. Logs are deleted after analysis.

## Mixed serial admission and discovery validation

Pending globals now participate in the VM-thread serial admission fence and clear when their outcomes are recorded. A pending discovery/global call prevents dispatch of an overlapping tool callback, matching the current serial-only boundary. Real-VM tests verify one global callback and zero tool callbacks on rejected overlap. Host identity is7.

Discovery rejects null query/options, null or fractional limits, and non-string namespaces. The native4096 search limit cap remains a documented resource difference; integral exponent spellings and upstream global-event journalling still need work. Focused mixed engine/discovery race tests count3 pass7.626s/7.905s. Full root normal suite passes (durable54.595s/tools5.225s); static/logging checks and non-CGO build pass. This fixes serial admission only; full parallel replay, model/custom globals, runtime namespace configuration and final acceptance gates remain unfinished. Logs are deleted after use.

## Serial discovery global journalling

Discovery calls now share the durable issue/delivery sequence with tool calls. A `Global` discriminator pins authority and replay identity; global arguments are stored as a spread array in the bounded argument object. Snapshot discovery runs after issue commit, and value/error/time delivery commits before guest resolution. Replay consumes stored global outcomes without consulting a fresh registry. Journal validation checks target identity as well as name/arguments. Host protocol identity is8.

The actual WASM reopen test now issues searchTools followed by a tool, inspects two issued/delivered records with the correct global/tool targets, and reopens without repeating the tool effect. Discovery/error and mixed reopen race tests count3 pass17.517s. Full root normal suite passes (durable54.352s/tools4.920s); static/logging checks and non-CGO build pass. This remains a serial native protocol; full upstream parallel batching/acknowledgement, runtime namespace configuration, custom/model globals and complete output integration are unfinished. Final race/security/SBOM/profile/crosswalk gates are incomplete. Logs are deleted after use.

## Host namespace configuration snapshot

`Options.CodemodeNamespaces` is validated and copied before Session ownership opens, then included in each immutable run start. Empty names/prefixes, duplicate names/prefixes and sanitised-name collisions reject; nested longest-prefix namespaces remain valid. Persisted start validation checks the same native resource bounds. These stricter ambiguity checks are native admission rules and need comparison with the upstream configuration contract.

The real-VM mixed global/tool reopen test now describes a namespace, searches and calls a tool; it inspects three issued/delivered records. Reopen supplies changed host namespace text, but replay still returns the original persisted namespace outcome and keeps the tool effect count1. Focused namespace/reopen race tests count3 pass11.177s. Full root normal suite passes (durable55.127s/tools5.005s); static/logging checks and non-CGO build pass. Parallel replay, custom/model globals, complete output files and final full race/security/SBOM/profile/crosswalk gates are unfinished. Disposable logs are deleted after use.

## Fresh full race and dependency gates

Fresh `go test ./... -race -shuffle=on -count=1 -timeout=600s` passes on Go1.27.1/GOMAXPROCS2 across all root-module packages. Durable357.168s, tools29.195s, QuickJS18.085s, tests21.205s and oauth6.921s. This supersedes the historical full-race receipt for the current working state; subsequent runtime changes require new verification.

Pinned govulncheck1.7.0 policy scan fails with13 undocumented reachable findings: GO-2026-6599,6600,6603,6604,6605,6607,6608,6609,6610,6611,6612,6613,6617. No exceptions were added and Go1.27.2 approval is still absent. Updated the policy's stale passing description without changing enforcement.

Pinned go-licenses1.6.0 check passes, with warnings that assembly/native dependencies are not recursively inspected. Embedded guest MIT notices are retained; automated Go-module scanning does not cover all guest provenance. CycloneDX-gomod1.12.0 generates/validates a working-tree dependency SBOM with22components and digest `c32ba34aae4fc10ec32da7ef3d74f18866593a9e5cc7a7fbb0a1f297d519f767`. Version is `working-1.2.0`; VCS field is unchanged HEAD `f44bf375763046ebef6f4eb49fea70ea046fc3ed`, which cannot identify uncommitted runtime changes. The temporary SBOM is disposed after validation and does not replace preserved release assets. Embedded WASM/prelude need explicit SBOM coverage before acceptance.

Parallel replay/custom model globals/full output, exact crosswalks, whole-port profiling and security acceptance remain incomplete. Full logs are disposed after recording these conclusions; published refs/stash are unchanged.

## Embedded guest SBOM coverage

SBOM generation now adds explicit MIT components for QuickJS-WASI3.6.2 and pi-codemode1.2.0, including embedded-file SHA256 hashes, relative asset paths and root dependency edges. Hash scope explicitly identifies the embedded file rather than the whole npm package. Validation recomputes expected assets/notices and rejects missing/duplicate components, field drift and missing graph edges. Existing release SBOM assets are untouched.

Augmentation idempotency/root-edge tests and existing normalizer/validator negative self-tests pass. Working-tree SBOM generation/validation passes with24components and digest `6e7e02b638f7fb9d8373da5bf04b366339ee87a3f8438bb8d4dfcf7f4f8c39ca`. A deliberate guest-hash mutation with a recomputed outer checksum fails validation as expected. Temporary SBOM and probe logs are deleted after use. This does not establish independent guest builds or release provenance for uncommitted changes. Security remains blocked by13 findings under user-pinned Go1.27.1; full parallel/globals/output parity and remaining acceptance work are incomplete. Runtime code was unchanged, so the preceding full race receipt retains its scope.

## Actual callback settlement and WASM delivery rejection

A real QuickJS cancellation test holds an admitted Go callback after it sees cancellation. Engine execution cannot return until that callback is released and joined; late store writes are absent, and a fresh execution on the same engine succeeds. Race count3 passes8.401s.

The rejected-delivery fault test now also runs the concrete WASM adapter. It rejects native base/delta journal delivery, checks the durable prefix has one issued call/no deliveries, no final VM result and no store writes, then reopens and completes without repeating the host effect. Race count3 passes16.293s; after adding explicit no-result/no-store prefix assertions, count1 passes6.263s. Static/logging checks pass before that assertion-only addition. The first generated fixture had a quoting syntax error, corrected before execution. Runtime code is unchanged; previous full-race receipt remains scoped to it. Parallel replay/custom globals/output files and final security/profiling/crosswalk acceptance remain incomplete. Logs are deleted after analysis.

## Shared guest call budget

The VM-thread call counter now counts globals and tools together; global-only scripts cannot bypass the4096-call native ceiling. An internal MaxCalls option can lower that ceiling, never raise it; invalid values reject before VM admission. Host identity is9. A real-VM32-call limit fixture verifies exactly32 global callbacks before resource rejection. The initial4096-call stress probe reached a resource error at3860callbacks under race and did not prove exact ceiling behaviour; the bounded fixture verifies the counter without extending deadlines. Maximum-ceiling stress acceptance remains unverified.

Focused engine/global and durable discovery/reopen race tests count3 pass8.609s/18.910s. Full root normal suite passes (durable56.340s/tools5.008s); static/logging checks and non-CGO build pass. The earlier full-race receipt predates this runtime counter change; focused race coverage does not replace a fresh full run. Parallel replay/custom model globals/output files and final security/profiling/crosswalk gates remain unfinished. Logs are deleted after analysis.

## Stress-probe diagnosis and handle ownership

Bridge string conversion errors now preserve active timeout/abort classification instead of reporting every failure as a resource limit. A200ms sequential-global loop test reproduces deadline termination and requires ErrorTimeout. Thus the prior3860callback stress failure did not establish guest memory exhaustion or the maximum call ceiling. Exact4096stress acceptance remains unverified; bounded32call admission tests still cover the counter.

The initial prelude run callback returned a guest handle that was discarded. It is now freed alongside the script function handle, including error paths. This is a per-run ownership fix; no measured memory/performance reduction or causal link to the stress probe is claimed. Host identity is10.

Global budget/timeout race tests count3 pass17.212s; static/logging checks and non-CGO build pass. Fresh full normal root suite passes (durable59.036s/tools5.372s). Full race still predates these runtime changes. Parallel replay/custom globals/output and final security/SBOM/profiling/crosswalk acceptance remain unfinished. Logs are deleted after diagnosis.

## QuickJS setup admission bounds

Native QuickJS setup now validates source UTF8/1MiB size, nonnegative start clock/heap limit, catalogue/store counts,4MiB aggregate setup strings, valid store JSON, executable callbacks and unambiguous tool aliases before module instantiation or JSON expansion. Source/catalogue byte budgets are native resource limits and may reject inputs accepted by upstream. Global duplicate names reject; namespace/global reserved-name collisions still need audit. Host identity is11.

Tests cover invalid source/store/limits, alias collisions, invalid identifiers and duplicate globals; valid raw/sanitised aliases and null stores remain accepted. Focused full QuickJS/real-durable race subsets pass24.814s/24.294s. Fresh full root normal suite passes (durable57.135s/tools4.528s, GOMAXPROCS2); static/logging and non-CGO build pass. Full race receipt predates recent runtime changes. Parallel replay/custom globals/output and final security/profiling/crosswalk gates are unfinished. Logs are deleted after use.

## Promise-job cancellation and post-result effects

The QuickJS job drain explicitly checks context health before each guest job and stops immediately once a final result is reported. Queued microtasks cannot issue another effect after exit/result admission. Host identity is12. Real-VM tests require a continuously resolving promise loop to time out promptly, verify fresh-engine recovery, and schedule a tool microtask before exit to assert zero post-exit effects.

Focused job/exit race tests count3 pass16.142s. Fresh full root normal suite passes (durable57.204s/tools5.240s); static/logging checks and non-CGO build pass. Full race receipt predates recent VM runtime changes. Parallel replay/model globals/output and final security/profiling/crosswalk gates remain unfinished. Disposable logs are deleted after analysis.

## Paid model globals serial candidate

Opt-in starts now expose `models.classify` and `models.generateImages` as journalled global calls that dispatch through existing hidden nested durable paid-model tools. The public global names map to native reserved tool names; ordinary discovery stays empty for those tools. Model-reference conversion requires provider/id and context; malformed references return a persisted error delivery that rejects the guest promise. Existing typed lookup/auth, usage and uncertain-settlement rules remain in force. Host identity is13.

Real-VM classifier tests catch an invalid reference, execute a scoped synthetic provider, then reuse the settled run with one paid effect. Race count3 passes10.154s. Fresh full root normal suite passes (durable59.614s/tools5.505s); static/logging and non-CGO build pass. The first fixture used nonexistent test APIs and was corrected before execution. The image global mapping is implemented but lacks a real-VM dispatch test. Opt-out/revocation/reopen, full model catalog globals, custom globals, parallel capacity and output files still need work. No live-provider or complete upstream parity acceptance. Full race/security/profiling/crosswalk gates are unfinished; logs are disposed after use.

## Concrete image model and opt-out tests

Real-VM image globals now have scoped synthetic-provider tests checking host-only credentials, stripped model transport headers/endpoint, persisted usage6tokens, hidden tool discovery and settled reuse with one provider dispatch. A paid-opt-out test requires the models namespace to be absent. Combined image/opt-out/paid lifecycle race tests count3 pass36.214s; static/logging checks pass.

The first image fixture omitted conversation builtin initialisation, causing missing usage-document settlement errors and a secondary nested integrity failure. Initial speculative settlement/runtime changes were reverted; the fixture now initialises the root like existing billed paid-model tests. No production runtime semantics changed in this batch. The fixture's compile errors were corrected before execution. Opt-out/revocation reopen, uncertain paid global settlement, model catalog/custom globals, parallel protocol and output files remain incomplete. No live-provider acceptance; final security/profiling/crosswalk gates remain unfinished. Disposable diagnostics are deleted after use.

## Paid host revocation after reopen

Fresh paid-global dispatch now checks the current Harness PaidModels setting as well as the persisted start permission. Retained internal registrations cannot override host opt-out. Stored deliveries replay unchanged; a new paid call after revocation gets a committed error delivery and no provider dispatch.

A real-WASM test pauses before its second paid-call issue, closes/reopens with PaidModels=false and the same retained registry, replays the first delivery and catches second-call revocation. Provider count remains1 and VM starts2 across native backends. Race count3 passes12.149s. Full root normal suite passes (durable58.064s/tools4.581s); static/logging and non-CGO build pass. This native host revocation policy needs explicit upstream crosswalk treatment. Full race receipt predates recent runtime changes. Uncertain paid-global settlement, model catalog/custom globals/parallel/output files and final security/profile/crosswalk work remain incomplete. Logs are disposed after analysis.

## Real WASM uncertain paid settlement matrix

The existing paid-image settlement fault matrix now also runs through `models.generateImages` in concrete QuickJS. Definite rejection, uncertain-before append and uncertain-after append close the Session; reopen never redispatches the provider. Native journal uncertain-after recovers the confirmed nested result and6tokens; unconfirmed cases recover no image output and0tokens. Tests inspect nested prefixes before reopen and compare output/usage after completion. The native interrupted result remains recoverable without fabrication.

Combined paid-fault/revocation race tests count3 pass26.479s; static/logging checks pass. Initial copied-fixture variable references were corrected before execution. Runtime code is unchanged in this batch. Full parallel replay/model catalog/custom globals/output files and final refreshed race/security/profiling/crosswalk acceptance remain incomplete. Logs are disposed after analysis.

## Journalled typed model catalogue globals

Opt-in model namespace now includes getModelsOfType/getModelOfType/getAvailableOfType. These global calls use scoped native ModelAccess, bounded credential/transport-free DTOs and committed deliveries; stored outcomes replay without querying a changed catalogue. Current host opt-out rejects fresh catalogue calls. Missing models return null, unsupported availability rejects guest promises. Native provider argument validation is stricter than upstream non-string omission and needs crosswalk treatment. Host identity is14.

Real-VM tests cover scoped classifier listing/lookup, headers/endpoint removal, foreign-provider lookup without global fallback, unsupported availability and settled reuse after catalogue removal. Race count3 passes11.356s. Full root normal suite passes (durable60.190s/tools4.800s); static/logging checks and non-CGO build pass. Auth-backed available results, unsettled catalogue reopen/revocation and chat/image variants need expansion. Custom globals, parallel replay/full output and final refreshed full race/security/profiling/crosswalk remain unfinished. Logs are disposed after use.

## Auth-backed catalogue replay

A real-WASM unsettled reopen test now exercises auth-backed image availability and scoped chat listing. It checks headers/API keys/endpoints are absent from guest results. After shutdown, the auth backend is changed to fail and the image model is removed; replay consumes stored deliveries, completes with one availability backend call and two VM starts. Native backends pass race count3 in10.946s; static/logging checks pass. Runtime code is unchanged in this batch.

This extends production-path catalogue replay coverage; live credential refresh and all model-type combinations are not accepted. Custom globals, parallel replay, full output and final refreshed race/security/profiling/crosswalk work remain incomplete. Logs are disposed after use.

## Failure text and prior effects

Native QuickJS text projection now appends script error details (name/message or guest stack) and the issue-order tool-call status list with the upstream warning that effects are not undone. Empty call lists report no calls. Sandbox/abort/timeout formatter branches exist; host deadline returns still use the native error boundary and do not settle a formatted run result. Host identity is15.

Real-VM tests execute a tool, emit console output and throw TypeError, then verify effect disclosure, error details, no failed store adoption and no repeated effect on settled reuse. Output budgets still apply after formatting. Race output/failure tests count3 pass17.554s. Full root normal suite passes (durable61.354s/tools4.481s); static/logging checks and non-CGO build pass. Status/wall-time headers, image files/full-output spill and exact JS formatting are incomplete, along with custom globals/parallel protocol/final race/security/profile/crosswalk acceptance. Logs are disposed after use.

## Journalled host custom globals candidate

`Options.CodemodeGlobals` now admits versioned process-local callbacks with first-argument or spread semantics. Start snapshots persist only name/version/spread metadata. Calls/outcomes use the shared serial journal; current callback absence/version change rejects fresh execution, while stored deliveries replay. Callback panics become fixed errors; results cross bounded DTO validation before delivery. Reserved built-in/model namespaces, invalid identifiers, duplicate names and namespace collisions reject at Open. Hosts must bump versions for semantic changes; arbitrary callback effects cannot be proven replay-safe by the runtime.

Real-WASM tests pause before a second custom call, reopen after callback removal, replay the first outcome and catch revocation with one callback effect/two VM starts. Focused custom/discovery race tests count3 pass17.388s. Full root normal suite passes (durable61.254s/tools4.539s); static/logging and non-CGO build pass. Spread/panic/cancellation/fault expansion and exact upstream global configuration parity still need work. Parallel replay/full output and final refreshed race/security/profiling/crosswalk remain unfinished. Logs are disposed after use.

## Custom-global alias isolation, panic and joins

Custom callbacks now receive detached arguments rather than an alias into the in-memory issued journal. A real-WASM spread callback mutates its received object; stored arguments remain unchanged, panic text is hidden, and reopen reuses both value/error deliveries with one callback and one panic. A cancellation test holds the admitted callback after context cancellation; Harness.Close cannot finish until release, and neither a final result nor late store writes settle.

Spread/panic/reopen/cancellation race tests count3 pass17.351s. Full root normal suite passes (durable61.546s/tools5.405s); non-CGO build and diff checks pass. Static/logging passed before the final cancellation-test-only addition. Runtime argument-isolation changed, so the prior full race receipt remains stale. Global callback side-effect settlement fault policy/exact upstream crosswalk, full parallel/output and final security/profiling acceptance remain incomplete. Logs are disposed after use.

## Custom-global unconfirmed delivery policy

A real-WASM rejected-delivery fault test confirms custom-global callbacks are at least once: after callback completion but unconfirmed delivery, reopen invokes the callback again (count2), commits one delivery and settles no earlier result/store patch. Confirmed deliveries continue to skip callback execution. The exported callback documentation now requires pure or externally idempotent callbacks and directs durably settled effects to nested tools. Versioning/panic containment alone cannot provide exactly-once external effects.

Native backend fault tests race count3 pass18.374s; static/logging checks pass. Runtime code is unchanged except documentation. This receipt verifies native recovery behaviour; exact upstream parallel/global boundary crosswalk remains incomplete. Full parallel/output and final refreshed race/security/profiling/crosswalk gates are unfinished. Logs are disposed after analysis.

## Return/error item ordering

Return values and failure descriptions now join the text-item list before upstream-style numbering and console grouping. Returns count toward `text N/M`; console output is last. Updated real-VM mixed output, budget and unsettled-reopen assertions preserve the original effect/replay checks. Host identity is17.

Output/failure/reopen race tests count3 pass31.475s. Full root normal suite passes (durable63.435s/tools4.644s); static/logging checks and non-CGO build pass. Exact JS JSON formatting, status/wall-time headers, images/spill, full parallel protocol and final refreshed race/security/profiling/crosswalk remain incomplete. Logs are disposed after use.

## Guest JSON order and numeric preservation

Return text now indents the guest's JSON directly, preserving object insertion order, numeric spelling/precision and HTML/Unicode strings. String returns still decode directly. Native return/store values use json.Number decoding to avoid a float64 round trip before durable validation. Real-VM tests cover out-of-order keys,9007199254740992,1e21,HTML and emoji; clock/reopen tests retain numerical/effect assertions. Host identity is18.

Focused output/clock/reopen race tests count3 pass26.197s. Full root normal suite passes (durable62.838s/tools4.517s); static/logging checks and non-CGO build pass. Direct guest JSON formatting is tested for these cases; complete upstream rendering/status/images/spill and parallel replay remain unfinished. Final refreshed full race/security/profiling/crosswalk work is incomplete. Logs are disposed after use.

## Exact guest numeric arguments

Tool/global argument decoding now uses json.Number before durable journal admission, avoiding float64 re-encoding that changes guest decimal values. Discovery result decoding follows the same rule. A real-WASM tool/custom-global test checks guest JSON decimal1000000000000000100 in both host callbacks, closes before final settlement and reopens with one tool and one global execution. The contract preserves serialized guest JSON numbers; JavaScript computation still uses its native number semantics. Host identity is19.

Numeric/reopen race tests count3 pass9.609s. Full root normal suite passes (durable72.012s/tools4.918s); static/logging checks and non-CGO build pass. Full race receipt predates recent runtime changes. Parallel protocol/full output and final security/profiling/crosswalk remain unfinished. Logs are disposed after use.

## Intrinsic and ambient global admission fence

Host custom globals now reject intrinsic/ambient roots such as Date, Math, Promise, globalThis, fetch and prototype-related names at Harness Open and internal engine setup. Native host injection cannot silently replace deterministic/frozen built-ins or reintroduce those ambient API names. The list is an explicit native admission policy; future guest intrinsic changes need audit. Host identity is20.

Reserved-name/ambient-isolation and custom-global race suites count3 pass7.618s/40.378s. Full root normal suite passes (durable70.526s/tools4.742s); static/logging checks and non-CGO build pass. Arbitrary approved host callbacks still carry explicit capabilities and must honour cancellation/idempotency. Parallel/full output and refreshed race/security/profiling/exact crosswalk remain unfinished. Logs are disposed after use.

## Persisted image output candidate

Codemode results now retain separate bounded image blocks with permitted inline MIME/base64 validation at admission and stored-journal validation. Concrete QuickJS image output no longer becomes an unsupported error; image blocks survive a zero text budget and settled reuse returns detached copies. Native image blocks remain separate from text ordering; environment-backed image file labels and model transcript/tool integration are not implemented. Format/pixel decoding is exercised by real PNG fixtures but generic result validation checks MIME/base64 only. Host identity is21.

Real-WASM tests decode a2×1 PNG, check dimensions/red channel without generated hashes, mutate the returned block and verify settled reuse remains intact. Invalid block type/MIME/base64 reject. Race count3 passes8.520s. Full root normal suite passes (durable64.972s/tools4.506s); static/logging and non-CGO build pass. Full parallel/output files/spill/status and final refreshed race/security/profile/crosswalk remain unfinished. Logs are disposed after use.

## Failed-image output unsettled reopen

A real-WASM test emits an image after one nested tool effect, stages a store write and throws, then shuts down before result settlement. Reopen replays the tool delivery, retains the emitted image (decoded1×1 blue pixel), discards failed store writes and completes with effect1/VM starts2. Combined failed-image/reuse race tests count3 pass15.643s; static/logging checks pass. Runtime code is unchanged. Image file labels/transcript integration, full parallel/output spill/status and final refreshed race/security/profiling/crosswalk work remain unfinished. Logs are disposed after use.

## Explicit environment output writer candidate

Added optional `CodemodeOutputWriter` environment capability keyed by task/run/fixed filename. Hosts must confine paths to owned roots, overwrite idempotently and own cleanup; no ambient filesystem writes or automatic temp allocation were added. Truncated text can be saved as output.txt and its path persisted. Images can receive capability-written file labels with duplicate data sharing one write. Writer panic/error details are hidden from output; write failures do not discard script results. Current projection still keeps a bounded prefix, not upstream head/tail/line-margin rendering. No default local filesystem implementation exists yet.

Real-WASM spill tests verify complete text/path, bounded result and no rewrite on settled reuse. Panic containment and existing image/text tests pass race count3 in24.797s. Full root normal suite passes (durable64.636s/tools4.635s); static/logging and non-CGO build pass. Writer cancellation/reopen/fault/image-file tests and whole-result pre-write validation need expansion. Parallel replay/transcript/status output and final refreshed race/security/profiling/crosswalk remain unfinished. Logs are disposed after use.

## Output pre-write validation and cancellation joins

Before environment output writes, native results now detach/validate all persisted fields with projected text and discard sandbox-provided fullOutputPath. Only a validated writer grants persisted paths. Original text remains available for spill; cancellation is checked before each image/write and after writer return. A cancelled writer cannot settle the result/store patch, and Harness.Close joins its actual callback. Host identity is23.

Tests reject conflicting store patches before any writer call and hold a cancelled real-VM spill writer until release, inspecting no settled result/late store afterward. Output/prewrite race tests count3 pass15.655s. Full root normal suite passes (durable70.026s/tools4.538s); static/logging and non-CGO build pass. Full original-text byte budgeting, writer failure/image dedup/reopen/default filesystem, upstream head-tail rendering and parallel protocol still need work. Final refreshed race/security/profile/crosswalk remain incomplete; logs are disposed after use.

## Full-text cap and image writer failures

Full text now rejects invalid UTF8 or more than4MiB before projection/spill, including host-supplied sandbox results. Real-WASM image writer tests emit one image twice, require one write/two labels, decode saved bytes, and verify settled reuse performs no write. Writer failure hides private errors, retains both image blocks and still adopts a successful store patch. Host identity is24.

Image-writer/full-text/cancellation race tests count3 pass16.585s. Full root normal suite passes (durable63.752s/tools4.565s); static/logging and non-CGO build pass. The4MiB cap is a native resource contract. Stable writer overwrite after unsettled reopen/default filesystem, head-tail/transcript/status and full parallel protocol still need work. Final refreshed race/security/profile/crosswalk remains incomplete. Logs are disposed after use.

## Stable output writer address after rejected settlement

A real-WASM fault test writes output.txt then rejects final result/store settlement through base/delta writes. Before reopen, neither result nor store patch is adopted. Reopen writes again with exactly the same task ID/run key/filename/content; the host capability can overwrite idempotently, and final settlement succeeds. External writes are at least once and are not rolled back by storage rejection. Native backends pass race count3 in8.820s; static/logging checks pass. Runtime code is unchanged. Default filesystem implementation, head-tail/transcript/status/full parallel and final refreshed race/security/profiling/crosswalk remain incomplete. Logs are disposed after use.

## Native bounded head/tail projection

Long text now retains Unicode-safe head/tail context with an in-budget truncation marker; small budgets retain the prefix contract. Saved-output path hints appear only when they fit with useful body text, and FullOutputPath remains separately available. Complete text still reaches the output writer. This native strict-budget projection differs from upstream warning/token-count/line-margin formatting; exact rendering parity is unfinished. Host identity is25.

Projection/output/writer race tests count3 pass37.127s. A short projection fuzz run passes31986executions. Full root normal suite passes (durable65.025s/tools4.533s); static/logging and non-CGO build pass. Default filesystem/transcript/status/full parallel and final refreshed race/security/profile/crosswalk remain incomplete. Generated fuzz cache and logs are disposed after analysis.

## Label-aware spill boundary

Image labels are now included before truncation/spill selection. Labels alone can trigger output.txt creation; the saved full text includes the same labels/body being projected. Combined labelled text is checked against the native full-output cap. A real-WASM one-token test emits short text plus an image, verifies two writes (image/full text), complete labelled spill and no rewrite on settled reuse. Host identity is26.

Focused label/image/spill race tests count3 pass23.355s. Full root normal suite passes (durable66.760s/tools5.122s); static/logging and non-CGO build pass. Original output-item ordering of image labels, default filesystem/transcript/status/full parallel and final refreshed race/security/profiling/crosswalk remain incomplete. Logs are disposed after use.

## Settled result union validation

Codemode result validation now rejects values without HasValue and successful-return presence alongside errors, while preserving explicit null. The same checks run before output writes and against stored final candidates. Concrete adapter errors clear return presence instead of producing contradictory DTOs; emitted images and failed store patch handling retain their contracts. Host identity is27.

Union/failure/output/image-reopen race tests count3 pass29.281s. Full root normal suite passes (durable76.752s/tools5.612s); static/logging and non-CGO build pass. Full parallel/output integration and final refreshed race/security/profiling/crosswalk remain incomplete. Logs are disposed after use.

## Integrated codemode profile and interrupted race refresh

A fresh full-root race command terminated without an exit code after recording only the root-package pass. No owning test process remained on inspection. This is incomplete verification, not a passing full-race receipt; the historical full race still predates current runtime changes.

Go1.27.1/GOMAXPROCS2 integrated `TestCodemode120` CPU/heap profiling passes12.676s. CPU samples12.13s: sandbox/engine construction56.39% cumulative, wazero compilation56.31%, register allocation24.98%. Alloc_space totals1795.47MB, of which compilation1042.61MB/58.07%; engine executions453.09MB/25.24%. Alloc_objects totals7890116; reflect.unsafe_New41.59% flat, durable encodeBounded27.92% cumulative, validateState27.90%, cloneJSONValue10.14%. The workload includes isolated engine construction in many tests and all codemode replay/fault/VM/output paths; these totals are not per-production-run estimates.

No measured optimisation is claimed. Production sandboxes already share compiled code across executions. Test-engine sharing would change workload/isolation; removing defensive JSON copies or state validation would weaken contracts. Whole-port profiling still needs completion. Raw CPU/heap captures, matching binary/test log and interrupted race log are deleted immediately after analysis. Full parallel/output/default filesystem/transcript and final security/crosswalk acceptance remain incomplete.

## Pinned codemode test crosswalk scaffold

Added `codemode-test-inventory.json` with149 anchored single-line test declarations in eight pinned upstream files, source line/name verified against the target checkout. All per-declaration dispositions remain unreviewed. `codemode-crosswalk.md` maps each file to native coverage and specific missing contracts: parallel event/ack/divergence, model capacity, raw tool/prompt integration, declarations refs/constraints, fingerprint/rejection properties, output formatting and portable host paths. Counts are source enumeration, not executed suites or parity evidence.

This documentation batch changes no runtime. Full-root AI/durable path dispositions and per-test acceptance still need completion. Security, refreshed race and whole-port profiling remain open.

## Source parser per-test crosswalk

Reviewed all five pinned upstream source-parser declarations and transcribed their acceptance/rejection cases into native tests. Plain source/comment handling, CRLF first-line options/line preservation, false directive markers, misplaced directives and all listed invalid inputs pass. Misplaced directives now identify the source line. Race count3 with existing directive/numeric tests passes1.148s; static/logging checks pass.

Inventory marks these five declarations as native case coverage with explicit adaptations: resolved defaults instead of sparse options, durable rejection type/most wording, stricter duplicate/numeric-work bounds. The other144codemode declarations remain unreviewed. No complete parser/whole-port parity claim. Logs are disposed after use.

## Structural declaration expansion

Native declaration types now render const literals, type-array nullable unions, anyOf/oneOf unions, allOf intersections with union parentheses, boolean property schemas, empty closed objects and schema-valued additional properties. These descriptions grant no runtime validation/execution rights. Structural/catalogue/real-discovery race tests count3 pass9.466s; static/logging checks pass. Full root normal suite passes (durable74.967s/tools5.961s); non-CGO build passes. Host identity is28 because discovery descriptions changed.

Tuples, constraints/comments, local/recursive refs and full declaration size limits remain incomplete. No upstream declaration test is marked fully accepted solely from helper coverage. Full parallel/output and final refreshed race/security/profiling/crosswalk remain unfinished. Logs are disposed after use.

## Array and tuple declaration slice

Native declarations now match pinned array renderer precedence: non-array items yields Array<T>, otherwise prefixItems/legacy array items yields a fixed tuple; absent entries yield unknown[]. Boolean item schemas and object/array type inference from structural keywords are supported. Upstream renderer does not express optional/open tuple constraints here. Runtime schema validation is unchanged. Host identity is29.

Pinned tuple/structural/discovery race tests count3 pass8.167s. Full root normal suite passes (durable79.381s/tools6.627s); static/logging and non-CGO build pass. Refs, constraint comments and declaration budgets remain incomplete; helper slices do not grant full upstream-test acceptance. Parallel/output and final refreshed race/security/profiling/crosswalk are unfinished. Logs are disposed after use.

## Local declaration references candidate

Local JSON-pointer declaration refs now resolve against the supplied root schema, including $defs/definitions and escaped pointer keys. Recursive/unresolved/remote refs stay unknown; no asset fetch occurs. Expansion uses deterministic sorted traversal, depth16 and2048node budgets. Runtime schema validation remains unchanged. Host identity is30.

Reference/structural/tuple/discovery race tests count3 pass7.299s before the final sorted-traversal adjustment. Full root normal suite passes after it (durable64.915s/tools4.629s); static/logging and non-CGO build pass before that adjustment. Constraint comments/character budgets and full per-test declaration acceptance remain incomplete. Changes after checkpoint b0002f2 are uncommitted; no automatic publication. Parallel/output/final refreshed race/security/profiling/crosswalk remain unfinished. Logs are disposed after use.

## Declaration character and input budgets

Declaration rendering now defaults to16000UTF16characters, with a lower-only internal budget. Oversized output becomes unknown; pre-render checks bound schema nodes4096, literal/key bytes65536 and host depth32, while reference expansion retains depth16/node2048 limits. Tests exercise the pinned50property/100character case, oversize constants and cyclic host schema inputs. These native input budgets are stricter than upstream; constraint-tag drop/retry is not implemented yet. Runtime schema validation is unchanged. Host identity is31.

Budget/ref/structural/discovery race tests count3 pass7.227s. Full root normal suite passes (durable65.017s/tools4.514s); static/logging and non-CGO build pass. Comments/constraint fallback/per-declaration acceptance/full parallel/output and final refreshed race/security/profiling/crosswalk remain unfinished. Post-checkpoint changes are uncommitted; logs are disposed after use.

## Declaration comments checkpoint and user pause

Property descriptions and direct constraint annotations now render as line comments, including integer, bounds, string/array/object constraints, default and deprecated tags. Line separators are escaped in tags; tags above80UTF16characters are omitted. Named objects no longer receive an implicit additional-property index signature unless requested. Oversized annotated declarations retry after dropping constraint lines. Nullable-branch constraint merging, draft-04 exclusive-bound normalisation and exact fallback rendering still need work. Host identity is32.

Focused declaration/reference/discovery race tests count3 pass7.333s. Static/logging checks and non-CGO build pass. The initial full suite exposed stale integer-annotation expectations in the tuple fixture; corrected full root suite passes (durable67.927s/tools4.590s, Go1.27.1/GOMAXPROCS2). No tests or numerical contracts were removed.

Rui authorised committing/pushing this checkpoint and explicitly pausing work. Remaining parity/security/profiling/race/crosswalk items are unfinished; resume only on a new user instruction. Diagnostic logs are disposed after use.

## Resumed nullable constraints and recovery-test limitation

Resumed at Rui's instruction after checkpoint89fe371. Constraint annotations now combine a sole non-null anyOf/oneOf branch with property constraints, choose tighter numeric bounds, recognise integer type arrays and normalise draft-04 boolean exclusive bounds. Runtime schema validation is unchanged; discovery host identity is33. Focused nullable/declaration/discovery race tests count3 pass9.062s; vet/staticcheck/logging and non-CGO build pass.

The full shuffled root suite failed `TestTaskRecoveryToolDecidingHoldRejectedUncertainAndReopen/memory/rejected-fallback-adopts`: confirmed receipt expected known-prefix, but recovered interrupted receipt. Seed1792007779664605178. Focused lifecycle race count5 passes40.309s; rerunning the entire durable package with that seed passes80.179s. The failure is not reproduced or fixed and remains an acceptance limitation; assertions/effect counts were not weakened. Latest batch therefore has no passing full-root receipt. Diagnostic logs are disposed after recording the failure/seed. Parallel/output and refreshed full race/security/profiling/crosswalk remain incomplete. Current changes are uncommitted; no new publication authority.

## Recovery fixture synchronisation

The deciding-hold fixture asserted known-prefix recovery without waiting for coalesced Output to commit. Added the existing Details(nil) progress-flush barrier before settlement fault injection; receipt/effect/usage assertions remain intact. Focused race count10 passes88.059s.

The next same-seed full run exposed an independent background-cascade fixture race: waiting for the child (already cancelled by its background owner) did not establish that the newly launched ancestor abort had committed. Added an actual parent-abort-handler admission signal before mark inspection; ownership/background joins and outcome assertions are unchanged. Focused race count20 passes4.219s. Full root normal suite with original failure seed1792007779664605178 now passes (durable88.159s/tools6.390s). Vet/staticcheck/logging pass. These are test synchronisation fixes, not runtime weakening; long-running full race refresh remains incomplete. Current declaration changes and fixtures remain uncommitted.

## Schema-driven annotation fallback

Oversized declaration fallback now rerenders the expanded schema with constraint annotations disabled. It no longer deletes lines from rendered text or compacts literal/prose whitespace. Descriptions beginning with @ and strings containing repeated spaces remain intact; output still becomes unknown if descriptions/types cannot fit. Host identity is34.

Declaration/nullable/discovery race tests count3 pass8.339s. Fresh shuffled full root normal suite passes (durable88.812s/tools6.416s); static/logging and non-CGO build pass. Constraint tag grouping/line-separator spelling and full per-declaration crosswalk still need work. Parallel/output/final refreshed race/security/profile remain incomplete. Changes are uncommitted; diagnostic logs disposed after use.

## Pinned constraint-tag projection

Constraint tags now group on one comment line, and CR/LF/U+2028/U+2029 use upstream-style Unicode escape spellings. Tags longer than80UTF16characters are dropped before escaping; descriptions remain. Pinned grouping/separator/long-tag tests and nullable/ref/fallback/discovery race count3 pass8.160s. Full root normal suite passes (durable75.269s/tools6.064s); static/logging and non-CGO build pass. Host identity is35. Complete declaration test dispositions, parallel/output and final refreshed race/security/profile remain unfinished. Changes are uncommitted; logs disposed after use.

## Exact primitive/object/description declarations crosswalk

Transcribed all expectations from the first three pinned declaration tests. This exposed union rendering with an unknown branch; it now collapses to unknown as upstream does. Exact cases include primitives/literals/unions/intersections, sorted required/quoted properties, open/closed objects and nested descriptive comments. Host identity is36.

Focused renderer/discovery race tests count3 pass9.134s; full root normal suite passes (durable83.169s/tools6.060s); static/logging and non-CGO build pass. Inventory now records8reviewed source/declaration tests and141unreviewed declarations, with native bounded/API differences explicit. Complete remaining declaration/prompt/global/test crosswalk, parallel/output and final race/security/profile remain unfinished. Changes uncommitted; logs disposed after use.

## Exact pinned annotation crosswalk

Transcribed complete expected strings for five upstream annotation/fallback declarations: direct constraints/defaults, nullable tighter bounds, draft-04 exclusivity, long-tag omission/line separators and budget fallback. This fixed bound-tag ordering to minimum/exclusiveMinimum/maximum/exclusiveMaximum. Host identity is37. Annotation/fallback/discovery race count3 passes8.981s; full root normal passes (durable67.355s/tools4.541s); static/logging/non-CGO pass.

Inventory now records13reviewed source/declaration tests and136unreviewed. API/resource differences remain explicit; complete declaration APIs, parallel/output and final refreshed race/security/profiling/crosswalk are unfinished. Changes uncommitted; logs disposed after use.

## Schema declaration test dispositions

All eleven pinned schemaToType test declarations now have native exact cases, including the recursive Item/local legacy enum/remote ref fixture. Reference/tuple/character budget race count3 passes1.205s; static/logging checks pass. Runtime code is unchanged in this batch.

Inventory records16covered source/schema declarations,6explicit declaration-API gaps and127unreviewed. The six gaps are tool signatures, MCP-shaped output signatures, per-tool samples, tool/global blocks, namespaced/explicit globals and description comment escaping. Native tool sections are not equivalent to those APIs. No full renderer/parity acceptance. Parallel/output/final refreshed race/security/profile/crosswalk remain incomplete; changes uncommitted and logs disposed after use.

## Tool signature/sample APIs

Added RenderCodemodeToolSignature/RenderCodemodeToolSample with pinned normalised identifiers, unknown absent schemas, plain MCP-shaped output types and exact per-tool sample framing. describeTool now calls the sample renderer; catalogue section formatting is unchanged. Host identity is38.

Pinned API/discovery race tests count3 pass7.804s. Full root normal passes (durable80.059s/tools5.833s); static/logging/non-CGO pass. Inventory now has19covered tests,3explicit global declaration-block gaps and127unreviewed. Global descriptions/signatures/block rendering, parallel/output and final race/security/profile/crosswalk remain unfinished. Changes uncommitted; logs disposed after use.

## Tool/global declaration blocks

Added RenderCodemodeDeclarations and descriptive global metadata for ordered tools, standalone globals, namespace groups and explicit signatures. Description comments escape terminators; schemas use native bounds. Exact pinned tool/global/namespaced/signature/comment cases pass race count3 in1.178s. Full root normal passes (durable81.497s/tools5.478s); static/logging/non-CGO pass. Runtime guest semantics unchanged.

All17declaration test cases plus5source cases now have native coverage/adaptations;127other declarations remain unreviewed. These helper APIs are not yet integrated with executable custom-global metadata or model prompt assembly. Full parallel/output/final race/security/profile/crosswalk remain incomplete. Changes uncommitted; logs disposed after use.

## Executable global declaration metadata

Custom global configuration now carries optional description/signature/input/output schema metadata. Open validates and deeply copies schemas; immutable run starts persist detached metadata with callback identity. CodemodeStart.CodemodeDeclarations renders that snapshot without live-registry access. Metadata does not validate arguments or grant additional capability; explicit signatures remain descriptive.

Tests mutate caller schemas/config after Open and VM metadata after Start; later runs retain original descriptions/schema types. Metadata/custom replay/declaration race count3 passes11.952s. Full root normal passes (durable82.460s/tools6.006s); static/logging/non-CGO pass. Model prompt/raw codemode tool installation and full builtin-global declarations remain unfinished, along with parallel/output/final race/security/profile/crosswalk. Verified increment committed/pushed under Rui's ongoing authority; logs disposed after use.

## Builtin global snapshot declarations

Snapshot declarations now include discovery signatures and opt-in model catalogue/paid signatures alongside custom metadata. Opt-out excludes the models declaration namespace; internal paid-tool names remain hidden. Model DTO/context signatures intentionally use unknown for incompletely modelled structures and are not exact upstream model type declarations. Tests cover builtin presence, opt-out and custom namespace retention with actual paid-opt-out coverage.

Focused snapshot/metadata/opt-out race count3 passes8.194s. Full root normal passes (durable74.066s/tools4.596s); static/logging/non-CGO pass. Raw codemode tool/prompt assembly, full model declaration shapes, parallel/output and final race/security/profile/crosswalk remain unfinished. Increment committed/pushed under ongoing authority; logs disposed after use.

## Registered constrained tool metadata prerequisite

Durable registered tools now accept bounded json_schema/grammar constrained-sampling metadata, persist it in offers and propagate detached copies into model protocol tools. Strictness/types/variant names/empty or oversized sources reject. Model contribution controls still reject constrained tool additions; this slice covers registry-owned tools only. The raw grammar-backed codemode registration is not installed yet.

Registration/DTO/protocol-copy tests pass race count3 in1.146s; full root normal passes (durable79.622s/tools6.503s); static/logging/non-CGO pass. Production HTTP request/persisted-intent/reopen tests need expansion before metadata acceptance. Increment committed/pushed under ongoing authority; logs disposed after use. Parallel/output/final race/security/profile/crosswalk remain unfinished.

## Constrained tool production request and contribution records

Actual durable/OpenAI HTTP testing exposed rejection in system tool-contribution records. Contributions now persist bounded constrained-sampling metadata and compare grammar/strictness changes during prompt planning. Registry offers reach the production HTTP custom-tool grammar payload and persisted generation checkpoint; malformed empty grammar remains rejected. Request inspection follows the existing provider's top-level custom format shape.

Native backend wire/intent race tests count3 pass2.534s; full root normal passes (durable84.310s/tools6.337s); static/logging/non-CGO pass. Initial fixture used nonexistent compatibility fields and an incorrect nested custom-payload shape, corrected before passing evidence. Constrained contribution copy/reopen/intent retry tests and grammar-backed codemode installation remain unfinished. Parallel/output/final race/security/profile/crosswalk remain incomplete. Increment committed/pushed; logs disposed after use.

## Constraint contribution reopen/projection isolation

Receipt-to-model projection now deeply copies constrained grammar maps. Native backend tests persist a typed system contribution, mutate caller input, reopen and mutate the projected tool; persisted metadata stays unchanged. Prompt planning emits remove/add declarations for grammar changes. Combined reopen/planning/HTTP race count3 passes2.381s; full root normal passes (durable77.679s/tools6.011s); static/logging/non-CGO pass. Fixture API/root/storage-field mistakes were corrected before passing evidence.

Raw codemode tool/prompt installation, intent retry and parallel/output/final race/security/profile/crosswalk remain unfinished. Increment committed/pushed; logs disposed after use.

## Model-facing codemode extension candidate

Added opt-in NewCodemodeExtension with model-only replay-safe registration, pinned1.2.0 Lark source grammar and code-string schema. Host supplies/owns the sandbox. Execute uses the native run journal and projects text/image blocks into a standard tool result. Real model-generation tests offer the grammar tool, call concrete QuickJS, execute one nested tool and return one script transcript result. Initial duplicate Content+Blocks projection was corrected; tests require exactly leaf text.

Native backend real-model/VM race count3 passes13.292s; full root normal passes (durable76.742s/tools5.657s); static/logging/non-CGO pass. This is a serial candidate: description warns overlapping calls reject; no prompt section yet. Default tool output cap remains50KiB rather than upstream4MiB/100000lines; expanded safe output-cap policy still needs implementation. Paid/global host configuration is not extension-scoped. Full raw grammar stream/provider/model retry/reopen, prompt/parallel/output and final race/security/profile/crosswalk remain unfinished. Increment committed/pushed; logs disposed after use.

## Codemode prompt integration

The extension now renders a persisted `codemode` prompt section from the execution runtime's callable tools, namespaces and global declarations. Model-only tools are excluded. Deselecting the model-facing codemode tool removes its section. Native memory/journal tests inspect real generation requests, reopen storage, verify unchanged prompts do not create another system entry, and verify deselection removes the persisted section.

Combined model-tool and prompt race tests, count 3, pass in 32.525s. The full normal root suite passes (durable 86.427s, tools 6.192s); vet, staticcheck, logging checks, non-CGO build and diff checks pass. Diagnostic logs were removed after analysis. Raw-stream integration, output-limit parity, parallel replay, declaration audit and final security/profiling acceptance are unfinished.

## OpenAI completions raw grammar execution and reopen

Streamed custom input now maps to the grammar schema's sole required string property (`code` for codemode). Assistant replay emits custom calls with raw input when grammar support is enabled. Invalid grammar schemas and non-string replay inputs fail before HTTP; empty custom input produces valid JSON deltas. The earlier fixed `input` mapping and function-call replay prevented real codemode execution.

Memory/journal HTTP tests split raw JavaScript across SSE chunks, execute concrete QuickJS and one nested effect per submission, inspect the outgoing custom-call/result transcript, verify persisted source, reopen storage and run a second submission. This covers settled reopen; interrupted HTTP retry/recovery and other provider APIs need separate verification. The grammar wire fixture now declares its required string property; the HTTP assertion accounts for provider trimming of grammar whitespace.

OpenAI package race count 3 passes in 3.583s; durable HTTP/VM/reopen race count 3 passes in 32.939s. Full normal root tests pass (durable 88.529s, tools 6.292s), with static/logging/non-CGO and diff checks. Diagnostic logs were disposed after analysis. Parallel execution, output-limit parity, remaining declarations and final acceptance are unfinished.

## Responses raw grammar stream integration

Responses streaming now uses the existing grammar tool view to map custom input to the schema's required string property. Custom-tool deltas form JSON argument fragments, including final closure and empty input, with monotonic-input validation. Tests cover split, done-only, item-only and empty inputs plus non-monotonic rejection.

The native HTTP/QuickJS/reopen integration now runs both Completions and Responses across memory and journal storage, inspecting custom-call/output replay and nested effect counts. The Responses fixture initially lacked the provider registration import; adding it resolved `missing_terminal_message` without changing generation settlement.

Responses package race count 3 passes in 3.170s; combined two-API durable integration race count 3 passes in 68.196s. Full normal root tests pass (durable 92.141s, tools 6.074s), with static/logging/non-CGO and diff checks. Diagnostic logs were disposed after analysis. Interrupted recovery, output limits, parallel replay, remaining declarations and final acceptance are unfinished.

## Interrupted model-facing codemode settlement

A new memory/journal test pauses the concrete VM after its final result, with one confirmed nested delivery and before codemode settlement. It closes the harness, reopens the same persisted submission and resumes. The script runs twice, the nested external effect runs once, and the recovered conversation contains one codemode call and one result. This tests the model-facing tool and submission lifecycle; interrupted HTTP transport recovery still needs separate coverage.

Focused race count 3 passes in 19.476s. Full normal root tests pass (durable 89.908s, tools 5.983s), with static/logging/non-CGO and diff checks. Diagnostic logs were disposed after analysis.

Output-limit parity requires a storage design change: default journal strings are capped at 256 KiB, the configurable string ceiling is 512 KiB, and records are capped at 1 MiB. Enabling a 4 MiB outer tool limit alone would conflict with those bounds. Ordinary tool limits and persisted storage ceilings remain unchanged. Parallel replay, declaration review and final acceptance are unfinished.

## Deterministic declaration review

Reviewed all 16 pinned `deterministic.test.ts` declarations and recorded partial native mappings or explicit gaps in the inventory. Together with 22 source/renderer declarations, 38 of 149 declarations are reviewed; 111 remain unreviewed. Reviewed cases include missing parallel ordering/idle/watermark, rejection properties and ordinary real-clock execution semantics; review does not establish parity.

New concrete VM tests check pinned seed 1/seed 0 random values with 1e-15 absolute tolerance, independence from start time, UTC offset and absent GC intrinsics. The delivery-clock test exposed backward time assignment in the direct sandbox adapter. Deliveries now clamp to the previous clock; replay-visible identity advances to host39. Focused deterministic/clock/recovery/identity race count 3 passes in 34.914s. Full normal root tests pass (durable 85.681s, tools 5.773s), with static/logging/non-CGO and diff checks. Diagnostic logs were disposed after analysis.

## Elapsed clock and reservation rollback

WASI monotonic time now reports elapsed delivery-clock time from `StartedAt`, so `performance.now()` advances with `Date.now()`. Nanosecond overflow saturates instead of wrapping. Clock semantics advance replay identity to host40. Engine clock race count 3 passes in 18.350s; deterministic/clock/recovery/identity race count 3 passes in 35.995s.

The full shuffled run exposed a reservation-rejection waiter timeout, reproduced in a focused race run. Provisional reservation removes tickets; rollback previously depended on mutable claims when notifying bound waiters. Reservation now captures original ticket owners and signals them on rollback. The existing test retains the strict barrier and adds effect/attempt diagnostics. Focused reservation race count 100 passes in 13.271s; reservation/reconcile/clock/model-recovery race count 3 passes in 24.381s. The previously failing full-suite seed `1792016869742943826` passes (durable 72.252s, tools 4.592s). Static/logging/non-CGO and diff checks pass. These are focused race receipts, not a fresh full race pass. Diagnostic logs were disposed after analysis.

## Portable review and full root race refresh

Reviewed all seven pinned portable declarations: native wall deadlines, heap limits and cancellation joining have partial mappings; deterministic compute budgets, remote execution/exchange batching and server override contracts are missing. The inventory now has 45/149 reviewed declarations and 104 unreviewed. No implementation parity follows from this review.

At revision `6b4422f`, the initial full root race run reached the 300s package-wide timeout during a test that had run for three seconds. A durable-only rerun at the same seed passed in 465.782s. The complete root race suite then passed with `-shuffle=1792017546493000040 -count=1 -timeout=900s` (durable 461.621s, tools 29.249s, QuickJS 31.043s). The first timeout is not a passing receipt; the final complete invocation supplies the fresh root race result. Raw diagnostic logs were removed after analysis. Nested-module consumer gates, whole-port profiling, security approval, output storage, parallel replay and remaining declaration review are unfinished.

## Nested tool error outcomes

Pinned `codemode.ts` intentionally resolves defined structured output even when `isError` is true. Native tests now preserve that rule for objects and explicit null. Schema-less failures previously acquired an implicit structured text value, causing codemode to resolve them. They now retain the error union; nested integrity validation checks that union. Error text falls back to error-severity diagnostics, then `Tool <name> failed`, matching the pinned outcome conversion. Replay identity advances to host41.

Concrete VM tests cover text precedence, diagnostics, empty failures, structured objects/null and settled reuse with one nested effect. Focused tool-error/nested race count 3 passes in 31.601s. Full normal root tests pass (durable 71.196s, tools 4.589s), with static/logging/non-CGO and diff checks. Initial fixtures used an unsupported diagnostic severity and omitted structured schemas; both were corrected before passing evidence. Diagnostic logs were removed after analysis. The prior full-race receipt predates this change; final acceptance is unfinished.

## Description cases and deferred prompt tools

Reviewed all five pinned durable description declarations. Native tests cover standalone-first namespace grouping, longest prefix, budget omission markers and empty catalogues. Prompt integration now omits deferred tools inline while retaining them in the executable/discovery catalogue. Deferred state is captured in tool-description snapshots. Exact upstream description and extension options remain partial; inventory review is now 50/149, with 99 unreviewed.

Prompt/grouping/catalogue race count 3 passes in 15.807s. Full normal root tests pass (durable 73.111s, tools 4.609s), with static/logging/non-CGO and diff checks. The initial helper fixture omitted identifiers; it was corrected before passing evidence. Diagnostic logs were removed after analysis. Final acceptance remains unfinished.

## Extension inline budget

`NewCodemodeExtension` accepts one optional `CodemodeExtensionOptions` value. A nil `InlineBudget` uses 3000 estimated tokens; explicit zero omits inline tool declarations. The constructor captures the integer, so later caller mutation cannot change prompt rendering. Negative budgets and multiple option sets fail. Namespaces, globals and paid-model authority remain harness-scoped.

Concrete model/VM tests verify zero-budget prompt omission while `searchTools` still finds and executes a tool. Inline-budget/prompt/grouping race count 3 passes in 30.538s. Full normal root tests pass (durable 71.162s, tools 4.581s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Complete upstream extension configuration and final acceptance are unfinished.

## Generated-but-unshown images and paid-model review

Codemode output now includes the pinned notice when confirmed `models.generateImages` deliveries contain images and the script displayed none. Counting persisted deliveries avoids redispatching paid calls on replay. Any displayed image suppresses the notice, matching upstream's rule; this is not a per-image omission count. Replay identity advances to host42.

Concrete paid-image/VM tests verify the notice and settled reuse with one provider effect; helper tests cover plural wording and shown-image suppression. Paid-image/notice race count 3 passes in 27.484s. Full normal root tests pass (durable 88.614s, tools 6.057s), with static/logging/non-CGO and diff checks. Initial fixtures referenced the wrong image API; they were corrected to the repository's images provider/catalog before passing evidence. Diagnostic logs were disposed after analysis.

All seven paid-model declarations now have explicit mappings or gaps: 57/149 declarations reviewed, 92 unreviewed. Four-call capacity and queued-call completion fencing require parallel execution; dedicated classifier-error/context and displayed-image writer counterparts need expansion. Final acceptance is unfinished.

## Shown paid-image writer integration

Expanded the paid-image test to use a valid generated PNG and a confined environment writer. The concrete VM passes the provider's output to `image(block)`, suppresses the unshown notice, preserves one image and its saved path, and reuses the settled run without another provider call or image write. Saved bytes decode as a one-pixel PNG. Initial fixtures used nonexistent writer/helper fields; they were corrected to `Options.Env` and the existing writer API.

Generated-image race count 3 passes in 13.484s. Full normal root tests pass (durable 84.163s, tools 5.957s), with static/logging/non-CGO and diff checks. Diagnostic logs were disposed after analysis. Declaration review remains 57/149; this expands an existing partial mapping. Final acceptance is unfinished.

## Classifier failures and malformed contexts

Concrete VM tests now cover a classifier provider returning `stopReason: error` and `errorMessage: quota exceeded`: the script receives the structured result, and settled reuse makes one provider request. Array state, empty questions and unknown question types reject with zero classifier requests. Native diagnostic wording differs from upstream's `context.state`/`Expected context` text.

Focused classifier race count 3 passes in 18.979s. Full normal root tests pass (durable 79.474s, tools 6.008s), with static/logging/non-CGO and diff checks. Fixtures were corrected to the existing typed catalogue and classifier registration API before passing evidence. Diagnostic logs were disposed after analysis. Declaration review remains 57/149; two existing partial mappings gained integration evidence. Final acceptance is unfinished.

## Sandbox lifetime review

Reviewed five pinned timeout/lifetime declarations. New VM tests verify that returning with an unawaited call succeeds, cancels the host callback and joins its actual return; a never-settling Promise returns a script error. Native default-bounded timeouts differ from upstream's infinite-timeout mode. Review total is 62/149, with 87 unreviewed.

Focused lifetime race count 3 passes in 14.479s. Full normal root tests pass (durable 74.358s, tools 4.653s), with static/logging/non-CGO and diff checks. The initial test reassigned a callback-captured release channel and triggered a fixture race; a once-only close corrected it before passing evidence. No production lifetime changes were needed. Diagnostic logs were removed after analysis. Final acceptance is unfinished.

## Executable isolation cases

Eight pinned sandbox isolation declarations now run against concrete QuickJS-WASI: absent host globals, eval/Function containment, denied dynamic import, builtin patch resistance, instance-only frozen intrinsics, instance overrides, non-string error coercion and frozen tools/console. Native callbacks use object arguments. No production changes were needed; these cases do not complete security acceptance.

Focused isolation race count 3 passes in 10.519s. Full normal root tests pass (durable 74.041s, tools 4.748s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Inventory review is 70/149, with 79 unreviewed. Final acceptance is unfinished.

## Store mutation and limits review

Four pinned store declarations now have executable mappings. Concrete VM tests cover detached loaded values and initial snapshots, set/delete, invalid keys, cyclic JSON, per-value/total limits and recovery. Upstream reports pending writes on failed scripts; the native engine intentionally discards them, and durable applies only successful patches. The first test expected upstream write reporting and failed; it was corrected to test and record the native adaptation. No production store changes were made.

Focused store race count 3 passes in 20.607s. Full normal root tests pass (durable 72.832s, tools 4.495s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Review total is 74/149, with 75 unreviewed; final acceptance is unfinished.

## Return values and script error locations

Seven pinned return/error declarations now have concrete VM coverage: JSON returns, top-level await, output retained before failure, source line preservation, stacks without prelude frames, non-Error throws and BigInt return errors. No production changes were needed. Native result types differ; this does not complete output storage or full tool-result rendering.

Focused return/error race count 3 passes in 15.796s. Full normal root tests pass (durable 74.430s, tools 4.529s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Review total is 81/149, with 68 unreviewed; final acceptance is unfinished.

## Tool dispatch review

Eight pinned tool declarations now have explicit mappings or gaps. Concrete VM tests verify async alias dispatch, immutable `ALL_TOOLS` names/descriptions, catchable callback Errors, safe membership checks and hidden prototype/JSON access. Parallel dispatch, exact missing-name suggestions and mutable sandbox registration remain incomplete. Native alias collisions reject instead of upstream's first-wins behaviour.

Focused tool-dispatch race count 3 passes in 9.147s. Full normal root tests pass (durable 73.043s, tools 4.642s), with static/logging/non-CGO and diff checks. No production changes were needed. Diagnostic logs were removed after analysis. Review total is 89/149, with 60 unreviewed; final acceptance is unfinished.

## Custom-global namespace review

Concrete VM tests cover top-level callbacks excluded from tool-call records, spread arguments with undefined encoded as null, frozen namespaces, safe membership, missing-member suggestions and rejected store/load shadowing. Eight helper/global declarations now have mappings. Exact diagnostics, constructor shorthands and parallel callback semantics retain explicit partial mappings; no production changes were needed.

Focused globals race count 3 passes in 8.232s. Full normal root tests pass (durable 73.044s, tools 4.547s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Review total is 97/149, with 52 unreviewed; final acceptance is unfinished.

## Sandbox declaration review complete

Reviewed the twelve remaining sandbox declarations. New concrete tests verify concurrent independent WASM executions retain isolated globals and deep recursion yields a catchable RangeError. Output/MIME/pretty-print mappings remain partial, and output overflow classification differs. Node worker paths/watch notifications and injected worker/WASM failure APIs have no native equivalent. Parallel independent executions do not implement concurrent calls inside one script.

Focused execution race count 3 passes in 10.488s. Full normal root tests pass (durable 95.257s, tools 6.212s), with static/logging/non-CGO and diff checks. No production changes were needed. Diagnostic logs were removed after analysis. Review total is 109/149, with the 40 durable-runtime declarations unreviewed. This inventory excludes parameterised declarations by its documented counting rule; complete security/parity acceptance is unfinished.

## Durable journal/restart review

Reviewed the first 23 durable-runtime declarations against pinned assertions and native serial journal, globals, identity, deadline and fault tests. Parallel issue/delivery prefixes, idle/waiting watermarks, interrupted-prefix flushing and legacy started-fingerprint transport are absent. Serial restart mappings remain partial where the exact upstream divergent-call fixture has no direct counterpart. Review total is 132/149, with 17 unreviewed.

Focused serial journal/restart/deadline/identity/fault race count 3 passes in 16.786s. This increment changes only review documents; production and test source are unchanged from the preceding verified checkpoint. Diff checks pass; diagnostic logs were removed after analysis. The review does not resolve implementation or acceptance gaps.

## Enumerated declaration review complete

The final 17 durable store/output/discovery declarations have coverage mappings or explicit gaps. All 149 enumerated declarations are reviewed; parameterised declarations are outside this inventory's counting rule. A stale catalogue-reopen file reference was corrected to the existing catalogue test, and every listed native file reference was checked.

Native fork-store, bounded projections, writer replay, image/error preservation and discovery have partial or adapted coverage. Upstream parallel journal batching, 6 MiB spill retention, 4 MiB outer tool output and 100,000-line limits remain gaps. Exact model result headers/default filesystem and several full fixtures are incomplete. Review completion is not parity acceptance.

Focused store/output/discovery/source/tool-error/failure race tests pass in 20.407s. This increment changes review documents only; diff checks pass and diagnostic logs were removed after analysis. Implementation, AI audit, profiling/security and final verification are unfinished.

## Interrupted HTTP follow-up recovery

Both OpenAI APIs now have a test that pauses the final follow-up HTTP stream after codemode and its nested effect settle, closes the harness, reopens the same submission and resumes. The recovered request carries the persisted custom call/result transcript; the nested effect remains single and the transcript has one call/result pair.

The fixture exposed cancellation ErrorEvents with no message or pending partial messages being settled as terminal failures during shutdown. Generation now leaves those intents resumable when the harness lifecycle has ended. Received terminal responses still commit through the existing shutdown contract. Combined two-API interruption and received-terminal race count 3 passes in 33.293s. Full normal root tests pass (durable 75.700s, tools 4.640s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Final acceptance remains unfinished.

## Parameterised bridge audit

Audited the seven malformed-worker payload cases excluded from the 149-declaration enumeration. Native strict store-patch tests cover malformed entries and values; the worker-channel injection API is absent. Failed done messages now require an object with a string message and optional string name/stack. Scalars, empty objects and malformed fields become sandbox errors. Message-only objects remain valid for non-Error script throws. Replay identity advances to host43.

Focused parser/return/stack/isolation/bridge race count 3 passes in 43.634s. Full normal root tests pass (durable 76.468s, tools 4.590s), with static/logging/non-CGO and diff checks. The first broader QuickJS race run failed: one bridge stress case hit its 3s deadline and the new parser initially rejected valid message-only errors. The parser was corrected; the bridge subset then passed unchanged. No timeout threshold was widened. Diagnostic logs were removed after analysis; a fresh full race remains required.

## Return and operation bridge validation

Unknown host operation names now terminate the VM as sandbox errors instead of silently returning undefined. Successful return parsing uses a shared validator that distinguishes undefined from explicit null and rejects invalid or oversized JSON. Direct tests exercise the production validators used by the host bridge; they do not simulate an injected worker channel. Replay identity advances to host44.

Focused malformed/return/stack/isolation/bridge race count 3 passes in 44.075s. Full normal root tests pass (durable 79.586s, tools 4.598s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Parameterised return/unknown-operation mappings now have direct validation evidence, with transport differences explicit. Final parity and security acceptance remain unfinished.

## Concrete replay-divergence counterparts

Added memory/journal reopen tests using a concrete VM behind an identity-preserving drift fixture. After two confirmed nested deliveries, the reopened VM changes call arguments, issues fewer calls or ends immediately. The external source and runtime identity remain unchanged, forcing call-stream validation to detect drift. All three cases reject without another effect. No production changes were needed.

Focused divergence race count 3 passes in 16.815s. Full normal root tests pass (durable 76.961s, tools 4.638s), with static/logging/non-CGO and diff checks. The initial expected error wording was corrected to the existing native error. Diagnostic logs were removed after analysis. Three partial crosswalk entries gained direct restart evidence; parallel watermark and final acceptance gaps remain.

## WASI random byte packing

Pinned `vm.ts` consumes one mulberry32 word per four bytes, emits little-endian bytes and discards unused bytes at each `random_get` boundary. Native code incorrectly consumed one word per byte and took its high byte. The generator now matches the pinned packing; independent TypeScript-derived vectors cover whole words, aligned reads, partial reads and empty requests. This changes replay-visible host semantics, advancing identity to host45. JavaScript Math.random pinned seed tests still pass.

Generator/clock race count 3 passes in 8.125s; deterministic/reopen/identity/divergence race count 3 passes in 48.257s. Full normal root tests pass (durable 76.937s, tools 4.469s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. The earlier chunk-independence test encoded the wrong contract and was replaced with request-boundary vectors. Final acceptance is unfinished.

## Protected global roots

Custom-global admission now rejects `performance`, `InternalError`, `module` and `WebAssembly` roots, including `performance.now`. These names were missing from the explicit intrinsic/ambient denylist. Checks run both in durable configuration validation and before native VM setup, preventing host injection from replacing deterministic clock intrinsics or introducing ambient-looking APIs. Replay identity advances to host46.

Engine admission/global/clock race count 3 passes in 17.187s; durable reserved/clock/identity race count 3 passes in 15.812s. Full normal root tests pass (durable 76.044s, tools 4.593s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Final parity and security acceptance are unfinished.

## Removed tool replay and fresh authority

A concrete memory/journal test reopens after one confirmed tool delivery, removes the registration and resumes the script. The stored delivery replays; the next fresh call produces a catchable error without another effect. Previously the fresh-call admission rejection faulted the parent task. Codemode now checks current callable tools before fresh ordinary calls and journals revocation as an error delivery. Storage/admission errors from admitted calls still propagate; paid globals retain their separate authority fences. Replay identity advances to host47.

Revocation/replay/divergence race count 3 passes in 44.893s. Full normal root tests pass (durable 76.438s, tools 4.620s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Parallel authority/watermark and final acceptance gaps remain.

## Model-facing status and persisted wall time

The codemode extension now prefixes text results with `Script completed` or `Script failed`, wall time rounded to one decimal second, and `Output:`, matching the pinned header structure. Runtime wall time is measured from persisted start to host result preparation, overwrites sandbox-provided timing and is stored in the settled result for reuse. Replay identity advances to host48.

Both HTTP API/reopen/interruption tests now inspect header-bearing transcripts (focused race 42.217s). Host-owned timing and model execution/recovery race count 3 pass in 28.460s. Full normal root tests pass (durable 76.757s, tools 4.684s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Outer output limits and full upstream failure rendering still need work; final acceptance is unfinished.

## Model-facing failure transcript

A concrete generation/VM test now verifies failed scripts produce one error tool result with the original call identity, `Script failed` header, retained prior text/console and script error. The model can consume the error and finish the submission. Store changes from the failed script are absent from the persisted store's values map. No production changes were needed.

Focused failure race count 3 passes in 12.893s. Full normal root tests pass (durable 76.221s, tools 4.718s), with static/logging/non-CGO and diff checks; the final focused rerun also checks the correct nested store envelope. Diagnostic logs were removed after analysis. Parallel/output-storage and final acceptance gaps remain.

## Deferred tool discovery and execution

Expanded the zero-inline-budget model integration to ordinary and deferred registrations. A deferred tool is absent from direct model offers and inline prompt declarations, but the real VM can find it with `searchTools`, inspect it with `describeTool` and execute it. The resulting header-bearing transcript reaches the model. No production changes were needed.

Focused inline/deferred race count 3 passes in 22.390s. Full normal root tests pass (durable 77.821s, tools 4.786s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Parallel/output-storage and final acceptance gaps remain.

## Grammar definition wire preservation

Completions and Responses now preserve grammar definition whitespace, matching pinned constrained-sampling resolution. Whitespace is trimmed only for blank checks; nonblank Lark takes precedence, otherwise regex is selected. Unsupported capability still falls back to function tools as upstream specifies. Both HTTP API tests now require the complete pinned grammar string, rather than its trimmed form.

Both provider package race count 3 passes (Completions 2.730s, Responses 2.486s); real two-API HTTP/VM/reopen race passes in 20.983s. Full normal root tests pass (durable 77.733s, tools 4.639s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. The broader AI audit and final acceptance are unfinished.

## Completions historical grammar declarations

Completions now derives grammar input properties from system tool declarations, retaining removed definitions and applying the latest declaration for each name in first-declaration order. Legacy context tools fill only undeclared names. This matches pinned `getDeclaredTools` and the existing Responses tool view. Replay validation, assistant custom-call encoding and streamed input mapping use the same declaration set, avoiding current-offer schema changes rewriting historical calls.

Provider race count 3 passes in 2.960s; both-API HTTP/reopen/interruption race passes in 30.412s. Full normal root tests pass (durable 77.048s, tools 4.566s), with static/logging/non-CGO and diff checks. Fixtures were corrected to typed tool references and assistant-call selection before passing evidence. Diagnostic logs were removed after analysis. Broader AI audit and final acceptance remain unfinished.

## Transcript removal and re-add ordering

Tool and prompt-section removals now remove their old ordering slots. Re-adding appends one new slot, matching upstream Map ordering. Previously a removed/re-added name could appear twice in the active tool loadout or rendered system prompt. Tests cover replacement in place, re-add ordering, one section rendering and both provider-context resolution modes.

Root-package race count 3 passes in 1.017s; prompt/two-API HTTP/recovery race passes in 38.181s. Full normal root tests pass (durable 78.224s, tools 4.492s), with static/logging/non-CGO and diff checks. Diagnostic logs were removed after analysis. Broader transcript ordering/AI audit and final acceptance remain unfinished.

## Work remaining

* Expand provider boundary evidence to all three strict-limit phrases, inline tool changes, hooks/cancellation and timeout retry interactions before final acceptance.
* Finish AI changed-path audit and boundary expansion: shared fixed schemas and OpenRouter key/region/offline paths are implemented; generic coding-agent config schema is outside the current package scope. Generated catalogs/pins pass; final full-record/audit checks still apply. Codex Uint8Array change is a native byte-buffer adaptation to inspect explicitly.
* Complete durable fatal-backend upstream crosswalk; recovery fault-test migration and full durable normal/race gates pass. Audit remaining direct read validation paths; expanded request-error matrix and shared BM25 ranking are implemented. Finish tool-search boundary coverage and finish the keyed nested-tool candidate's remaining integrity/concurrent/deep-cancellation/fault boundaries and sequential executionMode; default-key allocation, concurrent admissions, deep cancellation, model structured-output transcript disposition, first bounded deep joins and settlement matrix are implemented; model caller/recovery authority checks and typed model lookup are implemented. Finish real-provider paid request/auth boundary and malformed/error outcome evidence; classifier/image synthetic lifecycle/fault matrices are implemented and implement provider auth adapters/request/image quality/resource/animation parity and codemode additions; invocation-bound host auth/availability capability is implemented. Deferred discovery/model loading is implemented. Document idle retention and typed order/cursor read errors are implemented; attached watch/commit-timer coverage still needs final integrated review. Restart abandonment and owned-task methods are implemented; expand version/migration/background/cascade/barrier cases and finish every changed production/test disposition.
* Keep decoded npm subject checks distinct from unperformed cryptographic signature/certificate verification.
* Complete crosswalks, serial full/affected/catalog/static/security/SBOM/consumer/fuzz gates and pre-release CPU/alloc_space/alloc_objects analysis/tuning/disposal.

Published1.1.0 and older release refs/assets/stash are preserved. Implementation is incomplete and uncommitted.
