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
| Dependencies | Reconcile `package-lock.json`, the coding-agent install lock, and the release manifests and archives from the new pin. Do not use the v1.6.0 installation as a verified v1.6.1 oracle. The model-regeneration gate and checked-in generated catalog remain at v1.6.0 until the new archives and regeneration are verified. |

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
