# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Native durable M1 locally accepted — v1.0.1 replacement pending

The useful native storage → generation → owned-tool → answer path is implemented and independently accepted locally. Final candidate commit/push, exact-SHA hosted acceptance and the required same-v1.0.1 tag/release/asset replacement are separate pending decisions. No replacement has occurred; initial provider runtime/tag/releases/assets below are preserved.

- Official input: `@earendil-works/pi-durable@1.0.1`, fixed gitHead `a7229ddc21810d6245105978033b7df645ecc2f7`, npm archive SHA-256 `c4bc1ea49653ee972045a5de755756b633e3be0e1528ae32f5f32d82e012db7d`. Core source is unchanged from the accepted v1.0.0 design; v1.0.1 delta is package metadata. SLSA is advertised; signature verification was not performed.
- Accepted M1a storage checkpoint: `ea08a31ac9b742716fe775cf65f308c664abcbc5`, tree `ce2ffda6730da5a068afa660e19034df132d3b5c`, parent `f121d9704f797213824d6e7f1ed14a3523dc4538`; normal Rui local commit, ten new paths / 3798 additions. Native stdlib memory/journal atomic records, strict detached JSON, durable reservations, bounded framing, adopt-or-poison settlement and close/cancellation fences.
- Accepted M1b generation checkpoint: `8d20e868e3931edb69dc5ee2085bdf8297f8323a`, tree `c83bcecfb63c8a5944c253b5dc431031dcdba2c5`, parent M1a above; normal Rui local commit, nine paths / 2541 additions / six deletions. Persistent real provider requests, request-ID dedup, pinned model/context/settings, terminal/usage atomicity, reopen recovery and noncooperative close. The separately authorised OAuth test-only closure assertion accepts EOF or ECONNRESET; production OAuth is unchanged.
- M1c useful vertical: copied/pinned offered tools, strict supported-schema validation, final repaired execution arguments committed before effects, distinct generation-owned task IDs, conservative stored/current replay policy and implementation/version/schema identity. Original model-call arguments stay in the assistant entry/context; execution/recovery arguments live separately in the child intent. Tool result entry/checkpoint usage, aggregate spend, bounded output and application-document changes adopt atomically. Abort marks precede signal/drain and child settlement precedes parent cleanup; Close joins code without inventing abort outcomes.
- Final uncommitted M1c scope is exactly 13 paths: seven new `durable` test/tool, feature-document and example files; five authorised existing durable integration files; this root `RELEASE.md` ledger only. The previously accepted 12-path patch is unchanged; the extra path records local acceptance and pending publication. No foundation, root API/provider/catalog/dependency/workflow/Makefile/AGENTS or other source changes. Any final normal runtime commit must parent M1b `8d20e868…`; its actual SHA/tree will be recorded in the external receipt after separate authority, with no guessed candidate SHA here. M1a/M1b are still local and unpushed.
- Local gates: 22 captured exit-zero commands across the working candidate and a genuine clean Git clone—each passed full tests/race/shuffle/vet, `make check`, `make test-repro`, SBOM/security/licence/self-tests, all-three-catalog regeneration, deliberate metadata corruption, twelve hydration faults and the public example. Nice10, GOMAXPROCS2, private Go cache and package/test parallelism2; the granted heavy window was released at 2026-10-04T00:59:08Z. Independent public tool → application document → answer/reopen-dedup probe passed with two model calls / one tool effect; `go run ./examples/durable` prints `The sum is 5.` without live credentials.
- Tested source/tree identity: `2c67427aead836afb7855c39ea16681b3a4f7222`. Genuine `git clone --no-local` validated clean detached synthetic snapshot `496796116d7c6ca592941057980a9827ca6e606d` only in the external clone, never on the project branch or a release target. Final documentation receipts and this ledger receive static scope/diff/link/inventory checks only; no source/test changed after the accepted gates.
- Precommit working SBOM: `968032a18d277992af131d4f55f096e32aa64ebc1667337b78db0a55ab7c448e`, version `8d20e868e393`, full VCS revision M1b above. Clone SBOM: `cbd06e4f37ba6f6453131c70987613b70ee4cfe9aa5bccdc1b08266ec1dd35c1`, version `496796116d7c`, full synthetic validation revision above. Both have 18 components / 19 dependency entries / four root edges; checksum/validators passed and only five expected root revision identity fields differ. These are baseline/test-snapshot validation artifacts, not final candidate provenance. Exact candidate postcommit SBOM/security and hosted evidence require their own authority. No reachable vulnerabilities for Go 1.26.6; known licence assembly-inspection warnings only.
- Exact fixed inventories: [60 source paths](docs/durable/contract-crosswalk.md), [42 test suites](docs/durable/test-crosswalk.md), [public API/example/limits](docs/durable/README.md). Named native proofs, links and rows were checked; no hidden skips. Storage applicability stays 13 complete / seven partial / three unsupported of 23 cases. Full pi-durable parity is not complete: M2 generic task/extension/fork/watch/steering boundaries, M3 history/deltas/compaction/SQLite/reclamation and M4 coding environments/subagents are explicit gaps. Journal files are native GODJNL1, with host cross-process exclusivity and filesystem/device durability limits; no universal exactly-once model billing/tool effects.
- Evidence: `/workspace/tmp/go-ai-durable-m1a/`, `/workspace/tmp/go-ai-durable-m1b/`, `/workspace/tmp/go-ai-durable-m1c/{full-validation,clone-validation,structural-review,frozen,frozen-ledger}/`. Previous freezes and the failed partial-fixture timeout are preserved; deterministic durable acknowledgement/guaranteed fixture release corrected the fixture before final gates. No failures were waived.

After exact final runtime hosted acceptance, the user-required same-v1.0.1 native and alias replacement must preserve the initial `55b27b68…` runtime, annotated object `aa21fb89…`, release metadata/assets and acceptance evidence before any separately authorised ref/asset change. Replacement status is **pending**, with no successful retag or replacement publication recorded. Older v1.0.0 history stays unchanged; other ports have separate acceptance and publication receipts.

## Historical initial v1.0.1 provider runtime and publications

- Official package `@earendil-works/pi-ai@1.0.1`, gitHead/tag `a7229ddc21810d6245105978033b7df645ecc2f7`; previous official v1.0.0 `a13d35a742c6ef8462812a28fbe1d8c8b7431c32`.
- Artifact `/workspace/tmp/pi-ai-audit-101/pi-ai-1.0.1.tgz`, SHA-256 `8a9e69b1309cf93405d87729fa123c8b11c6be7c646b16f34f8bef7b792f9138`; npm SHA-512 checked independently. SLSA advertised, signature verification not claimed.
- Accepted runtime `55b27b68b23f133f69984ba3ca5bae60a88b51f3`, tree `510c965c6731751938b7f50bb8b1fbdd5442ee12`; parent/base/rollback `0ad0d8d7e2827d72db1a88a569e8231af676ab1f`. Normal Rui-authored/committed `Port pi-ai v1.0.1 runtime and catalog parity`, 47 paths / 2491 additions / 343 deletions. One shared guarded main push succeeded. Native and alias publications independently accepted; v1.0.0 history is unchanged.
- Exact source diff19 paths (1added/18modified), +546/-153; six changed tests;171 whole-corpus rows (164 executable+7support). [Source dispositions](docs/v101/changed-paths-crosswalk.md), [six changed tests](docs/v101/changed-tests-crosswalk.md), [171-row crosswalk](docs/v101/test-corpus-crosswalk.md), [provenance](docs/v101/provenance.json).
- Manifest digests: source paths `ac9e4b76f7bb921a251ac5f5e14af48b1fa73f6e40d4d49e41ee11d5fb902278`; changed tests `fd49b5003edf22d18b5c4a4fa5b4998e5bad49136cc6d356527dd7c3d655a659`; corpus `9d24da3ede393a95a7131b1c9ac494f57d8165161d6eb581109c86809131abfc`.
- Implemented runtime changes: Anthropic fixed prefix+inline definitions/redefinitions/cache/OAuth names; Bedrock eligible adaptive binding+beta; direct Cloudflare classifier results; capacity retries; ChatGPT occupied callback bind/connection cleanup and optional context-aware prompt cancellation. Narrow root RoleSystem preservation and generator metadata completeness fixes support production paths.
- Native catalogs generated offline from packaged schema6 provider JSON: total1615 = chat1536/41providers/10APIs, image59/1/1, classifier20/5/2. Deltas chat+17/-13/54changed, image+2/0/0, classifier+5/0/0;42upstream modules. Full-record metadata including cost tiers validated; native implicit image output and unused Responses supportsReasoningEffort:false are explicit adaptations. No dependency/workflow/durable changes.
- Fresh final local gates passed: focused3x/race3x, full tests/race/shuffle, exact failing shuffle seed, issue1 fixture shuffled10x, `make check`, `make test-repro`, all-kind regeneration/three corruption gates, twelve hydration faults, SBOM validation/self-tests, vulnerability and licence checks. [Validation receipt](docs/v101/local-validation.md); frozen logs `/workspace/tmp/go-ai-v101-local/frozen/`. Precommit base-revision SBOM digest `f16ee4dacff469c60b78cac04a95ce1c48437fb0aeacd5b988c10ac4413febfa`; postcommit security/SBOM validation passed at exact runtime, digest `c68c79a635c9d90848db52795d30384ee60f035bb3c20bbad0775fc5e2237bd4`. Both have 18 components / 19 dependency entries / four root edges. No reachable vulnerabilities for Go 1.26.6; known licence assembly warnings only.
- At initial provider publication, durable work was an accepted design awaiting separate foundation → generation → owned-tool handoffs, and Rust's provider lane had separate coordination. The locally accepted implementation now appears above; this historical provider runtime contains no durable code. Its initial tag/runtime/assets and evidence are unchanged, and same-v1.0.1 replacement still requires exact final hosted acceptance and separate ref/asset authority.

### Hosted runtime acceptance

Sole normal push [CI run `37158302991`](https://github.com/rcarmo/go-ai/actions/runs/37158302991), attempt 1, exact runtime SHA above: check job `111306227161` and fuzz job `111306227362`, all steps successful with no skips. Artifact `11287280453`, `go-ai-sbom-55b27b68b23f133f69984ba3ca5bae60a88b51f3`, downloaded archive digest `ae45a30f0ee4dc99fe50f6c0bfa73e0fa61545e9fda95b1650ed95772854f1d6`. Inner SBOM digest `d27e97f7fbc50ecac0737218a144c15bc7a58d38783bf3a98cf5d172f0f464ea`; checksum and validator passed: root short version `55b27b68b23f`, full VCS revision, MIT, 18/19/4 topology. Local/hosted differences are confined to five generator executable hashes; root/dependency/licence fields match. Builder variation is consistent with those hashes, without proving a sole cause.

### Native and upstream alias publications

Both use runtime and workflow tooling `55b27b68b23f133f69984ba3ca5bae60a88b51f3` through the unchanged existing publisher. Native tag object `aa21fb898b9982f552ade897645f3c77e798a537`, annotated by Rui Carmo <rui.carmo@gmail.com>, message `go-ai v1.0.1`, targets and peels to that runtime. The first raw-SHA workflow-ref request failed HTTP 422 and created no run/release; its evidence is preserved. One separately authorised corrected request with named `ref=v1.0.1` returned HTTP 204. No tag recreation, move or unapproved retry occurred. Alias dispatch used the same verified named tag and returned HTTP 204.

| Publication | Release | Publisher run / job | SBOM asset / SHA-256 | Checksum asset / SHA-256 |
| --- | --- | --- | --- | --- |
| [Native v1.0.1](https://github.com/rcarmo/go-ai/releases/tag/v1.0.1) | `402718119` | [37158803651](https://github.com/rcarmo/go-ai/actions/runs/37158803651) / `111307689683` | `608652399` / `96d9662da9225f84698335105626c08ea02938cc48fc5038564ec87dd1a1983a` | `608652400` / `d6a09762a956d19171653d2af7d92518a0a2c9c881fabd60bd904dc81770a7f5` |
| [Upstream-v1.0.1](https://github.com/rcarmo/go-ai/releases/tag/upstream-v1.0.1) | `402719070` | [37159011129](https://github.com/rcarmo/go-ai/actions/runs/37159011129) / `111308312697` | `608658027` / `d27e97f7fbc50ecac0737218a144c15bc7a58d38783bf3a98cf5d172f0f464ea` | `608658026` / `ffa3498853af1c4e007bebbd87b11731e776710a210616facedf3a50726a69d2` |

Both runs are workflow_dispatch, attempt 1, all steps successful. Both releases are public/non-draft/non-prerelease, with exactly `sbom.cdx.json` and `sbom.cdx.json.sha256`. Unauthenticated downloads, API digests, sidecars and validators passed. Native root version is `1.0.1`; alias root is `55b27b68b23f`; both retain the exact full runtime VCS revision and 18/19/4 topology. Native differs from hosted only in four version-aligned root fields; comparison after expected native normalization has zero field differences. Alias SBOM has zero field differences from accepted hosted evidence. Alias ref is lightweight and targets the same runtime; native tag/object/release/assets were preserved during alias publication.

All 57 older tag refs and 14 older release records/assets were preserved; final inventory is 59 refs / 16 releases. Runtime, native/alias refs and assets remain at `55b27b68…`. Evidence: `/workspace/tmp/go-ai-v101-candidate/`, `/workspace/tmp/go-ai-native-v101-publication/`, `/workspace/tmp/go-ai-upstream-v101-publication/`.

This two-path documentation receipt has subject `Record v1.0.1 native and alias publication [skip ci]` and runtime parent `55b27b68b23f133f69984ba3ca5bae60a88b51f3`. Its external receipt records the exact docs SHA, guarded push and zero exact-SHA Actions runs. Documentation-only static/link/ledger checks apply; no runtime tests or SBOM regeneration at docs HEAD, and no docs tag/release.

## Historical accepted v1.0.0 audit

- Package: `@earendil-works/pi-ai`
- Release/tag: `v1.0.0`
- Upstream tag/gitHead: `a13d35a742c6ef8462812a28fbe1d8c8b7431c32`
- Official npm artifact: `/workspace/tmp/pi-ai-100.tgz`
- Official npm artifact SHA-256: `f39b99c29b8598f175b10840e5d2a81983e7c0ce5cae4d7df83a1007447d2c2b`
- Previous accepted upstream baseline: `v0.99.2` / accepted Go runtime `86a439d949de78ce7e8ba4cd3f975312785c2a73`
- Current repository baseline before this audit: `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e`
- Rollback SHA before the v1.0.0 local candidate: `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e`
- Original accepted parity runtime SHA: `6795b5235ecd04110c838e48d5958b514f283996` (historical receipt).
- Published v1.0.0 runtime SHA: `561ae451d16a6b3a31a274472aed7f831f6fb5eb`, the separately accepted classifier successor selected explicitly for both native and alias publication.
- Original accepted runtime tree: `ebcbe7f87dd19907a2648f0b256f0a79b26f6105`.
- Runtime commit: `Port pi-ai v1.0.0 runtime and catalog parity`, authored and committed by `Rui Carmo <rui.carmo@gmail.com>`, parent `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e`.
- Original documentation receipt: `05edf10e5bcd284261629b77e56922a04aa6282b`, a `RELEASE.md`-only child of runtime `6795b5235ecd04110c838e48d5958b514f283996`. Both remain historical v1.0.0 parity evidence.
- Publication state: native `v1.0.0` and historical alias `upstream-v1.0.0` are published and independently accepted on runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb`. Original `6795b5235ecd04110c838e48d5958b514f283996` remains unchanged. No docs/tooling HEAD is a tag target; any additional release mutation needs separate authorisation.

## Separately accepted classifier issue #1 successor

The auditor accepted runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb` and its hosted evidence. [Issue-specific contract, migration and evidence](docs/issues/classifier-1.md) records the separate nine-path follow-up, 1271 insertions / 94 deletions, to upstream classifier paths unchanged in the original eight-path v1.0.0 delta. It corrects object state, instructions/criteria, required answer fields, System One/Cloudflare envelopes and llama.cpp rendering/readout. Response hooks follow decoded 2xx success and precede semantic parsing. No catalog, dependency, OAuth, chat/image, workflow or release-tool changes were included.

- Runtime commit: `Fix classifier context and question contract`, normal Rui-authored commit; parent and issue rollback `05edf10e5bcd284261629b77e56922a04aa6282b`. Original runtime `6795b5235ecd04110c838e48d5958b514f283996` was not amended or retargeted.
- Classifier documentation receipt SHA: `5fe43c855937d0b280983fe619a47d8457934281`, direct child of runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb`, subject `Document classifier issue hosted acceptance [skip ci]`, touching only `RELEASE.md` and `docs/issues/classifier-1.md`. Its exact-SHA Actions queries returned zero runs. It served as publication tooling, never runtime/SBOM acceptance or tag target.
- Sole normal push CI: [36934719250](https://github.com/rcarmo/go-ai/actions/runs/36934719250), exact classifier runtime head SHA, event `push`, attempt 1, success; check job `110612170075` and fuzz job `110612169810`, all steps passed.
- Sole artifact `11198105688`, `go-ai-sbom-561ae451d16a6b3a31a274472aed7f831f6fb5eb`: downloaded ZIP SHA-256 `7575ef74adfec3d2f70260e606b286a2e8129134f567564852e30e3a35cdb67b` matches the API digest. Hosted inner SBOM checksum `c36564ec7bdd18ca29aaeb6eab8138f84fb00e5fba22ba3fce32c129e39123e1`; corrected local checksum `9abd48d360224279a7cbac5f3067486f28527e5c99a50544d9e70813fa42b232`.
- CycloneDX 1.6 root library `github.com/rcarmo/go-ai`, MIT, version `561ae451d16a`, matching module/version purl and bom-ref, exactly one root full `vcs.revision=561ae451d16a6b3a31a274472aed7f831f6fb5eb`; 18 components / 19 dependency entries / four direct root edges, no dangling refs or local paths.
- Local postcommit and hosted security/licence checks passed: no reachable vulnerabilities for Go 1.26.6; existing assembly-inspection warnings only. Independent structural/delegated comparison found exactly five generator executable hash differences under `/metadata/tools/0/hashes/N/content`; application, dependency, root, graph and licence fields match. Local Go 1.26.3 versus hosted Go 1.26.8 is consistent with those pinned `cyclonedx-gomod v1.12.0` builder differences; the sole cause was not independently proven.
- Evidence: `/workspace/tmp/go-ai-issue1-candidate/{corrected-root12,hosted}/`; rejected full-root-version artifact is labelled under `rejected-root40/`. Auditor independently verified the run, downloaded artifact, checksum, graph and generator-only differences. Docs-only validation does not rerun accepted runtime matrices or regenerate artifacts at docs HEAD.

No issue closure or cross-port completion is recorded. Native and alias v1.0.0 publication is separately complete below. Native pi-durable remains a separate accepted read-only design with no implementation authorisation; it is not part of these releases.

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

Both v1.0.0 publications are independently accepted. They target classifier runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb`, never classifier docs/tooling `5fe43c855937d0b280983fe619a47d8457934281` or this final publication receipt. Original parity runtime `6795b5235ecd04110c838e48d5958b514f283996` remains historical. Classifier rollback is parent `05edf10e5bcd284261629b77e56922a04aa6282b`; original parity rollback `869b7a62bfceed02c7cc3abb05e5c4aa3cfc2c6e` is preserved. Published tags/assets are immutable; rollback never retargets a released version.

### Native v1.0.0

- [Release `401591099`](https://github.com/rcarmo/go-ai/releases/tag/v1.0.0), title `go-ai v1.0.0`, public/non-draft/non-prerelease.
- Create-only Git Data annotated tag object `15473ce688bb61f601f3ead71782e4554d1d9eeb`; `refs/tags/v1.0.0` has type `tag`, exact tagger `Rui Carmo <rui.carmo@gmail.com>`, message `go-ai v1.0.0`, direct target type `commit` / runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb`. Tag/ref/release were absent immediately before creation; object/ref matched before and after publication. Peeled tag object is the provenance authority.
- Sole publisher [run `36974565269`](https://github.com/rcarmo/go-ai/actions/runs/36974565269), attempt 1, success, job `110735490140`, all steps passed. Tooling checkout `5fe43c855937d0b280983fe619a47d8457934281`; separate exact runtime checkout `561ae451d16a6b3a31a274472aed7f831f6fb5eb`; inputs `release_mode=native`, `release_tag=v1.0.0`, `upstream_version=v1.0.0`. No API failure, fallback or retry.
- Canonical public `sbom.cdx.json`, asset `604996010`, SHA-256 `358b691f3d9d5852ccd24e77150caa3d8391be330f8b4fcaf075db5301cba755`.
- Canonical public `sbom.cdx.json.sha256`, asset `604996011`, SHA-256 `cef96c108aab4f1c4323ba64fc7ad3c75b08645a62fe80e808f7d79d7d154f75`.
- Native root version `1.0.0`, module purl/bom-ref `go-ai@1.0.0`, full root `vcs.revision` equal to runtime above. Compared with accepted runtime hosted SBOM, exactly four fields differ: root version, root purl, root bom-ref and root dependency ref. After pinned native-version normalization of a comparison copy, decoded data has zero differences. Public assets were freshly generated from the runtime, not copied from an alias.

### Historical upstream-v1.0.0 alias

- [Release `401593562`](https://github.com/rcarmo/go-ai/releases/tag/upstream-v1.0.0), title `SBOM for @earendil-works/pi-ai v1.0.0`, public/non-draft/non-prerelease.
- Established lightweight alias ref `refs/tags/upstream-v1.0.0`, type `commit`, exact runtime `561ae451d16a6b3a31a274472aed7f831f6fb5eb`. Existing native tag/release/assets were preserved during alias creation; no old alias was retagged.
- Sole publisher [run `36975033815`](https://github.com/rcarmo/go-ai/actions/runs/36975033815), attempt 1, success, job `110736901833`, all steps passed. Same separate tooling/runtime checkouts; inputs `release_mode=upstream`, `release_tag=upstream-v1.0.0`, `upstream_version=v1.0.0`. No API failure, fallback or retry.
- Canonical public `sbom.cdx.json`, asset `605003187`, SHA-256 `c36564ec7bdd18ca29aaeb6eab8138f84fb00e5fba22ba3fce32c129e39123e1`.
- Canonical public `sbom.cdx.json.sha256`, asset `605003186`, SHA-256 `7a31fd6557d22aa43f17da9bc598ac36bfc98199061ff387aa2f815fa0a9f8e5`.
- Historical alias root version `561ae451d16a`, module/version purl and bom-ref, full root `vcs.revision` equal to runtime above. Freshly generated alias SBOM is byte-identical to accepted runtime hosted artifact `11198105688`; independent structural comparison also has zero differences. It was not copied from the native SBOM.

### Public validation and history preservation

Both releases have exactly the two canonical assets. Unauthenticated public downloads matched their GitHub asset digests and internal checksums. Checked-in SBOM validators passed: CycloneDX 1.6, root library `github.com/rcarmo/go-ai`, MIT, exactly one full root VCS revision, 18 components / 19 dependency entries / four direct root edges, no dangling refs, local absolute paths or checked secret-bearing fields. Pinned `cyclonedx-gomod v1.12.0` / normalizer / validator/self-tests passed in both workflows. Vulnerability gates report no reachable vulnerabilities for Go 1.26.6; licence checks passed with known assembly-inspection warnings. No accepted broad runtime matrices were restarted.

Native publication preserved all 55 prior tag refs and 12 prior releases. Native/upstream v0.99.2 tag objects and re-downloaded public assets matched their before snapshots. Alias publication then preserved all 56 prior refs and 13 prior releases, including complete native v1.0.0 tag object and re-downloaded asset digests. Only the authorised native and alias additions occurred. Original upstream eight-path manifests and historical runtime/classifier receipts remain unchanged.

Evidence: `/workspace/tmp/go-ai-native-v100-publication/` and `/workspace/tmp/go-ai-upstream-v100-publication/`, including API snapshots, dispatch/run/job/full logs, public downloads, validators, structural comparison and before/after history receipts. The auditor independently accepted both publications.

### Final documentation and separate durable work

This final `RELEASE.md`-only Rui evidence commit has subject `Record v1.0.0 native and alias publication [skip ci]` and parent `5fe43c855937d0b280983fe619a47d8457934281`. Resolve its exact SHA with `git log -1 --format=%H --fixed-strings --grep='Record v1.0.0 native and alias publication [skip ci]'`; its external post-push receipt records the literal docs SHA and zero Actions checks. It is not runtime or a tag target. Documentation scope/diff/link/lineage checks only; no SBOM regeneration at docs HEAD.

Native pi-durable M1a design is separately accepted at `/workspace/tmp/go-ai-durable-readonly/plan.md`. It has no implementation/build/dependency/commit/push authorisation. Completing these v1.0.0 releases neither implements pi-durable nor closes the classifier issue or completes other ports. No additional release, runtime, workflow or issue-ledger mutation is authorised.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation, fix, adaptation, N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
