## Generation presentation and stored-partial recovery

Production generation now publishes attempt/message presentation through the runtime-owned document progress lane. Task intent, model/settings/cutoff and retry state still commit before provider dispatch. New partials no longer write `generationCheckpoint.Partial`; legacy checkpoint partials remain readable.

Target: official upstream v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native implementation: `2387014a690da8110d723067d99dfe4271ff0454`; rollback: `34bd6165a26ef91683ddea595b8e817a35e4add9`. Native v1.4.0 and its assets are unchanged.

### Source assertions and baseline

The three recovery functions in `durable/harness_generation_recovery_main_test.go` translate `harness-deferred-progress.test.ts:116-209`:

* Stored partials must be earlier observed memory prefixes. A replica built only from actual stored-publication operations must equal Storage. Reopen converts that stored partial into one aborted entry, retries the request, produces `answer`, and clears live presentation.
* A stream held with an infinite window leaves neither an attempt nor a partial in Storage after a crash. Reopen produces only `answer`, not a synthetic aborted entry.
* A crash before conversion retains the stored partial. A later crash while the resend is held must not retain that resend or convert the first stored partial twice.

On unchanged `34bd616`, stored-partial/replica recovery passes all three backends. The other two functions fail six backend leaves in the combined 0.772s run: first-store output is wrongly durable, and the held resend becomes a second aborted entry. Initial native handle/type/operation-result adapter errors were corrected before this baseline.

The native adapter controls provider prefixes and clock/timer jobs, uses real conversation watches and storage snapshots, and feeds its replica through ordinary asynchronous stored subscriptions. It waits for real callback sequence acknowledgement before comparing the replica. A failed Session joins provider hosts, drops held data and reopens the same native backend; it is not an OS process kill. Source faux streaming, shared object identity and synchronous-observer timing receive no credit. No TypeScript/Vitest execution or claim that all eleven Harness bodies are ported.

### Changed runtime paths

`generation_presentation.go` writes only `pi.live.generation` for new attempt/message presentation. It reads interrupted output from the matching conversation/run task, ignores foreign, retired or cleared presentation, and retains the legacy checkpoint fallback. Recovery appends an aborted entry and removes its presentation in one transaction. Abort consumes the current logical partial; a failed Session instead discards anything not stored.

Status projection no longer synthesizes a stored attempt from the intent checkpoint. Retry, poll, tools and terminal status retain their durable paths. Presentation starts after durable intent and before model lookup/hooks/provider dispatch. Internal callers without a bound runtime keep immediate `commitPartial` behavior; ordinary Runtime Commit and anonymous Harness deferred-admission guards are unchanged.

Integration exposed two further gaps and fixed them without relaxing admission:

* Clean-close task requeue wrote directly to core storage and left the Session's receipt baseline stale. A subsequent held-document flush failed `stale prepared storage baseline` in the existing tool-handoff/reopen test. Requeue now settles and adopts each real receipt, still suppressing closing-observer publication and preserving checkpoints/restart counts.
* Agent events observed task checkpoint partials and stored subscriptions only. Their private subscription now follows logical memory, and message-start/update projection reads live presentation. Legacy task-partial projection remains available; storage-only flushes do not replay agent partials. The publication-timer test now observes memory presentation and independently checks the default one-second persistence alarm and its terminal cancellation. The original 0/5000ms interval test passes unchanged.

### Previously failing generation oracles

Both fixtures were run unchanged after the affected production path was integrated, before promotion:

* `TestMainHarnessDeferredProgressBatching`: twenty memory partials and eight changed-document stored batches on memory, journal and SQLite, versus the retained twenty/twenty-four baseline. Both literal source bounds (`memory > 12`, `stores < memory/1.5`) and the first-memory-only raw-storage control pass. This is a structural batch count, not profiling or a latency claim.
* `TestMainViewUpstreamGenerationOracle`: the full existing frames/touches relation, literal generation tuple, operation replay and final idle-view checks pass on all three backends (0.162s). No counter filtering, fabricated callback or relaxed count. The first command selected only the batching test because its stored-touch regex used the wrong name; the exact stored-touch function was then run and passed.

They are now `durable/harness_deferred_progress_main_test.go` and `durable/view_generation_persistence_main_test.go`. Historical failure receipts remain at their original pins; these tests are no longer pending or excluded from default discovery.

### Fault controls and focused verification

Go 1.27.3, GOMAXPROCS=4. The three recovery functions, legacy/owner fence, promoted oracles, affected generation/value/operation/progress/tool tests, Close140 requeue tests, agent/nested-event tests and pinned scheduler-close tests pass `-count=3 -shuffle=42`: CGO1 16.710s, CGO0 15.559s. CGO1 race `-count=3 -shuffle=7`: 87.109s. These affected boundaries were reopened because production generation, events and close requeue changed; completed standalone observer/retention/lifecycle/deadline/view-memory gates were not repeated.

Two later fault-sensitive native controls pass separately on the same final runtime: CGO1 1.077s, CGO0 0.583s; race 6.928s, with the same counts/shuffle seeds. `TestMainHarnessRecoveryCrashAfterConversion` lets conversion store, then refuses the next append before new intent: reopening must not duplicate the converted entry. `TestMainGenerationAgentEventsBeforeStorage` requires message-start/update before any storage, then uses an actual dependent entry callback as a barrier to reject partial replay during the flush. The legacy/owner test also covers old checkpoint receipts and foreign-task/owner, retired and cleared live values.

Seven active faults reject intended leaves:

| Fault | Failing leaves |
| --- | ---: |
| Store all presentation immediately | 9 |
| Skip interrupted conversion | 3 |
| Keep converted live message | 3 |
| Read checkpoint partials only | 4 (three backend leaves plus unit fence) |
| Store only the initial attempt immediately | 3 |
| Route agent events through stored subscriptions | 3 |
| Omit close-requeue receipt adoption | 2 |

The first keep-converted probe passed the ordinary repeated-crash scenario: the next intent also clears old presentation. The after-conversion fault boundary was added to isolate atomic removal. The old three-second interval assertion likewise allowed a stored-event probe to pass after the real one-second deadline; the controlled infinite-window event test now isolates memory delivery. Neither ineffective probe received fault-sensitivity credit. A nonexistent handle method and input-message filtering in that new fixture were corrected before final checks.

Durable vet, Staticcheck v0.8.2 (both CGO modes), module verification, formatting, diff and protected-prototype comparisons pass. Go tests/vet overlay both logical and canonical prototype paths; Staticcheck uses a physical temporary package copy excluding that file. Corrected a close-requeue helper-signature compile probe before checks. Owned source copies, overlays, mutations, logs and completed build cache were disposed after use; installed dependencies/toolchains, pinned source cache and protected prototype are preserved. No dependency, storage-format, coalescer, full-suite, profiling, CI or release changes.

### Remaining alignment

Review source210 onward: clean-close stored progress, abort waiting on a held-presentation flush, restart/progress accounting, tool progress/integration and summarisation ownership. Broader generic synchronous observers, shared/public value identity, tracker/WeakRef, reporting and JavaScript thenable/non-error-throw semantics remain unresolved. Equal-sequence idle-index rebuilds and exact deadline arithmetic still need authorised CPU/allocation acceptance profiling. Catalogue/package-build correspondence and the broader AI/codemode/schema/support review also remain open. Full v1.5.0 alignment is unverified.
