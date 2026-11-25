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
