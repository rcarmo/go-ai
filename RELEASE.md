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
- Accepted runtime SHA: `6795b5235ecd04110c838e48d5958b514f283996`.
- Accepted runtime tree: `ebcbe7f87dd19907a2648f0b256f0a79b26f6105`.
- Runtime commit: `Port pi-ai v1.0.0 runtime and catalog parity`, authored and committed by `Rui Carmo <rui.carmo@gmail.com>`, parent `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e`.
- Original documentation receipt: `05edf10e5bcd284261629b77e56922a04aa6282b`, a `RELEASE.md`-only child of runtime `6795b5235ecd04110c838e48d5958b514f283996`. Both remain historical v1.0.0 parity evidence.
- Publication state: HOLD. A future release-target decision must explicitly choose original runtime `6795b5235ecd04110c838e48d5958b514f283996` or accepted classifier successor `561ae451d16a6b3a31a274472aed7f831f6fb5eb`; never a docs/tooling HEAD. No tags, releases, aliases or durable SBOM uploads are authorised.

## Separately accepted classifier issue #1 successor

The auditor accepted runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb` and its hosted evidence. [Issue-specific contract, migration and evidence](docs/issues/classifier-1.md) records the separate nine-path follow-up, 1271 insertions / 94 deletions, to upstream classifier paths unchanged in the original eight-path v1.0.0 delta. It corrects object state, instructions/criteria, required answer fields, System One/Cloudflare envelopes and llama.cpp rendering/readout. Response hooks follow decoded 2xx success and precede semantic parsing. No catalog, dependency, OAuth, chat/image, workflow or release-tool changes were included.

- Runtime commit: `Fix classifier context and question contract`, normal Rui-authored commit; parent and issue rollback `05edf10e5bcd284261629b77e56922a04aa6282b`. Original runtime `6795b5235ecd04110c838e48d5958b514f283996` was not amended or retargeted.
- Separate documentation SHA: the direct child of runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb` containing this entry, subject `Document classifier issue hosted acceptance [skip ci]`, touching only `RELEASE.md` and `docs/issues/classifier-1.md`. Resolve its exact SHA with `git log -1 --format=%H --fixed-strings --grep='Document classifier issue hosted acceptance [skip ci]'`; the external post-push receipt records the literal SHA. It is excluded from runtime/SBOM acceptance and release targeting.
- Sole normal push CI: [36934719250](https://github.com/rcarmo/go-ai/actions/runs/36934719250), exact classifier runtime head SHA, event `push`, attempt 1, success; check job `110612170075` and fuzz job `110612169810`, all steps passed.
- Sole artifact `11198105688`, `go-ai-sbom-561ae451d16a6b3a31a274472aed7f831f6fb5eb`: downloaded ZIP SHA-256 `7575ef74adfec3d2f70260e606b286a2e8129134f567564852e30e3a35cdb67b` matches the API digest. Hosted inner SBOM checksum `c36564ec7bdd18ca29aaeb6eab8138f84fb00e5fba22ba3fce32c129e39123e1`; corrected local checksum `9abd48d360224279a7cbac5f3067486f28527e5c99a50544d9e70813fa42b232`.
- CycloneDX 1.6 root library `github.com/rcarmo/go-ai`, MIT, version `561ae451d16a`, matching module/version purl and bom-ref, exactly one root full `vcs.revision=561ae451d16a6b3a31a274472aed7f831f6fb5eb`; 18 components / 19 dependency entries / four direct root edges, no dangling refs or local paths.
- Local postcommit and hosted security/licence checks passed: no reachable vulnerabilities for Go 1.26.6; existing assembly-inspection warnings only. Independent structural/delegated comparison found exactly five generator executable hash differences under `/metadata/tools/0/hashes/N/content`; application, dependency, root, graph and licence fields match. Local Go 1.26.3 versus hosted Go 1.26.8 is consistent with those pinned `cyclonedx-gomod v1.12.0` builder differences; the sole cause was not independently proven.
- Evidence: `/workspace/tmp/go-ai-issue1-candidate/{corrected-root12,hosted}/`; rejected full-root-version artifact is labelled under `rejected-root40/`. Auditor independently verified the run, downloaded artifact, checksum, graph and generator-only differences. Docs-only validation does not rerun accepted runtime matrices or regenerate artifacts at docs HEAD.

No issue closure or cross-port completion is recorded. Publication HOLD continues; any future runtime target requires explicit authorisation.

## Scope evidence

- Changed paths: `8` canonical rows, stored at `docs/v100/changed-paths.txt`, SHA-256 `b8db49581470036b68078ac093dc6b41eaa92222647b14cf44a92b870d54eab4`.
- Changed tests: `3` rows, stored at `docs/v100/changed-tests.txt`, SHA-256 `fbe3c63453261a58352b5e238f5f9488f33a13016485c7e3a49cce17bfdaaca2`.
- Whole upstream test corpus: `171` rows, stored at `docs/v100/test-corpus-171.txt`, SHA-256 `9d24da3ede393a95a7131b1c9ac494f57d8165161d6eb581109c86809131abfc`.
- Detailed path matrix: `docs/v100/changed-paths-crosswalk.md`.
- Changed-test crosswalk: `docs/v100/changed-tests-crosswalk.md`.

## Current Go implementation/adaptation summary

Implemented or adapted in accepted runtime `6795b5235ecd04110c838e48d5958b514f283996`:

- Exact generated catalog refresh from official v1.0.0 package artifact via checked-in generator: `1532` chat models, `57` image models, and `15` classifier models. The official package has `42` provider modules, with `typesafe` classifier-only and no chat rows, so Go's model-bearing chat provider count remains `41`.
- OpenAI Responses resolves grammar support from transcript-declared tools and provider capability. The same map selects function/custom declarations, call replay and result replay. Foreign provider/API IDs normalize to `fc_<hash>` before the type-prefix gate; custom replay omits foreign raw/fc_/ctc_ IDs while same-source valid ctc_ IDs remain. Different-model IDs are omitted. Missing/null custom input becomes empty text. Production OnPayload tests cover true/false/default capabilities, call+result round-trips, transcript additions and radius#115.
- Anthropic OAuth supports backward-compatible login method selection and default browser login via the shared callback server. Both flows use the PKCE verifier as state. Copy-code accepts raw code, `code#state`, URL and query input; supplied mismatched state fails before token exchange. Zero-value `&AnthropicProvider{}` Login/Refresh uses non-mutating default configuration. Authorization includes `code=true` and official browser/headless labels. The shared completion hook exchanges tokens before success, returns 502 on token failure and prevents repeat exchange. Both 200 and 502 callback responses are length-delimited and flushed before Login completion; graceful shutdown finishes active HTTP responses. Repeated asynchronous browser launch tests cover the prior EOF race. Authorization and refresh requests POST JSON to `https://platform.claude.com/v1/oauth/token`, with `Content-Type` and `Accept` both `application/json`. Authorization sends exactly `grant_type`, `client_id`, `code`, `state`, `redirect_uri`, and `code_verifier`; refresh sends exactly `grant_type`, `client_id`, and `refresh_token`, with no scope. A missing refresh token in the response preserves the existing token. Production-path tests retain the default platform URL and route only the transport to a local server. Live credential/browser completion is N/A.
- Shared OAuth callback HTML uses the official Pi SVG path geometry and fills `#F09082`, `#4D9ABF`, and `#F1BE58`, with HTML-escaped `Signed in to <provider>.` text. Real HTTP tests check geometry, fills, Anthropic text, content type and malicious-provider escaping. Anthropic uses this shared path instead of a private duplicate callback page.
- Version/provenance artifacts for v1.0.0 are added under `docs/v100/`.

## Validation evidence

Focused gates passed:

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

Logs: `/workspace/tmp/go-ai-v100-consolidated-logs/{auditor-overlay,async-production,oauth,responses,race,staticcheck,check,repro,shuffle,diff-check}.log`. Full candidate inventory including untracked files: `docs/v100/local-candidate-inventory.txt`. Licence scanning reported known assembly-inspection warnings but exited successfully; govulncheck reported no reachable vulnerabilities. After the single candidate commit, `make sbom-check sbom-self-test vuln-check vuln-self-test license-check` passed before the one guarded push. The regenerated local artifact records the exact candidate SHA. Local/postcommit receipts are preserved in `/workspace/tmp/go-ai-v100-candidate-6795b52/`.

## Hosted acceptance and SBOM provenance

The auditor independently accepted runtime `6795b5235ecd04110c838e48d5958b514f283996` and tree `ebcbe7f87dd19907a2648f0b256f0a79b26f6105`:

- Sole normal push CI run: `36927064765` ([run](https://github.com/rcarmo/go-ai/actions/runs/36927064765)), event `push`, exact `headSha=6795b5235ecd04110c838e48d5958b514f283996`, success.
- Check job: `110586893844`, `Vet, Staticcheck & Deterministic Tests (1.24.x)`, all steps green.
- Fuzz job: `110586894322`, `Fuzz Tests`, all steps green.
- Sole run-scoped artifact: `11194655969`, `go-ai-sbom-6795b5235ecd04110c838e48d5958b514f283996`; archive digest `5031d55e7f55de6f1bc66b4adeeeeaca957a9a39dd9077c07b14dd6d6f83441f` matched the downloaded ZIP.
- Canonical archive files: `sbom.cdx.json` and `sbom.cdx.json.sha256`.
- Hosted inner SBOM SHA-256/checksum: `3d5a5ba1b2e8f08ebb0e1250b0df68ccddc6714e3bac26e27f6fbe8fec3db544`.
- Local postcommit inner SBOM SHA-256: `4226283deb1555c91de654075f0879d5ebd185eb7f907a768c5bc579e84f5b9b`.
- Downloaded SBOM validation: CycloneDX 1.6, root library `github.com/rcarmo/go-ai`, MIT, version `6795b5235ecd`; root purl and bom-ref identify that short candidate revision. Exactly one root `vcs.revision=6795b5235ecd04110c838e48d5958b514f283996`; full revision occurs once. There are 18 components and 19 dependency entries, with four resolved root edges and no unresolved graph references, local absolute paths or known secret fields.
- Hosted security/licence gates passed: no reachable vulnerabilities for `go1.26.6`; licence checker exited successfully with known assembly-inspection warnings. SBOM normalizer/validator, vulnerability-policy and typed-regeneration fault tests passed.

Local and hosted SBOM bytes differ. Decoded JSON has exactly five differences, all at `/metadata/tools/0/hashes/N/content` for the pinned `cyclonedx-gomod v1.12.0` generator executable. CI's `go run` selected `go1.26.8`; local generation used `go1.26.3`. All application, dependency, root, provenance, graph and licence data are identical. Checksums verify artifact integrity; the exact revision and semantic graph comparison establish provenance across the tool builds.

Independent auditor semantic receipt: `/workspace/tmp/go-ai-v100-auditor-hosted-36927064765/semantic-diff-receipt.json`. Agent run/artifact/log/download receipts: `/workspace/tmp/go-ai-v100-candidate-6795b52/`.

Remaining adaptations/N/A: deterministic callbacks and intercepted HTTP transport cover production request, token and callback behaviour; live Anthropic credential/browser completion was not run. Node/SDK-specific browser launch/UI mechanics are adapted to Go's optional selection/auth/prompt callback surface. No live or SDK-only case is represented as a hidden skipped test.

## Durable SBOM release assets

No v1.0.0 durable SBOM release asset, native tag or upstream alias has been created or authorised. Original runtime `6795b5235ecd04110c838e48d5958b514f283996` and separate classifier successor `561ae451d16a6b3a31a274472aed7f831f6fb5eb` have independent hosted acceptance receipts. Publication stays on HOLD until an explicit decision selects a runtime target and authorises publication. Neither documentation SHA nor a tooling HEAD is a release target. Historical v0.99.2 and earlier refs/releases remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation, fix, adaptation, N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
