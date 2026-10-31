## Vertex known-file ADC

Vertex streams can now acquire credentials from the known gcloud ADC file when `GOOGLE_APPLICATION_CREDENTIALS` is empty. Unix uses `HOME/.config/gcloud/application_default_credentials.json`; Windows uses `APPDATA/gcloud/application_default_credentials.json`. Explicit filenames take precedence and retain their file errors without falling back.

`VertexADCCredentialPath` shares candidate selection between native credential-presence checks and the Google provider. It performs no filesystem I/O. Scoped empty HOME/APPDATA values suppress the default location; the provider checks candidate existence before calling the existing typed service-account/authorised-user loader. Real API keys skip file discovery/acquisition. The library's default detector is never called, and no metadata request is made.

The pinned Google Auth 10.6.2 archive matches upstream 81f3cd41's lockfile integrity. Its `_tryGetApplicationCredentialsFromWellKnownFile` uses Windows APPDATA or Unix home/.config and loads an existing gcloud file after the explicit-environment check. The provider registry also checks file presence plus project/location. Scoped HOME/APPDATA selection extends the native provider-environment isolation contract; the SDK itself uses process-global home discovery. Native suppression and the narrower trusted-file boundary apply here.

## Tests

`vertex_default_adc_main_test.go` has nine production-path cases. Six fail against the earlier explicit-file-only implementation; all nine now pass:

| Cases | Result |
| --- | --- |
| Process home / scoped home / ADC marker | Acquire from the selected known file, then infer with matching token/quota |
| Explicit filename plus valid default | Select the explicit file |
| Missing explicit filename | Fail without default-file fallback or token/inference requests |
| Corrupt default file | Fail before token/inference requests |
| Scoped empty home | Suppress ambient default selection and credential-presence marker |
| Real API key | Infer in key mode without touching the configured credential file |
| Absent default file | Fail without token/inference requests; include Vertex model/diagnostic in the assistant error |

Eight root path-selector cases check Unix/Darwin home, Windows APPDATA, scoped empty values, explicit precedence, preserved filename whitespace and process fallback. Windows selection is exercised as a host-side branch test; it has not been runtime-tested on Windows. The provider's missing-credential error now includes the Vertex assistant carrier while retaining the pinned `No API key for provider: ...` diagnostic. Gemini's missing-key path is unchanged.

All homes, credential files and HTTP endpoints are test-owned. Canonical token transport is mapped to an owned fake server and rejects unexpected URLs before network I/O. A fake ambient credential filename/quota cannot override scoped empty values. Existing synthetic routing tests explicitly suppress home/default-file discovery; their scope is still routing. Root validation also used an empty owned process HOME/APPDATA. No live credential files or metadata endpoints were used.

## Checks and limits

With Go 1.27.3/GOMAXPROCS=4, root normal tests, root/Google race, root vet, Google-scoped staticcheck v0.8.2, module verification and the vulnerability gate pass. Govulncheck reports no reachable vulnerabilities for that toolchain. The Windows/amd64 Google package cross-build passes with CGO disabled. Dependencies, accepted SBOM/assets, catalogues and v1.4.0 are unchanged. Temporary source archives/excerpts and disposable test output were removed after use.

Missing/unusable default paths keep the native missing-key preflight; marker-only callers retain their existing manual-header path when no file is selected. Google Auth's metadata/GCE fallback, external accounts, impersonation, non-default universes, unusual field coercions and broader auth-header/retry behaviour are unsupported or unreviewed. In-place writes and stalled filesystem calls retain the [file-I/O limits][io]. The nine-case source Vertex crosswalk stays partial; full semantic acceptance and CPU/allocation profiling are incomplete.

[io]: vertex-file-io.md
