# Archived pi v1.6.0 port plan

Editable plan revision 250, preserved before the live checklist was shortened. The current plan supersedes its scheduling and status. Links inside the snapshot are relative to the repository root.

> **Target:** official pi v1.6.0, `faac585884ae2c013a6b1400f3e9e77f10b34bac`. Never substitute v1.5.0, v1.5.2, or moving upstream main. [Source, assets and verification receipts](docs/upstream-v1.6.0/README.md). Round 1 starting production baseline: `3507d5974397ef7ee42373e35b44637174bd670e` on `origin/main`.
>
> **Working agreement:** the root coordinates integration, owns `/work/go-ai`, and alone pushes directly to `origin/main`. Leads own separate local branches/worktrees below `/work/port-lanes/`. Do not edit another worktree or run parallel heavyweight verification in the shared container. Use `flock /tmp/go-ai/v160-focused-verify.lock` around Go builds/tests/static analysis and Oracle runs; source `scripts/project-env.sh`, use Go 1.27.3, `CGO_ENABLED=0`, `GOFLAGS=-p=1`, Staticcheck v0.8.2. Use unchanged pinned originals and public production seams, exact count/order/value assertions, executable pre-fix reds where there is a gap, and a discriminating fault control. Distinguish originals, native translations and supplemental probes. No unapproved skips; only D25 alternate-engine availability is pre-authorised for exclusion. No broad pre-release gate, hosted CI, tags, or releases without separate authorisation. Preserve data, migrations, ownership, published assets and modernc SQLite-only runtime. Use deterministic receipts/channels rather than sleeps or polling.

## Baseline — closed
- [x] Record historical evidence separately. v1.5.0 last complete sweep: 32 failing Durable groups and 25 other packages passed; last fully verified tree `89c031e`. v1.5.2 focused checkpoints are in `notes/upstream-v1.5.2-restart.md`; no later full count.
- [x] Verify the v1.6.0 tag, release archives and scoped inventory. `19e16c5` and `7ee5335`: 2,148 verified blobs, 396 changed paths, 63 unchanged moved declarations. Scoped model regeneration alone is repinned (`d6986cd`). Preserve this baseline in ledger rows, not as blanket acceptance.

## Round 1 — verified and pushed (`707d392`)
- [x] **Durable — root:** `8c2fce7` preserves the parked child's restart intent and fixes projected document delta counts without persisting a new field. All ten unchanged park-child originals passed; exact public Go crash coverage, two fault controls, parked retention and explicit abort passed. Earlier SQLite copy ordering is `91f7ade`; generation crash coverage is `8135d0a`.
- [x] **AI — `/root/ai_lead`:** `5a596c4` fixes missing cached continuation fallback over WebSocket/SSE. Both unchanged variants and 14 exact source assertion sites mapped; old fallback and duplicate-start controls failed as expected.
- [x] **Codemode/Schema — `/root/codemode_lead`:** `c06946b` verifies persisted namespace/discovery and authority after SQLite reopen with changed installed declarations. Seven selected originals passed; the combined native test is supplemental and two mutations distinguish the two replay paths. Earlier scoped dynamic refs are `9fb1a97`.
- [x] **Oracle/evidence — `/root/oracle_lead`:** seeded exact machine-readable ledger, source-byte validator and handoff template in Docs. Reconciled at `707d392`: 165 scoped source sites, 61 mapped, 17 partial, 6 candidates, 81 unmatched. This slice is not the whole-port denominator and not an executed test count.

## Round 2 — one packet per lead, base `707d392a1ec3ba0eb06b211ca49f79679e61122a`
- [x] **Durable — root:** `61dcb8e` maps the three publication continuation cases via an explicit synchronous Go turn; both exact failure paths preserve the earlier result and fence later publication. Ordinary code after `Commit` returns is not synchronized. Cold task read follow-up is below.
- [x] **AI — `/root/ai_lead`:** `2e8158b` maps all ten sites across three started lifetime-limit variants. Public assistant retry and stream boundaries already matched; internal-retry mutation failed.
- [x] **Schema — `/root/codemode_lead`:** `e128328` provides per-validation host resources and maps ten validity vectors in groups 1, 2, 3, 10 and 11. Upstream error-agreement rows remain partial because native uses one validation path.
- [x] **Oracle — `/root/oracle_lead`:** `5eac162` adds second scoped slice; `7dc2908` reconciles 193 locators (81 mapped, 27 partial, 13 candidate, 72 unmatched). Earlier six filtered Codex case identities remain open. Integrated focused tests, vet and Staticcheck passed; main pushed.

## Round 3 — integrated at `ea3b257`
- [x] **AI — `/root/ai_lead`:** `dcae3f8` maps account scope and retention none; explicit cleanup now sends the peer-visible `1000/debug_close`. Scoped close/debug reset is supplemental.
- [x] **Schema — `/root/codemode_lead`:** `045692c` completes all 31 draft 2020-12 `refRemote.json` validity mappings. The 31 independent check/error-agreement rows remain partial.
- [x] **Oracle — `/root/oracle_lead`:** `3556f35` reproduces the scoped npm graph: six workspace entries, 305 placements, 546 edges. Graph provenance does not establish third-party archive bytes, native behavior or platform acceptance.

## Round 4 — integrated at `acf9666`
- [x] **Durable — root:** `f2f071d` maps `session-group-commit.test.ts:486`: guarded cold task reads through public Storage preserve the first read cause against a different later write failure. Added `ReadLine.Task`; remaining scoped lazy reads and ownership are open.
- [x] **AI — `/root/ai_lead`:** `64cf75b` ports idle timeout before and after start across seven scoped source sites. Exact connect/dial timeout diagnostics and peer idle close reason remain open.
- [x] **Schema — `/root/codemode_lead`:** `a1cc1d7` adds 13 external dynamic vectors; ledger has exact validity evidence for all 44 draft 2020-12 dynamicRef cases. Independent error-agreement, draft 7 and compiler annotations remain open.
- [x] **Oracle — `/root/oracle_lead`:** `3a3f879` verifies seven of nine retained npm archives and byte identity of native QuickJS WASM. OpenAI/Google archive provenance, loaded-asset and semantic correspondence remain open. At `acf9666`, validated 329 scoped locators: 177 mapped, 92 partial, 6 candidates, 54 unmatched. Integrated focused tests and static checks passed; main pushed.

## Round 5 — integrated at `022fe0c`
- [x] **Durable — root:** `bf65050` avoids requesting a third host scope once an earlier continuation fails; `12f9fd0` scopes cold task read memoisation; `ff8daa8` closes the last two exact comparisons in the ten-case parked-child ledger. All 29 recorded source sites in that slice now map.
- [x] **AI — `/root/ai_lead`:** `fab6902` closes the exact connect-timeout fallback case with one dial, correct time-budget error, and no leaked logical start. Peer-visible idle close, timeout normalization and six unmatched Codex sites remain open, along with unscoped AI cases.
- [x] **Schema — `/root/codemode_lead`:** `6103325` maps all 23 draft 7 remote-reference validity vectors and preserves the explicit draft policy. The independent check/errors surface and broader compiler semantics are open.
- [x] **Oracle — `/root/oracle_lead`:** `b4bf4bf` reproduces the offline native asset correspondence slice, including exact pinned SQLite migration bytes and QuickJS, Photon, selected AI/Chord and Vitest inputs. At `022fe0c` the source-byte validator confirms 375 scoped assertion sites: 205 mapped, 113 partial, 5 candidates, 52 unmatched. These counts are not the full port denominator. Focused integrated Go tests, vet, Staticcheck and offline evidence reproduction passed; main pushed.

## Round 6 — integrated and pushed at `b0fd6a8`
- [x] **Durable — root:** `37e811e` fixes missing canonical task outcomes in atomic legacy queue withdrawal, with exact Storage/public settlement and write-budget controls. Three scoped ownership originals and two input-entry originals passed. Timings and the separate queued descendant join and tool reselection reds remain open.
- [x] **AI — `/root/ai_lead`:** `6e81079` sends `1000/idle_timeout` without awaiting peer acknowledgment before fallback or settlement. Three exact cache-age sites map through a baseline-green public probe. Before-output lifetime retry and cache eviction frames remain open.
- [x] **Schema — `/root/codemode_lead`:** `9fda426` implements scoped `unevaluatedItems`. All 142 selected originals and 71 native exact validity cases passed. Independent error-agreement rows remain partial.
- [x] **Oracle — `/root/oracle_lead`:** `e261639` closes the two missing archive-byte gaps (OpenAI and Google GenAI), offline reproduction passed. The ledger at `b0fd6a8` validates 526 scoped sites: 279 mapped, 191 partial, 5 candidates, 51 unmatched. Focused integrated tests, vet and Staticcheck passed. No broad acceptance.

## Round 7 — integrated and pushed at `c188d0d`
- [x] **Durable — root:** `801ae06` corrects two stale fixtures and maps all five tool reselection assertions through canonical task and transcript receipts. Corrected legacy queued descendant test passes unchanged production; two separate failed-run fixture failures remain open.
- [x] **AI — `/root/ai_lead`:** `18ae28d` maps all three pre-output code-only lifetime rejection sites; coverage only, exact identical sends and one logical start across two sockets. Disabling reconnect failed the control.
- [x] **Codemode — `/root/codemode_lead`:** `ed3bada` fixes declared spread/non-spread global worker envelopes. Unchanged originals, wire controls and a separate published worker run passed; two source sites remain partial pending reproducible integrated guest coverage across backends.
- [x] **Oracle — `/root/oracle_lead`:** `21529dd` reproduces exact Google GenAI loaded bytes for one unchanged case, with a behavior-pass/digest-fail control. At `c188d0d`, 533 scoped sites validate: 287 mapped, 193 partial, 2 candidates, 51 unmatched. Integrated focused tests, vet, Staticcheck and offline reproduction passed.

## Round 8 — integrated and pushed at `3802c68`
- [x] **Durable — root:** `2148b55` guards cold submission reads by ID and request key in Session/Tx; eight compiled red branches now pass. `4045d29` and `bd9ed01` map the four failed-run policy originals, with a supplemental canonical owner-join receipt. Older raw-hold fixtures remain open.
- [x] **AI — `/root/ai_lead`:** `426be06` sends exact cache age/idle close reason without waiting for the peer before the replacement can send; both compiled EOF cases and both fault controls discriminated.
- [x] **Schema — `/root/codemode_lead`:** `e81bcb5` affirms 129 exact `unevaluatedProperties` validity cases; all 258 selected unchanged originals pass. Independent error agreement remains partial.
- [x] **Oracle — `/root/oracle_lead`:** `31af6e0` reproduces one exact OpenAI SDK load with a behavior-pass/digest-fail control. At `3802c68`, 798 scoped sites validate: 423 mapped, 322 partial, 2 candidates, 51 unmatched. Integrated focused Go repeats, vet, Staticcheck and offline reproduction passed.

## Round 9 — integrated and pushed at `10a9f34`
- [x] **Durable — root:** `ac65656` publishes ordered signal keys in the task graph and detaches watcher values. All 21 unchanged signal originals passed; added deadline, explicit abort and persisted asleep/reopen witnesses. Of 54 scoped signal assertion sites, 36 map and 18 remain unmatched.
- [x] **AI — `/root/ai_lead`:** `4ce5e6a` closes seven previous non-mapped Codex normal-delta rows and re-verifies the continuation ID; exact arrays, store flags and nine counters pass unchanged production. Eight partial sites remain in this slice.
- [x] **Codemode — `/root/codemode_lead`:** `f49d6fb` maps prompt order, budget, deferred stability, harness sections and grammar. Keep empty-section representation and reference identity partial.
- [x] **Oracle — `/root/oracle_lead`:** `21ef11f` reproduces all 43 retained provider JSON files plus the exact manifest and generated catalog asset. Ledger at `10a9f34`: 852 scoped sites, 481 mapped, 319 partial, 2 candidate, 50 unmatched. Integrated focused repeats, vet, Staticcheck and offline reproduction passed.

## Round 10 — user-requested checkpoint pushed at `ed9f713`
- [x] **Durable — root:** `27722fc` maps all 18 remaining signal sites with exact delivery results, live abort mark, deadline reuse rejection, ordered consumption and restart/checkpoint continuity. All 54 sites from the 21 unchanged originals now map; new coverage passed unchanged production.
- [x] **AI — `/root/ai_lead`:** `e6eaa98` maps the eight remaining partial Codex rows with exact request shapes and field absence. All 79 scoped Codex rows now map; unseeded cases remain open.
- [x] **Codemode — `/root/codemode_lead`:** journal/result readback was deferred from this push and integrated in Round 11 (`4560ddc`, `028d1e5`).
- [x] **Oracle — `/root/oracle_lead`:** `e349cd7` reproduces one observed QuickJS inline compile against the pinned archive and native embedded bytes. This is not a native/worker load trace. Integrated focused tests passed twice, vet and Staticcheck passed, and 852 scoped assertion sites validate: 507 mapped, 311 partial, 2 candidates, 32 unmatched.

## Round 11 — integrated and pushed at `d1bd6c9`
- [x] **Durable — root:** `12b01d9` implements beforeTool approval sleeps before execution and wake on registry/open. All nine originals passed, with compiled reds for missing gate sleeps and three wake paths. Exact stale-snapshot mutation failed as expected.
- [x] **AI — `/root/ai_lead`:** `fd6e4ec` sends Vertex API keys only in `x-goog-api-key`, preserves explicit header and ADC precedence, and handles blank options. Nine originals and exact offline request probes passed; source SDK constructor checks remain partial.
- [x] **Codemode — `/root/codemode_lead`:** `4560ddc` and `028d1e5` preserve omitted/null identity and compact output, and resume already decided outcomes without restarting QuickJS. Seven journal sites mapped.
- [x] **Oracle — `/root/oracle_lead`:** `df45e62` reproduces 1,736 scoped Git paths and 214 active added/modified suites; five had ledger slices at the inventory baseline. Integrated focused repeats, static checks and offline inventory passed. Ledger: 903 scoped sites, 542 mapped, 334 partial, 2 candidates, 25 unmatched.

## Round 12 — integrated and pushed at `7b90c60`
- [x] **Durable — root:** `6474cae` distinguishes context retention from the idle waiter set, and `c99a168` skips only filesystem entries that vanish after enumeration. All 25 idle-scope and one list-race originals passed. Seven older fixture/wrapper failures reproduced unchanged and remain open.
- [x] **AI — `/root/ai_lead`:** `0ff387b` fixes generated `session-id` affinity and suppression under `none`; maps ten strict/required/reasoning/affinity originals. All 42 unchanged Responses compatibility originals passed, not all native cases mapped yet.
- [x] **Codemode — `/root/codemode_lead`:** `2ec8d05` pins new worker argument presence across SQLite reopen and keeps legacy projections for old starts. Six unchanged originals passed; separate full backend result remains distinguished from worker adapter verification.
- [x] **Oracle — `/root/oracle_lead`:** `93cb2cd` audits the 37 primary queue files, verifies the unchanged directory race offline, and distinguishes conditional withheld tests from exclusions. Integrated tests, vet, Staticcheck and receipts passed. Ledger: 929 scoped sites, 563 mapped, 338 partial, 2 candidates, 26 unmatched.

## Rounds 13–14 — integrated and pushed at `c32240b`
- [x] **Durable — root:** `2c2496a` closes used-provider sessions after actual joins, including failed shutdown; `331e1bb` resumes on passive submit; `f5eb3a3` verifies late registry replacement. All 17 lifecycle originals passed; unmapped sites remain.
- [x] **AI — `/root/ai_lead`:** `ef1fd39`, `d80569b`, `f2f15d3` complete native mappings of all 42 Responses compatibility cases, with exact service-tier costs.
- [x] **Codemode — `/root/codemode_lead`:** `f227477` preserves completion/fork order, prompt header/body separation, and OpenAI Completions multi-block tool text.
- [x] **Oracle — `/root/oracle_lead`:** `ffc8083`/`de45202` reproduce six fetch-option originals and native request-local transport; dispatcher stays partial. Focused integrated repetitions, vet, Staticcheck and offline receipts passed. Ledger: 995 scoped sites, 631 mapped, 338 partial, 2 candidates, 24 unmatched.

## Round 15 — integrated and pushed at `16f9de4`
- [x] **Durable — root:** `09064db` ports delegated cold conversation reads, first cause and scope-local memoisation. ReadLine now exposes conversation and submission lookups.
- [x] **AI — `/root/ai_lead`:** `1d57a84` verifies all 73 pinned max-thinking and supports-xhigh originals and native capability/wire mappings.
- [x] **Schema — `/root/codemode_lead`:** `dff056d` verifies exact first diagnostics for two fixture groups and witnesses a dynamic-reference discrepancy. Exhaustive structured diagnostics remain partial.
- [x] **Oracle — `/root/oracle_lead`:** `962c51d` reproduces two Together model originals and verifies published Kimi K3 metadata. Focused integrated tests, vet and Staticcheck passed; assertion-ledger denominator unchanged.

## Round 16 — integrated and pushed at `29dba29`
- [x] **Durable — root:** `81647ad` uses delegated public context scans with pinned cutoff, exact pagination and guarded errors. All 22 unchanged context originals passed.
- [x] **AI — `/root/ai_lead`:** `18eebe0` maps 35 unchanged offline cache-retention originals, including four synthetic-key guards, with 26 wire and nine catalogue checks.
- [x] **Schema — `/root/codemode_lead`:** `87f408e` verifies recursive coercion at public tool validation; reuse, standalone coerce and cyclic JS representation remain open.
- [x] **Oracle — `/root/oracle_lead`:** `2184e61` reproduces three Fireworks originals and exact native metadata/wire behavior; 22 unselected cases remain open. Focused repeats, vet and Staticcheck passed.

## Round 17 — integrated and pushed at `27757e5`
- [x] **Durable — root:** `7cb2b35` corrects six idle/context/owner fixture groups with valid task records and a real active generation owner. The bounded-recovery red has a precise diagnosis: atomic end-run withdrawal of 20 inputs exceeds `MaxWrites=16`.
- [x] **AI — `/root/ai_lead`:** `5335762` adds 50 cache-retention locator sites (33 mapped, 17 partial); pre-SDK callback object capture stays distinct from Go wire capture.
- [x] **Codemode — `/root/codemode_lead`:** `3781641` verifies the actual published portable guest's exact terminal result and spread callbacks. Three unchanged originals passed. Native inline guest and journal replay remain open.
- [x] **Oracle — `/root/oracle_lead`:** `164bed5` reproduces ten Qwen catalogue originals and Exact IDs across four native plans; 110 unselected cases remain open. Integrated focused repeats, vet and Staticcheck passed. Addendum `6c50599` maps nine max-thinking sites. Ledger: 1,054 scoped sites, 673 mapped, 355 partial, 2 candidates, 24 unmatched.


## Round 18 — integrated and pushed at `2670fa7`
- [x] **Durable — root:** `dd64984` replaces the obsolete intermediate-hold expectation with atomic ordered withdrawal across reopen; four unchanged Oracle cases and the separate native write-budget checks passed. `458019c` matches the exact fault outcome when a transaction ends its conversation owner. Three wider ownership fixture groups reproduced on unchanged production and remain open.
- [x] **AI — `/root/ai_lead`:** `e7e4901` matches Completions affinity formats and preserves catalogue overrides. All 27 unchanged prompt-cache cases and focused native wire checks passed.
- [x] **Codemode — `/root/codemode_lead`:** `6c0b921` verifies bundled inline QuickJS and the published portable guest at the same namespaced-global boundary. The Node worker topology and guest-hosted journal replay remain open.
- [x] **Oracle — `/root/oracle_lead`:** `6415d79` reproduces ten unchanged Fireworks Messages cases in loopback-only isolation and fixes two missing headers. Twelve Fireworks cases remain open across both slices. Focused integrated repeats, vet and Staticcheck passed. Ledger: 1,055 scoped sites, 674 mapped, 355 partial, 2 candidates, 24 unmatched.

## Round 19 — integrated and pushed at `08b02b9`
- [x] **Durable — root:** `2b738e4` closes all three reproduced ownership fixture groups: canonical terminal receipt, actual historical SQLite format, and paused migration advertisement after waits drain. `397b5e3` adds guarded delegated conversation-owner reads; exact hit, miss and fatal failure all discriminate.
- [x] **AI — `/root/ai_lead`:** `8037794` records all 60 exact Completions prompt-cache assertion sites across 27 unchanged case variants; native HTTP and source SDK representation remain distinct.
- [x] **Codemode — `/root/codemode_lead`:** `22b5686` uses the real published portable guest and SQLite reopen to verify the exact `[true,true]` undefined-journal result without replaying prior effects. Node worker topology remains open.
- [x] **Oracle — `/root/oracle_lead`:** `2a85ded` verifies the remaining 12 unchanged Fireworks cases, bringing the file to 25 offline passes across disjoint receipts. Exact environment-key enumeration remains partial. Integrated repeats, vet and Staticcheck passed. Ledger: 1,115 scoped sites, 734 mapped, 355 partial, 2 candidates, 24 unmatched.

## Round 20 — six delegated lanes, integrated and pushed at `d0f5e86`
- [x] **Integration — root:** focused integrated tests twice, vet, Staticcheck and offline receipts passed. Inventory can now compare any exact native commit without changing the historical snapshot. Ledger: 1,150 scoped sites, 759 mapped, 363 partial, 2 candidates, 26 unmatched; 14 of 214 changed suites have a ledger slice.
- [x] **AI — `/root/ai_lead`:** four unchanged Responses prompt-cache cases and exact native key handling at the original proxy URL; four sites mapped.
- [x] **Codemode — `/root/codemode_lead`:** actual published guest does not receive a reply before SQLite stores it; exact callback `first` maps, and the early-delivery fault was caught.
- [x] **Oracle — `/root/oracle_lead`:** all 120 unchanged Qwen cases now have offline receipts; Go tries the plan-specific credential before the regional fallback. Exact source key-list discovery still unmapped.
- [x] **Durable — `/root/durable_lead`:** five unchanged read-only originals passed. Native SQLite verifies refusal, inspection and recovery. Torn JSONL remains upstream-only; crash recovery assertion mappings are partial because SQLite retains a single writer.
- [x] **Schema — `/root/schema_lead`:** exact ordered tool diagnostics and tagged union messages, including 42 recursive lines. Structured error arrays and JSON Pointers remain partial.
- [x] **Inputs — `/root/inputs_lead`:** exact 637,405 bytes observed at the native QuickJS compiler and matched to the pinned archive; behavior-pass/digest-fail control passed. Worker and historical loads remain open.

## Round 21 — six delegated lanes, integrated and pushed at `dbd2495`
- [x] **Integration — root:** focused Go tests twice with the published guest and selected provider/schema regressions; vet, Staticcheck, offline SQLite and inventory reproduction passed. Ledger: 1,182 scoped sites, 806 mapped, 354 partial, one candidate and 21 unmatched. Fifteen of 214 changed suites have a ledger slice.
- [x] **AI — `/root/ai_lead`:** strict schemas preserve property order and reject unsupported untyped nodes; grammar diagnostics match the original. Ten unchanged cases passed; 30 of 32 assertions map. Explicit JS `false` and the native allocation ratio remain open.
- [x] **Codemode — `/root/codemode_lead`:** six replay assertions map through the published guest after SQLite reopen, including exact call counts and clock-test output.
- [x] **Durable — `/root/durable_lead`:** all five foreground/background abort and queued-withdrawal assertions map to the public SQLite test.
- [x] **Schema — `/root/schema_lead`:** exact structured paths, keywords and messages for all six selected assertions. Exhaustive diagnostics and compiled-validator reuse remain open.
- [x] **Oracle — `/root/oracle_lead`:** audited all 42 changed primary suites through round 20. Of 37 files in the historical queue, 24 have upstream-run evidence and 13 had none located at that checkpoint; constrained sampling ran in round 21.
- [x] **Inputs — `/root/inputs_lead`:** observed Node SQLite 3.54.0 and Go SQLite 3.54.1 in the real migration connections. Exact Go adoption of the raw upstream fixture remains open.

## Remaining critical path
- [ ] Finish Durable scoped and lazy direct reads, pending ownership/admission and lifecycle, and remaining viewer cases. Track oversized commit refusal as a native resource-limit difference; any change must preserve atomic failed-run withdrawal and separately verify native limits. Review ordinary Go versus explicit continuation differences against the source API contract.
- [ ] Close unmapped AI/Codemode and compiler/typed values evidence, including independent Schema check/error behavior. Scope and run required credential/platform probes only with authorisation and exact inputs.
- [ ] Prove loaded-input provenance for the verified archives, extend native transitive mapping and exact assertion inventory beyond current slices.

## Merge gate — root with evidence coordinator
- [ ] Each lead hands off local commit SHA and base; list exact source tests, native tests and assertions, pre-fix or supplemental status, mutation and static receipts, uncovered branches, and next action. Do not report a full package count from a filtered run.
- [ ] Review crosswalk/ledger entries against the pinned source, merge non-overlapping packets, re-run the affected focused integration tests serially on the resulting candidate, and push verified Rui-authored commits directly to main. No lead pushes or changes another lane's files. Round 1 demonstrated independent packets integrating together, not whole-port throughput. Re-estimate once the critical-path Session and next dependency slices are in the ledger.

## Release gate — not authorised yet
- [ ] Close all outstanding provider/runtime/Schema/Storage/platform/credential assertions and the exact transitive inventory. Record no unsupported divergence, unmapped requirement, or unauthorised exclusion. Reconcile exact revisions, evidence and rollback pointers.
- [ ] After explicit authorisation, verify the exact integrated candidate with the designated acceptance suite, static/dependency gates and reproducibility checks. Measure CPU, `alloc_space`, and `alloc_objects`; act on measured outliers and remove analysed scratch. Do not claim race coverage (CGO-free).
- [ ] Record a release candidate only after acceptance. Hosted CI, tags and release publication remain separately authorised.
