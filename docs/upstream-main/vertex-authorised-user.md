## Vertex authorised-user files

Vertex streams now accept an explicit `authorized_user` file through provider-scoped `GOOGLE_APPLICATION_CREDENTIALS`. The adapter uses cloud-auth's typed `AuthorizedUser` loader, validates the three required string fields before token I/O and acquires a refresh grant for each stream. [Known gcloud files](vertex-default-adc.md) also use this loader; metadata fallback is unsupported.

The pinned Google Auth 10.6.2 archive matches upstream 81f3cd41's lockfile integrity. `UserRefreshClient.fromJSON` requires `client_id`, `client_secret` and `refresh_token`; `OAuth2Client.refreshTokenNoCache` sends those fields with `grant_type=refresh_token` to `https://oauth2.googleapis.com/token`. It ignores credential-file `token_uri`. Go's `handleUserCredential` uses `Options3LO` with `StyleInParams`, which matches the form and keeps client secrets out of Basic Auth headers. Refresh grants contain no JWT assertion or scope parameter.

`vertex_authorised_user_main_test.go` has nine cases. Six fail against the unchanged service-account-only implementation; the three missing-field controls already fail closed. All nine pass with the typed user loader:

| Cases | Result |
| --- | --- |
| Acquire, empty API key, malformed `token_uri` | Refresh at the fixed Google URL, then infer with the access token and file quota |
| Repeated acquire | Two streams send the configured refresh token; a returned rotated token does not enter a shared cache or rewrite the file |
| Caller Authorization/quota | Still acquire; preserve caller values on inference and keep caller Authorization out of the token request |
| Grant failure / cancellation | One assistant error/aborted terminal with model/diagnostic; no inference |
| Missing client ID, secret or refresh token | Fail before token and inference requests |

The tests intercept only token transport and reject unexpected URLs before network I/O. All credential files, fake secrets and HTTP endpoints are owned by the test. No live credentials or metadata endpoints were used.

## File and account isolation

Upstream `api/google-vertex.ts` constructs a fresh GoogleGenAI inside each stream. `vertex_file_isolation_main_test.go` checks the corresponding native file lifetime with service accounts: two repeated streams, atomic replacement with another account, deletion, malformed replacement, restoration, two concurrent accounts and a pre-cancelled call. Six successful streams produce six acquisitions and six inference requests. Failure paths make neither request.

The two accounts deliberately share endpoint, project and key ID while using different RSA keys, emails and quota values. The token endpoint validates each issuer/signature; inference checks the matching bearer token and quota. A fake ambient credential file and quota setting cannot override scoped values. These controls pass without a production change to file ownership or caching.

## Checks and remaining work

Root normal tests, Google race, root vet, Google-scoped staticcheck v0.8.2, module verification and the pinned vulnerability gate pass with Go 1.27.3/GOMAXPROCS=4. Govulncheck reports no reachable vulnerabilities for that toolchain. Dependency manifests and accepted SBOM/assets are unchanged; the preceding dependency licence/SBOM review still applies. Temporary source archives, extracted code and disposable test output were removed after use.

File bounds and the [existing isolation fences][file] also apply to user files: trusted regular files up to 4 MiB, default universe only, known-file discovery only, no shared credential cache and no token redirects. [Unix FIFO replacement and opened-descriptor checks](vertex-file-io.md) now reject raced non-regular/oversized objects without blocking on FIFO open. Filesystem metadata/regular-read stalls and in-place writes still lack hard cancellation/snapshot guarantees. Strict typed field admission, expiry/rotation behaviour within a reused client, other auth headers/retries, external-account/impersonation/GCE and non-default universes also need review. The nine-case source Vertex crosswalk stays partial; acceptance and CPU/allocation profiling are incomplete.

[file]: vertex-service-account.md
