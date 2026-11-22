# Third-party npm archive receipt: tranche 1

[`npm-archives.jsonl`](npm-archives.jsonl) records nine placements from the [pinned npm dependency ledger](README.md). Each row identifies the npm lock key, version, registry URL and exact SHA-512 integrity. The verifier reads the npm lock directly from official pi v1.6.0, `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

| Package | Version | Retained archive |
| --- | --- | --- |
| `quickjs-wasi` | `3.6.2` | Verified; archived WASM also matches the native embedded asset byte for byte. |
| `@silvia-odwyer/photon` | `0.3.3` | Verified. |
| `canvas` | `3.2.3` | Verified. Native correspondence for the upstream AI fixture generator is unresolved. |
| `vitest` | `4.1.12` | Verified. Neither test-runner equivalence nor actual use in an earlier run is implied. |
| `@anthropic-ai/sdk` | `0.129.0` | Verified. |
| `@aws-sdk/client-bedrock-runtime` | `3.1127.0` | Verified. |
| `@smithy/node-http-handler` | `4.12.1` | Verified. |
| `@google/genai` | `2.32.0` | Unresolved: no archive at the exact integrity key in either checked cache. |
| `openai` | `7.59.0` | Unresolved: no archive at the exact integrity key in either checked cache. |

Verification hashes the **compressed archive bytes** retained at the recorded cache key. It does not trust the filename or derive archive provenance from `node_modules`. All seven matches were in the user npm cache; the project cache had none at these keys. Archive presence does not prove that the original tests used those bytes, and archive absence here does not prove absence in every artifact store.

For `quickjs-wasi`, the verifier also compares `package/quickjs.wasm` from the verified archive bytes with `internal/quickjs/guest/quickjs.wasm` at native commit `ea3b25710f18a4d9e4de52715cf5c311b91cd165`. That proves asset identity, not WASI host, prelude or replay parity. Native locators elsewhere in this tranche are candidates for behavioral mapping. In particular, the Go image processor is not Photon, and the Go AWS/Smithy modules and HTTP transport are not the JS SDK or Node handler.

## Reproduce offline

From the native repository, use Bun and `tar` with a local mirror containing the Oracle pin:

```sh
ledger=docs/upstream-v1.6.0/dependencies
bun run "$ledger/verify-archives.ts" "$pinned_git_mirror" \
  "project=$project_npm_cache/_cacache" "user=$HOME/.npm/_cacache" \
  > /tmp/pi-v160-npm-archives.jsonl
cmp "$ledger/npm-archives.jsonl" /tmp/pi-v160-npm-archives.jsonl
```

Point each label at the same retained npm content for an identical receipt; cache additions change the observation. The verifier reads the caches and Git objects without installing packages, fetching files, or changing the Oracle. A blob with the wrong integrity at the expected key fails the run instead of being counted as absent.

**Next:** locate retained Google GenAI and OpenAI archives; verify transitive Vitest and provider dependencies in the next tranche. Record which archives and built assets the focused Oracle runs actually used. Reconcile runtime and test seams against the [assertion ledger](../assertions/README.md) before promoting native candidates. No exclusions or release acceptance are recorded here.
