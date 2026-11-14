# Pinned upstream oracle

The oracle runs original pi-durable v1.5.0 tests at `f9a26365568127461c47a07a37dcb42c140a3ac5` without editing their assertions. These executions establish reference behaviour, not native Go parity.

## Commands

Source `scripts/project-env.sh`, then use:

```sh
bun run scripts/prepare-durable-oracle-assets.mjs --upstream /path/to/pinned/pi
bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi
bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi --all
# Original file/case isolation, with no test edits:
bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi --file test/tools-images.test.ts --case 'gives the same result as the calling thread'
```

The selected mode now passes **200 unchanged cases +55 independent probes**:192 tests in ten original files (including all11 input-entry cases), eight unchanged Storage conformance cases and55 probes. It reports zero skipped/todo cases. The runner reports255 total cases:192 original-file cases plus63 supplemental cases; the supplemental total includes those eight unchanged conformance cases.

`--all` discovers all 99 files under `packages/durable/test`. It starts a fresh Vitest process per file, one worker at a time, with a384 MiB V8 heap cap (768 MiB for the image file). A single process for the whole corpus previously stopped this 2 GiB workspace with an OOM; the sharded attempts completed without a workspace OOM. Subprocess failures remain failures, not skips. The full command continues to collect failures and exits nonzero if any group failed. JSON reports and transform caches are temporary and deleted on exit.

## Module and credential boundaries

- Every durable package export resolves to the retained pinned source. Dependency-wide `source` conditions are not enabled in Vitest.
- Original subprocess fixtures explicitly request `--conditions=source`. The installed Chord 1.5.0 artifact ships `dist`, not `src`; a temporary Node resolution hook selects that published artifact for Chord only. No assertion or implementation function is replaced. This repaired the original no-Buffer UTF-8 subprocess test.
- The full-corpus run used an isolated temporary HOME, without copying managed Pi credentials. Actual managed authentication/configuration was not changed.
- Missing test dependency `fake-indexeddb@6.2.6` was installed at the exact version declared by the pinned durable manifest, in the project dependency cache, and linked into the reference installation. This adds no native backend; Go remains SQLite-only.

## Restored exact reference assets

`scripts/prepare-durable-oracle-assets.mjs` verifies the retained source before installing missing assets. It refuses to overwrite differing existing files, verifies archive SHA256 and every generated catalog against the release manifest, and deletes extraction scratch. It does not edit original tests.

- `@earendil-works/pi-ai@1.5.0` npm archive SHA256: `e320fe7bd2b927782ba9a37d356fe5837f1aa96a46432610e1b3b9ccb748ea5c`. Its generated `dist/providers/data` supplies the42 catalogs missing from the retained AI source. The fetched archive also matched npm's SHA512 integrity, `VeWxNqTHSqRCf8AcY/6kvNwh+neRL6MPxZPePVztIQy6qqK9MR/YoCSN/ZgDnIyCD3pqEXgRyQbzk4KV4uvjbg==`.
- `packages/chord/test/sqlite/helpers.ts` was fetched at the exact target commit; SHA256 `9f41d665ad1d41f237ba549881b8b3c91adc805ab84adc9c070a57cbbb05407e`.
- The original Cloudflare suite now passes all36 cases. Both provider E2E files now load, then skip under their original credential gates with isolated HOME. Those live-provider assertions remain unverified, not passing.
- The image suite still exhausts a768 MiB worker heap, including isolated `gives the same result as the calling thread`. No workspace OOM occurred. The full-corpus command is not green; raising the worker cap did not repair acceptance.

## Bounded image-memory diagnostic

The isolated equality case also failed with a1 MiB V8 young generation at the768 MiB old-generation cap. A further isolated1 GiB old-generation experiment, budgeted against the measured non-reclaimable workspace memory, still exhausted its worker heap. Wait4 resource accounting reported maximum descendant RSS **1,503,332 KiB**, elapsed28.025s, exit1; cgroup oom/oom_kill remained zero. The runner was restored to768 MiB. No assertion, equality engine or image implementation was replaced. The temporary resource-accounting binary/source were deleted after analysis. A rejected optimize-for-size NODE_OPTIONS attempt failed before assertions and provides no test coverage.

This remains a reference resource blocker, not accepted image coverage. Do not infer that merely raising the heap to1 GiB repairs it or that original-worker errors may be ignored.

## Historical full-corpus result and remaining limits

The 99-file attempt reached every original Node test file. **95 original files completed successfully; four original groups remain unaccepted:**

| Original file | Observed blocker |
|---|---|
| `provider-session-cache-e2e.test.ts` | Retained AI source lacks generated `providers/data/amazon-bedrock.json`; module loading fails before the credential gate. |
| `system-order-cache-e2e.test.ts` | Same missing generated provider data. |
| `sqlite-cloudflare.test.ts` | Missing retained sibling source `packages/chord/test/sqlite/helpers.ts`. |
| `tools-images.test.ts` | Worker hits the 384 MiB heap cap after 32 passing cases; remaining assertions are not accepted. The workspace itself stays running. |

The supplemental new probe initially used the wrong bound entries signature and lookup receiver. Those fixture errors were fixed; both new probes then passed in selected mode. The last full attempt's aggregate is not a green receipt: it also included that subsequently fixed probe failure and an unhandled image-worker error. Platform-declared skips remain unverified; their original declarations are unchanged. No failing original test was removed, rewritten or labelled passing.

`test-workers` is outside this Node configuration, as in upstream's own Vitest configuration; those cases are not covered by this run. Alternative original engines are executed as reference implementations only and do not change the authorised SQLite-only native scope.

Next: resolve image execution within the2 GiB workspace without changing its assertions, then rerun all99 files. Credential- and platform-declared skips remain unverified. Do not raise an acceptance deadline, reduce an assertion, or mistake reference-file coverage for native assertion mapping.

The retained source snapshot has no `.git`. Existing retained hashes and the complete durable test tree are checked; commit identity is not independently reverified.
