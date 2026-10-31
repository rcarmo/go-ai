## Vertex real-key collection URLs

Real Vertex API keys now use collection URLs without requiring a project or location. The pinned `createApiKeyClient` omits both fields; native previously required them and constructed project-scoped requests even with real keys.

Target: `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/ai/src/api/google-vertex.ts` and its nine-case `google-vertex-api-key-resolution.test.ts`. The old native URL-helper tests did not exercise ADC acquisition, environment resolution or the registered HTTP path.

`vertex_key_main_test.go` adds eight URL cases and three registered loopback HTTP cases. Before production changes, all eleven fail: missing project/location, an unwanted project path, or failure before HTTP. The environment case first used the helper's custom provider alias; correcting it to the canonical Vertex provider established the missing-project failure. That initial credential-lookup failure was a harness issue.

| New cases | Expected result |
| --- | --- |
| Default, blank or generated-placeholder base URL | Global Express host, `/v1/publishers/google/models/...` |
| Custom root | Append `/v1/publishers/google/models/...` |
| Custom `/v1`, `/v1beta2/` or versioned collection | Preserve the version and collection path |
| Project/location hints with a real key | Omit project/location from the default request path |
| Option, model or scoped environment key | Successful HTTP stream without project/location |
| Explicit User-Agent and custom header | Preserve caller values in all three HTTP cases |

The locked SDK 2.21.0 archive was checked against the pinned lockfile SHA-512. Its constructor selects the global Express host for a key without a project. A probe executed the actual URL methods (`getRequestUrlInternal`, `constructUrl`, `shouldPrependVertexProjectPath` and supporting methods) in an anonymous class with source-derived client options. Five default/custom/versioned inputs produced the expected URLs without project prefixes. No installation, package import or auth code ran. An initial object-literal extraction failed because class methods require separators in object literals; the corrected anonymous-class probe passed. This was a probe syntax error, not a SDK behaviour failure.

The implementation changes only the real-key URL branch. It trims custom bases, discards generated `{location}` placeholders, recognises source-style version segments and keeps existing model/key escaping. Two old native expectations contradicted the source/SDK: the project-hint audit now checks Express and retains its exact regional/project path as an ADC-marker control; the custom-key test now requires the exact collection URL.

## Nine-case upstream disposition

This file remains development-verified-partial. Real-key routing, generated base handling, custom/versioned key collections and explicit headers have native checks. Model-level keys are an additional native compatibility control, not a source test. The following work is still required:

- The provider does not acquire ADC tokens itself. Marker-to-URL tests cannot establish ADC authentication, key-file handling, scopes or credential refresh.
- Marker/environment fallback and exact marker classification need production-path tests. Generic native credential precedence and provider-scoped isolation are unchanged.
- ADC custom collection URLs still use native project-scoped construction; the SDK's `ResourceScope.COLLECTION` suppresses that prefix. Default global/multi-regional ADC host selection also needs review.
- Versioned ADC bases, the complete nine-case constructor/header crosswalk and live credentials need further verification.
- Query-based key transport and model-name/path escaping are preserved native contracts; SDK auth-header and endpoint composition need their own review with existing isolation fences intact.

## Development checks

Go 1.27.3, `GOMAXPROCS=4`: all eleven cases, Google normal/race, full root normal, root vet, Google-scoped staticcheck v0.8.2 and `git diff --check` pass. The SDK archive, extracted source, failed-probe scratch and disposable logs were removed after analysis. No dependencies, catalogues or release refs changed. Broader convergence, the 23 root staticcheck findings outside Google, acceptance and profiling are incomplete.
