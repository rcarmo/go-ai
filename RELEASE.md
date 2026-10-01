# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v1.0.0`
- Upstream tag/gitHead: `a13d35a742c6ef8462812a28fbe1d8c8b7431c32`
- Official npm artifact: `/workspace/tmp/pi-ai-100.tgz`
- Official npm artifact SHA-256: `f39b99c29b8598f175b10840e5d2a81983e7c0ce5cae4d7df83a1007447d2c2b`
- Previous accepted upstream baseline: `v0.99.2` / accepted Go runtime `86a439d949de78ce7e8ba4cd3f975312785c2a73`
- Current repository baseline before this audit: `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e`
- Rollback SHA before the v1.0.0 local candidate: `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e`
- Publication state: blocked. This v1.0.0 candidate is local-only until auditor accepts scope and gates; no commit, push, tag, release, or publication is authorized yet.

## Scope evidence

- Changed paths: `8` canonical rows, stored at `docs/v100/changed-paths.txt`, SHA-256 `b8db49581470036b68078ac093dc6b41eaa92222647b14cf44a92b870d54eab4`.
- Changed tests: `3` rows, stored at `docs/v100/changed-tests.txt`, SHA-256 `fbe3c63453261a58352b5e238f5f9488f33a13016485c7e3a49cce17bfdaaca2`.
- Whole upstream test corpus: `171` rows, stored at `docs/v100/test-corpus-171.txt`, SHA-256 `9d24da3ede393a95a7131b1c9ac494f57d8165161d6eb581109c86809131abfc`.
- Detailed path matrix: `docs/v100/changed-paths-crosswalk.md`.
- Changed-test crosswalk: `docs/v100/changed-tests-crosswalk.md`.

## Current Go implementation/adaptation summary

Implemented or adapted for v1.0.0 locally:

- Exact generated catalog refresh from official v1.0.0 package artifact via checked-in generator: `1532` chat models, `57` image models, and `15` classifier models. The official package has `42` provider modules, with `typesafe` classifier-only and no chat rows, so Go's model-bearing chat provider count remains `41`.
- OpenAI Responses resolves grammar support from transcript-declared tools and provider capability. The same map selects function/custom declarations, call replay and result replay. Foreign provider/API IDs normalize to `fc_<hash>` before the type-prefix gate; custom replay omits foreign raw/fc_/ctc_ IDs while same-source valid ctc_ IDs remain. Different-model IDs are omitted. Missing/null custom input becomes empty text. Production OnPayload tests cover true/false/default capabilities, call+result round-trips, transcript additions and radius#115.
- Anthropic OAuth supports backward-compatible login method selection and default browser login via the shared callback server. Both flows use the PKCE verifier as state. Copy-code accepts raw code, `code#state`, URL and query input; supplied mismatched state fails before token exchange. Zero-value `&AnthropicProvider{}` Login/Refresh uses non-mutating default configuration. Authorization includes `code=true` and official browser/headless labels. The shared completion hook exchanges tokens before success, returns 502 on token failure and prevents repeat exchange. Both 200 and 502 callback responses are length-delimited and flushed before Login completion; graceful shutdown finishes active HTTP responses. Repeated asynchronous browser launch tests cover the prior EOF race. Authorization and refresh requests POST JSON to `https://platform.claude.com/v1/oauth/token`, with `Content-Type` and `Accept` both `application/json`. Authorization sends exactly `grant_type`, `client_id`, `code`, `state`, `redirect_uri`, and `code_verifier`; refresh sends exactly `grant_type`, `client_id`, and `refresh_token`, with no scope. A missing refresh token in the response preserves the existing token. Production-path tests retain the default platform URL and route only the transport to a local server. Live credential/browser completion is N/A.
- Shared OAuth callback HTML uses the official Pi SVG path geometry and fills `#F09082`, `#4D9ABF`, and `#F1BE58`, with HTML-escaped `Signed in to <provider>.` text. Real HTTP tests check geometry, fills, Anthropic text, content type and malicious-provider escaping. Anthropic uses this shared path instead of a private duplicate callback page.
- Version/provenance artifacts for v1.0.0 are added under `docs/v100/`.

## Validation evidence

Focused gates completed so far:

- `go test ./inference/provider/openairesponses -run 'Test.*(Grammar|ForeignToolCall|Namespace|Request)' -count=1` — passed.
- `go test ./oauth -count=1` — passed.
- `go test ./tests -run 'Test(RegisterBuiltinModels|GeneratedModelMetadataParity|V0844CatalogCounts|V0850CatalogCounts|V100)' -count=1` — passed.
- `PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-100/tar/package/dist/models.generated.js ./scripts/check-model-regeneration.sh` — passed for chat/image/classifier exact regeneration.
- `PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-100/tar/package/dist/models.generated.js python3 scripts/test-check-model-regeneration.py` — passed; deliberate text/image/classifier corruption gates fail as expected.

Local gates passed after all consolidated Responses/OAuth and async callback fixes:

- `go test ./oauth -run 'TestAnthropicOAuth|TestV100OAuthCallbackPage' -count=1` — passed.
- `go test ./oauth -count=1` — passed, including endpoint-echo redaction and invalid-selection tests.
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1` — passed.
- `make staticcheck` — passed.
- `make check` — passed, including deterministic tests, vet/staticcheck, logging, regeneration/fault gates, SBOM validation/self-tests, vulnerability and licence checks.
- `make test-repro` — passed, including build, race, regeneration and security gates.
- `git diff --check` — passed.

- `go test -race -overlay /workspace/tmp/pi-ai-audit-100/auditor-test-overlay.json ./oauth -run TestAuditorV100AsyncBrowserCallbackFlushesBeforeLoginCloses -count=3` — passed.
- `go test -race ./oauth -run 'TestV100AnthropicAsyncBrowserCallback|TestAnthropicOAuthBrowserCompletes' -count=3` — passed for async 200/502 delivery and duplicate exchange controls.
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...` — passed.

Logs: `/workspace/tmp/go-ai-v100-consolidated-logs/{auditor-overlay,async-production,oauth,responses,race,staticcheck,check,repro,shuffle,diff-check}.log`. Full candidate inventory including untracked files: `docs/v100/local-candidate-inventory.txt`. Licence scanning reported known assembly-inspection warnings but exited successfully; govulncheck reported no reachable vulnerabilities. This is evidence for the uncommitted working tree on `869b7a6`; its local SBOM records that base HEAD and must be regenerated for an authorised candidate commit. No candidate commit or hosted CI run exists yet. Commit/push/tag/release authorization is withheld.

## Durable SBOM release assets

No v1.0.0 durable SBOM release asset has been created or authorized. The current v1.0.0 work is local-only. Historical durable release assets for v0.99.2 and earlier remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation, fix, adaptation, N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
