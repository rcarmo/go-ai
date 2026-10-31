## Explicit Vertex service-account acquisition

Vertex streams can now acquire tokens from an explicitly configured service-account file. `GOOGLE_APPLICATION_CREDENTIALS` uses provider-scoped environment resolution. The adapter requests cloud-platform scope, acquires before inference even with caller Authorization, merges absent Authorization/quota headers and applies native header suppression afterwards. A second typed path supports [explicit authorised-user files](vertex-authorised-user.md). Default gcloud discovery and other ADC credential types are unsupported.

Reference: `81f3cd412758ebdd92324c7d63b669a189408d0a`, Vertex `keyFilename` wiring and locked SDK `NodeAuth`/`buildGoogleAuthOptions`. The earlier [review](vertex-adc-adapter-review.md) records source acquisition/header ordering. This increment covers explicit service-account files; it does not close the nine-case Vertex crosswalk.

## Dependency choice

`cloud.google.com/go/auth` v0.24.2 is now a direct dependency. It supports the existing Go 1.26.0 module minimum and typed credential loaders, per-call token contexts, configurable HTTP clients and synchronous refresh. The SBOM identifies auth v0.24.2, compute/metadata v0.10.0 and enterprise-certificate-proxy v0.3.23 as Apache-2.0; gax-go/v2 v2.27.0 is BSD-3-Clause. Redistribution notices require release-packaging review before publication.

An isolated comparison counted 19 non-standard packages for `cloud.google.com/go/auth/credentials` and 11 for `golang.org/x/oauth2/google`; neither selected import graph includes Google API or gRPC. The shared comparison module selected metadata v0.10.0 for both candidates. These counts are import-graph measurements, not runtime performance measurements. `go mod tidy` also fetched dependency-test support such as Google API/gRPC; those downloads do not change the selected credentials import graph. No OAuth/JWT cryptography was hand-written. `go mod tidy` also reclassifies the existing AWS eventstream requirement as direct without changing its version.

## Production boundary and tests

`vertex_auth.go` uses the typed service-account loader with request-owned credentials, synchronous refresh, an explicit native HTTP client and disabled dependency logging. It never invokes default ADC detection or GCE metadata. It avoids the library quota accessor's process-global environment lookup: scoped `GOOGLE_CLOUD_QUOTA_PROJECT` resolution and the validated file field supply the default header instead.

`vertex_service_account_main_test.go` first established seven failures on unchanged b4e601d. Those cases now pass. Three supplementary suppression/endpoint controls also pass. A canonical-endpoint transport control additionally exposed a selected-library mismatch before its correction:

| Cases | Contract |
| --- | --- |
| Configured file with marker / empty key | Acquire once, then infer with acquired token and quota header |
| Existing caller Authorization/quota | Still acquire once; preserve caller values |
| Token endpoint failure | One assistant error with model/diagnostic; no inference |
| Cancellation during acquisition | One aborted assistant terminal; no inference; token handler/request released |
| Unsupported credential type / malformed JSON | Reject before token or inference requests |
| Suppressed Authorization/quota | Acquire, then keep both headers absent on inference |
| Malformed `token_uri` | Ignore the field, as Node JWT.fromJSON does; still use canonical endpoint |
| Token redirect | Fail before inference or any noncanonical token transport attempt (native isolation fence) |

Fixtures generate an isolated RSA key and 0600 credential file beneath the configured project run directory. Transport-only interception routes the canonical token URL to an owned fake server; unexpected URLs are rejected before network I/O. The credential file's custom/malformed `token_uri` is ignored. Fake token endpoints check the JWT grant, RS256/key ID, issuer, canonical Google audience, cloud-platform scope, bounded timestamps and cryptographic signature. Fake inference endpoints check request headers and ordering. Cancellation is synchronised with token-request arrival rather than a short timing assumption. A fake process quota setting plus scoped empty override checks isolation. Synthetic routing-only tests now explicitly mask ambient credential-file configuration; they still do not count as acquisition tests. No live credentials or metadata endpoints were used.

## Limits and follow-ups

- Trusted explicit `service_account` and `authorized_user` files are supported. Files must be regular and at most 4 MiB. Non-default universes and other credential types fail closed. Locked google-auth-library 10.6.2's JWT.fromJSON does not read `token_uri`; internal `gtoken/getToken.js` fixes `https://oauth2.googleapis.com/token`. Integrity was verified against the pinned lockfile. Go's loader instead honours that file field: a failing transport-isolated control caught this, so the adapter now normalises only that field using raw JSON values before calling the typed loader. The native token client also refuses redirects and closes owned idle connections; full source redirect-equivalence review remains open. These incremental constraints differ from the full SDK configuration domain and require disposition before full convergence.
- Default files, external accounts, impersonation, metadata/GCE and universe variants need separate isolated fixtures and implementations. Explicit authorised-user refresh grants have their own [tests](vertex-authorised-user.md). Empty-key/default-ADC streams without an explicit file retain the existing native missing-key behaviour.
- No shared token cache exists: each stream rereads the file and owns its credentials. [File/account controls](vertex-authorised-user.md) verify repeated streams, atomic replacement, deletion/corruption, restoration and concurrent accounts. [Unix nonblocking open and descriptor checks](vertex-file-io.md) close the FIFO/directory/oversized stat-open replacement window and preserve regular-file symlinks. Metadata/regular-read stalls, in-place writes and non-Unix runtime behaviour still need review before broader file guarantees.
- Known service-account Authorization/quota headers are covered. Other returned auth headers, auth retries, higher-level registry discovery and the full constructor/header crosswalk remain open. Caller/model header precedence and native suppression/isolation fences are preserved.
- Query-key transport, model escaping, error wording/timestamps and wider SDK/provider convergence remain open. No full ADC or acceptance status is assigned.

## Development checks

With `scripts/project-env.sh`, project-owned run `TMPDIR`/`TMP`/`TEMP`, Go 1.27.3 and `GOMAXPROCS=4`: root normal tests, Google race, root vet, Google-scoped staticcheck v0.8.2, `go mod verify` and `git diff --check` pass. Dependency gates also pass: a scratch CycloneDX 1.6 SBOM validates with 28 components; pinned govulncheck v1.7.0 reports no reachable vulnerabilities for Go 1.27.3; go-licenses v1.6.0 accepts dependency licences, with eight existing non-Go-code inspection warnings (x/sys, wazero, websocket and compress). Accepted canonical SBOM/assets were not overwritten. Disposable import-graph reports, temporary module files and test credential scratch were removed after use. Catalogues and v1.4.0 are unchanged. Root staticcheck's 23 findings outside Google, acceptance and CPU/allocation profiling remain incomplete.
