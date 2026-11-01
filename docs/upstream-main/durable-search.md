## Durable search at 81f3cd41

The native ranker now separates names from text, weighs namespace text, and sorts by exact name, term coverage, then BM25 relevance. Equal ranks retain document order. Name weight is 3, namespace weight 0.5; only text is length-normalised. Matching every local-name term adds the sum of all query IDFs, including terms absent from the corpus.

`SearchDocument{ID, Text}` retains its public two-field layout. ID supplies the name; callers should exclude it from Text. `SearchCodemodeTools` supplies namespace name, description and instructions through the internal parallel field. Both codemode and deferred-tool search stop repeating names in text. The ranker performs no storage writes or tool loading.

ASCII tokenisation now splits digit/capital and acronym boundaries, uses the pinned 21 stop words and folds `searches` to `search`. Pinned main also stems `status` to `statu`; the old v1.2 test expectation changed after direct verification against `tokenize` in the pinned source. The independent BM25 formula and its 1e-14 tolerance are unchanged.

### Development checks

On c43ab6c, the new test file fails ten subcases: two token cases and eight ranking cases. After the implementation:

- Five token subcases and twenty ranking subcases pass. Ranking exercises public `RankDocuments` and production `SearchCodemodeTools`; namespace-bearing fixtures use codemode.
- Five independent score cases check name, namespace, repeated namespace terms, text and unseen-query IDFs. Name normalisation checks BOM versus NEL trimming and supplementary UTF-16 characters.
- The pinned TypeScript test file passes under Bun: 27 tests, 43 assertions. Only the `vitest` import and relative source import were adapted to `bun:test` and the isolated source copy; assertions were unchanged.
- Go 1.27.3/GOMAXPROCS=4 root normal tests, affected durable search/tool-search race tests, root vet and module verification pass. Dependencies, accepted assets and v1.4.0 are unchanged.

The combined full-normal/full-durable-race shell exceeded its 280-second tool limit. A separate full durable race run then reached its 180-second suite limit while `TestCodemodeUTF16MainQuickJSExecutionReopen/remote/journal` had been active for four seconds. That exact subtest passes independently under race in 8.057 seconds. The full durable race suite has not passed in this increment; acceptance must rerun it with a suitable suite budget and investigate any actual stall. No CPU/allocation profiling was performed during these development checks.

### Unicode and remaining work

Native tokenisation still uses ASCII word boundaries. The pinned implementation applies NFC and `Intl.Segmenter` dictionary segmentation. Its eight Unicode token fixtures, eight Unicode ranking fixtures, mixed-language identifiers and canonical-equivalence case need native equivalents. Non-ASCII case-folding in exact-name normalisation also needs review. Passing the source's 27 tests under Bun does not establish native Unicode parity. The corpus row remains partial.

The eight new source test files have the following lexical declaration counts. Table expansions are separate; these counts are not executed native cases.

| Source test file | Direct test declarations | `each` declarations |
| --- | ---: | ---: |
| session-cached-storage | 7 | 0 |
| session-conversation-tails | 5 | 0 |
| session-group-commit | 34 | 0 |
| store-storage-commit | 16 | 3 |
| harness-context-range | 1 | 0 |
| harness-group-commit | 17 | 0 |
| harness-idle-scopes | 14 | 2 |
| search | 11 | 2 |

`src/testing/commit-writes.ts` is a helper, not a ninth test file. The conversation-tail assertions include retained reads, commit updates, empty roots versus forks, older cutoffs, 4096-conversation/256-marker limits and immutable records; native mapping is incomplete. Other new files still need assertion-level review and native tests. Next: cached-storage and conversation-tail tests against native storage interfaces, followed by commit/publication ordering and fault paths.

Pinned references: `packages/durable/src/search.ts` and `packages/durable/test/search.test.ts` at `81f3cd412758ebdd92324c7d63b669a189408d0a`. Disposable source copies, failed-run binaries/fixtures and run scratch were removed after analysis. Full convergence, dependency/support review, staticcheck closure and acceptance/profiling are incomplete.
