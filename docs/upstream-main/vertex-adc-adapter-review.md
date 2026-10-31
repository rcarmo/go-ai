## Vertex ADC adapter review

Vertex ADC needs credential acquisition and SDK-compatible auth-header merging. Native currently supplies neither automatically; the routing and synthetic-header tests do not establish ADC support.

Target: `81f3cd412758ebdd92324c7d63b669a189408d0a`. No native production or dependency files changed during this review.

## Source flow

- `packages/ai/src/providers/google-vertex.ts` resolves a stored key before `GOOGLE_CLOUD_API_KEY`. ADC discovery checks the configured credential path or the gcloud default file, plus project/location. The provider registry/file-system abstraction and the runtime adapter are separate layers.
- `packages/ai/src/api/google-vertex.ts` trims/classifies its option key. Without a real key, it creates a Vertex client with project/location and optional `googleAuthOptions.keyFilename`. Empty-key/default-ADC handling therefore cannot be closed by the native missing-key preflight.
- Locked `@google/genai` 2.21.0 constructs `GoogleAuth`; the root lockfile selects `google-auth-library` 10.6.2 (Apache-2.0).
- The SDK's `buildGoogleAuthOptions` defaults scopes to `https://www.googleapis.com/auth/cloud-platform` and rejects explicit scope sets missing that scope.
- `NodeAuth.addGoogleAuthHeaders` awaits `getRequestHeaders(url)` first, then adds each returned header only if absent. Existing caller Authorization does **not** bypass credential acquisition. A future adapter cannot use the current synthetic caller-header tests as proof of this behaviour.
- API-key mode uses the SDK's key-header path, preserving an existing header. Native query-key transport is still an open crosswalk item.

The exact SDK archive was integrity-checked against the pinned lockfile, inspected without package installation/import/auth execution, then deleted with its extracted source. No live credentials or metadata service were queried.

## Go candidates inspected

Both candidates support the repository's Go 1.26.0 module requirement. Versions are assessment inputs, not new requirements in `go.mod`.

| Candidate | Findings |
| --- | --- |
| `golang.org/x/oauth2` v0.37.0 | BSD-style licence; Go 1.26.0. Google typed `CredentialsFromJSONWithType` APIs reject unexpected credential types. Untyped loaders are deprecated for security risk. Includes a compute-metadata dependency; default discovery can probe GCE metadata. Token-source context/client lifetime and returned-header coverage need mapping. |
| `cloud.google.com/go/auth` v0.24.2 | Apache-2.0; Go 1.26.0. Typed `credentials.NewCredentialsFromJSON/File`, context-bearing token retrieval, `QuotaProjectID`, a configurable token HTTP client and synchronous refresh controls fit the pending contracts. Untyped `DetectOptions.CredentialsFile/JSON` are deprecated for security risk. Declared module dependencies include Google API/gRPC; the actual selected import/build graph must be measured before adoption. |

Both libraries warn that even typed loading requires validation of externally sourced credential configuration. Credential type, token/service URLs, external-account or executable sources and universe settings cannot be accepted blindly. Do not call default detection during tests: it can consult process-global files/environment and metadata independently of native provider-scoped options.

## Next implementation boundary

1. Inspect the cloud-auth candidate's selected import graph and licence/security impact before changing `go.mod`; compare with OAuth2 if the graph is disproportionate. Do not hand-roll JWT/OAuth cryptography.
2. Add failing registered-path tests against unchanged native code using a project-owned isolated credential file and loopback token/inference servers. First cover explicit service-account file acquisition, cloud-platform scope, token/header delivery and acquisition failure with no inference request. Fake credentials and endpoints only.
3. Use typed credential loaders and explicit provider-scoped file selection. Keep account/endpoint/token caches isolated; verify file replacement, refresh and cancellation before sharing sources between streams. Avoid background refresh surviving its owner.
4. Match all returned auth headers and caller/model/options precedence. Preserve native suppression/isolation fences while reviewing any source conflict. Update synthetic routing fixtures to include isolated fake acquisition when testing the full SDK contract.
5. Cover authorised-user/default gcloud files, empty keys, scopes, refresh, quota-related headers, explicit-invalid-path behaviour and higher-level registry discovery. Metadata/GCE, external accounts and impersonation require separate isolated fixtures; do not probe live services to fill coverage gaps.

No adapter or dependency is selected yet. ADC acquisition remains an implementation gap, not an accepted divergence. Source nine-case Vertex disposition stays development-verified-partial. Full convergence, the 23 root staticcheck findings outside Google, acceptance and profiling are incomplete.
