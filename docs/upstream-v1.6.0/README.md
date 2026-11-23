# pi v1.6.0 parity target

The upstream target is [pi v1.6.0](https://github.com/earendil-works/pi/releases/tag/v1.6.0) at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The prior [v1.5.2 notes](../../notes/upstream-v1.5.2-restart.md) and [v1.5.0 acceptance evidence](../upstream-v1.5.0/) are historical. The model-regeneration gate is pinned to v1.6.0. The repo-wide acceptance gate has not been switched. See [what still blocks a parity claim](acceptance-gaps.md) for checks that need access or approval and the unresolved behavior differences.

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

### Signal sleep graph and task endings

All 21 unchanged cases in `harness-signals.test.ts` passed. The native task graph omitted ordered signal keys from a sleeping node. It now sends those keys in the watcher update and returns detached snapshots. The compiled baseline failed on the missing `signals`; reversing the projection's keys failed the order check. The native witness also checks inactive wake, sleeping inspection and the exact delivered result. Separate public SQLite checks cover deadline expiry, explicit abort, and wake after reopen without first enabling scheduling. The ledger keeps source sites without an exact native comparison open.

### Codex normal continuation deltas

The unchanged cached delta case passed. Public native streaming now checks the same two full/delta arrays and custom tool output type, both explicit `store: false` flags, absent versus null first continuation, and all nine source counters. This is coverage of unchanged production. Changing the output type or adding explicit null failed the intended assertions. Eight partial Codex sites remain in this ledger slice: four developer prompt lines, request effort, output token cap, and two cached effort delta lines.

### Codemode namespace prompt receipts

Six unchanged selected declaration originals passed. Native system entries and QuickJS now map prompt order, empty catalogue, deferred stability, truncation warning, per-conversation sections, and exact constrained-sampling grammar. Both extension orders preserve first-declaration membership. These are coverage-only checks; four compiled controls distinguish deferred leakage, last-declaration precedence, grammar bytes and the missing budget warning. JS reference identity on the membership result stays partial because the Go guest receives detached values.

### Retained provider catalogue assets

The [catalogue receipt](dependencies/provider-catalog-assets.md) compares all 43 selected provider JSON files, the schema-v6 manifest and the generated model file against the verified pi-ai archive. All matched, and the native regeneration manifest matches the archived member. A valid JSON edit and a substituted filename with the same count both failed the verifier. This reproduces retained data, not historical module loads or a new model regeneration.

Round 9 focused integrated tests passed twice, and vet, Staticcheck v0.8.2 and the offline catalogue receipt passed. No broad acceptance or profiling was run.

### Exact Durable signal lifecycle and restart comparisons

The remaining cases cover public result codes, including delivery to a still-live abort-marked task; two ordered sleeps with `b` delivered before `a`; exact refusal to reuse an expired key; and a crash-shaped SQLite seed whose wake and restart count survive the same checkpoint but clear on progress. These are coverage of unchanged production. Together with the earlier signal suite they map all 54 scoped signal assertion sites. The full 21-case Oracle file previously passed unchanged on the same pin.

### Exact Codex prompt, effort, and cap requests

Four unchanged selected originals passed. The native probes compare complete developer/user inputs, absent `instructions`, historical effort ordering from the actual first SSE response, token cap and null-versus-absent fields, and the three cached low/high/high turns. Four discriminating compiled mutations failed. All 79 sites in the current Codex ledger slice now map. Unseeded Codex and other providers are outside that count.

### Observed QuickJS compiler input

The [WASM load receipt](dependencies/quickjs-wasm-load.md) reproduced one pinned inline Oracle test compiling the exact 637,405-byte QuickJS-WASI asset, also embedded by the native package. A valid extra custom section preserved behavioral success but failed the separate digest check. This is one current Oracle compile, not a native load trace or worker/remote acceptance.

The Round 10 signal, Codex and QuickJS checkpoint passed focused integrated Go tests twice, vet, Staticcheck v0.8.2 and offline receipt reproduction. The Codemode journal lane was not included in this checkpoint. Broad acceptance and profiling were not run.

### Tool approval gates before execution

All nine unchanged `harness-tool-gates.test.ts` originals passed. The canonical tool task now saves the `call` sleep and slot status before recording execution intent, then runs the current ordered `beforeTool` chain on wake. Invalid waits go through `afterTool` and settle without running the tool. Registry change and reopen wake only gate sleeps, not an `execute` park. The process-local revision is taken from the phase's actual registry snapshot; adopting a sleep after that snapshot was replaced makes the gate runnable again. A deadline already reached still expires its keys. The legacy pre-v4 adapter refuses the sleep rather than executing past the gate.

The compiled baseline executed an unapproved call and accepted an empty sleep. After adding sleeps, three registry/open cases still failed to wake. Stamping the current registry at commit instead of the captured phase snapshot reproduced the stale-gate failure. The focused native test verifies the second sleep's fresh key, three gate passes, and exactly one tool result. No real shell command is executed by the test.

### Codemode journal readback across SQLite reopen

Three unchanged originals passed: journaled globals, undefined globals, and an already decided outcome. The native run distinguishes omitted non-spread arguments from `null` in persisted identity and retains compact result JSON without reordering properties or normalizing number spelling. Exact `[1,2]` and `[true,true]` receipts survive close/reopen without repeated effects. A separately gated `AfterTool` leaves an outcome committed but unpublished; restoring it publishes exactly `first` without starting QuickJS twice. Wrong presence, null result, formatting, forged spread, changed outcome, and rerun controls failed as intended. Omitted arguments in the official worker adapter remain open.

### Google Vertex key placement and option resolution

All nine unchanged `google-vertex-api-key-resolution.test.ts` constructor cases passed and have offline native transport probes. The pinned SDK places Vertex API keys in `x-goog-api-key`, not in the URL. The native path now matches that wire behavior, falls through from whitespace-only options and preserves explicit ADC markers and caller headers. Wrong query placement, blank-option precedence and header replacement each failed controls. Gemini Developer API behavior is unchanged. The original's SDK constructor records are distinct from the Go transport surface; ephemeral-token and live credential/platform behavior remains open.

### File-level parity inventory

The [pinned test-file inventory](test-inventory/README.md) reproduced 1,736 scoped Git paths, including 214 active added or modified suites. At its recorded baseline, five of those 214 had a ledger slice, and 209 had no ledger rows. The first primary queue is 21 AI and 16 Durable suites; some have earlier receipts that still need reconciliation. These are file counts at the recorded baseline, not claims about suite execution, semantic status, or a parity percentage.

The integrated gate, journal and Vertex checks passed twice, along with vet, Staticcheck v0.8.2 and offline inventory reproduction. Broad acceptance and profiling were not run.

### Parked scopes age their retained context

All 25 unchanged `harness-idle-scopes.test.ts` originals passed. A translated parked-scope case showed that native context retention still treated parked work as active. The idle index now tracks context activity separately from idle-wait targets. Committed sleeping, waiting and completing work lets context age, while runnable descendants still keep the scope busy. The parked task still blocks `WaitForIdle`. The compiled baseline retained a busy range after the sleep receipt. The exact native range checks and the 200-step randomized recompute passed. Reverting the context/index changes reproduced the existing seven legacy fixture and wrapper-authority failures outside this packet; they remain open.

### Directory listing under concurrent replacement

The unchanged directory-race original passed and its offline [receipt](test-inventory/env-node-list-race.json) reproduced. A native rename is placed between enumeration and the reclaimed sidecar's `lstat`, without a timer. The baseline returned `not_found` for the whole listing; the Go environment now omits only the vanished enumerated entry. A missing directory and cancellation during entry inspection still propagate as errors.

### Responses strict tools and session affinity

All 42 unchanged `openai-responses-compat.test.ts` originals passed on integration. Ten have exact new transport mappings: four Cloudflare/Copilot strict variants, required `additional_tools`, Copilot reasoning omission, and four affinity cases. Generated affinity now uses `session-id`; retention `none` suppresses generated headers while explicit overrides and Azure behavior are preserved. Four native affinity cases reproduced baseline wire divergences. Five faulty request variants were detected. Azure `prompt_cache_key` under `none` and the remaining untranslated originals are not closed by this packet.

### Codemode worker argument presence

Six unchanged named cases across worker, inline and remote passed. The committed public worker translation distinguishes omitted and explicit `null` non-spread arguments on new starts; bare-null or omitted spread arguments remain invalid. SQLite compares replay identity under both persisted modes while host capability changes. Old starts retain their old projection. A separate byte-verified published-worker probe observed exact `[true,true]` twice. Four discriminating controls failed as expected. The full three-backend result remains distinct from the adapter tests.

### Primary changed-suite receipt reconciliation

The [primary queue](test-inventory/primary-queue-reconciled.md) reconciles the 37 files without ledger rows at the inventory baseline: 15 with prior selected-source receipts, one new exact original receipt and 21 with no receipt in the audited records. Three conditional areas were withheld, not excluded. This is historical evidence, not the current total of mapped tests.

Focused integrated Go tests passed twice, as did vet, Staticcheck v0.8.2 and offline receipt reproduction. No broad acceptance or profiling ran.

### Harness provider cleanup and scheduling boundaries

All 17 unchanged `harness-lifecycle.test.ts` originals passed. The native Harness now cleans up only provider session IDs used by its invocations after they actually join, including on automatic failure close. Another Harness and an unused conversation are not swept. The three compiled resource cases initially leaked every allocation, and cancelling a stubborn host was insufficient to allow the early-cleanup mutation. Cleanup panics no longer prevent later provider callbacks; aggregate causes remain available. `Harness.Closed` waits for this shutdown without initiating it.

A passive submit now requests scheduler progress, as the other successful public progress calls already did. The translated marker test ran six callers on the compiled baseline; passive submit alone left the marker paused. Reads, watches and document acquisition leave a paused Harness paused. The separate late-registration case observes `missing_task`, a ready pending task after registration, and exactly one invocation of the replacement on resume. Timing for failed admission is not inferred from successful calls.

### Responses remaining wire defaults and costs

The full 42-case Responses compatibility file passed unchanged. Native public HTTPS fixtures preserve endpoint authority and cover the remaining 27 affinity, clamp, reasoning and token-cap cases, plus all five service-tier cost cases. The five tiers compare transmitted values and the three cost components at the pinned 12-decimal precision. Production already passed; mutations distinguish absent fields, wrong cache-key length, reasoning support, and returned versus requested tier precedence. This closes the scoped file, not other Responses contracts or live platform behavior.

### Codemode completion order and fork store

The selected unchanged completion-order, fork store and text-layout cases passed. Native reopen already returned the exact completion order `[2,1]` without repeated finished effects. Fork store now uses separate public header and result blocks; the provider path preserves both in OpenAI Completions, joining textual blocks in source order with LF. Before the fix the fork probe could not see the separate body, and the compiled wire test lost it in Completions. Native controls for altered replay order, fork policy and merged output failed as expected. Explicit empty JavaScript output shape is separate.

### Request-local HTTP transport

The exact six-source [fetch-option receipt](test-inventory/fetch-option.md) reproduced offline, and [native adapters](test-inventory/fetch-option-native.md) exercise all six source groups via the public request-local transport. OpenAI, Azure, Mistral, Codex SSE and pi-messages retain ambient defaults when no override is supplied, and a request-local override does not affect WebSocket dialing or global clients. Native OpenAI and direct-HTTP grouped controls failed before the fix. The Google ambient request test has no separate native Undici dispatcher field.

The integrated focused Go tests passed twice, vet and Staticcheck v0.8.2 passed, and the offline fetch receipt reproduced. No broad acceptance or profiling ran.

### Scoped conversation and submission reads

On a conversation projection miss, Session and transaction reads consult delegated Storage through the failure guard. `Harness.Conversation` now uses the scoped read instead of a full snapshot. `ReadLine` exposes the same conversation and submission lookups. Its transaction-local memo holds both hits and misses and clears on a newly adopted image.

These are supplemental public boundary probes for `transaction.ts` and `harness.ts`. Eight compiled hit/failure branches failed before the change. The exact backend failure survives Session termination; bypassing the conversation memo increases the transaction's backend reads from one to two. A separate probe verifies that the next read scope fetches its own miss.

### Thinking levels, diagnostics and Together models

The 15 unchanged `max-thinking.test.ts` cases and 58 unchanged `supports-xhigh.test.ts` cases passed. Native tests verify ordered levels, explicit disabled entries, catalogue metadata and eight Codex max-effort payloads captured before transport. No production change was needed for these cases.

The Schema tests verify the exact first diagnostic for two `unevaluatedProperties` groups. The paired check/error ledger rows remain partial: the native API returns a first diagnostic, while the pinned explainer emits an ordered list. In the dynamic-reference probe, the source emits `/bar` and `/baz` when the native validator reports only `baz`; both accept the same `bar` when `baz` is absent.

The [Together model receipt](test-inventory/together-models.md) reproduces two unchanged originals offline. The native comparisons use the published Kimi K3 limit of `1048576` and distinguish absent developer-role metadata from explicit `false`. The separate dummy-key case remains open in this receipt.

### Prompt cache-retention payloads and model policy

All 35 byte-verified `cache-retention.test.ts` originals passed in a network-isolated namespace. Four credential guards used synthetic keys; the source payload hooks stop before sending. Native coverage uses request-local transport to capture 26 serialised payloads and checks nine model records. It distinguishes missing fields from explicit `null`, compares cache options as whole objects, and checks Anthropic TTL, OpenAI affinity and Cerebras strict field absence. The unchanged native baseline passed; all six fault controls discriminated. The focused tests passed twice after restoration. This run did not use live providers.

### Bounded context, recursive coercion and Fireworks

All 22 unchanged `harness-context.test.ts` originals passed. The supplemental delegated adapter probes use public `ScanEntries` at a pinned cutoff. They verify same-sequence projection changes, scoped request-error classification and exactly two ordered pages at a historical cutoff. Clearing the cutoff lets the third entry through and fails the test. The delegated scan remains serial on the Session line.

Four selected recursive coercion originals passed. Native tool validation checks eight exact parse outcomes and twenty repeated conversion and input-preservation cases. Standalone coercion, compiled-instance reuse and JS schema-object cycles remain open.

The [Fireworks model receipt](test-inventory/fireworks-models.md) reproduces three unchanged cases against the verified release catalogue. It checks the DeepSeek Messages route, GLM Fast alignment and Kimi's exact compatibility and `max` request. The 22 unselected Fireworks cases remain open. Focused integrated Go tests, vet and Staticcheck v0.8.2 passed.

### Live portable Codemode guest

All three unchanged namespaced-global originals passed (worker, inline and remote). The native test starts the published portable guest through `RemoteCodemodeSandbox` and checks exact terminal output and all callback arguments, including the nested `null` and the empty spread array. It ran twice against the verified v1.6.0 package and QuickJS WASI 3.6.2. Two controls independently broke the return value and callback arguments. This covers the portable guest path; equivalent native inline execution and journal replay through this guest remain open.

### Durable idle and owned-host fixtures; Qwen plans

Six stale Durable fixture groups now pass with valid task inputs and outcomes. The owned-host fixture asserts that the actual child ID stays in the idle wait target set after queued input withdrawal. The delegated context wrapper regression was closed separately by the scoped scan change. `TestFailedRun140BoundedWithdrawalRecovery` remains red: the canonical end-run transaction attempts atomic withdrawal of 20 inputs against `MaxWrites=16`, reports `invalid write count`, and publishes a terminal failure instead of the expected held decision. The pinned source withdraws the queue in the end-run transaction; a bounded recovery design must reconcile that atomic boundary with the native write limit.

The [Qwen receipt](test-inventory/qwen-token-plan-models.md) reproduces ten selected unchanged catalogue cases against all four verified plan assets. Native tests compare exact plan membership and reject same-count ID substitutions. The other 110 Qwen cases were outside this selection. Integrated focused Go tests, vet and Staticcheck passed. The cache-retention ledger adds 50 sites (33 mapped, 17 partial); 1,045 total scoped locators validate against pinned bytes.

### Atomic failed-run withdrawal across reopen

All four unchanged `harness-failed-run-inbox.test.ts` cases passed again against the byte-verified v1.6.0 source. The canonical generation ends the run and withdraws its queued inputs in the same transaction. The older Go test expected a legacy intermediate held decision, which the canonical source does not publish.

The replacement Go probe checks one publication: the placed input settles with `model_error`, then all 20 queued inputs settle in reverse inbox order with `run_failed` and no entry or answer. The queued write survives and is placed only when a new input runs after SQLite reopen. Provider calls are exactly one before reopen and two after the fresh input. Disabling withdrawal changes the publication to one settlement and fails the probe. The existing small-budget atomicity and document retirement probes passed without changing `MaxWrites`. Oversized canonical commits remain a native resource-limit difference; the Oracle does not exercise the Go-specific limit.

### Ending owner rejection

The unchanged `harness-ownership.test.ts:582` case passed. The native translation stages a conversation and its first task while ending the owner. It reproduced a `faulted` outcome with an extra `durable: rejected:` prefix. Final owner admission now persists the exact source message and remains distinguishable as a no-write rejection to Go callers. The refused transaction publishes neither child and starts no child phase. The pre-fix overlay reproduced the prefix mismatch; focused ownership checks passed twice. The three other ownership groups that failed the wider selector also failed against the unchanged baseline and remain open.

### Inline QuickJS and provider affinity

The three unchanged namespaced-global cases passed again. Native tests ran twice through both bundled inline QuickJS and the verified published portable guest, matching the full result and callback values. The independently hosted upstream Node worker topology and guest-hosted journal replay remain open.

All 27 unchanged OpenAI Completions prompt-cache cases passed offline. The native adapter now honours OpenAI no-session and OpenRouter session-ID formats while preserving cache field, retention, opt-out and explicit-header behaviour. Three wire cases failed before the change. The catalogue generator now preserves explicit format overrides; the lead verified unchanged generated chat, image and classifier outputs.

The [Fireworks loopback receipt](test-inventory/fireworks-loopback.md) reproduces ten unchanged Messages cases. Native Fireworks and OpenRouter now emit the expected affinity headers; storage, tool payload and generated catalogue data are unchanged. These ten cases and the earlier three catalogue cases leave 12 Fireworks cases open. Integrated focused Go tests and the Anthropic package passed twice; vet and Staticcheck v0.8.2 passed.

### Guarded conversation owners and historical ownership fixtures

Conversation creation and forks keep staged owner records authoritative. For a committed owner, delegated Storage now supplies the task record through the Session guard, even when the validation image already contains it. All three new public probes failed before the change: the backend was skipped for a hit, an authoritative miss, and a failure. Each now performs one lookup; a read error becomes the Session's exact first failure and admits no creation. The immutable v1.6.0 transaction source pins the lookup order. These are supplemental adapter probes.

The three previously red ownership groups now pass with unchanged ownership enforcement. Canonical SQLite uses a complete terminal task receipt, the no-execution legacy case uses the historical SQLite frame format and verifies reopen, and paused inspection expects the source's `ready` migration flag only after dependencies drain. Suppressing that flag broke all four legacy generation variants. The two selected unchanged task-document originals passed.

### Portable guest journal replay and complete Fireworks oracle

The unchanged undefined-result original passed again. The published portable guest ran the exact script across a real SQLite close and reopen. The native result was `[true,true]` with no error, and two guest starts produced exactly two global effects. The fault control converted the first omitted reply to `null` and produced `[false,true]`. Other worker-hosted journal cases and the separately hosted Node worker topology remain open.

The [remaining Fireworks receipt](test-inventory/fireworks-remaining.md) covers 12 unchanged originals. Across the three disjoint receipts, all 25 Fireworks originals passed offline. Eleven of the latest 12 cases have full native checks; the dummy-key case still lacks a Go public equivalent for the source's exact environment-key list. Custom Messages affinity formats and live-provider behavior remain open.

The OpenAI Completions prompt-cache ledger adds 60 mapped sites across 27 executed original variants. It records native HTTP values and presence checks separately from the source SDK mock. The integrated focused Go tests passed twice with the live portable guest; vet and Staticcheck v0.8.2 passed. A total of 1,115 scoped assertion locators validate against pinned bytes.

### Responses prompt-cache keys at the original proxy URL

All four unchanged `openai-responses-prompt-cache.test.ts` cases passed offline. Go already matched their behavior. The strengthened test uses the same proxy URL as upstream, captures exactly one request without opening a connection, and checks that `prompt_cache_key` is either exactly `session-123` or absent. Wrong keys, an empty key in place of omission, and a wrong endpoint each failed their fault control. This checks the serialized request; no live provider was contacted.

### Codemode keeps uncommitted replies from the guest

The unchanged upstream case passed. When SQLite refuses the first result's journal commit, Go runs only `first` and sends no reply to the published Codemode guest. Reopening SQLite shows that the call was issued but its result was never stored. A fault that sent the reply before committing was caught. This covers the published portable guest; it does not cover the separate Node worker transport.

### Read-only Durable viewers leave SQLite unchanged

All five unchanged `harness-read-only.test.ts` cases passed, including the upstream JSONL case. The Go tests confirm that a viewer can see a running task without writing or waking the scheduler. They also check six read-only refusals, a successful commit that writes nothing, and an empty store whose head stays unchanged. After a real writer crash, the next writable open recovers the same task exactly once. That crash test preserves SQLite's single-writer lock; it does not reproduce the original's overlapping writer setup. The torn-JSONL test remains upstream-only and has not been excluded.

### Tool validation lists the errors it finds

Go now reports the ordered field errors in one tool response instead of stopping after the first. It also reports which tags a tagged union accepts. The depth-40 recursive test checks all 42 lines in order. The unchanged upstream runs passed: 40 validator cases, two selected coercion cases and all 13 AI validation cases. The exact model-facing error format matches the pinned test. The separate Schema API still needs structured errors with JSON Pointer paths and keyword names; not every constraint is covered by this change.

### Qwen plan keys and native QuickJS inputs

The [Qwen receipts](test-inventory/qwen-token-plan-remaining.md) now cover all 120 unchanged cases in that file. Go tries each plan's own environment key before the shared key for its region. The tests used synthetic keys and did not contact Qwen. The source also exposes an exact list of candidate environment keys; that list is not exposed by Go.

The [native QuickJS receipt](dependencies/native-quickjs-wasm-load.md) checks the bytes passed to the Go runtime's actual WASM compiler. All 637,405 bytes matched the verified upstream QuickJS-WASI archive. Appending a harmless WASM section left script results unchanged but failed the byte check. This verifies one native inline compile, not every worker or historical load.

### Conversation abort joins foreground work only

The unchanged upstream case passed. When Go's public `Abort` returns, both foreground tasks have ended and both background tasks are still live. The queued input has reason `aborted`, and the queued write stays queued. The SQLite test passed without a production change. Fault controls caught both a no-op abort and a cascade that incorrectly stopped background work.

### Structured Schema errors and the current test queue

Go can now report each covered validation error with its JSON Pointer path, keyword and message, without changing the existing tool-error text. The six assertions in the five unchanged diagnostics cases matched exactly. Tests caught wrong path escaping, a wrong keyword and the wrong order. Compiled-validator reuse and exhaustive errors for every constraint still need work.

The [updated test queue](test-inventory/primary-queue-r21.md) reviews all 42 changed primary suites using evidence through round 20. It found an upstream run for 24 of the 37 files in the earlier queue; 13 still had no run on record at that checkpoint. It separates actual test receipts, written accounts and Go comparisons. The audit did not run tests or turn any open case into an exclusion.

### Codemode reuses finished calls after reopening SQLite

Both unchanged upstream tests passed. The published guest returned the exact expected result after reopening SQLite. The two finished echo calls each ran once, the replay-safe hold ran twice, and the guest started twice. The clock and randomness test returned `ok` and echoed once. A separate Go check confirmed both guest starts received the same clock and seed; it does not claim equality of sampled values that the original never compared. Changing the first replayed reply or the resumed seed was caught by the corresponding test.

### Strict schemas and grammar streaming

All ten unchanged constrained-sampling cases passed. Go now keeps the upstream property order in strict schemas, rejects untyped nodes that upstream also rejects, and still accepts enum-only nodes. Responses grammar errors use the pinned wording. Go checks the exact streamed input at both tested sizes and confirms that replay drops foreign item IDs. Thirty of the 32 source assertions have native checks. An explicit JavaScript `false` setting and the native memory-allocation ratio remain open; no profiling ran.

### SQLite engines used in the migration tests

The [engine receipt](dependencies/sqlite-migration-engines.md) records the versions from the two actual database connections. The unchanged upstream migration ran on Node's SQLite 3.54.0; Go's translation ran on pure-Go SQLite 3.54.1. Both preserved the migrated task. The Go fixture supplies owner and allocation records, so this does not prove that Go can open the exact raw upstream fixture.

### Draft 7 remote references: exact errors

All 46 unchanged upstream checks passed: 23 values and 23 checks that validation agrees with the error list. Separate Go validation and explanation calls returned 12 empty lists and 11 exact errors, including the expected paths, keywords and messages. Removing the host reference registry or erasing a path broke the new comparisons even though the affected value was still rejected. No production change was needed. The two public calls share validator internals; this does not establish a second compiler or cover the other schema drafts.

### Completions tool choice

All 50 unchanged upstream cases passed offline. The Go tests cover 13 assertions from the first five cases: required and disabled tool choice, omission of unsupported strict flags, and the exact required fields for local and Groq models. No production change was needed. The tests also check automatic and named tool choices at the request boundary. These checks do not cover all 50 cases or the source SDK's internal objects.

### Photon in the resize worker

The [Photon runtime check](dependencies/photon-runtime.md) records what the actual Node image worker loaded. Its JavaScript and WASM match the verified archive byte for byte, and the worker returned a PNG resized from 400×100 to 200×50. Changing those bytes in scratch kept the resize result but failed the archive comparison. The Go image tests passed separately and do not load Photon.

### Submission waits preserve cancellation and close ordering

All 12 unchanged upstream cases passed. Go now returns the caller's original cancellation cause without cancelling the submission itself. A wait whose first storage read overlaps shutdown returns `ErrClosed`. A separate check confirms the observer sees the exact settlement before `Wait` returns. These changes and checks cover three cases; the JavaScript `unhandledRejection` check remains upstream-only.

### Codemode refuses replay when the sandbox changes or time runs out

Both unchanged upstream tests passed. After SQLite reopen, the published guest did not start again or run the new `late` call when the sandbox identity changed or the saved deadline had passed. Go now gives the expected reason and still lets callers recognize the deadline error. The version-0 variant remains partial: Go rejects the attempted rewrite of a saved start at the storage boundary, before the source's replay scenario can be reproduced.

### llama.cpp classifier behavior

All 15 unchanged classifier cases passed offline using simulated server replies. Go now distinguishes an explicit zero temperature from an omitted setting and preserves the server's error details. The tests also check the exact token limits used when deeper readouts are needed. Two prompt comparisons still differ because Go sorts map keys while JavaScript keeps their insertion order. No live server was contacted. See the [test results and remaining differences](test-inventory/llama-cpp-classify.md).

### Remote Schema errors, after-run tasks, and z.ai requests

All 62 unchanged draft 2020-12 remote-reference checks passed. Go's separate validation and explanation calls matched all 31 cases: 16 empty error lists and 15 exact error records. Removing the registry or erasing an error path was caught. No production change was needed. This covers remote references, not all dynamic-reference or unevaluated-property diagnostics.

The unchanged `harness-after-run` and `harness-tool-hooks` files passed 47 cases together. One after-run case now has exact Go comparisons through the public task scan: one task is created with the installed version, it ends, and only the installed initializer runs. Changing the initializer to the older token failed that comparison. The other cases are not claimed as mapped by this check.

All ten selected Groq and z.ai Completions cases passed unchanged upstream. Go now sends `clear_thinking: false` when z.ai thinking is enabled, and sends the mapped effort only when supported. The request checks also verify tool-stream flags, preserved reasoning on replay and effort omission when thinking is off. No live provider was contacted.

### Specification example and duplicate submission IDs

The unchanged specification-usage test passed under Vitest. A separate TypeScript run on the original import closure, with the missing declarations restored in scratch, still reported `MessagePort.onmessage` under the repository's Node-only options. Adding the DOM library let it compile and caught a deliberately wrong type assertion. This does not count as a pass under the original compiler options. The unchanged submit-in-commit example ran and printed the expected seven lines in order.

The Go submission test checks that a duplicate request returns the same ID without advancing SQLite's commit counter. Reusing the ID for a different input type returns the exact error, and another conversation can use the same request ID independently. The unchanged upstream case passed, and bypassing Go deduplication broke the test. No production change was needed.

### QuickJS in the published Codemode guest

The [portable guest check](dependencies/portable-quickjs-load.md) observed the exact QuickJS WASM bytes at compile and instantiation, plus three JavaScript loads. They match the verified archives. Changing the WASM bytes in scratch left the guest result unchanged but failed the digest check. This verifies the published portable subprocess, not the standalone Node worker or Cloudflare setup.

### Image reads refuse oversized files before loading them

Nine unchanged [image-read tests](test-inventory/tools-images.md) passed offline. Go now checks the byte limit before reading the full PNG, and matches the expected PNG, GIF and BMP errors. Bypassing the check still rejected the PNG but read several megabytes first; the new test caught that. The Go checks cover three source cases. Other image behavior and worker timing remain open.

### Closing a Codemode guest keeps its work replayable

The unchanged host-cutoff test passed. Go previously published a `context canceled` tool result after the host closed a guest, so a replacement guest never ran. Now closing the guest leaves the result unset. After SQLite reopen, the replacement guest returns `done`, and the earlier echo still ran only once. The test also checks that ordinary caller cancellation stays separate. This covers one active portable guest execution, not shutdown of a worker pool.

### Tool details and the standalone Codemode worker

All 42 unchanged Harness tool cases passed offline. Go already distinguishes explicit null tool details from omitted details. The public receipt test checks both the presence marker and the value: explicit null replaces the earlier object, while an omitted value retains it. A fault that ignored the presence marker returned the old object and failed the test. This maps the value and presence, not the JavaScript field layout of the entire receipt.

The [standalone Node worker check](dependencies/worker-quickjs-load.md) ran the unchanged namespaced-globals case and observed the real worker loading the pinned entry and instantiating the transferred QuickJS module. A scratch byte change left the behavior test passing but failed the input checks. The archived `dist` worker and Cloudflare were not part of this run.

### Codemode describes what scripts can actually call

The unchanged description test passed. Go now sends the expected call syntax and discovery guidance to the model. It mentions `models` only when the inline sandbox exposes that capability and the host allows it. Execution permissions did not change. Tests caught both incorrect wording and a description that advertised a capability the guest did not have.

### Completions keeps parallel tool calls separate

All seven unchanged stream cases passed offline. Go now keeps one text block and one thinking block even when tool calls arrive between their pieces. A tool call retains its first ID, and parallel calls stay separate. A stream that ends without a required finish reason reports the exact upstream error. The Go tests compare all 63 assertions in this slice, including event counts, event order within each call, and the final blocks. No live provider was contacted.

### Exact dynamic-reference error lists

All 88 unchanged checks passed for the 44 dynamic-reference cases. Go already agreed on which values were valid, but 17 errors differed. The standalone explainer now returns the expected error lists, with the right paths, keywords and order. The change leaves validation and the error text shown to models alone. Deliberately changing scope, fallback wording or annotation scope broke the exact comparisons.

### Nested tool receipts and image preparation

All 35 unchanged nested-tool tests passed offline. The Go check verifies that the child belongs to the right call, that only the parent's result reaches the transcript, and that the nested result document is retired. The stored result includes duration, but the Go caller's result does not expose it. The Go fixture now expects the upstream tool task version, 4. A fault that put the nested result in the child's terminal record was caught.

The [image preparation checks](test-inventory/tools-images-preparation.md) cover the expected colours and decoded dimensions for all eight EXIF orientations. Go now reports the BMP-to-PNG conversion in the read result. The two remaining unchanged worker timing cases also passed in a separate isolated run with a 1,400 MiB Node heap, so all 34 source image-tool cases have now run across three selections. The worker timing and idle restart behavior have no exact Go mapping yet.

### Provider capabilities and the published Node worker

All 30 unchanged provider tests passed in a network-isolated run. The new Go checks read the real catalogue: they distinguish an absent grammar flag from `true`, check which OpenAI models support strict schemas and grammar tools, and verify strict-tool support for the Anthropic and Copilot models in the source case. Hiding the gpt-5.4 grammar flag made the test fail. Other provider cases are not mapped by this check.

The [published Node worker check](dependencies/published-worker-quickjs-load.md) ran the pinned script through the package's actual `dist/runtime/worker.js` entry. It observed the archived JavaScript and the transferred QuickJS module, and checked the exact result and callback arguments. Altering the WASM bytes left the behavior unchanged but failed the archive checks. Cloudflare and other worker builds remain outside this run.

### Unevaluated array errors include every rejected item

All 142 unchanged checks passed for the 71 unevaluated-array cases. Go already agreed about validity, but 24 error lists differed. The standalone explanation now lists every rejected index in order with the expected keyword and message. It leaves model-facing errors alone. Tests caught missing later items, a wrong keyword, and a missed `contains` match. Unevaluated object properties still need separate work.

### Object-property errors preserve order and report every rejection

All 258 unchanged checks passed for the 129 unevaluated-property cases. Four Go error lists were missing a second property. The standalone explanation now includes every rejected property and keeps the source key order when it receives raw JSON. Go maps still use a stable order because they do not retain insertion order. Validation and model-facing errors are unchanged. Fault controls caught missing errors, reversed order, and an incorrect dynamic-reference annotation.

### Guest discovery, nested-call duration, and indexed idle checks

Both unchanged Codemode cases passed. The inline and published portable guests return the full expected discovery object. Empty catalogues omit the prompt section. New portable starts record access to the three read-only discovery functions; older journals keep their original environment, and paid and custom permissions do not change.

All 35 unchanged nested-tool cases passed. Go now returns the exact duration stored with a successful nested result, and leaves it absent on a fault with no saved result. Tests caught a one-millisecond change and a made-up zero. The original includes the task ID in the result; Go exposes that ID through its handle.

The unchanged seeded idle-index test passed. Go's corresponding 200-step test checks the actual context fallback counter: zero task-graph scans. Bypassing the index left the busy results correct but caused 12,385 scans, which failed the new check. The counter changes no scheduling decisions.

### Reasoning fields survive Completions replay

All nine unchanged cases in this slice passed offline. Go now uses `reasoning_content` for OpenCode Go reasoning in both streamed and replayed messages. Other providers keep their original field. The request checks also cover instruction roles, Xiaomi's explicit empty reasoning field, and the catalogue settings for OpenRouter, Xiaomi and Qwen. Only the two OpenCode Go directions failed before the fix.

### Portable file tools return the expected errors and diagnostics

All 13 selected unchanged [file-tool cases](test-inventory/portable-file-tools.md) passed offline. Go now returns an ordinary error if no environment is configured, and text reads with no notices return an empty diagnostic list. Eleven cases already matched. A one-line error in the continuation offset broke the expected result in both implementations. The other 23 source cases, including host-specific process tests, remain open in this packet.

### Completions options and usage now match all 50 cases

The last 19 unchanged Completions option and usage cases passed offline. Go omits disabled thinking for Moonshot Kimi K2.7 Code, preserves it for K2.6, and omits unmapped Ant Ling effort on the direct route without changing the simple route's clamp. Earlier token and cache accounting already matched. All 50 cases in the file now have native comparisons; this does not establish SDK lifecycle or live-provider behavior.

Go also now accepts and preserves the explicit JSON `false` tool-sampling declaration. Responses sends exactly the same provider tool definition for `false` and for omission, as upstream does. Durable preserves the explicit value in a typed offer without giving caller JSON any additional marshal authority. Before the fix, Go rejected the literal at decoding.

### Shared nested calls, replay errors, and standalone coercion

The unchanged nested-tool suite passed again. Go checks that a repeated key reuses the same child, that conflicting arguments are refused, and that cancelling a wait leaves the child running. Concurrent calls also share one child. The source's insertion-order distinction stays partial because Go maps do not keep that order.

Both unchanged replay-divergence cases passed. Go now reports the expected diagnostic when replay changes the number of calls or a global's arguments. The live portable guest tests use valid persisted records rather than rewriting an immutable journal; the rejection behavior matches without weakening storage guards.

All 49 unchanged coercion tests passed, along with the selected relative-root test. Go now exposes standalone conversion and parsing with host-registered references and structured errors after conversion. The focused native checks include all 42 diagnostics in the depth-40 case. Tool behavior is unchanged. JavaScript-specific values, reference identity, and reusable compilation remain open.

All eight selected [Linux Bash cases](test-inventory/linux-bash-tools.md) passed in isolation; the prior file-tool receipt and this one cover 21 distinct originals out of 36. No Bash runtime change was needed. The Windows shell cases and the remaining original behaviors stay open.

### Allocation growth, early payload capture, and JSON coercion

The unchanged Responses grammar allocation test passed. The Go checks used the same input sizes and measured cumulative allocated bytes while processing and consuming the stream. Ten times as much input used about 9.9 times the bytes, below the expected limit of 25 times. A scratch-only change that copied all accumulated input after every piece grew allocation by 71.5 times and failed. This was one focused test, not release profiling.

All 35 unchanged cache-retention tests passed offline. Go now checks the expected payload inside the public callback, before any network request. It requires exactly one callback and no HTTP request when the callback stops the operation. The separate HTTP-level checks remain in place. These tests cover the callback behavior and values, not JavaScript SDK class identity.

All 49 unchanged coercion tests passed. The new Go comparisons cover 27 assertions about optional nulls, arrays and tuples, primitive and union conversions, and object properties. No production change was needed. The tests distinguish tuple conversion from array wrapping and reject conversions that are not in an enumeration.
