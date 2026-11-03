# SQLite-only storage

Runtime `f284e255e0711ff2cf2950240fdf83b9c5652f5c` uses **one storage implementation: pure-Go SQLite through modernc.org/sqlite**. Memory/Journal implementations, constructors and fixture selection paths are removed. Codemode still uses QuickJS in wazero. Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`; full alignment is unverified.

## Removed alternatives

The change deletes `storage_memory.go`, `storage_journal.go` and journal reclamation. SQLite retains canonical-path ownership, inode locking, joined close, WAL transactions, synchronous=FULL, bounded native records and logical checkpoint reclamation. Shared ownership/configuration definitions now live in `storage_ownership.go`. The persisted SQLite configuration shape is unchanged, including its existing `sync=true` field.

The shared fixture opens a private SQLite file and reopens that same file. Its optional test file holder stores only a path; it has no alternate state or persistence implementation. General, UTF-16, trace, checkpoint and limited-capacity fixtures all use this engine. The public example and external tool/codemode consumers use `OpenSQLite`.

Journal framing, checksum, torn-tail, sync-policy and journal-only crash/reclaim tests are removed with that unsupported format. SQLite conformance, rollback, corruption, prefix isolation, reclaim, process-exit and ownership controls remain. Generic delta/ancestry corruption fixtures now insert invalid native payloads into real SQLite frame rows and require reopen to reject them. Close/cancellation and lost-acknowledgement tests wrap actual atomic SQL appends. The terminal-winner fixture fails acknowledgement of a commit, allowing earlier ID reservations to finish.

Existing databases, old journal files, durable data and published assets are not deleted or automatically migrated. Removing the constructors intentionally breaks callers using those alternatives. `Storage` interfaces and test failure adapters remain to preserve admission/error boundaries; they do not supply another engine. The native SQLite schema still differs from upstream's facade/schema. The legacy `MaxJournalBytes` profile field remains readable but does not impose a SQLite database quota; wider limit/API reconciliation needs source review.

## Focused verification

All commands source `scripts/project-env.sh` and use Go1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=4`, `GOTOOLCHAIN=go1.27.3`, and project-owned `runs/sqlite-only/20261103` paths. Logical/physical overlays exclude only the protected continuation prototype.

* **29 selected durable functions/count3/shuffle42:87 passes,0 failures/skips,5.064s.** The selection is checked against declared names and executed results. It covers conformance, real SQL rollback/reclaim, lost acknowledgements, alias/symlink and prefix boundaries, limits/exhaustion, close fences, terminal winner recovery, ancestry/delta corruption, secret exclusion, task detach, paid-result settlement, group/crash recovery and the four cutoff-order/salvage controls.
* Three selected durable/tools functions pass/1.771s: environment/reopen, coding-tools HTTP/reopen and failed truncated bash output.
* Both exported-API codemode consumers pass/0.963s, including the large-output control, using `NewQuickJSSandbox` with SQLite.
* Four native adapter failure/cancellation controls pass/0.091s. Concrete SQLite recognition does not bypass wrapper Snapshot authority.
* Cross-process ownership and exit-without-close prefix recovery pass/0.074s. `GO_AI_PROFILE_CHILDREN=0`; no captures are created.
* The public example prints `The sum is 5.` using its owned SQLite directory.
* Vet and Staticcheck v0.8.2 with tests pass for durable, durable/tools, tests and the example. All1456 Go files match the validated static copy. Selected dependency closures contain no CGO files. Formatting/diff and protected-prototype comparisons pass; dependencies, nativev1.4 ref/assets and QuickJS/wazero are unchanged.

Compilation probes caught obsolete journal members, duplicate storage cases, an unrelated GIF-field replacement and external fixture visibility errors; those are corrected and receive no runtime-failure credit. A draft terminal-acknowledgement fixture failed an ID reservation first and redispatched its scripted provider; the corrected commit-only seam passes. Earlier selection-name probes omitted two guessed names; only the final declared/executed-name-checked selection above receives complete-selection credit.

No full suite, profiling, race/CGO run, TypeScript/Vitest, hosted CI, release or asset publication ran. The large mechanical fixture migration is compiled by the selected packages; assertions outside the focused selection have not all executed or been reconciled with f9. Earlier multi-engine receipts are historical. The362/210 test corpora and428-path inventory are unchanged, and no whole-file disposition is promoted.

After analysis, disposable migration/static-copy/test/log scratch (`du -sh`:28M) and Go build cache (`du -sh`:1007M) were removed with no active Go/compiler/static jobs. The protected prototype, source mirror, installed dependencies/toolchains, durable data and intentional assets are preserved.

## Remaining alignment

[Cutoff ordering and salvage](cutoff-order-salvage.md) records the preceding local runtime changes and adapters. Its four functions also pass the new SQLite-only gate. The later [poll-boundary checks](poll-boundary.md) confirm that the pinned poll API returns only a terminal message; it exposes no harness streaming progress. Real interrupted-fetch, abort-handler and repeated-cancellation checks pass without runtime changes. The seeded legacy-phase controls are historical inputs; they do not establish a streamed-poll API. Full registry, facade/schema/migration, platform, legacy-contract and per-path convergence remain open.
