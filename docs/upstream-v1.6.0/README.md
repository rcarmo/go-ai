# pi v1.6.0 parity target

The upstream target is [pi v1.6.0](https://github.com/earendil-works/pi/releases/tag/v1.6.0) at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The prior [v1.5.2 notes](../../notes/upstream-v1.5.2-restart.md) and [v1.5.0 acceptance evidence](../upstream-v1.5.0/) are historical. The model-regeneration gate is pinned to v1.6.0. The repo-wide acceptance gate has not been switched.

## Verified inputs

[`source-delta.tsv`](source-delta.tsv) compares v1.5.2 and v1.6.0 across AI, Durable, Codemode, Schema, Telemetry, Chord, and the npm lockfile. The inventory checks 2,148 tracked files byte for byte and reports 396 changed paths. It includes all of Chord to expose dependency changes for review. It is not a transitive dependency inventory, assertion ledger, or approved exclusion list.

```sh
bun run scripts/inventory-upstream-v152.ts /path/to/pi-git /path/to/v1.6.0-source --v1.6.0
```

The published npm archives match registry SHA-512 integrity. Their SHA-256 digests are:

| Package | SHA-256 |
| --- | --- |
| `@earendil-works/pi-ai` | `d68312ec574bc959cb7fee26da4f4876d067aed9bbbe76e876359e60d2294747` |
| `@earendil-works/pi-durable` | `e76ddd7fdf50d53a9945de87adfd620f5734df6d57a075ef8168948ba6ddd855` |
| `@earendil-works/pi-codemode` | `22f7dabd30eeaac7e38a133b72008ccfc379732f2ed86d760297cb9d385a42db` |
| `@earendil-works/pi-schema` | `a308dfc8981c5badc8e113a97d5390788d6bcc82ed4e2aa2708844438ef88d25` |
| `@earendil-works/chord` | `3967193a8f1bdb79881e4f0715a170853a5946fbb4326f2403529a5a9faedc17` |
| `@earendil-works/pi-telemetry` | `7a28233ebfdd751940619be6a879bda0fe6eb31ac2056e8798e16293c0c39ec5` |

## First review

- Durable adds typed signals, signal-only sleep, waits that resume when any listed task ends, tool parks, nonblocking nested calls and submission notifications. Review these together with persisted generation/tool version upgrades, wake recovery and hook child tasks.
- Compare native lifecycle workarounds against the new abort, waiting and ownership tests before removing them. `session-group-commit.test.ts` is unchanged; v1.6.0 does not remove the between-part publication requirement.
- Review Codemode extension namespaces and the image-fit pipeline in Durable as well as in the standalone packages. The `packages/codemode` and `packages/schema` deltas only touch release metadata; outstanding native gaps still apply.
- AI changes include provider retry rules, resource handling, and the published model catalogue. Port focused assertions before changing the model-regeneration gate.

## Test consolidation and focused receipts

`19e16c5` merged provider cause tests into `tests/error_body_test.go`, source-derived retry cases into `tests/retry_assistant_test.go`, and both generations of line-read tests into `durable/session_read_line_test.go`. Four test files were removed and one renamed. A Go parser comparison found all 63 non-import declarations unchanged. The selected Go tests passed before the move and twice after it; vet, Staticcheck, and diff checks passed. No native assertion was removed or renamed.

The original v1.6.0 files ran unchanged: AI `error-body.test.ts` and `retry.test.ts` passed 73 cases; Durable `session-group-commit.test.ts` and `harness-signals.test.ts` passed 61. The workspace runs used the published, integrity-checked dependency `dist` assets; Vitest's incomplete Bun install was replaced with the exact locked tarball. These are focused source receipts, not native acceptance.

### Connection retry classification

The 13 new cases in `packages/ai/test/retry.test.ts` map to `TestSourceV160RetryConnectionFailures` in the consolidated retry suite. The public classifier now recognizes both Codex WebSocket lifetime-limit encodings, standalone socket codes, native peer-reset wording, and a gateway's `Connection closed.` error. Generic connection limits and socket codes embedded in identifiers are not retried. Nine positive cases failed before the implementation. Dropping underscore from the boundary caused the exact `ERR_EPIPE_ID` negative case to fail. The 52 unchanged source cases, focused Go regressions, vet, Staticcheck, logging and diff checks passed. This verifies classification, not WebSocket reconnection or session-resource handling.

### Retaining queued input on conversation abort

`harness-input-entries.test.ts:258` maps to `TestSourceV160SubmissionOnAbortKeep`. The native test uses the real Harness on SQLite with channel-controlled provider requests. It checks both aborted settlements, the exact retained inbox item and its entry metadata, queued status without a live run, later placement in provider order, and explicit withdrawal of a kept input.

With only the new input field in place, the compiled test timed out waiting for the ordinary queued input: the old abort path required a placeholder generation task, which canonical inbox-only follow-ups do not have. The current path settles eligible inbox-only inputs and removes them in the same transaction. It leaves queued writes and `onAbort: "keep"` items alone. Removing the keep check emptied the inbox and failed exact equality. All 12 unchanged source cases passed, plus focused Go regressions twice (7.999s), vet, Staticcheck, logging and diff checks. Cascade-specific retained-input assertions are still open.

### Published model catalogue

The regeneration gate uses the verified v1.6.0 `pi-ai` archive. Its schema-v6 manifest covers 43 provider files and has SHA-256 `cc46553f3f008788e667600a873d3d83a4df8fe46c67b06c65b16fd828450e8b`. Before the port, the generator rejected the release's structure hash. It now accepts the exact manifest and verifies every provider hash and the record structure. Historical manifest checks remain callable.

The three generated files match fresh generation byte for byte. The public registry comparator matches all 1,698 chat, 74 image and 81 classifier records, not just their IDs. Sample expectations changed only where the published records changed, including Mistral cache pricing, GLM and Kimi metadata, and absent Laguna records. The manifest/provider integrity tests and metadata-corruption self-test passed, as did focused Go tests and 74 unchanged upstream cases in `models-runtime`, `models-entry` and `model-data-validation`. Vet, Staticcheck, logging and diff checks passed.

### Signal definitions

`harness-signals.test.ts:84` maps to `TestSourceV160DefineSignal`. Signal definitions now reject empty or slashed names, zero versions, and non-JSON value types at schema-bearing nodes, including draft-7 dependencies. Annotations are not walked as schemas. A token rejects an empty key and returns the exact signal/key pair for a nonempty key. The constructor's schema is detached from the caller's input.

A compile-ready empty implementation failed the name, version, schema and key assertions. Removing traversal of `dependencies` again failed the nested `bigint` case. The selected unchanged original, focused Go tests, vet, Staticcheck, logging and diff checks passed. This covers construction only; delivery, schema checking of incoming values, waits, expiration and migration are separate assertions.

`Harness.Signal` and `Tx.Signal` now implement delivery to a live task. The JSON payload must pass validate-only schema checking. A missing or closed task does not write a memo, and a duplicate retains the winner at `pi.signal.<name>/<key>`. `TestV160SignalDeliveryStoresOnlyTheWinningJSONValue` holds a running phase, compares the stored memo exactly, and checks delivery after completion. This is a supplemental data-path probe: it does not replace the upstream signal-wait scenario. The compiled empty delivery failed the missing-task assertion, and a duplicate-as-delivered mutation failed the duplicate assertion. Focused Go and static checks passed; five already-red scheduler groups reproduced at the previous HEAD. Signal-driven wakes, deadlines and concurrent/state-change composition are still open.

The first half of the self-signal case (`harness-signals.test.ts:463`) now has a supplemental probe: a task stages a signal to itself and changes to a sleeping state in the same commit. Before the change, the state advanced but its memo was lost. Live candidate replacement now merges earlier staged memos, without retaining them on completing or terminal states. Reverting the merge reproduced the exact missing memo; focused Go and static checks passed. The original's wake and final outcome assertions remain open.

`TestSourceV160SelfSignalWakesWithExactReason` translates the full self-signal scenario. A phase stages the signal and parks without a deadline in one commit. The scheduler stores the consumed memo and running wake reason together before invoking the next phase. Its result exactly matches `{reason: "signal", signal: "test.answer", key: "self"}`. The compile-ready test originally faulted on the missing deadline; suppressing the wake reason failed exact outcome equality. The selected unchanged original, focused Go regressions (8.384s), vet, Staticcheck, logging and diff checks passed. Deadline races, observation across reopen, and retained wakes through repeated checkpoints still need their own translations.

`TestSourceV160SignalDeliveredBeforeSleep` maps the early-delivery case in `harness-signals.test.ts`. `TaskRuntime.ReadSignal` reads the retained payload after the wake consumes its key; the exact result includes both the signal wake and the original value. Missing values and expired keys are distinct from a delivered `null`. The compiled empty reader returned `[null]` in place of an array containing the value, and the targeted fault reproduced that result. The selected unchanged original, focused Go regressions, vet, Staticcheck, logging and diff checks passed. Version migration is wired into the reader but still needs the source-derived migration scenarios.

`TestSourceV160SignalReadAfterWrite` translates the two sequential `Tx.Signal` calls. The earlier delivery stages a table write, so the next call returns `ReadAfterWrite` for `signal` and neither memo persists. Before applying the shared read guard, both calls were admitted. The selected unchanged original, focused Go regressions, vet, Staticcheck, logging and diff checks passed. Parallel source deliveries are a separate ordering assertion.

`TestSourceV160LateSignalExpiresBeforeReservation` advances the injected clock on the Session line and delivers to the sleeping task before any reservation can run. The delivery itself returns `closed` and stores expiration, so the outcome includes exactly the `expired` wake and key state. The compiled red and a disabled-deadline control both returned `delivered`. The selected unchanged original, focused Go regressions (5.974s), vet, Staticcheck, logging and diff checks passed.

`TestSourceV160SignalWakeSurvivesSameCheckpointAndReopen` compares exact running and pending task records and all five phase observations with the original. The wake is retained when a runtime recommits the same checkpoint and through SQLite reopen, then dropped on checkpoint change. The compiled red lost the wake on recommit; retaining it across changed checkpoints failed the final absence assertion. The selected unchanged original, focused Go regressions, vet, Staticcheck, logging and diff checks passed.

`TestSourceV160WaitAnyResumesWithEndedTasks` covers the first `any` wait case. Two children sleep on signals; the second completes, the parent's reservation records only that terminal member, and the parent aborts the remaining child. The compiled red rejected `any` as a policy; a fault that included all members failed exact outcome equality. The selected unchanged original and focused Go regressions passed (9.424s), along with vet, Staticcheck, logging and diff checks. The paused observation/reopen case is separate.

The paused `any` wait now survives reopening with the child definition missing. A blocked child can be aborted without resuming scheduling, and an invocation can settle a blocked owned child without waiting on an unavailable handler. The active wake changes only when a member is terminal, and the parent's result lists that member. The batch also rejects consumed signal keys and empty waits with the pinned diagnostics, and confirms a ready signal supersedes a future deadline after reopen. The focused Go batch passed twice (7.240s), all 21 upstream signal cases ran unchanged, and vet, Staticcheck, logging and diff checks passed. Source runs are not a count of native assertion coverage.

### Submission settlement notifications

The five notification cases in `harness-park-children.test.ts` now map to `TestSourceV160SubmissionNotifications`. A settlement sends the exact public record, including `notify`, under the built-in `SubmissionSettled` signal. A write already settled at admission can notify a task that has never run; two writes settled by one boundary compose both memos into one task write. The production Storage recorder checks co-location and payload equality. Invalid settled-record shapes are rejected and a missing task does not roll back settlement. The focused Go batch passed twice (12.812s), all five selected originals passed, and vet, Staticcheck, logging and diff checks passed.

`Tx.Signals` is the synchronous Go form of starting several signal deliveries together. It decides each request against the earlier staged candidate, preserving request order and the first value for a duplicate key. Sequential `Tx.Signal` calls still reject read-after-write. The grouped-delivery test checks the exact result list and both consumed payloads. Separate version tests check newer-version and missing-migration errors, successful migration, and rejection of an invalid migrated value. The focused Go batch passed twice (8.324s), the three selected originals passed unchanged, and vet, Staticcheck and diff checks passed.

### Tool parks and `pi.tool` version 4

The tool definition now writes v4 and retains the source migrations of previous inputs and checkpoints. Execution can park on signals, time or an owned-task wait. The park stores its arguments and checkpoint, clears slot output, and keeps details and diagnostics. A resumed call records its intent before running again, receives its wake and checkpoint, and measures its duration from the tool task's first run. Invalid parks route through `afterTool` and produce an error result; they do not leave a parked call. Tool argument decoding keeps the UTF-16 exception confined to version-qualified execute and parked checkpoints.

Focused tests cover signal parks live and after SQLite reopen, timed wake with exact 500 ms duration, and mixed, empty, unowned, reused-key and result-plus-park rejections. The grouped Go regression batch passed twice (21.010s), and the five selected originals, vet, Staticcheck, logging and diff checks passed. The historical progression regression now expects the v1.6.0 wait wake and tool version while preserving the other fields. Nested waits, cancel handlers, crash recovery, park authorization in wrappers and the full generation v3 upgrade still need separate verification.

### Cancelling parked tools

A tool can clean up a parked call through `ToolRegistration.Cancel`. The callback receives the validated arguments, checkpoint and execution environment, without a wake. It cannot start nested calls or tasks. A callback error is reported without changing the aborted outcome. An abort that arrives before the park is saved uses the refused park's checkpoint; a missing tool produces the pinned cancellation report instead.

Five unchanged `harness-tool-parks.test.ts` cancel cases passed. The corresponding native tests exercise a parked abort, a throwing callback at restart limit one, a cut-off woken run, a removed registration and a park refused after abort. All five failed on `43f8ee5` with only the registration field added. The focused Go batch passed twice (23.442s); removing the callback checkpoint made three native cases fail. Vet and Staticcheck passed. The nested-environment schema rejection and four historical recovery-deselection failures also reproduce on `43f8ee5`; they remain open, not exclusions. Crash replay of a woken park is a separate packet.

### Recovering a woken tool

A safe replay keeps the details and diagnostics from a woken attempt but clears output. A non-replayable woken attempt calls `Cancel` with the saved checkpoint before settling as interrupted. Tool removal after a park retains the slot details and diagnostics in the unavailable result.

The three unchanged `harness-tool-parks.test.ts` waking cases passed. The native safe and unsafe cases kill the writer process after observing the woken invocation and its committed progress, then reopen SQLite. Both failed before the change; the removed-tool case already passed. The focused Go batch passed twice (11.971s), including the historical safe and unsafe progress cases. Suppressing the recovery cancel produced the expected missing-callback failure. Vet, Staticcheck and diff checks passed. The tests do not map wrapper authority or nested wait behaviour.

### Tool wrapper park authority

A composite tool may return only a park its base returned to the same invocation. The check uses pointer identity, not JSON equality. Separate invocations can use the same park object, but a wrapper cannot copy it or reuse it without calling the base again. Wrapper composition also carries the base cancellation callback.

The three unchanged wrapper cases passed. The native translation splits the first into pass-through and forged branches. On `8d4cd84` the forged branch saved a park and the stale branch reached the consumed-key check instead of rejecting the wrapper. The focused Go batch passed twice (14.404s), as did vet and Staticcheck. Inverting the authority check saved the forged park and refused the genuine one.

### Generation v3 hook decisions

`pi.generation` v3 retains v1/v2 checkpoints and adds the `afterTools` and `yield` waits. Hooks can request ordinary generation-owned tasks. An `afterTools` decision can terminate the round or leave it to consume queued steers normally. A `yield` wait exposes the answer and tasks without settling the inputs. Its first valid continuation wins unless the inbox has a user message or reset. If the answer stands it can pass to `afterRun`; abort preserves the yielded answer without publishing a run failure.

All 12 unchanged `harness-hook-tasks.test.ts` cases passed. Nine native translations cover decisions, report attribution, ownership, the wait policy, the yield marker, queue precedence and abort. Six original native branches compiled on `36f4ed7` with only the public callback fields added and settled the run without starting a child. The grouped Go batch, including execution of imported v1/v2 generations, passed twice (16.013s). Mutations discarding `terminate` and `continue` produced the opposite answer boundaries. Vet, Staticcheck and diff checks passed. The three source recovery cases (hook re-asking before child creation and the two scheduler/restart yield endings) still need native translations.

### Fitting images before tool settlement

`durable/tools.FitImage` and `FitImages` accept a processor, correct MIME claims from the bytes and preserve unchanged payloads. `NativeImageProcessor` uses the pure-Go image pipeline. `CreateFitImagesExtension` fits model-facing tool output to the current agent model's dimensions and byte budget before settlement. It keeps nested images unchanged and appends a single `image` diagnostic after the tool's own diagnostics.

All 11 unchanged `images-fit.test.ts` cases passed. Native tests cover pass-through bytes, processor failure, text order, resizing and notes, byte budget, EXIF, BMP, MIME correction, omission and SQLite transcript results, including nested bypass. No-op API stubs produced compiled failures in the helper and transcript tests. Replacing the model width with the default left the wide result unfitted. The focused Go batch passed twice (5.241s), as did vet and Staticcheck. This packet does not verify large Codemode images which are chunked before the post-tool hook.

### Tool child keys and parked owners

A tool can start nested calls without waiting, then reattach by key across wakes. Ordinary tasks created with `TaskOptions.Key` share the nested call index and can be retrieved only with the same kind and JSON input. Once woken, the tool must supply explicit keys. Closing and reopening a Harness no longer abandons a restart-sensitive child whose owner is parked or still at its call gate. Cancellation blocks both blocking and nonblocking nested starts.

All ten unchanged `harness-park-children.test.ts` cases passed. Four native branches cover nonblocking `any` wakes, an unsafe parked parent across reopen, keyed reuse and cross-kind collisions, and rejection of implicit keys after wake. Three compiled branches failed before implementation; the ordinary two-child wake already worked. The focused regression batch passed twice (18.048s), as did vet and Staticcheck. Mutations reinstating abandonment and accepting changed input reproduced the corresponding failures. Two historical nested-restart test groups still time out at their handler barriers on `847641b`; the remaining source park-child cases are not mapped by this packet.

### Google continuation and HTTP transport

Google Generative AI and Vertex now treat `CONTINUATION` as `length`, retaining the raw reason and ending the stream without another request. Both accept a request-local `http.RoundTripper` and honour a custom ambient transport when there is no override. The standard transport still uses the configured Go timeouts; the SDK's automatic continuation and per-request dispatcher have no native analogue.

The unchanged `google-stop-reason-continuation` and `fetch-option` suites passed eight cases. The native Google tests exercise both adapters and both transport paths, with exact call counts and terminal content. The API-only baseline ignored both transports and misclassified the finish reason. Removing the mapping left both transports working but failed all four terminal assertions. The focused Go batch passed twice (0.266s), as did vet and Staticcheck.

### OpenAI-compatible instructions and Mistral strict tools

OpenAI Completions defaults to the `developer` role only for OpenAI, while explicit compatibility overrides remain authoritative. A top-level `reasoning_tokens` count now supplies the fallback when nested usage omits it or sends zero. Copilot's published model already supplies its `reasoning_effort` mapping. Mistral Conversations opts into strict tool schemas through `MistralCompat`; the default keeps the original `required` fields and sends `strict: false`.

The unchanged OpenAI Completions and Mistral tool-schema files passed 52 cases after restoring the 43 verified npm catalog assets in the oracle. Native tests capture the outgoing roles and effort, consume Copilot usage over SSE, and check the Mistral request before network processing. Six non-OpenAI role cases, both usage fallbacks and the Mistral default failed on the preceding implementation. Restoring unconditional strict mode or dropping the fallback reproduced those failures. The focused Go batch ran twice, and vet and Staticcheck passed. This receipt does not imply native coverage of every assertion in the two source files.

### Responses reasoning-removal diagnostics

Responses and Codex share a terminal parser for `input_transformations`. It stores one diagnostic for reasoning removals other than normal turn boundaries, preserves unknown reasons and ignores malformed reports. It also records removals on incomplete responses without changing the stop reason. A diagnostic without an error omits the error field on the wire.

All five unchanged `openai-responses-input-transformations.test.ts` cases passed. The native loopback cases check both adapters, exact details, timestamp presence, absence of an error field and malformed-input tolerance. Four positive cases failed on `fd153d4`. Removing the turn-boundary filter added the unwanted entries in both the mixed and boundary-only cases. Focused Go tests passed twice, as did vet and Staticcheck.

### Anthropic non-strict schemas and published cache policy

Anthropic's non-strict tool envelope retains `$defs`, `definitions` and `additionalProperties`, while still dropping unrelated root metadata. This preserves property references and whether a tool accepts extra arguments. The exact shape test failed on `43e8f1c`; dropping `definitions` and `additionalProperties` again reproduced both failures. The three unchanged strict-tool-schema originals passed, as did the focused Go batch twice, vet and Staticcheck.

The nine selected `cache-retention.test.ts` catalog cases passed without credentials. The native catalog check also found no inferred warming policy on those models, including GPT-6.2 Astra and Sol. No production cache change was needed for these cases; the unselected live cases are not accepted by this receipt.

### Anthropic message thread lifecycle

Anthropic now selects threads by endpoint or explicit compatibility override, independent of provider aliases. For cached sessions, it hashes the sent settings and normalized message prefix, consumes the prior reply ID once and sends only the suffix on a continuation. Session cleanup clears the bounded thread cache. An explicitly non-continuing replay leaves the current thread intact. Recognized thread rejections resend the full hooked payload once and retain a diagnostic, without retrying unrelated errors or permanently disabling threads after a failed resend.

All 33 unchanged `anthropic-message-threads.test.ts` cases passed. Ten native wire branches cover aliases, gateways, uncached requests, replay, prefix edits, cleanup, thread renewal, disabling, failed resends and unrelated errors. The lifecycle branches failed with only the transport seam, and three retry branches failed before the resend implementation. Removing normalization caused the alias and replay cases to recreate the thread. The focused Go batch passed twice (0.285s), as did vet and Staticcheck. Native translations for the remaining source cases, including concurrent retries and OAuth tool replay, remain open.

### Codex request prefixes and cached effort

Codex sends the initial prompt as a leading input item, without a synthetic default or top-level `instructions`. It honours the model's output-token opt-out and sends at least 16 tokens when a cap is present. Models with mid-conversation effort support keep request effort fixed and carry turn effort in `configuration_update` items, including `none` when appropriate. The assistant retains its provider effort so the next request can replay the same prefix. Cached sockets now expire at 45 minutes.

Six selected unchanged `openai-codex-stream.test.ts` cases passed. The native SSE cases failed before the projection changes, and the WebSocket test now checks three turns with one full request and two deltas. Removing historical effort markers lost the second turn's continuation. The focused Go batch passed twice (0.529s); vet and Staticcheck passed. Existing tool anchoring and context budgets were checked with the new wire placement. Unselected reconnect and replay cases still need separate evidence.

### Codemode extension namespaces

Selected extensions declare namespace metadata independently of tool names. The first declaration of a namespace supplies its metadata, and the first selected extension naming a tool supplies its membership, even when that extension has no namespace. Prompt headings include empty namespaces. Codemode stores explicit membership with the start record; discovery uses that snapshot without granting new tool authority. Prefix-based configuration remains available when no selected extension declares namespaces.

All 49 unchanged cases in `codemode-describe.test.ts` and `codemode.test.ts` passed. The public harness translation checks exact guest discovery output and prompt placement, including first declarations and an empty namespace. The compiled red threw on an unknown description instead of returning `undefined`; removing the first-tool-declaration guard made the namespace filter admit `echo`. The earlier native QuickJS fixture now checks `undefined` for both unknown descriptions while retaining its invalid-argument assertions. This packet does not map all 49 cases or verify namespace changes across an interrupted execution.

### Large Codemode images at tool settlement

Codemode keeps full image bytes available to model-facing tool hooks. It still journals script images in bounded chunks for replay. The tool settlement then chunks the final output if it still exceeds the transcript's inline limit.

A supplemental QuickJS/SQLite test emits a PNG above that limit and checks the resized image and exact diagnostic in both the provider request and the public transcript. Restoring early transcript chunking made the submission unanswered. Focused fitting and Codemode image-chunk tests passed twice, along with vet and Staticcheck. Five broader native Codemode groups remain red with and without this change: generated-image replay notice, large-image snapshot reopen, paid-image uncertain settlement, scoped image global reuse, and failed-image output reopen. No new source-original count is claimed.

### Nested tool phase configuration

Nested tool admission and execution now resolve the captured `AgentChange` through the normal agent conversion instead of decoding it as a stored `agentState`. The strict storage decoder correctly rejected the change-only `CompactionSet` field. This fix leaves the persisted format and the strict decoder unchanged.

Six selected unchanged Codemode and harness-tool originals passed. Three existing native nested groups (environment, restart policy, and active parent attribution) and four Codemode image/settlement groups passed twice after the fix. Reintroducing the invalid decode only in the child executor kept both environment and tool execution at zero. The large-image public snapshot reopen assertion remains red and is separate from agent decoding.

### Codemode transcript image reopen

The SQLite entry row now carries private chunk references alongside its source-shaped model field. Public entry JSON still omits those addresses. After a reopen, Session finalization accepts existing retired Codemode document addresses without trying to validate the discarded run or journal payload; active records still undergo the original checks.

The historical large-image reconnect test first failed on an empty image after reopen. Keeping the references exposed a second failure: the next submission was rejected while validating a retired run tombstone. With both fixes, the reconnect snapshot returns the exact bytes, stays detached, and the next provider call sees the image without rerunning the script. Focused Codemode image and journal groups, the exact entry wire test, selected historical SQLite corpus tests, and the fitting tests passed twice; vet and Staticcheck passed. This change does not reconstruct references omitted by an earlier writer.

### Schema dynamic reference validation

The native JSON Schema check now follows `$dynamicRef`. A named fragment must first resolve to a matching `$dynamicAnchor` before it can select the first matching anchor from the current resource stack. Pointer references and ordinary anchors keep their static meaning. The reference memo key includes dynamic scope so repeated values cannot reuse a result from another resource path.

The pinned Schema run passed 89 selected assertions: both check and error agreement for all 44 `dynamicRef.json` vectors, plus the relative-reference coercion regression. The byte-identical fixture (SHA-256 `dd763124c3d1432977b9c276bf682fa746f82a69b1d9bbc1d79fbdb2a3dda5e4`) supplies 31 native vectors through `CheckJSONSchemaValue`; the Tool API probes also check unchanged accepted values and repeated coercion through an embedded resource. Before implementation, dynamic refs admitted invalid data. Removing outer-scope lookup reproduced failures while keeping static resolution intact. Focused native Schema and Durable signal tests passed twice, as did vet and Staticcheck. The five external-registry groups (13–17), annotations, and remaining compiler diagnostics are not accepted by this packet.

### SQLite copy refusal ordering

SQLite now settles the staged public group before publishing any receipt. If native staging rejects a copy, the backend checks its structural claims without the source payload. A structurally valid copy still reads its source in the SQL transaction after the accepted prefix has been flushed. A missing or mismatched source refuses that part and commits the prefix; a failed SQL read rolls the prefix back and returns `Document copy <id> was rejected` with the original error as its cause. Duplicate IDs and occupied addresses refuse without a source query.

Eight selected SQLite originals passed. The public native tests reproduced both the skipped query and the wrong prefix settlement before the fix. Bypassing the structural guard queried an occupied-address copy and rolled back a prefix that should have committed. The injected SQL error deliberately uses the same Go type as an invalid storage request; the I/O path still rolls back and preserves its identity. Focused copy, historical corpus and reopen cases passed twice, along with vet and Staticcheck. This does not establish all SQLite or viewer obligations.

### Generation hook decision recovery

The three remaining `harness-hook-tasks.test.ts` recovery assertions now have native coverage. The after-tools test kills a host at the hook before it stores a task, then verifies that the next host asks again and commits the child and wait. A third host resumes that exact wait and answers after the child receives its signal, without asking again. The two yield tests use public SQLite task records to drive a restart limit and a scheduler fault. Both preserve the committed answer and leave no live run or run failure.

All 12 unchanged source cases passed. The native recovery tests passed twice alongside the existing hook decision and after-run recovery cases, vet and Staticcheck. Removing the two answered-yield guards changed the public settlements to `unanswered` with `restart_limit` and `faulted` respectively. This packet adds coverage to the production behavior already implemented; it does not change it.

### Fresh generation record fixtures

The pinned `GenerationTask` definition writes version 3 and preserves older checkpoints during migration. Seventeen assertions in thirteen historical native test files now expect version 3 only for newly created generations, including the exact records observed through public commit publication. Imported records, custom versioned tasks, and the other generation comparisons are unchanged.

Before the update, the representative tests failed on the stored version 3. The complete focused batch for the changed expectations passed twice (19.613s), including reset, boundary admission and publication, model error and no-model transitions, abort, polling, and the request record sent to a large local model. Vet and Staticcheck passed. This is fixture reconciliation, not a repo-wide sweep.

### Parked child abandonment after an unsafe caller crashes

A recovered tool's execute phase no longer overtakes a parked nested child's independent restart abort. Ordinary explicit abort still supersedes that intent. A public Storage adapter can expose the materialized document and its delta count without the original revision records; the local synthetic base now carries that count so a child's post-reopen settlement can append a delta. No new fields are persisted.

All ten unchanged `harness-park-children.test.ts` cases passed. The new public Harness/SQLite test kills the caller's process after its nested tool parks. On reopen it checks the settled submission, the child's exact `restart` reason and aborted outcome, the one exact afterTool diagnostic, and absence of `nestedTools`. Supplemental checks compare receipt order and verify neither tool executes again. Removing the scheduling fence loses the restart reason; discarding the projected delta count prevents settlement. The focused native batch, including parked-parent retention, explicit cancellation, adapter bootstrap and delta-history controls, passed twice (8.214s), as did vet and Staticcheck.

### Codemode namespace snapshots during replay

The public Harness/QuickJS/SQLite probe changes the installed extension after a discovery reply and a tool effect are confirmed. On reopen, both the journaled reply and fresh discovery retain the original namespace and ordered membership. The confirmed effect does not run again; a new call to the removed tool is denied; a still-authorised tool that moved namespaces runs once. This is supplemental composition of source start-data and replay contracts, not a one-to-one original.

Seven selected unchanged originals passed: `codemode.test.ts` (finished calls, journaled globals, discovery) and `codemode-describe.test.ts` (first declaration, layout, empty namespaces, harness prompt). Changing the saved startup namespaces broke start identity. Changing only fresh discovery leaked replacement metadata and emptied the original filter. Restored native verification passed twice with vet and Staticcheck. No production change was needed for this packet.

### Codex missing cached continuation over WebSocket and SSE

A failed replay attempt that has not emitted its own start can fall back to SSE without emitting a second logical stream start. The public Go transport test checks stop reason and terminal text, one start and no error, socket order `[1, 1, 2]`, exact delta input, four-item full input without a continuation ID, fetch counts and debug counters.

The two unchanged `recovers a missing cached websocket continuation via %s` variants passed (`websocket`, `sse`). Before the fix SSE recovery emitted `start, error` without issuing SSE. Restoring the old guard reproduced the red; allowing another start produced `start, start, done` on both transports. Focused native verification passed twice with vet and Staticcheck. Started lifetime-limit retry and broader session resources remain open.

### Started Codex lifetime-limit retry boundaries

Three unchanged source variants passed: the limit arrives after response metadata, rate-limit metadata, or partial thinking. The native public-stream and assistant-retry test checks two distinct attempts, one start per attempt, terminal error and retained thinking on the first attempt, an exact recovered result on the second, closure of the first socket and a fresh request without a continuation ID or SSE. The existing provider behavior already passed. Forcing an internal retry collapsed the two public attempts into one for all three variants; the control was removed. Focused provider tests passed twice along with vet and Staticcheck.

### Host-supplied Schema references

`ValidateJSONSchemaValueWithResources` resolves only documents supplied by the host. It uses a per-call registry, retains retrieval URI and `$id` base behavior, fetches nothing, and leaves the existing public check and tool coercion APIs unchanged.

Twenty unchanged assertions ran for groups 1, 2, 3, 10 and 11 of pinned `refRemote.json`: ten validity checks and ten upstream check/error consistency checks. The native path maps ten exact validity cases and checks exact diagnostics for five rejections; it does not expose separate native check and error APIs. All ten native comparisons failed without the registry. Substituting the root base for a remote retrieval URI broke the two nested-reference cases. Focused native tests passed twice along with vet and Staticcheck. The other remote groups, draft 7, external dynamic scope and compiler annotations remain open.

### Caller continuations between Session publications

Go cannot order arbitrary code scheduled after a function returns against another goroutine. `Session.CommitWithContinuation` provides an explicit synchronous turn: the current result is available and its observers have run, but the writer has not published the next part. A turn can read an unaffected task from Storage; it rejects a read that would wait for a later unpublished task. A task read failure or an explicit continuation error fences subsequent publication without revoking the current result. Ordinary `Commit` does not add this turn.

All three selected unchanged `session-group-commit.test.ts` originals passed (698, 770 and 796). The native public Session/Storage tests store the second and third parts in one call, check that the second continuation sees exactly two publications, and verify that both failure paths publish only `lead, one` and reject `two` with the unchanged cause. The inert API stub produced compiled failures. Delaying the continuation until all parts had published made every case observe the third publication. Focused native publication tests passed twice (4.277s), along with vet and Staticcheck.

### Codex socket partition and cleanup

The public stream probes check cached handshakes for two authenticated accounts, exact account and authorization headers, reuse counters, and the lack of an SSE request. With retention `none`, each request uses and closes a fresh socket, omits the prompt cache key, and creates no session debug record. Both unchanged source originals passed. Removing either partitioning or one-shot retention made the native handshake count incorrect.

The supplemental explicit-close probe found bare EOF instead of the source's `1000/debug_close`. The provider now sends the normal close frame before cancelling its reader, closing both target account sockets without closing the other session's socket. Close and scoped debug reset remain separate. Replacing the handshake with immediate close failed on both target sockets. Focused provider verification, vet and Staticcheck passed.

### All draft 2020-12 host registry remote references

The host registry test now covers all 31 validity vectors across 15 groups from `refRemote.json`. The nine added host documents are byte-identical to the pin. All 62 unchanged originals passed (31 validity and 31 error-agreement variants). The native test checks validity and exact rejection diagnostics through one public validation path, not two independent APIs. Incorrect folder rebasing and a missing canonical URN registration each failed an otherwise valid case. Both controls were restored; no production change was needed. Draft 7 remote references, external dynamic scope and annotation/compiler coverage are separate.

### Pinned npm dependency graph

The [dependency ledger](dependencies/README.md) records the npm lock closure from AI, Durable, Codemode and Schema: six workspace entries including Chord and Telemetry, 305 npm placements and 546 declaration edges. Of those edges, 521 resolve and 25 have no optional-peer slot. Those absences are not test exclusions. The generator revalidates pinned inputs and six workspace archives offline; the integrated regeneration was byte-identical. Controls rejected altered input bytes and incorrect hoisting. Third-party archive bytes, native correspondence and package-external/platform evidence remain open.

### Cold task reads through public Storage

An absent task in a delegated Session's projection is not an authoritative negative result. The scoped Session and transaction reads now use the backend for a cold task ID, after the relevant publication gate. `ReadLine.Task` exposes the same committed read at the line boundary. Adoption still supplies cached records.

The unchanged source case at `session-group-commit.test.ts:486` passed. The public Go fixture holds a real Storage commit, makes a cold task lookup fail, then releases a different write failure. It verifies no publication and checks the original read cause through both the later call and the Session end. Before the fix the transaction returned absence. Bypassing the guarded read made the later write error win. Focused tests passed twice (1.939s), along with vet and Staticcheck.

### Codex WebSocket read idle limits

A WebSocket wait respects the configured idle interval, restarting it when the next frame arrives and keeping caller cancellation separate. Before any output, the provider records one failure and makes one SSE fallback. After start it reports `WebSocket idle timeout after 50ms` with no SSE for the pinned fixture. Both selected unchanged originals passed. The compiled baseline reached caller abort; disabling the deadline or doubling the interval failed the controls. Focused integrated provider tests passed twice with vet and Staticcheck. Connect-timeout dial evidence and the wire-level idle close reason remain open.

### External dynamic Schema scope

The external host registry already supports the pinned draft 2020-12 dynamic resource cases. Groups 13–17 add 13 exact native validity and diagnostic cases: strict tree, extendible array, both declaration orders and the detached dynamic anchor. All 26 selected unchanged originals passed. The fixture and the three external documents match the pin; ignoring outer dynamic scope wrongly accepted four values. Integrated tests passed twice. Independent native check/error agreement, draft 7 and compiler annotations remain open.

### Third-party archives, tranche 1

The [offline archive verifier](dependencies/archives.md) matched pinned SHA-512 for seven of nine scoped retained archives. The verified QuickJS-WASI archive contains the same WASM bytes as the native embedded asset. Neither archive presence nor byte identity alone proves native behavior or which bytes a past Oracle run loaded. OpenAI and Google GenAI archives were not present at the exact integrity keys in the checked caches. Offline regeneration matched byte for byte; tampered and empty-cache controls are recorded separately.

A supplemental public Storage scope witness found that the next scope was still requested after a continuation failed, although it did not publish. The writer now stops before requesting that scope. Both failure fixtures issued three requests before the fix; the restored focused test checks exactly two. Existing deferred-scope failure checks still pass, as do focused vet and Staticcheck.

A supplemental public read test covers the transaction's memoised task lookup (`transaction.ts` committed task read). The same transaction and line callback each read a missing ID once; a subsequent public Session read still consults Storage. The memo is scoped to the adopted image and returns detached records. Before the fix, and with memoisation removed, the first transaction issued two requests. Focused tests passed twice with vet and Staticcheck.

### Exact retained-child and paired-memo comparisons

The parked-child source slice now compares the exact retained-child text `x: kept` after reopen and the complete paired-settlement memo key list in the durable batch. The focused Go park, crash-recovery and notification cases passed twice (6.851s), with vet and Staticcheck. All ten unchanged originals were already verified at the same pin; these are stricter native comparisons, not additional original cases.

### Codex WebSocket connect timeout

A stalled upgrade now yields exactly `WebSocket connect timeout after 50ms` in the pinned fixture, makes one dial and one SSE request, and delivers one logical start and the recovered first text. Caller cancellation does not trigger fallback. The baseline returned wrapped network text and redialled if a retry budget was available; separate mutations reproduced each failure. The unchanged connect-timeout original, integrated native tests, vet and Staticcheck passed. Peer-visible idle shutdown and other timeout-normalisation cases remain open.

### Draft 7 host-registry references

The host registry accepts an explicit draft 7 reference policy. A `$ref` ignores sibling constraints and rebasing, and a fragment-only `$id` registers its anchor without replacing the enclosing document. The default draft remains 2020-12. All 23 draft 7 remote validity vectors and 11 invalid diagnostics passed in Go. The 56 selected unchanged originals include the 46 remote validity/error-agreement cases and 10 from the sibling-policy groups. The remote fixture alone was already green; supplemental draft-policy cases exposed the compiled reds and distinguished four controls. This packet does not verify a separate native error API or all draft 7 compiler keywords.

### Retained Oracle and native input correspondence

The [native input receipt](dependencies/native-inputs.md) verifies retained QuickJS and Photon assets, selected AI/Chord archive members, and Vitest launch files. It reproduces the pinned SQLite migration statements byte for byte and records the separate `modernc.org/sqlite` runtime. Integrated offline regeneration matched exactly. These are asset, module and focused test receipts, not historical module-load traces or package-wide native parity. The [provider archive receipt](dependencies/provider-archives.md) now verifies both exact Google GenAI and OpenAI tarballs against the pinned lock SHA-512 values. The module-load gap remains open.

### Queued generation outcomes at withdrawal

Three selected unchanged ownership originals (`harness-ownership.test.ts:656`, `:695`, `:804`) and the two input-entry abort originals (`harness-input-entries.test.ts:237`, `:258`) passed. The SQLite legacy queue fixture, which gives each waiting input a generation task, was red in all three scope cases: the aborted task lacked the canonical outcome required by Storage. Explicit withdrawal and scheduler cascade now commit the outcome with the submission and inbox changes. The scoped fixture checks a public persisted task outcome and `unanswered/aborted` with no entry or answer. Removing the task outcome reproduced the three failures.

The write-budget probes distinguish refusal without partial adoption from successful atomic withdrawal and task document retirement. The old budget fixture's write row now starts pending instead of claiming a finished write with no entry. The legacy queue checks do not map all of the upstream scheduler or publication timing: the fixture verifies passive writes remain available and are eventually placed, rather than checking the original's immediate `queued` status. The public canonical `onAbort: keep` control still passes. The separate owned-descendant join fixture and four tool recovery reselection cases remained red at the start of this round and are not closed here.

### Codex idle close and cache age

The two unchanged read-idle cases and the cached connection age case passed. The cache age fixture checks two connections, send order `[1, 2]`, two created, and zero reused. Its native baseline already passed; doubling the age cap instead reused connection 1. A supplemental wire probe covers `1000/idle_timeout` for cached fallback and one-shot timeout. The baseline saw EOF; a wrong close reason and waiting for the peer to acknowledge the close both failed. The peer now sees the close while stream settlement and fallback proceed without waiting for its acknowledgment. The fixture keeps caller cancellation distinct. Cache idle and age eviction close reasons and the pre-output lifetime retry remain open.

### Draft 2020-12 unevaluated array items

The pinned `draft2020-12/unevaluatedItems` slice passed all 142 unchanged cases: 71 exact validity checks and 71 source error-agreement cases. The byte-identical fixture maps all 71 validity values through public `CheckJSONSchemaValue`; 27 failed on the compiled baseline. Evaluated indexes are scoped to an array instance and combine successful branches, `contains` matches, and reference annotations. Three mutations distinguished failed-branch leakage, dropped dynamic-reference annotations and dropped `contains` matches. The separate native diagnostic tests assert three paths; they do not map the source's independent error agreement.

Round 6 focused Go tests for Durable, Schema and Codex passed twice, along with selected validation and strict-schema tests in `./tests`, `go vet`, and Staticcheck v0.8.2. No broad acceptance suite or profiling was run.

### Restored Durable recovery receipts

`harness-tools-recovery.test.ts:229` passed unchanged. The native reopen fixture now checks canonical terminal tool receipts and their linked transcript entries, rather than a private checkpoint that canonical storage no longer retains after terminalization. It checks `done`, exact cwd sequence, the result error flag and interrupted text. Clear cwd, revoked callers, removal from the model's offer, and deferred replacement are native controls. Unchanged production passed; disabling safe replay made the retained cwd case run just once.

The queued descendant fixture initially admitted a model-visible write into an idle legacy fixture, causing an inbox boundary to place the input before cancellation. A data-only passive write keeps the input queued. The corrected witness verifies zero placement before the child joins and `unanswered/aborted` with no entry or answer afterwards. This also passes unchanged production. Two separate older failed-run fixtures still reproduce `deterministic handler/commit barrier timed out` and `task owner missing` on the Round 7 baseline; they were not counted as passing.

### Codex lifetime rejection before output

The unchanged code-only rejection original (`openai-codex-stream.test.ts:1966`) passed. Public Go streaming reconnects once over a fresh socket, resends identical `response.create` bytes without a continuation ID, and produces one start and successful completion with exact usage and zero SSE requests. This is coverage only. Inverting the reconnect guard instead made one dial, one send, and an unwanted SSE request.

### Codemode worker global argument envelopes

The declared `spread` flag controls argument decoding on the Go worker adapter. Spread globals still accept only arrays; a native negative control checks that a malformed scalar never reaches the host. Non-spread globals can now send a single JSON value. The compiled probe used to reject the pinned string with `cannot unmarshal string`. Removing the array restriction or selecting the wrong declaration failed the controls. Undefined and bare-null legacy replay identity are unchanged.

The unchanged namespaced-global and undefined originals passed across the worker, inline and remote backends on integration. The lead's published v1.6.0 QuickJS worker bridge also returned the exact `[ ["list", "first"], "a", false ]` source result. The committed test fixes the worker protocol events and checks exact native calls and replies; it does not rerun that full published bridge from the Go suite.

### Google GenAI loaded member

The [loaded-input receipt](dependencies/google-sdk-load.md) proves Node returned the exact 1,198,102-byte provider archive member for one unchanged pinned Oracle case. A changed member still passed the behavioral case, but the separate provenance check rejected the digest. Both outcomes reproduced offline. Historical loads, OpenAI and transitive inputs remain open.

Round 7 focused integrated Go tests passed twice, along with vet and Staticcheck v0.8.2. No broad acceptance or profiling was run.

### Delegated cold submission reads

The public Session and Tx witnesses exercise both a submission ID and a request key (`session/transaction.ts`, committed submission reads). All eight compiled baseline probes reported not found without consulting Storage. A miss in a delegated read image now falls through to the corresponding guarded backend read. The witness checks exact record values, one read of the right key, and sealing with the original cause on failure. Bypassing the guard returned the cause but never failed the Session. These are supplemental probes, not new mappings of the `session-group-commit.test.ts:486` task original.

### Failed-run inbox policy and join

All four unchanged `harness-failed-run-inbox.test.ts` originals passed. The exact native comparisons cover the first `unanswered/model_error`, default retention and resumption, `second answer` for `continue`, a write-only boundary that places the note on `continue` but retains it on `withdraw`, and `unanswered/run_failed` without an entry or answer. An additional held-owner check attaches a live child to the active canonical generation and shows that both submissions settle while the failed outcome still waits for the actual child return. This is coverage of unchanged production. The two older raw-hold and queued-generation ownership fixtures remain open; they do not represent a newly queued canonical input.

### Codex normal close on cache eviction

The unchanged age and two read-idle cases passed. Supplemental wire tests age the socket before the next request or invoke the matching idle expiration callback. They now see `1000/connection_age_limit` or `1000/idle_timeout` rather than bare EOF. The next socket sends before the old peer acknowledges; exact close-reason and blocking-handshake controls failed. The original age assertion mappings remain unchanged.

### Draft 2020-12 unevaluated object properties

The pinned `draft2020-12/unevaluatedProperties` slice passed all 258 unchanged cases: 129 validity checks and 129 independent source error-agreement cases. The byte-identical fixture maps all 129 values through `CheckJSONSchemaValue`, six of which failed on the compiled baseline. Successful child schemas contribute keys through static and dynamic scopes, without leaking failed branches or sibling object annotations. Three mutations distinguished nested propagation, dynamic contributions and failed-branch leakage. Four native diagnostic cases are supplemental; independent error agreement remains open.

### OpenAI loaded member

The [loaded-input receipt](dependencies/openai-sdk-load.md) proves one focused, unchanged Oracle case loaded the exact 787-byte OpenAI 7.59.0 archive member. The expected behavior-pass/digest-fail control and the normal receipt reproduced offline. The copied closure is not a module-load list. Historical and transitive loads remain open for both SDKs.

Round 8 focused Go tests passed twice in Durable, the Codex provider, the root Schema package and integration validation tests. Vet and Staticcheck v0.8.2 passed. No broad acceptance or profiling was run.
