# llama.cpp completion hooks report each HTTP attempt

The llama.cpp classifier now reports each completion response before reading its body. A failed completion followed by a retry reports both statuses in order. Tokenize and template requests still bypass payload and response hooks. This change opts in only at the llama.cpp completion call site; it leaves retry classification and other providers unchanged.

## Pinned originals

All 16 unchanged `packages/ai/test/llama-cpp-classify.test.ts` cases passed at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`, including successful hook replacement and two failed completion attempts. Seven files were byte-compared with pinned Git. The test SHA-256 was `8ade7041462c422e62974f1bbcde9068627e79b7112a2a56b0ce6f69ea1a5652`.

The Node 26.13.0 and Vitest 4.1.12 run used a disposable copy, scrubbed environment and isolated network namespace:

```sh
node ../../node_modules/vitest/vitest.mjs run test/llama-cpp-classify.test.ts \
  --pool=forks --maxWorkers=1 --reporter=json
```

A separate capture reproduced one completion hook for malformed JSON (200), an unreadable error body (503) and a rejecting hook (503). All three returned an error stop. These captures confirm the header-phase behavior of `llama-cpp-classify.ts:260–265` independently of the original assertions.

## Native comparison

On unchanged Go base `7dc7844ad04e576051f1934a15d6bd86bcaf32c1`, the existing classifier witnesses and the new scripted success case passed. The two 503 completion requests returned an error stop with no response hooks. Unreadable-body and rejecting-hook controls also produced no callbacks.

`TestSourceV161LlamaCPPResponseHooks` uses public `Classify` and an inert HTTP transport. It compares hook count, statuses, request order and byte-identical retry bodies, and checks that the successful payload replacement reaches the wire. The unreadable-body control checks hook-before-read order; a rejecting hook closes the unread body without making another attempt. The existing malformed-response matrix now uses the pinned header-phase expectation for completion; preflight silence, error diagnostics and body bounds remain covered.

Four compiled controls reported only the final response, observed preflight requests, ignored rejection or removed response headers. Each failed the intended leaves. Restored verification used Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./tests -run '^TestSourceV161LlamaCPPResponseHooks$' \
  -count=3 -shuffle=42 -p 1 -timeout 40s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test . ./tests \
  -run '(Classifier|Decisions|LlamaCPP|Provider.*Retry|ProviderErrorResponse)' -count=2 -shuffle=42 -p 1 -timeout 65s
go vet -p 1 . ./tests
```

Staticcheck 0.8.2 also passed. No live llama.cpp server, catalogue changes or other classifier hook changes are included.
