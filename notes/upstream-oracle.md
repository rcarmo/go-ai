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

The selected mode now passes **200 unchanged cases +68 independent probes**:192 tests in ten original files (including all11 input-entry cases), eight unchanged Storage conformance cases and68 probes. It reports zero skipped/todo cases. The runner reports268 total cases:192 original-file cases plus76 supplemental cases; the supplemental total includes those eight unchanged conformance cases.

`--all` discovers all 99 files under `packages/durable/test`. It starts a fresh Vitest process per file, one worker at a time, with a384 MiB V8 heap cap (768 MiB for the image file). A single process for the whole corpus previously stopped this 2 GiB workspace with an OOM; the sharded attempts completed without a workspace OOM. Subprocess failures remain failures, not skips. The full command continues to collect failures and exits nonzero if any group failed. JSON reports and transform caches are temporary and deleted on exit.

Current probes also require exact empty, submissionId-only and entryId-only CompactionResult arms. The latest selected258 and separate original compaction176 cases pass, zero skipped.

## Commit callback results

An added original SessionImpl probe preserves undefined/null/false/zero/empty/string and cyclic host identity as callback results, preserves thrown-value identity and remains usable after a callback rejection. Selected268 and original session-group40+76 supplements116 pass, zero skipped. Native value/publication controls remain separately identified in commit-callback-values.md; complete source batching/outcome acceptance is still open.

## Image-generation content

An added original-source probe executes real createModelTools/provider machinery with empty text/image data/MIME fields, compares the full input/output block packet and done text, then rejects nine missing/null/type-invalid inputs with one provider effect total. Selected267 and original codemode-models7+75 supplements82 pass, zero skipped.

## Classifier images

The classifier probe also passes supplied empty/nonempty image arrays unchanged, retains empty data/MIME strings and rejects null/non-array/null blocks/wrong type/missing MIME/null data before another effect. The fake original model declares image capability; its earlier text-only setup error is not runtime-failure evidence. Counts remain selected266/codemode81, zero skipped.

## Classifier context strings

The original internal classifier executes empty question IDs, instructions and criterion strings for choice/bool plus a one-level score. The fake provider receives the unchanged context without invented images. Empty choice/score collections, malformed bool criteria and unknown types reject before another provider effect. Selected266 and original codemode-models7+74 supplements81 pass, zero skipped.

## Tool-name string keys

An added source-derived probe executes original defineTool/createRegistry/wrapTool/resolveAgent with empty, digit/underscore, whitespace/punctuation, Unicode, NUL and129-byte names, complete wrapper/filter output, duplicate empty-name rejection and unchanged installation after rejection. Selected265 and original harness-registry10+73 supplements83 pass, zero skipped. The seven-name table is one probe, not seven original tests.

## Internal model-tool metadata

The original createModelTools metadata fixture pins models.ts SHA256 ca43ea7e9bd656766c995b9ff342d3b6d73d3c736beb3c05ed629effefcd02c8, independently matched with the remote target and recaptured byte-identically. The existing original-source probe compares full metadata and done/error output text for both tool types, including absent/empty/nonempty errors. Selected264 and original codemode7 plus72 supplements pass, zero skipped. These remain source execution receipts, separate from native production-path controls in typed-model-results.md.

## Executable original compaction records

scripts/capture-durable-compaction-request.mjs runs the original published1.5.0 Harness/CompactionTask with MemoryStorage to capture deterministic request/retry selected-record fixtures. Both recapture byte-for-byte. Native tests import untouched conversation/entry/task records through public SQLite.Commit and verify exact task JSON before execution/reopen. Builtin documents are omitted and explicitly initialised with Root; full store/database interchange remains unverified. The paired pinned-source SQLite probe requires exact checkpoint equality with the captured request fixture. Latest selected258 and separate original compaction176 cases pass, zero skipped.

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

The summary-budget probe also executes real pinned-source compactions with reserves101,4503599627370496 and9007199254740991, checking Number multiplication/floor and uncapped modelMax0. Latest selected259 and separate compaction177 pass, zero skipped.

The model-reference probe configures an original agent and compares its complete provider/modelId snapshot. Latest selected260 and separate original compaction178 pass, zero skipped.

Two new source-derived probes verify verbatim empty/whitespace hook decisions and ECMAScript provider trimming (BOM/NEL/U+180E/ordinary whitespace) through actual original Harness compaction. Latest selected262 and separate compaction180 pass, zero skipped.

The optional-error probe passes original JSON-decoded provider messages with absent, empty, space and nonempty errors through actual compaction. Latest selected263 and separate original compaction181 pass, zero skipped.

The model-tool projection probe runs original createModelTools with real Models and controlled providers. Explicit aliases select pinned model-tools source and its published pi-ai dependency. A first root-probe package-resolution error was fixed without implementation-failure credit. Latest selected264, separate codemode-models79 and compaction182 pass, zero skipped.
