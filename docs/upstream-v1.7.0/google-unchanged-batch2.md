# Google unchanged originals batch 2

This bounded receipt owns seven unchanged Google signed, thinking, stream-error and Vertex files from `fb0fe823539f6d04850f6e34748c01f01f212e1f`. No Anthropic, Bedrock or Google batch 1 ownership is included.

| Upstream test | SHA-256 | Pinned blob |
| --- | --- | --- |
| `packages/ai/test/google-shared-signed-empty-blocks.test.ts` | `10247e1390ec36c323b2b36c170aa97bc5c1c98446f5c25819ed2bb82d7962c0` | `2f7a803259c906373a51d9adf955306127143145` |
| `packages/ai/test/google-stop-reason-continuation.test.ts` | `d13917a46c45753350398219f26c75e2f29382b9fafa2305913bbff7e86738b3` | `ee2c00cdc2ef29d4ec7b5b572d9574f06cee8f8f` |
| `packages/ai/test/google-stream-errors.test.ts` | `1489c5438d1d937e29638d0ab35b1e335bf080212202beb0d9584a93b5509ad0` | `f838de5aee2e98543cf8d4bfb48ccd7db4d30fbe` |
| `packages/ai/test/google-thinking-disable.test.ts` | `dc899136ae7d5c367b846aa68fc693cbe20fe12a85dfd8b41ff97fb8e6f22b10` | `3f6e36c7168e21d55f0b6c8ba5e8498d8eb26917` |
| `packages/ai/test/google-thinking-level-map.test.ts` | `4c5c75fdf37fe7570bf8e4121e06b264c111f91878e2c3a80d2d2a80271bd04e` | `35255bdd060592b1799482250950c8ae39237b8f` |
| `packages/ai/test/google-thinking-signature.test.ts` | `81d47d5851bb3b9fa8cfc879df0a26747dd2ffd7c7093d22ce8bac74fb241dc1` | `853a6a60ccd327378989d3a16e6b8b7c27ebee98` |
| `packages/ai/test/google-vertex-api-key-resolution.test.ts` | `3874427500aeedbeba3e29f83e2b8de405d9615c2c76ddc3ebbcb434ec0053c1` | `2e108a01b133ea71424d82c2ad6936d9728b10d5` |

The seven Oracle files match both the recorded SHA-256 values and `git show fb0fe823539f6d04850f6e34748c01f01f212e1f:<path>` from the detached v1.7.0 source checkout. Vitest 4.1.12 ran unchanged on Node 26.13.0 under the shared `/tmp/go-ai/v170-shared-verification.lock` with `NODE_OPTIONS=--max-old-space-size=650`: **42 passed, 9 credential-gated skipped**. The nine skips all belong to `google-thinking-disable.test.ts`; no provider credentials were supplied or requested.

## Native comparison

The unchanged Google production routes already satisfy the deterministic packet. Existing native controls cover continuation mapping and two-adapter request suppression, blocked-versus-empty stream diagnostics, disabled thinking, level maps and token-budget maps, signature parsing/replay, and Vertex key, marker, header and URL resolution. A new source-derived input-side regression covers all four signed-empty transcript cases from the first original. It passes without a production change.

The native selectors use `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2` and the same shared lock. A pre-addition focused selector passed on the unchanged production implementation; the completed selector then passed twice with shuffle. Scoped `go vet ./inference/provider/google`, pinned Staticcheck `v0.8.2 ./inference/provider/google`, and `git diff --check` passed. Production files are unchanged.

Thinking-disable request shaping is represented by deterministic public/native Go paths; the upstream output-count and live-model assertions remain credential-gated. SDK constructor details are represented by observable native URL, header and credential behavior rather than JavaScript object identity.

This is a bounded local audit receipt, not full v1.7.0 acceptance, live credential replay, profiling, publishing or release authorization.
