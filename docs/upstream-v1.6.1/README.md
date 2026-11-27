# pi v1.6.1 native port

**Active target:** [official pi v1.6.1](https://github.com/earendil-works/pi/releases/tag/v1.6.1) at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The official Git tag resolves to that commit, and the AI, Codemode, Durable and Schema package manifests all declare `1.6.1`.

**Comparison base:** official v1.6.0 at `faac585884ae2c013a6b1400f3e9e77f10b34bac`, an ancestor of v1.6.1. The last integrated v1.6.0 native checkpoint was [`a40a761b`](https://github.com/rcarmo/go-ai/commit/a40a761b52a011d20f981c6c981e078bc4654820). The [v1.6.0 verification log](../upstream-v1.6.0/README.md) and its assertion ledger are historical. The later frozen-object checks are also v1.6.0 receipts; the paused Durable draft was not verified or merged. No v1.6.1 runtime parity is claimed by this retarget.

## What changed upstream

The [path inventory](upstream-changes.tsv) records all 591 Git changes between the two releases, including 97 in AI, 64 in Durable, and two each in standalone Codemode and Schema. It is a path inventory, not a test count. Regenerate it with:

```sh
git -C "$PI_MIRROR" diff --name-status --find-renames \
  faac585884ae2c013a6b1400f3e9e77f10b34bac \
  ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe
```

| Area | First comparisons |
| --- | --- |
| Codemode | Standalone `src/` and `test/` are byte-identical; its manifest and changelog changed. Start with Durable's changed Codemode paths: saved-result versus started-run selection, then spill and image output's saved/failed write results. Check the actual public worker and SQLite paths before classifying the new discriminants as private or visible. |
| Durable | Audit the changed scheduler, parks, signals, live state, transaction and storage paths against the v1.6.1 originals. Retain crash, ownership and publication guarantees. |
| AI | Reverify provider request and stream changes, including Codex SSE, alongside the model data and catalog regeneration. Do not treat the old wire receipts as a comparison against changed source. |
| Schema | Standalone `src/` and `test/` are byte-identical; verify its new published artifact and dependencies before carrying forward scoped evidence. |
| Dependencies | Reconcile `package-lock.json`, the coding-agent install lock, and the release manifests and archives from the new pin. Do not use the v1.6.0 installation as a verified v1.6.1 oracle. The retarget initially left model regeneration at v1.6.0; the verified Round 2 comparison below moves that gate to v1.6.1. |

## Evidence and retained gates

Start with byte-verified unchanged v1.6.1 originals. Test the same observations through the native production path on an unchanged baseline, then fix only demonstrated differences and run discriminating controls. Keep the new assertion ledger separate; carry forward a receipt only after checking its source, runtime and dependency inputs at the new pin.

The [existing acceptance gaps](../upstream-v1.6.0/acceptance-gaps.md) are starting points, not revalidated v1.6.1 conclusions. Live replay still requires provisioned credentials. Changes to callback accounting or generic Codex dispatch need a decision. Full acceptance, profiling and release remain separately gated. Preserve SQLite data, old saved starts and release assets. No cache pruning has been authorised.

## Retarget verification

The path inventory reproduced byte for byte from the two Git commits. The four manifest versions and the unchanged standalone source/test trees were checked directly against the fetched tag. On the native baseline (`42a37aae`), four focused QuickJS tests passed twice without skips. The historical ledger still validates its 2,122 locators against the v1.6.0 bytes; locator validation does not run those tests. No v1.6.1 original or published worker was executed during the retarget.

## First v1.6.1 runtime comparisons

The [dependency receipt](dependencies/README.md) records the exact Git checkout, the workspace closure and the published archives. Only the Codemode test scope is installed at this pin so far. The released source tarball contains provider data absent from Git, so the two are retained separately.

### Standalone tools use the first normalized alias; Durable starts stay strict

The two unchanged standalone cases passed on worker, inline and remote hosts. The unchanged native baseline rejected the normalized-name collision before dispatch; its omitted-argument and explicit-null control already passed. The native standalone engine now lets the pinned guest prelude choose the first normalized alias. The three calls return `dash`, `dash`, `mcp` and expose exactly the two expected `ALL_TOOLS` entries. Reversing registration changes the normalized alias but keeps the distinct raw alias callable. All descriptions still count toward the setup limits, even when shadowed.

The unchanged deterministic start case passed on worker and remote hosts. Native caller-controlled starts keep their previous refusal before reserving a worker or dispatching a call. This is a supplemental preservation check, not a match for the JavaScript throw envelope or wording. No saved starts, catalog data, limit, or invocation accounting changed.

At `d6c326d6`, ten focused Go tests passed twice without skips. Vet and Staticcheck v0.8.2 passed. The two v1.6.1 standalone assertions are mapped separately from the historical ledger. The Durable saved-start, settled-result and spill cases still need their unchanged originals at this pin.

### Durable reopens settled results and keeps the saved spill path

Five unchanged v1.6.1 originals passed in the [pinned Durable Node test scope](dependencies/durable-node-tests.md). The changed `done`/`started` result in `src/codemode/codemode.ts` is local to the transaction callback. The persisted run document is still version 1. The native public SQLite comparison preserves the decided result without a second VM launch; the unfinished run has no outcome at the cutoff and recovers without repeating its committed effect.

The published v1.6.1 worker matches the small text and image successes and the reopened spill path. The file paths appear in the model-facing result, the saved text and PNG bytes match exactly, the image block remains present, and reopening keeps one `output.txt` in the same directory. Eight native tests passed twice without skips at `9853e82c`. For the published subset, a test-only overlay changes just the opt-in environment name and expected package version in `durable/codemode_worker_published_v160_test.go`. The worker is extracted from the verified `pi-codemode` 1.6.1 tarball and uses the verified, unchanged `quickjs-wasi` 3.6.2 dependency. Native production code did not change for this comparison.

A separate source-derived failure fixture returns an error from the environment writer. Upstream keeps a successful script result, omits the text spill path, retains the image bytes and includes the writer's message in both failure labels. The existing native failure controls still keep output bounded, retain images, omit a failed spill path and redact writer error details. That message difference remains open; it is not part of the 19 mapped success and replay assertions.

## Next verified inputs

The published chat catalog has 1,696 entries (63 additions, 65 removals and 138 changed records); the 74 image entries are unchanged. The classifier catalog has five additions, five removals and four changed records. These are [archive-to-archive counts](dependencies/README.md#published-catalog-changes), not Go regeneration evidence.

The three [next Durable originals](durable-next.md) passed, covering invalid parks, ordered signal deliveries and late expiry. The next native comparison must distinguish the caller-facing tagged signal result from the unchanged persisted memo. No Go change was made for these cases.

## Round 2: publication, invalid parks, signals and catalog

### The published v1.6.1 worker keeps grouped journal publication

The unchanged batching original passed. The verified v1.6.1 npm worker returns `50` and publishes eight journal changes in each of two native repeats. The legacy policy on the same worker returns `50` with 54 publications in both repeats, so it does not satisfy the grouped threshold. The v1.6.0 artifact still uses its own opt-in version check and returned eight publications twice. The published prelude, portable entry point and worker entry point match across those archives. The new tests select the v1.6.1 package directly without overlaying the v1.6.0 fixture or changing the production policy.

### Invalid parks retain the public failure path

The unchanged v1.6.1 invalid-park original passed. For the three representable Go returns (output with a park, empty sleep and waiting on an unowned task), the SQLite harness completes the submission, sends an invalid-park error to the model and clears the live tool slot. Native checks also retain one `AfterTool` error receipt with the same task and call, and a failed terminal tool task with a committed result entry. The timed park still completes successfully; mixed and reused invalid parks remain separate controls.

The original's raw JavaScript `{kind: "nap"}` cannot inhabit Go's typed sleep/wait result. All four assertions in the original loop remain partial rather than using the old mixed case as a substitute. No production park or storage code changed. At `0e120500`, five focused tests passed twice without skips and vet passed.

### Prepared Schema checks own properties without prototype lookup

All 40 unchanged v1.6.1 validator cases passed. The prepared Go checks require an actual `constructor` property, accept it when present, reject the wrong type for the `toString` property, and do not treat `constructor` as a known type. Separate exact explanation checks cover the missing property, the wrong type and the unrecognized type. A valid object with both own properties remains accepted. Three controls isolate those refusals without introducing JavaScript prototype behavior into Go.

The verified v1.6.1 npm archive has the expected release digest and package manifest. Its 43 executable distribution members and all 172 distribution members match the v1.6.0 package by path and bytes. This is artifact identity, not proof of a runtime load. The focused native tests, vet and Staticcheck passed without production changes.

### Caller-facing signal results carry kind tags without a memo migration

Both unchanged v1.6.1 signal originals passed. Native public SQLite outcomes initially returned the right delivery decisions, winning values and deadline wake, but serialized the signal states as `{value: ...}` or `{expired: true}`. `SignalState.MarshalJSON` now emits `{kind: "value", value: ...}` or `{kind: "expired"}`. The Go `Value` and `Expired` fields, delivery protocol and persisted memo formats are unchanged.

A separate real SQLite reopen verifies both legacy memo forms, distinguishes an absent signal from an explicitly delivered null, and checks that mutating a sender or returned value does not mutate the memo. Three controls independently catch a missing value tag, the old expiry view, and a tag leaking into a stored memo. Twelve focused native groups passed twice, and vet and Staticcheck passed.

### The Go catalog regenerates from the verified v1.6.1 archive

The 28 unchanged catalog validation cases and three Claude generation cases passed. The old Go generator accepted the published modular JavaScript but rejected the exact v1.6.1 offline manifest. The offline path now admits that exact schema-v6 manifest, including its 43 file hashes, without dropping the older supported manifests. Both generator paths reproduce identical chat, image and classifier output. The public registries match all 1,851 published records: 1,696 chat, 74 image and 81 classifier. Image output did not change.

The local model-regeneration gate now checks the v1.6.1 archive and manifest. Independent controls reject changed chat, image and classifier metadata, a stale manifest and the wrong archive. Catalog generation is not an approval to publish a release or a claim that every provider API was tested.

### Workers AI preserves the pinned empty versus null replay content

All five unchanged `openai-completions-non-null-assistant-content.test.ts` variants passed, using the verified provider data from the release alongside the unchanged test and API sources. With the old Go catalog, the four models requiring an empty string sent JSON `null`; GLM-5.3 correctly sent `null`. After regeneration, the public request hook captures `""` for the four models and exact JSON `null` for GLM-5.3. All cases retain the exact four role positions and `tool-1` call ID. The hook stops each request before transport, so this is not a live Cloudflare test. No provider runtime code changed.

The integrated code at `47f5e092` passed 17 focused top-level Go tests twice, without skips. Vet, Staticcheck v0.8.2, the exact model-regeneration gate and its negative self-test passed. The active v1.6.1 ledger validates 36 scoped sites: 32 mapped and four partial. These are development comparisons, not full parity or release acceptance.

## Round 3: incremental verification

### A refused journal commit never reaches the v1.6.1 worker

The unchanged `codemode.test.ts` commit-before-delivery case passed. The same script in the published v1.6.1 worker calls only `first` when its journal commit is refused. The native storage decorator captures the rejected write, and the harness joins after the `lost` failure. The worker receives zero deliveries. Reopened SQLite contains the first issued call but none of the rejected delivery. These stronger native observations do not add source assertion sites.

The v1.6.0 worker's existing fence and v1.6.1 successful grouped delivery ran as controls. Three focused Go tests passed twice without skips; vet and Staticcheck passed. No production code changed.

Exact native test code: `cd76b19d`.

### The v1.6.1 worker cannot restart past the saved deadline

The unchanged deadline original passed. Native SQLite reopened with the saved deadline already 100 ms in the past. It returned an error containing `deadline passed`, without a new guest start or echo effect. The native fixture allows 3,000 ms for cold worker startup; this compares the saved-deadline behavior, not the original's 300 ms wall-clock window.

The unexpired control runs the same script and worker with the clock still before the deadline. It opens a second guest, makes one echo call, and keeps the original deadline. The prior v1.6.0 deadline and v1.6.1 lost-commit checks also passed. Four focused Go tests passed twice, plus vet and Staticcheck. No production code changed.

Exact native test code: `cf2199c2`.

### Published worker replay keeps an undefined global result undefined

The unchanged original passed. The published v1.6.1 worker and reopened native SQLite journal preserve the original `[true,true]` result: the global returns undefined before and after the replay boundary. The native test checks the committed no-value delivery and both actual guest starts. A separate control retains a committed explicit-null argument rather than interpreting it as an omitted argument; it does not assert a null result.

Four focused Go tests passed twice, plus vet and Staticcheck. No production code or saved format changed.

Exact native test code: `3dc0c63f`.

### OpenRouter retains native finish reasons without discarding visible output

All 13 unchanged originals in the selected Completions stop-reason file passed. Seven of the nine scoped native cases failed before the fix. Go now retains a distinct OpenRouter `native_finish_reason` in the raw stop reason and error diagnostic. It keeps a turn with visible output successful, preserves clean stops regardless of case, and marks reasoning-only or whitespace-only failed stops retryable. The provider `finish_reason: error` control is retryable; the content-filter outcome and unrelated providers are not promoted to that path. These comparisons cover the retryability decision, not a multiattempt retry run. Eight fault controls isolated the changed decisions.

The full provider package on the unchanged base also exposed four stale tests after catalog regeneration: one active Qwen field expectation and three tests that look up removed historical Groq or OpenCode IDs. This slice did not hide those failures or change their tests. The scoped OpenAI and retry checks passed; the broader catalog-test reconciliation is separate.

At `4b4a0641`, 7 focused top-level Go tests passed twice without skips. Vet and Staticcheck passed. The ledger maps 27 sites across the nine compared originals; the other four passing originals are not mapped here.

### Nested callers reuse the rewritten result without altering the raw model message

All three unchanged nested-tool originals passed. In native SQLite, the hook sees the actual parent task and call, one nested call is blocked, and the other returns the rewritten structured value. The raw caller result stays a separate tool-result message. A same-key retry returns the saved value without another child execution; conflicting arguments appear as an error to the model.

After reopening SQLite, the replay-safe caller reuses the first committed child and starts a distinct second child. The raw model result is `run 2: echo a, echo b`. A separate rewritten-receipt probe confirms that the saved rewrite is reused without calling the child or hook again. The native test checks the total task count and the first child's nested outcome before close. It does not reread both input discriminants and the first outcome discriminator after the completed reopen, so those two representation assertions remain partial. No runtime or storage code changed.

At `23ec2d6b`, three focused Go tests passed twice, with vet and Staticcheck.

### Completions preserves tool and trailing finish reasons

The four remaining unchanged originals in the v1.6.1 stop-reason file passed. The native public fixture checks a normal successful stop, a tool call with `tool_calls.STOP`, a trailing error frame that preserves the earlier native reason, and equal reasons that do not repeat the suffix. The tool-call fixture also retains the exact call ID and name. No production code changed. The active ledger now covers all 13 declarations in this one upstream test file, not the whole provider suite.

At `4c989882`, 7 focused top-level Go tests passed twice, with vet and Staticcheck.

### The completed reopen retains the nested input and result tags

A follow-up read on the actual reopened SQLite handle finds exactly two committed tool inputs tagged `nested` and confirms that the reused first child's terminal result still has the `nested` tag. The previously partial input-count and result-shape assertions are now directly checked after the turn completes. The unchanged originals are the same three already verified in this round. No production or stored schema changed.

At `1aa3c8e8`, three focused Go tests passed twice, with vet and Staticcheck.

### The full OpenAI provider package passes after catalog-test reconciliation

The four failing tests were asserting stale catalog inputs. The active Qwen expectations now match the exact published metadata for the three affected models. Its provider data bytes were already the same in v1.6.0 and v1.6.1. The retired Groq and OpenCode cases use their complete verified v1.6.0 records to keep the historical wire checks. Those records remain absent from the active registry. Both retained fixture records match the archived inputs, and neither active generation nor provider production code changed.

The scoped original tests and four fault controls distinguish the current metadata from the historical wire behavior. This is native test reconciliation, not new v1.6.1 direct assertion mapping.

At `5fe42737`, all 101 top-level tests in `./inference/provider/openai` passed twice with no exclusions or skips. Vet and Staticcheck passed. The active v1.6.1 ledger remains 90 scoped sites: 86 mapped and four partial. No full repository or release acceptance is claimed.

## Round 4: independent verification slices

### A reopened fork sees its own historical Codemode store

The unchanged fork-scoped store original passed. With the published v1.6.1 worker and native SQLite, the parent returns `1` and `[1]`. After reopening the database, a fork at the original input returns `[null]`. The native fixture also checks the two distinct rewindable, as-of store records and three guest starts. No production code or saved format changed.

Five focused Go tests passed twice, including the native inline and earlier published worker controls, large-store recovery, and refusal before delivery. Vet and Staticcheck passed.

Exact native test code: `b7f4ddd4`.

### The published worker preserves nested structured and text results

Three unchanged Codemode originals passed. The published v1.6.1 worker gets the exact schema-less failure text and structured value, reads a schema tool's failure value when present, and catches the failure without a value otherwise. A schema-less tool with an image returns its caption or the empty string, not an image value. A separate diagnostic control checks that the reported error message takes precedence over the result text.

Four focused Go tests passed twice, including the v1.6.0 worker and SQLite controls. Vet and Staticcheck passed. No tool runtime or storage code changed.

Exact native test code: `9e5e9040`.

### Prepared Schema reports the selected branch and escapes both pointer characters

All 40 unchanged validator originals passed. Prepared Go validation reports `/1/width` and `minimum` for the selected image branch, and `/0/kind` with the exact alternatives for an unrecognised tag. For a property named `a/b~c`, the diagnostic path is `/a~1b~0c`. Valid controls produce no diagnostics. Separate faults changed branch selection, allowed an unknown tag to fall through, and disabled each of the two pointer escapes. Each failed the intended assertion.

Three focused Go tests passed twice, with vet and Staticcheck. No production validation, coercion or tool rules changed.

Exact native test code: `e1550863`.

### Google preserves XHIGH on level models and uses the high budget on older models

All 20 unchanged thinking-map originals passed. The old Go resolver rejected `xhigh` even when the model mapped it. Google Generative AI and Vertex now send `XHIGH` on the level-based Gemini 4 fixture. The budget-based fixtures use the existing high budget: 1,234 for the explicit Generative AI override and 32,768 for Vertex's Gemini 2.5 Pro default. The separate Flash-Lite minimal control remains 512. Uppercase maps, a zero override, the dynamic budget, and unsupported-level clamping remain independently checked.

The native tests inspect the public payload hook and a scripted REST request. They do not compare Vertex SDK constructors or call a live provider. Five fault controls isolated the level, budget, case, zero, and Flash-Lite behaviors. The delegated Google package run passed without exclusions.

At `fe46a5cd`, 6 focused Go tests passed twice, with vet and Staticcheck.

### SQLite distinguishes stored and refused commits without changing the saved format

Four unchanged originals passed: two `StoreStorage` cases and the SQLite replacement case in both write orders. Go's public `CommitResult.Kind()` now returns `stored` or `refused` after the caller checks the separate Go error. A refusal with a null, undefined or false value still counts as a refusal. Existing Go fields, JSON encoding and SQLite records are unchanged.

The native tests check contiguous success receipts, a refused batch that cannot write any of its entries, and a suffix that is not attempted. The committed prefix and its sequence survive reopen, and another write continues from that sequence. The replacement probe checks that a third document incarnation is refused and only the replacement stays current. Fault controls misclassify a null refusal, shift a receipt and leak a suffix. They all fail as intended.

This scope does not reproduce Chord's private outbox or count its backend transactions. The native method and refusal value also do not reproduce the TypeScript object's property and `Error` string envelope. Those limits stay explicit in the ledger.

At `1650f5be`, 5 focused Go tests passed twice, with vet and Staticcheck.

At the combined Round 4 code checkpoint `7aab002c`, 11 focused Go tests and all 102 Google provider tests passed twice without skips. Vet and Staticcheck passed on the root, Durable and Google packages. The active v1.6.1 ledger contains 114 scoped sites: 106 mapped, six partial and two unmatched. The unmatched sites concern Chord transaction counting and outbox state. Full parity remains open.

### Next: Codex request-level reasoning baseline

Three changed v1.6.1 Codex originals passed: retained replies establish the first request effort, replies from the wrong model are ignored, and simple mode with omitted reasoning starts at `none`. On the unchanged native baseline, a scripted public `StreamSimple` call for the last case keeps the `none` reply marker but sends `medium` with an extra `configuration_update:none` item. Upstream expects `none` with only the user item. This is a reproduced request gap, not a fix. The distinct raw versus simple dispatch decision remains open.

## Round 5: Codex baseline and parallel checks

### Codex starts at the retained request effort and sends only changes

Four unchanged Codex stream cases and eight initial `max` variants passed. The old Go request failed all three native SSE cases: it always sent `medium` and included the initial and repeated efforts as configuration items. Go now uses the first retained reply from the requested model as the initial effort. With no such reply, it starts at the requested effort, including `none` on the simple omitted-reasoning path. The transcript carries only subsequent changes. Unrelated-model replies cannot set the baseline, and a compacted history with no retained reply starts fresh.

The cached WebSocket test preserves one full request and two deltas, with the original `low` request baseline and only one update to `high`. The existing off transition still emits `none` as a change. Initial `max` requests carry `max` in the reasoning object, with no configuration item. Earlier v1.6.0 fixed-medium expectations are historical; their active native controls now assert the checked v1.6.1 projection. Three compiled controls restore `medium`, allow a repeated level, or accept a different model's reply; each fails its intended assertion.

Only request projection changed. Raw versus simple dispatch, response markers, credentials, and cache ownership policies were not changed. No live request was made.

At `b2ce9574`, all 111 Codex provider tests passed twice without skips. Vet and Staticcheck passed.

### Google distinguishes a blocked prompt from an unfinished stream

Both unchanged provider variants passed. The old native stream reported a generic missing-finish error. Now both Google adapters report the first prompt block with its message and safety ratings, or, if there is no block, the exact number of chunks received before the stream ended. The scoped blocks are not retryable; the unfinished streams are. A valid terminal frame still wins. Malformed optional feedback cannot discard a valid candidate.

The scripted REST fixture made no live provider call and does not compare SDK constructors. Six fault controls failed as intended. The complete native Google provider package passed in this integration; shared retry policy and retry attempts were not changed.

At `d92b7467`, 103 Go tests passed twice without skips in this scope. Vet and Staticcheck passed.

### Prepared Schema keeps the enum cap and exact contains messages

All 40 unchanged validator originals passed. The prepared Go validator reports exactly 20 values and `4980 more` when a 5,000-member enum fails. It accepts the final member of both the 5,000- and 10,000-member enums. The four `contains` cases match the exact singular, plural, maximum and range wording. The short enum check also matches the complete diagnostic record. Three faults changed the cap, dropped the final member or changed `contains` wording. Each failed the intended test. No production code changed.

At `8cbfe2ff`, 6 Go tests passed twice without skips in this scope. Vet and Staticcheck passed.

### Compaction hook decisions do not spend provider usage

Three unchanged originals passed. Public SQLite verifies that a hook decline completes with an empty result, changes no usage, and makes no provider summary request. A supplied summary also leaves the existing nonzero usage unchanged. When the hook creates owned work, the summary can land and leave the live compaction view while the task is still completing. The task becomes terminal only after its owned child completes.

A supplemental busy-conversation control leaves the summary queued through child completion and places it only when the generation settles. Separate controls admitted a provider request or dropped the owned child; both failed. No production API or stored record changed.

At `4955153f`, 5 Go tests passed twice without skips in this scope. Vet and Staticcheck passed.

### The published Codemode worker keeps the failed script's call receipt

Both unchanged originals passed. The published worker reports `Error: boom` with one `echo (ok)` receipt. After a SQLite reopen with a changed host sandbox identity, it refuses the run without another guest start or echo effect. The saved start and journal remain unchanged. The source's version-0 variant remains partial because native storage refuses that saved-start rewrite before replay. See the [failure receipt and controls](codemode-failure-receipts.md).

At `32c954a6`, eight focused Go tests passed twice without skips. Vet and Staticcheck passed.

At the integrated Round 5 checkpoint `42225aca`, 24 focused Go tests passed twice without skips. Vet and Staticcheck passed across the five checked packages. The active v1.6.1 ledger has 153 scoped sites: 142 mapped, nine partial and two unmatched. This was not full local acceptance.

The historical v1.6.0 log remains a snapshot. Running its locator validator against current native files stops at a Completions test anchor that had already changed before Round 5 during catalog-test reconciliation. The original source receipts remain pinned; that check is not an active v1.6.1 result.

## Round 6: replay, scheduling, framing and dependency inputs

- [SSE framing](sse-framing.md): four unchanged cases ran. Public Go parsing matched framing across all chunk sizes and line endings, including a split CRLF and a tail at EOF. Go's documented omission of comments from `Raw` leaves one assertion partial.
- [Prepared Schema references](dependencies/schema-prepared-references.md): a registered reference accepts a valid point and rejects a missing coordinate. Recursive `oneOf` reports both errors from the selected branch, in order. No validator code changed.
- [Bedrock SDK input](dependencies/README.md#bedrock-sdk-input): the staged CommonJS entry matched the locked archive, and the pinned provider loaded it in an isolated Node import. This did not exercise a client or provider request.
- A fresh `main`-only checkout exposed two ledger references to commits that existed only in lane history. Both now point to identical receipts on `main`. The locator validator rejects unreachable commits; the repaired ledger was verified in a fresh checkout independent of the native and source test runs.
- [Codemode output](codemode-output-layout.md): the published portable guest kept the text and console order and spilled the complete 6 MiB and 200,000-line bodies while keeping previews within the asserted bounds. The preview text itself is not an exact comparison.
- [Durable inbox continuation](durable-yield-continuation.md): the queued write lands before the continuation, the original input settles against the second answer, and the write has its own receipt. No ownership or storage changed.
- [Responses tool-call replay](responses-tool-call-replay.md): OpenAI Responses and Codex now send `status: "completed"` on replayed function and custom tool calls. Full replay was checked offline. The Codex suite passed on rerun after one timeout-order failure; that test and live WebSocket prefix checks are not covered by the replay fix.

At the integrated checkpoint `aa2b72e4`, 11 focused Go tests passed without skips across five packages, with vet and Staticcheck. The pinned ledger has 175 sites: 163 mapped, ten partial and two unmatched. This was not full local acceptance or a live provider run.

## Round 7: provider errors, Durable boundaries and verified dependency inputs

- [Mistral raw stops and strict tools](mistral-stop-schema.md): the published catalog origin uses `/v1/chat/completions` without changing custom Go base paths. The exact provider error is `Provider stopped with: error` and remains retryable. The catalog-dependent strict-tool test uses the current model without restoring a retired ID.
- [Provider error response hooks](provider-error-responses.md): OpenAI Completions, OpenAI Responses, Azure Responses, Anthropic, and Google Gen AI notify the hook for each failed attempt. Google Gen AI presents an empty SDK-style error header map without dropping transport retry headers. Explicit native `RetryConfig` keeps precedence.
- [Durable yield boundaries](durable-yield-boundaries.md): when a queued reset or follow-up wins the boundary, the original input settles against the first answer and the continuation does not enter the transcript.
- [Published worker held replay and call details](codemode-held-replay-and-call-details.md): held calls are all checked before any new effect, while mixed success, failure, and cancellation produce exact call rows. Literal arguments in the conflicting-call diagnostic remain redacted in Go.
- [Prepared numeric and UTF-16 Schema](dependencies/schema-prepared-numeric-unicode.md): nonfinite values and lossless UTF-16 inputs matched the pinned predicates and scoped records.
- [Photon image worker](dependencies/README.md#photon-image-worker) and [shipped installer](dependencies/README.md#shipped-coding-agent-installer): Node loaded verified image-codec bytes; the separate static comparison identified the shipped lock's placement and peer differences without running an installer or CLI.

## Rounds 8–9: classifier, schema formats and Durable commit boundaries

- [Pi Decision](pi-decision.md) sends the pinned request shape, validates typed answers and preserves reported cost and response ID. Its error hook runs before reading the final error body. The five originals ran offline; live provider behaviour and intermediate retried classifier responses were outside those cases.
- [Google tool replay](google-tool-replay.md) preserves call and response IDs on prefixed Gemini models. [Google strict mode](google-strict-compat.md) honours explicit true and false overrides without changing the published model catalog.
- Prepared Schema now asserts [both pointer formats](dependencies/schema-pointer-formats.md), [URI references and templates](dependencies/schema-uri-formats.md), and [IPv6](dependencies/schema-ipv6-format.md) when enabled. All 196 draft 2020-12 fixture vectors compare the verdict and exact error array. Default annotations, legacy checks and tool validation stay unchanged.
- [Separate child wakes](durable-child-wakes.md) keep the original caller and attribute its final tool result after both children finish. [Terminal admission](durable-terminal-admission.md) checks the attributed input and same-commit SQLite publication.
- [Early Codemode store replay](codemode-early-store-replay.md) checks that a divergent return cannot publish a store write; finishing the pending call first commits the write in the supplemental case.
- The [local workerd example](dependencies/README.md#cloudflare-codemode-example-on-local-workerd) used a lock-verified binary and the verified published Codemode archive. The separate Durable Cloudflare suite had not yet been staged at this checkpoint.
- [llama.cpp response hooks](llama-cpp-response-hooks.md) and [Pi Decision retry hooks](pi-decision-retry-hooks.md) report every completion response before reading its body. A hook refusal closes the unread body and stops retry even when the hook returns a retryable HTTP error. Other classifier hook timing stays unchanged.
- [Bedrock profile credentials](bedrock-profile-proxy.md) reach the loopback proxy in a fresh subprocess. No AWS endpoint or provisioned credential was used.
- [Inline image admission](codemode-inline-image-admission.md) still differs: the verified published guest accepts the 1,048,684-byte original, 108 bytes above Go's 1 MiB setup cap. Native controls check the exact boundary. The cap and both partial assertions remain.

At the integrated checkpoint `2d800501`, 62 focused Go tests passed across 9 packages without skips. Vet and Staticcheck passed. At that checkpoint the ledger had 686 sites: 668 mapped, 13 partial and five unmatched. This was a focused run, not full local acceptance. The test-file crosswalk indexes at least one source case in 24 of the 68 changed test-file paths. Other receipts can exist outside the index.

### Upstream test-file crosswalk

[`test-crosswalk.tsv`](test-crosswalk.tsv) lists the pinned AI, Codemode, Durable and Schema test files, their change status since v1.6.0, and the cases indexed in the assertion ledger. Fixture cases count against their test runner. `scoped-mapped-sites` means all indexed sites in that file are mapped; unindexed cases in the same file can still need work. `not-indexed` means the active ledger has no entry. Standalone receipts and historical runs can coexist with that state.

```sh
bun run docs/upstream-v1.6.1/assertions/validate.ts "$PI_ORACLE" "$PI_MIRROR"
bun run docs/upstream-v1.6.1/test-crosswalk.ts "$PI_MIRROR" --write
bun run docs/upstream-v1.6.1/test-crosswalk.ts "$PI_MIRROR" --check
```

The crosswalk rejects duplicate source sites and stale output. These checks use pinned Git and ledger bytes; they do not run upstream or native tests.

### Open parity decisions

Go still refuses a raw version-0 saved-start rewrite and a source above its setup cap. The replay diagnostic redacts literal arguments, and the output writer failure has a separate redaction difference. The typed Go park API cannot receive the raw JavaScript `nap` value. The two Chord-specific transaction and outbox sites and three TypeBox symbol-key sites are unmatched. SQLite commit envelopes and SSE raw comments also differ.

Live OpenAI and Codex replay is authorised, but the workspace has neither an OpenAI key nor a Codex auth entry. Codex raw/simple dispatch and early settlement of cancelled host callbacks at capacity one still need decisions. Continue using the crosswalk for offline provider, scheduler, storage and schema cases. Full acceptance, profiling, hosted CI and release have separate approval gates.

## Rounds 10–12: provider, scheduler and storage comparisons

| Slice | Verified behaviour |
|---|---|
| [Pi Messages null frames](pi-messages-null-events.md), [index bounds](pi-messages-content-indexes.md), [terminal fields](pi-messages-terminal.md) | Parsed non-null wire objects reach the hook before conversion. Content indexes must address a valid block or the next start. Terminal messages keep the backend model and reported cost, and the first partial remains `pending`. |
| [Anthropic strict fallback](anthropic-strict-tool-fallback.md) and [schema dialect](anthropic-schema-dialect.md) | Failed and successful attempts reach the response hook in order. The fallback memo is bounded and clearable by session. The non-strict projection keeps `$schema` and draft-07 tuple items. |
| [System One billing](system-one-cost.md) and [OpenAI Decisions usage](openai-decisions-usage.md) | System One keeps the response ID and uses a provider total only for verified OpenRouter billing. Decisions accounts for cache reads and writes separately while preserving aggregate-input pricing. |
| [Turn limits](durable-turn-limit.md) | The optional persisted limit fires after a completed tool round, uses unanswered `turn_limit` settlement, and respects a changed or cleared value at the round boundary. |
| [Codemode store checkpoints](codemode-store-checkpoint.md) | Existing SQLite history matches the 64-delta and 65th-write checkpoint, as-of receipt and fork. The published guest separately reads parent and fork values after reopening. |
| [Prepared `duration`](dependencies/schema-duration-format.md) and [temporal formats](dependencies/schema-temporal-formats.md) | Configured validation rejects the invalid duration vectors. Existing date, time and date-time routines match all 184 prepared Draft 2020-12 vectors and exact errors. Legacy and default validation stay unchanged. |
| [Cloudflare storage](dependencies/durable-workers-storage.json) | Local Vitest loaded the unchanged pinned storage file and launched the verified workerd binary in a private, loopback-only namespace. All 35 storage tests passed. This is Cloudflare Durable Object evidence; the Go backend is still SQLite-only. |
| [Codemode namespace prompt](codemode-namespace-prompt.md) | The prompt and Lark grammar match through the public Harness. The published guest can still call the deferred tool; the tool remains absent from the prompt. |
| [Turn-limit continuation and recovery](durable-turn-limit-boundaries.md) | The targeted native controls cover a fresh continuation budget, termination at the cap, inheritance and replay after a killed process. |
| [Prepared IRI references](dependencies/schema-iri-reference-format.md) | Configured `iri-reference` rejects invalid text before the existing WHATWG parser normalises it. All 21 pinned vectors match. |
| [Cloudflare wakes](dependencies/durable-workers-wake.json) | Three unchanged local workerd tests cover alarm after eviction, charged mid-stream recovery and idle eviction. These are separate from the 35 storage tests. |
| [Bedrock signing precedence](bedrock-credential-priority.md) | Real Go SDK requests to local fixtures select the explicit or scoped profile before ambient keys, and retain ambient keys with an ambient profile or proxy. The JavaScript constructor shape is partial or unmatched. |
| [Request-size overflow](request-size-overflow.md) | The complete `request_too_large` error code indicates overflow. A longer identifier does not. HTTP 413 classification stays unchanged. |

The exact combined code candidate `bc1753b02324865d82dc554a0f40c2556ecb081a` passed 125 focused top-level Go tests across 14 packages without skips. Vet and Staticcheck v0.8.2 passed. That checkpoint recorded 1,345 source sites: 1,313 mapped, 25 partial and seven unmatched. These counts include only recorded sites. They do not establish full package parity.

The updated test-file crosswalk includes `test/` and `test-workers/` in the four target packages. It indexes evidence in 38 of 321 pinned files and 32 of 68 changed test-file paths. The [changed-path disposition](upstream-disposition.tsv) classifies all 591 changed paths by their role in this comparison. An unindexed file can still have a separate runtime receipt, as with the Cloudflare tests. A path outside the four packages is not an approved exclusion.

A fresh full-history `main` checkout reproduced the crosswalk and disposition and validated the source and native locators. This was a focused offline verification. Live replay still needs credentials; Codex dispatch, late callback settlement and the listed backend and representation differences remain open. Full acceptance, profiling and hosted release checks were not run.

## Final turn-limit comparison

The [remaining three pinned cases](durable-turn-limit-inbox.md) pass on the unchanged Go scheduler: independent runs get fresh limits, a steer that already joined the run settles in order with the original input, and a queued follow-up follows `keep`, `continue` or `withdraw`. All 11 original cases in `harness-turn-limit.test.ts` have now run unchanged across the three receipts. The native input and restart mechanism differences remain explicit.

The exact combined candidate `a4ef1b0d03f55323e18693881507b42a976f66fb` passed 128 focused Go tests across 14 packages without skips, with vet and Staticcheck v0.8.2. Schema regeneration inputs and generated bytes are unchanged from the earlier combined check. The active ledger now has 1,354 sites: 1,322 mapped, 25 partial and seven unmatched. A fresh full-history `main` checkout validated it and reproduced both inventories. The crosswalk still indexes 38 of 321 pinned test files, including 32 of 68 changed paths. Full parity remains open.
