## Live host persistence settings

The Harness now reads `HarnessSettings.Progress.PersistWithinMs` at each use. Harness deferred commits are still disabled; generation writes retain their existing storage policy.

Target: official upstream v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native implementation: `4023473f648492c11644d2f7a1238e35582265ec`; rollback: `6e80186dc92bdf795b01927a60e6064ba66c7282`. Native v1.4.0 and its assets are unchanged.

### Source and scope

`packages/durable/src/harness/harness.ts:218-230` supplies the Session with a getter for the current host's `progress.persistWithinMs`. `harness/agent.ts:66-79` resolves the same host policy through `persistWithin`. `packages/durable/test/harness-deferred-progress.test.ts:100-115` supplies `partialIntervalMs:0` and a persistence window of 1000ms, then tests memory visibility and batched storage. This checkpoint implements the live-setting prerequisite; that complete test body is unported.

The eleven deferred-progress bodies at lines 100,116,158,177,210,228,302,341,366,398,430 still require the storage, crash, task, tool and summarisation integrations described in [the earlier contract review](../upstream-main/deferred-persistence.md#harness-cases). Existing publication throttling, retry and recovery tests retain their earlier receipts. The known failing generation oracle was not rerun.

### Contracts checked

Four source-derived/native-control functions in `durable/harness_persistence_settings_main_test.go` exercise real Harness construction, `SetSettings`, task runtime settings, the Session policy getter and native JSON encoding:

* `TestMainHarnessPersistencePolicyDynamic`: public JSON field, live replacement, detached Open/SetSettings inputs, host-only policy despite request overrides, explicit Session commit override and the existing Harness deferral refusal before callback execution.
* `TestMainHarnessPersistencePolicyValues`: missing/zero/negative-zero/fraction/infinity values; negative, NaN and negative-infinity default to 1000. Construction, live replacement and resolved settings use the same policy.
* `TestMainHarnessPersistenceRuntimeAndJSON`: real task runtime settings retain infinity; public JSON serialises nonfinite values as `null`.
* `TestMainHarnessPersistenceJSONBoundary`: bounded native encoding matches public JSON, including omitted/zero/fraction/null fields; independent settings copies retain native infinity without aliasing. Caller marshalers (including an embedded native settings type) and nonfinite ordinary JSON are rejected.

`ProgressSettings` is an exact library-owned JSON type. The bounded encoder never invokes its marshaler. Only its `persistWithinMs` field converts nonfinite numbers to JSON null; other values retain existing finite-number and marshal-authority checks. `cloneSettings` restores the original typed number after the bounded JSON ownership copy. Persistence policy ignores per-request overrides; publication intervals keep their independent defaults.

### Baseline and focused checks

The saved unchanged-runtime baseline from this prerequisite failed 18 backend leaves: dynamic policy stayed at 1000 on three backends; four non-default value cases failed on each backend; three task-runtime/JSON leaves lost the field and infinity. This baseline was retained across session recovery, without repeating it. Initial implementation runs then exposed rejected library marshal authority. A new boundary test caught native `omitempty` dropping the required null; exact-type handling and field-presence handling fix both. A test's undefined rejection identifier and an initial Staticcheck copy missing the root embedded catalogue were setup errors, corrected before credited checks.

Final checks use Go 1.27.3 and GOMAXPROCS=4. The focused selection is:

```text
^(TestMainHarnessPersistence|TestHarnessSettingsReference|TestHostSettingsAtomicRefresh|TestHostStreamRuntime|TestConversationSettingsPartial|TestCuratedRequestSettings|TestJSON120|TestJSON140|TestCopyObject140|TestUsagePresenceMain|TestProgress140)
```

Normal `-count=3 -shuffle=42`: CGO1 0.800s, CGO0 0.457s. Race `-count=3 -shuffle=7`, CGO1: 3.286s. Durable vet and Staticcheck v0.8.2 pass in both CGO modes; module verification, formatting, diff checks and protected-prototype content comparison pass. Go tests/vet use overlays for both logical and canonical prototype paths; Staticcheck uses a temporary physical package tree excluding that file, with local dependencies and embedded input copied unchanged.

Six active fault overlays fail their intended assertions: constant policy (3 backend leaves), lost typed copy (3), request-selected policy (3), missing exact native type (24), omitted null (1), and broadly admitted caller marshal authority (1). An initial non-matching mutator anchor was repaired before execution; no compile error or inactive overlay received credit.

Only this prerequisite and affected settings/JSON/progress checks ran. No completed observer, retention, lifecycle, deadline, view-memory or generation gates were repeated. No dependency, storage-format, coalescer, full-suite, profiling, CI or release changes. Owned source copies, mutations, disposable logs and completed build cache were removed after use; installed dependencies/toolchains and the protected untracked prototype are preserved.

### Next integration

Translate the first missing Harness memory-versus-storage assertion through actual generation, while preserving durable intent before provider dispatch. Add memory-progress/waiter/abort/report ordering controls before enabling Harness deferral; then run the unchanged generation oracle once its affected production path is integrated. Public shared-value identity, tracker/WeakRef assertions, generic reporter re-entry and JavaScript thenable/non-error-throw semantics are also unresolved. Full v1.5.0 alignment is unverified.
