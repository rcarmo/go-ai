# OpenAI Completions replays the saved reasoning formats without a production change

The existing OpenAI Completions adapter round-trips saved reasoning fields, structured detail arrays and opaque envelopes. It ignores malformed signatures and still accepts an encrypted detail stored on a legacy tool call when its ID is non-empty.

## Pinned originals

All 16 unchanged `packages/ai/test/openai-completions-reasoning.test.ts` cases passed at official pi v1.7.0 `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The test SHA-256 was `107d8a2a9a9ba069fcbf5a3c6b6fca24a831b35c3e43d03086707aa04ce812e7`. The test, Completions adapter, reasoning helper and message transform matched pinned Git bytes. The lane used the [shared Node receipt](ai-originals.json) and verified stage read-only. Node 26.13.0 and Vitest 4.1.12 ran offline.

## Native storage and replay

The native tests use the public `Stream` entry point and an inert transport. Four round-trip rows stream an assistant message, serialise it to Go's transcript JSON, read it back and capture the next request. They check the exact `thinkingSignature` string and the selected replay fields. Three rows submit saved plain, structured and opaque formats directly. Nine rows use the original malformed values; the undefined and explicit-empty cases seed the corresponding Go message forms. The two legacy rows verify that a tool call with an encrypted detail replays it and that an empty detail ID does not. All semantic comparisons passed on unchanged base `eb11f6b1f508915e577b8a7a98497e2768206c87`; there is no production diff.

Seven of eight assertion sites are fully mapped, spanning 18 of 19 applicable comparisons. The remaining site is partial: the Node original spies on calls to `JSON.parse`, while Go checks only the same wire outcomes for the three bare inputs. The existing Go structured-detail decoder still attempts to decode nonempty bare strings. This packet does not claim parse-elision parity or change that path.

## Compiled controls

Five compiled controls failed the relevant rows: omit opaque replay, omit plain `reasoning`, drop stored detail arrays, accept an invalid opaque field, or accept an empty legacy encrypted ID. After byte restoration, focused tests, the full OpenAI provider package, vet and Staticcheck 0.8.2 passed with Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./inference/provider/openai \
  -run '^(TestSourceV170Completions(StoredReasoning|OpaqueReasoning)|TestCompletions(StoredOpaqueReasoning|OpaqueReasoningBoundaries)V170)$' \
  -count=3 -shuffle=42 -p 1 -timeout 45s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test \
  ./inference/provider/openai -count=1 -shuffle=42 -p 1 -timeout 75s
unshare -Un go vet -p 1 ./inference/provider/openai
```
