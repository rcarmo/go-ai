# Chord telemetry senders over a real in-memory link

All **three unchanged cases** in the changed `packages/chord/test/telemetry-ingest/sender.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures, skips, or todos. The [machine receipt](chord-telemetry-sender.json) lists their exact names and pinned bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-telemetry-sender-witness.mjs) records four evaluated modules: Chord's sender, recorder and ingest facet, and the shared Telemetry module exporting `telemetryNow`. It forwards the real calls at the ingest target and records ordered span names, metric presence and the fixture counter. The page sends no batch before binding, then ingests `page.click` followed by `chord.hold` with no metrics. An out-of-scope page batch ingests zero spans. The worker ingests `chord.hold` followed by `worker.job` and the `runs` counter with sum `2`. AI and Durable both import the same Telemetry package; neither was exercised as a Chord sender here.

Dropping the worker metrics from the scratch dependency leaves both worker spans intact but fails the unchanged assertion that the `runs` sum is `2`. The fault run selects this one original and leaves the other two unselected. The upstream checkout and retained installation stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-telemetry-sender.XXXXXX")
bash "$inventory/probe-chord-telemetry-sender.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-telemetry-sender.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one changed shared suite and an in-memory link. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Real network transport, other hosts, and native Go correspondence remain outside the receipt.
