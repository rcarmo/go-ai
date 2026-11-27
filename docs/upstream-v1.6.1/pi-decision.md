# Pi Decision sends canonical classifier requests

The Go classifier registry now handles `pi-decision`. It sends `POST <baseUrl>/decision` with a model ID, classifier context and optional session ID. It returns typed boolean, choice and score answers, the backend response ID and reported cost. Text-only models reject images before transport.

## Pinned originals

All five unchanged cases in `packages/ai/test/pi-decision.test.ts` passed at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test SHA-256 was `abd112be868a0522c05394edf6a213a72a04519d8592f5e5fb88e92dfad4a55e`. Nine staged files, including the test, adapter, shared classifier transport, error/retry helpers and config, were byte-compared with pinned Git.

The test ran offline with Node 26.13.0 and Vitest 4.1.12 from a disposable checkout and scrubbed environment:

```sh
node ../../node_modules/vitest/vitest.mjs run test/pi-decision.test.ts \
  --pool=forks --maxWorkers=1 --reporter=json
```

## Public Go comparison

On unchanged base `5c86bc760aabb93fe20e9310d610931ad7c5a016`, the existing classifier tests passed. An inert public `Classify` probe with the pinned model/API IDs compiled and returned `no classifier provider registered` with zero HTTP calls.

The five `TestSourceV161PiDecision*` witnesses use the public API and an inert transport. They cover the single canonical POST, all three answer types, response ID and authoritative usage, HTTP 402 explanation and empty answers, a 503 response hook before an unreadable body, four invalid answer cases and image forwarding/refusal. The fixture uses the pinned custom model and adds no catalogue records. Header checks use the existing native canonical header spelling.

Seven compiled controls independently omitted the session, changed billed cost, omitted the response ID, advanced body reading ahead of the response hook, accepted unknown choices, accepted an out-of-range probability or omitted images. Only the target witnesses failed. Restored tests, vet and Staticcheck 0.8.2 passed with Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./tests -run '^TestSourceV161PiDecision' -count=3 -shuffle=42 -p 1 -timeout 40s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test . ./tests \
  -run '(Classifier|Decisions|PiDecision|Provider.*Retry)' -count=2 -shuffle=42 -p 1 -timeout 60s
go vet -p 1 . ./tests
```

Pi Decision reports the final HTTP status and headers before reading the body. The existing classifier helper still does not report intermediate HTTP responses consumed by its retry loop. No live service, unusual SDK error metadata or arbitrary JavaScript-number token counters are claimed; Go keeps its existing integer usage counters and the other classifier adapters keep their established hook timing.
