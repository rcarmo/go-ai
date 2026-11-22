# Pinned release-delta test-file inventory

[`files.jsonl`](files.jsonl) records the exact **scoped Git path set** at both pins. The acceptance target is pi v1.6.0, `faac585884ae2c013a6b1400f3e9e77f10b34bac`; v1.5.2, `b4afdddf44e059c67ccf5097e5317d913521b470`, supplies historical delta only. Ledger links reflect native evidence base `ed9f7139ae74a01f99f63988b7467feef2440535`.

## Scope and work size

- **Primary:** AI, Durable, Codemode and Schema. **Shared dependency:** Chord and Telemetry. Include their test trees, tests outside those trees named `*.test.*` or `*.spec.*`, `src/testing`, benchmarks, package/runner configs, and Codemode's Cloudflare example support. Include the shared root runner files listed in the manifest.
- **Adjacent:** Test paths outside those six workspaces that directly reference one of the six packages at either pin, plus non-suite test support and package/runner config in those roots. The lexical reference rule is recorded in the manifest. It selects test files in p2, p3, Agent, Coding Agent and Env. Indirect-only app suites and unrelated application features are not inventoried by this rule; it does not exclude them from later dependency review.
- **Artifacts:** Runnable-name suites, generated conformance fixture inputs, support, assets, benchmarks, examples and configuration are distinct roles. In particular a changed fixture, support script or benchmark is not a new test case. Restore-only provider catalog assets are covered separately by the [archive asset receipt](../dependencies/provider-catalog-assets.md).

There are **1,736 scoped paths**: 112 additions, 193 modifications, four historical removals and 1,427 unchanged. `A`/`M`/`D` are checked against Git with rename detection disabled. The removals are one old AI generator suite, two Chord session suites and one Chord session example; no removed path counts as a v1.6.0 test pass.

| Active added or modified suite files | Files | With any assertion-ledger slice | Without assertion-ledger rows |
| --- | ---: | ---: | ---: |
| Primary | 42 | 5 | 37 |
| Shared dependencies | 71 | 0 | 71 |
| Adjacent direct references | 101 | 0 | 101 |
| **Total** | **214** | **5** | **209** |

Across changed and unchanged paths, 11 of 860 active suite files have ledger slices. The [existing ledgers](../assertions/README.md) contain 852 assertion rows: 507 mapped, 311 partial, two candidates and 32 unmatched. These are counts of recorded evidence, not a whole-suite assertion denominator. A conformance runner aggregates links to its fixture rows; the summary counts each ledger assertion once.

## Use the queue

`ledger-slice` means some declarations or fixture vectors are recorded, not that the file is fully mapped. `not-in-assertion-ledger` means no records in the specified native snapshot; a separate focused receipt may still exist. `support-not-an-assertion-unit` marks dependency inputs to inspect, not a skip. Each path row includes its historical and pinned blob/mode where present and any evidence links or review flags.

1. **Reconcile the 37 changed primary suites without rows:** 21 AI and 16 Durable. Check the focused receipts before translating new cases. Close partial/unmatched rows in the five already scoped changed suites alongside that work.
2. **Triage shared and adjacent behavior:** Chord has 71 changed suites in this snapshot; identify which contracts affect the native public seams, then map or document the dependency of the adjacent suites. A direct source reference alone does not prove equivalent application behavior is required in the port.
3. **Expand beyond the delta:** unchanged publication, runtime and Schema fixture behavior still matter. Start with existing scoped runner/fixture links; do not infer full generated case counts from source strings or file names.
4. **Resolve runtime requirements:** `review` flags point to parameterization/generation, live credentials, platform behavior and conditional or expected-failure gates. These lexical hints overlap and are not exhaustive. All flagged paths remain open for review. There are no new exclusions.

Show the primary changed work queue:

```sh
jq -r 'select(.kind=="path" and .scope=="primary" and .role=="suite" and
  (.change=="A" or .change=="M") and .coverage=="not-in-assertion-ledger") |
  [.change, .path, (.review | join(","))] | @tsv' \
  docs/upstream-v1.6.0/test-inventory/files.jsonl
```

## Reproduce

```sh
inventory=docs/upstream-v1.6.0/test-inventory
flock /tmp/go-ai/v160-focused-verify.lock bun run "$inventory/generate.ts" "$pinned_git_mirror" \
  > /tmp/pi-v160-test-files.jsonl
cmp "$inventory/files.jsonl" /tmp/pi-v160-test-files.jsonl
```

The generator reads the local Git objects and ledgers, confirms their source digests and checks the release diff. It does not run suites, count assertions by pattern, fetch dependencies or run acceptance.
