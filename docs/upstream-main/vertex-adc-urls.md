## Vertex ADC URL selection

Vertex ADC-style routing now uses the locked SDK's global/multi-regional hosts and custom collection paths. The provider still does not acquire ADC tokens.

Source: `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/ai/src/api/google-vertex.ts` (`createClient`, `buildHttpOptions`, scope resolution), and locked `@google/genai` 2.21.0. The SDK archive was checked against the pinned lockfile integrity. Its constructor's default-host branches and exact multi-region set (`us`, `eu`) were inspected. Seven URL-method probes ran the actual SDK `getRequestUrlInternal`, `constructUrl`, `shouldPrependVertexProjectPath` and supporting methods with source-derived client options. No package import, installation, auth implementation or credential file ran.

`vertex_adc_url_main_test.go` adds fourteen cases. Ten fail against 33866ca before production edits; four controls pass. All fourteen now pass:

| Cases | Routing |
| --- | --- |
| Default regional host | Keep regional host and project/location resource prefix |
| Global location | `aiplatform.googleapis.com`, retaining project/location prefix |
| Multi-regional `us` / `eu` | `aiplatform.us.rep.googleapis.com` / `aiplatform.eu.rep.googleapis.com`, retaining resource prefix |
| Blank/generated-placeholder base | Select the SDK default host |
| Custom root, `/v1/`, `/v1beta2`, versioned collection or global custom root | Collection path, without adding project/location or a second API version |
| Missing project / missing location with a custom base | Reject before constructing a URL |
| Registered HTTP with explicit synthetic caller Authorization | Correct collection path, no query key, caller header unchanged |

The SDK probes cover regional/global/us/eu defaults and three custom collection/version forms. The native pre-existing custom-ADC test expected an extra project prefix; its assertion now requires the exact SDK-style collection URL. Existing regional and fully qualified custom-collection controls still pass. Model/resource escaping and real-key routing are unchanged.

The HTTP test supplies `Bearer caller-test-token` explicitly. It verifies URL/header ownership only; it cannot establish token acquisition, key-file loading, credential scopes, refresh or ADC success. Neither URL helper tests nor synthetic HTTP authentication close those contracts.

## Development checks

Go 1.27.3, `GOMAXPROCS=4`: full root normal, Google race, root vet, Google-scoped staticcheck v0.8.2 and `git diff --check` pass. Raw SDK files were removed after inspection/probes. No dependencies, catalogues or release refs changed. Acceptance, CPU/allocation profiling and broader convergence are incomplete; the 23 root staticcheck findings outside Google still need review.

The [nine-case Vertex crosswalk](vertex-api-key-resolution.md) stays partial. Marker/environment fallback, exact placeholder classification, ADC acquisition/refresh and SDK auth-header/endpoint composition need further work. Provider-scoped credential isolation is unchanged.
