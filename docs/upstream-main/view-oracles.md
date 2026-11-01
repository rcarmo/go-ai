# Conversation-view test oracles

Target: `81f3cd412758ebdd92324c7d63b669a189408d0a`. Upstream test inputs and assertions govern parity. Native safety controls provide additional coverage; they do not replace upstream assertions. This checkpoint adds tests and fixtures only.

## Pending-operation adversarial tests

`packages/chord/test/delta-pending.test.ts:362–554` contains ten tests with eighteen sequences. The seventeen scalar-valid sequences are recorded in `durable/testdata/upstream-pending-81f3cd41.json` and exercised by `TestMainPendingUpstreamAdversarialOracles`.

The fixture preserves each input and the source’s explicit operation/count assertions. Its `wantValue` fields were captured by executing the pinned upstream `applyImmutable` on those inputs. Native input replay, folded replay and native wire replay must each match that independent result. This prevents two native replay paths sharing a bug from becoming their own oracle.

| Source assertion | Native fixture cases |
| --- | --- |
| Array leaf/splice/leaf keeps operations | `array-leaf-splice-set`, `array-append-splice-append` |
| Trim/append barriers preserve counts | `trim-append`, `append-trim-append` |
| Parent object replacement and descendant edits | `parent-object-set`, `object-set-append-set` |
| Primitive ancestor replacement | `ancestor-primitive-append`, `ancestor-null-string` |
| Delete/move/splice shifts invalidate leaves | `array-delete-shift`, `array-move-barrier`, `splice-independent-leaves` |
| Distinct dotted/numeric paths replay correctly | `dotted-keys`, `numeric-object-array-paths` |
| Setting the array end and folding that index | `array-set-end` |
| Root replacement discards earlier operations | `root-drops-prefix` |
| String set plus interleaved appends | `string-set-appends` |
| Structural barrier between interleaved leaves | `interleaved-structural-barrier` |

All ten original upstream adversarial test bodies also ran under Bun, using the pinned implementation’s `PendingOps`, `applyImmutable`, encoder and decoder. A small synchronous assertion adapter supplied `describe`, `it`, `toEqual` and `toHaveLength`; this was not a full Vitest run. Their original codec/replay assertions passed, including the split-surrogate sequence.

That eighteenth sequence is **not ported**: its first append is an isolated high surrogate. Native scalar validation rejects that input; no replacement input or weakened validation is counted as parity. The earlier scalar-domain decision remains open. Seeded tracker/diff fuzz suites, root-array/scalar documents, source 10,000-op cap, reference identity, cost and GC tests remain outside this port. Native 4,096-op boundary controls do not satisfy the source cap assertion.

## Exact hydration fixtures

`TestMainViewUpstreamHydrationOracles` ports the relevant inputs and assertions from `packages/durable/test/harness-view.test.ts:397–411,448–473`:

- Ten notes, `min: 4`: entries 6 onward and predecessor 5; default window contains all ten and omits `before` from its JSON object.
- Thirty notes, `min: 5, max: 10`: `from` at entry 20 includes ten; entry 28 includes two; newest plus one gives an empty window; entry 19 overflows to the newest five; `min: 40` from the first entry includes all thirty and omits `before`.

The source notes helper writes one entry per commit with `kind: "note", data: index`. The native port preserves those inputs, with the required empty native `Value` envelope. IDs are compared by the source’s relative slices, not fixed allocator numbers. Memory runs the source’s backend choice; journal and SQLite are additional native runs. No production change was needed.

## Verification and remaining assertions

The new oracle tests plus the earlier folding unit test pass five normal repetitions, shuffle 42, in CGO 0/1 (0.667s/1.434s), and three race repetitions (2.707s). Durable vet and staticcheck v0.8.2 pass both modes. Removing structural barriers fails seven oracle cases; collapsing path keys fails eight. The initial supplementary native codec probe used an array root with the object-only decoder; correcting its test envelope resolved that setup error without a production patch. Raw source extracts, assertion runner and mutation files/logs were removed after analysis.

The harness-view corpus remains partial. Existing frame-sequence tests with extra native callback barriers do not prove the source’s natural Promise/microtask frame-count ordering. Shared JavaScript object identity, dropped-entry GC, synchronous observer lifecycle and native detached ownership require their own dispositions. No sleeps, scheduling hints, relaxed assertions or identity substitutions were added to claim those tests passed. No hydration/scan optimisation was made without a demonstrated upstream-test requirement. Full-suite, full race, performance and pre-release acceptance remain open.
