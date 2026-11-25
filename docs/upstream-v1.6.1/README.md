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
