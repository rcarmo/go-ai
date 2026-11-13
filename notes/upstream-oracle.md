# Pinned upstream oracle

The oracle runs original pi-durable v1.5.0 tests at `f9a26365568127461c47a07a37dcb42c140a3ac5` without editing their assertions. These executions establish reference behaviour, not native Go parity.

## Commands

Source `scripts/project-env.sh`, then use:

```sh
bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi
bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi --all
```

The selected mode passes **189 unchanged cases +51 independent probes**:181 original-file tests, eight unchanged Storage conformance cases and 51 probes. It reports zero skipped/todo cases.

`--all` discovers all 99 files under `packages/durable/test`. It starts a fresh Vitest process per file, one worker at a time, with a 384 MiB V8 heap cap. A single process for the whole corpus previously stopped this 2 GiB workspace with an OOM; the sharded attempts completed without a workspace OOM. Subprocess failures remain failures, not skips. The full command continues to collect failures and exits nonzero if any group failed. JSON reports and transform caches are temporary and deleted on exit.

## Module and credential boundaries

- Every durable package export resolves to the retained pinned source. Dependency-wide `source` conditions are not enabled in Vitest.
- Original subprocess fixtures explicitly request `--conditions=source`. The installed Chord 1.5.0 artifact ships `dist`, not `src`; a temporary Node resolution hook selects that published artifact for Chord only. No assertion or implementation function is replaced. This repaired the original no-Buffer UTF-8 subprocess test.
- The full-corpus run used an isolated temporary HOME, without copying managed Pi credentials. Actual managed authentication/configuration was not changed.
- Missing test dependency `fake-indexeddb@6.2.6` was installed at the exact version declared by the pinned durable manifest, in the project dependency cache, and linked into the reference installation. This adds no native backend; Go remains SQLite-only.

## Full-corpus result and limits

The 99-file attempt reached every original Node test file. **95 original files completed successfully; four original groups remain unaccepted:**

| Original file | Observed blocker |
|---|---|
| `provider-session-cache-e2e.test.ts` | Retained AI source lacks generated `providers/data/amazon-bedrock.json`; module loading fails before the credential gate. |
| `system-order-cache-e2e.test.ts` | Same missing generated provider data. |
| `sqlite-cloudflare.test.ts` | Missing retained sibling source `packages/chord/test/sqlite/helpers.ts`. |
| `tools-images.test.ts` | Worker hits the 384 MiB heap cap after 32 passing cases; remaining assertions are not accepted. The workspace itself stays running. |

The supplemental new probe initially used the wrong bound entries signature and lookup receiver. Those fixture errors were fixed; both new probes then passed in selected mode. The last full attempt's aggregate is not a green receipt: it also included that subsequently fixed probe failure and an unhandled image-worker error. Platform-declared skips remain unverified; their original declarations are unchanged. No failing original test was removed, rewritten or labelled passing.

`test-workers` is outside this Node configuration, as in upstream's own Vitest configuration; those cases are not covered by this run. Alternative original engines are executed as reference implementations only and do not change the authorised SQLite-only native scope.

Next: restore the missing exact-version reference assets/helpers, run the image cases in a sufficiently bounded per-test process strategy, and rerun all 99 files. Do not raise an acceptance deadline, reduce an assertion, or mistake reference-file coverage for native assertion mapping.

The retained source snapshot has no `.git`. Existing retained hashes and the complete durable test tree are checked; commit identity is not independently reverified.
