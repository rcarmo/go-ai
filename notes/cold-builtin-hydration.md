# Cold builtin hydration and canonical LiveDoc v3

Pinned authority: f9a26365568127461c47a07a37dcb42c140a3ac5, harness/view.ts #build and harness/{agent,live,inbox,provider,usage}.ts.

## Ported behaviour

- A first conversation mount loads each present builtin through its pinned definition, in upstream order. It neither creates missing documents nor persists a migration on read. Later attachments share the existing mount.
- Agent/Inbox/Provider/Usage target version1; Live targets version3. Definitions/policies are explicit, never inferred from a stored base's version.
- Cold snapshot and mount values share the materialised tracker. Mutable projection bookkeeping receives the same logical materialised shape, not the old raw base. Incarnation bookkeeping records the materialised definition version.
- Live v1/v2 migration renames compaction reason to trigger and supplies run.turns0 for a nonnumeric value. Existing numeric turns, including1e30, are retained: typeof number must not be replaced with native ID/rational-range validation.
- New live documents are version3; builtin live writes acquire that same definition, so writing a legacy base persists the required migration. There is no separate native-v1 write contract.
- Initial live/inbox/usage acquisition is idempotent through typed tx.doc-equivalent acquisition rather than duplicate raw creation. Fresh-data fixtures now use version3. Explicit legacy v1/v2 fixtures remain to exercise migration.

## Test-first evidence

Independent pinned-source probes established cold all-builtin identity, old-live migration without storage, unchanged identity after storing its required base, and repeated initialization without another commit. Reference runner:145 unchanged tests +16 probes pass, with pinned hashes verified.

Native baselines failed all five cold builtin identities, both legacy migration shapes, fresh live version (1 instead of3), and repeated initialization (duplicate current address). A strengthened1e30 migration control exposed use of an overly restrictive numeric helper; that failed before replacing it with the source type test. Content assertions compare JSON values; handle identity assertions use the raw readonly API.

Native callback migrations/initializers/checkpoint rules now live together in harness_document_definitions.go. Source-derived regressions remain in the existing view identity suite, not helper-only substitutes. Legacy migration scenarios use the real Harness/Session storage and watch paths, and assert no mount write followed by exactly one required migration-base write.

## Reconciled tests and deterministic controls

- Native builtin-generation tests formerly expected every builtin version1; they now assert the pinned live3/other1 versions, retaining all content/usage checks.
- Timestamp control now uses an explicit clock100→200 at the actual phase transition, as upstream harness-tasks:342 does. Source uses the same host clock for cache and lifecycle work; total host-clock call count2 was an obsolete native assumption. Exact startedAt/endedAt and no inspection-clock-effect checks remain.
- A progress-lane assertion about a later waiter used to launch it before the failure, with no admission signal. It now starts after Snapshot observes the latched failure; exact first-cause checks remain.
- Confirmed-refusal telemetry recovery no longer races an old store Snapshot with automatic Session shutdown; it joins close and checks the reopened durable rollback, keeping task/trace/span assertions.
- The SIGKILL helper's existing keepalive timer now has an actual receiver. An unobserved channel timer was not keeping the child runtime alive, occasionally producing Go's deadlock exit2 before the parent's SIGKILL. Stage boundaries, parent3s deadline and actual SIGKILL/reopen assertions are unchanged; the timer is not stage synchronisation.

Final focused migration/builtin/lifecycle/SIGKILL controls pass20 shuffled repeats (seed42). Full durable389.642s/tools18.465s and repository-wide vet/Staticcheck pass. Earlier full failures and their fixture corrections are not reported as parity passes.

## Remaining acceptance

D08/D14/D17 remain open for their complete source assertion/lifecycle scope; this slice does not credit public builtin-token API coverage, arbitrary malformed legacy-value/error behaviour, key-order collision semantics, or cache-pressure/eviction/weak lifetime coverage. D04 continuation-turn bridge and D16 canonical public stored observation remain separate unfinished work. No complete upstream parity or release acceptance claim.
