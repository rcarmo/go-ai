# OpenAI Decisions splits cached input usage

OpenAI Decisions counts cache reads and writes separately from fresh input. The adapter bounds the two cache counts by reported input, keeps the derived token total, and prices each bucket at the model's rate. System One accounting and the generated catalogue are unchanged.

## Pinned originals

All 12 unchanged cases in `packages/ai/test/openai-decisions.test.ts` passed at official pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test SHA-256 was `7eb9b37c560e925ccc6e8aafeaa78478aec9f7c18cf8648e776bbfb953bac4ca`. Fifteen scratch files, including the test, Decisions adapter, usage and retry helpers, catalog loader and OpenAI provider sources, matched pinned Git bytes. The OpenAI provider JSON and data manifest matched the released pi-ai archive at SHA-256 `192c407bd69c06358c68519aa6290adc8362b132bee9117480c7604456d5073b`. Node 26.13.0 and Vitest 4.1.12 ran the originals offline in a scrubbed checkout:

```sh
node ../../node_modules/vitest/vitest.mjs run test/openai-decisions.test.ts \
  --pool=forks --maxWorkers=1 --reporter=json
```

## Native comparisons

The two billing cases use the public `Classify` entry point and an inert HTTP transport. The new case uses the generated OpenAI Decisions classifier model `gpt-6-luna` and the pinned response: 2,000 input tokens include 1,600 cache reads and 100 cache writes. Unchanged base `a872d0da5c5b5634c6ac377f2ee0e0b9d28aeed0` reported 2,000 fresh input and no cached tokens. It already returned total cost `.0002` because the Luna catalogue has the same rate for all input buckets. The fixed result reports `300 / 0 / 1600 / 100 / 2000` for fresh input, output, cache read, cache write and total, at the same `.0002` cost. The original long-context case's custom model still prices 300,000 input tokens at `.06`.

Separate native checks cover cache clamping, invalid detail values and derived totals. A model copy with distinct rates checks each cost component above the threshold when only total input, including cached input, qualifies for the tier. Response checks retain billed usage when a decoded answer is invalid, leave absent usage unset and reject usage in an HTTP error body. These checks are supplemental; the ledger maps three direct sites across the two original billing cases.

## Compiled controls

Five changes failed only the relevant native tests: ignore cache buckets, omit the write clamp, keep the pre-split cost, trust the reported total or discard billing after an answer error. After byte restoration, the focused tests, classifier and retry regressions, vet and Staticcheck 0.8.2 passed with Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./tests \
  -run '^(TestSourceV161OpenAIDecisions|TestOpenAIDecisionsUsage.*V161)' \
  -count=3 -shuffle=42 -p 1 -timeout 40s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test . ./tests \
  -run '(Classifier|Decisions|SystemOne|Provider.*Retry)' -count=1 -shuffle=42 -p 1 -timeout 65s
unshare -Un go vet -p 1 . ./tests
```

Go still uses its existing integer token counters. The tests sent no live requests.
