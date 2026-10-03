# v1.0.1 runtime validation and publication

Runtime `55b27b68b23f133f69984ba3ca5bae60a88b51f3` and its initial native/alias v1.0.1 publications are independently accepted. Tree `510c965c6731751938b7f50bb8b1fbdd5442ee12`, parent/base/rollback `0ad0d8d7e2827d72db1a88a569e8231af676ab1f`; normal Rui commit, 47 paths / 2491 additions / 343 deletions. One shared guarded main push succeeded. Accepted v1.0.0 runtime/tag/assets and original parity receipts are unchanged.

## Runtime and test scope

- Anthropic inline-tools beta from the initial-only request; fixed original top definitions plus hidden placeholder; later full inline definitions/redefinitions/removals; strict/eager/schema conversion; eligible user/system block caching, no cache/defer on inline definitions. Native mapping covers initial/late/replayed/incoming names for API-key OAuth and effective ANTHROPIC_AUTH_TOKEN; ordinary keys retain names.
- Bedrock adaptive binding eligible IDs/names Opus4.7/4.8/5, Sonnet5, Fable5; no4.6 or GovCloud binding. Effort/display/budget/user merge preserved. Production AWS signed request capture uses fake local endpoints.
- Cloudflare direct answers-key presence even null, nested Completed unchanged; strict answers and billed usage/hook order preserved. Clef/.24 and flash/.09 fixtures.
- Retry model-at-capacity match through actual bounded RetryAssistantCall; billing negative/max attempts/deadline cancel controls.
- ChatGPT existing1455 bind fails before host callbacks/token exchange; other bind errors propagate. Tracked listener closes accepted idle connections. Additive optional LoginCallbacks.OnPromptContext allows callback/manual competition, cancels and joins the losing prompt and callback waiter before return. Host must honour cancellation; noncooperative hooks can delay login. Legacy OnPrompt stays synchronous/manual-preferred, preserving compatibility without abandoning a goroutine; it cannot be force-cancelled. Tests verify prompt cleanup before callback-win/cancel returns, occupied port and no listener/idle leak.
- Narrow root integration preserves positional RoleSystem metadata without flushing pending tool rounds. No new HasToolRedefinitions Go API was invented for upstream JS deprecation.

## Offline catalogs

All1615 records generated through validated packaged schema6 JSON (42modules, structure03d2e1aeeee6eb16959d4f727b47b9b187efaf863c688a47889fb90d200e6812). Chat1536/41providers/10APIs (+17/-13/54changed), image59/1/1 (+2), classifier20/5/2 (+5). Generator validates manifest timestamp/hashes/structure, API/provider/type/ID identity, capacity/modalities/costs, formats before atomic rename, rejects without replacing accepted output. Twelve malformed-input/unchanged-output/no-temp-leak cases pass; three typed catalog drift gates reject corruption.

Independent exported-native/official full-record comparison:1615 identities, only59 implicit ImageModel output fields and one API-specific compatibility adaptation. `opencode/grok-build-0.1` contains `supportsReasoningEffort:false` in the official Responses catalog; fixed official openai-responses implementation does not consume that field. It is a completions-only native control, omitted for Responses without changing request semantics. No field/runtime expansion required. All other record fields match, including costs/tiers/inputLimits/compat. A hasCompat bug previously dropped16 strict-only/affinity-only values; generator presence detection corrected, all three catalogs regenerated and TestV101SingleFieldCompatibilityMetadataRetained checks six Anthropic strict-only records plus36 OpenCode affinity declarations. Raw+qualified comparison: `/workspace/tmp/go-ai-v101-local/catalog-independent/`.

Catalog count fixture now clears the three mutable registries and restores builtins with cleanup; exact1536/59/20 remains the acceptance target. Failing shuffle seed1791064406913863141 passes after isolation; no relaxed count.

## Gates

Final evidence directory: `/workspace/tmp/go-ai-v101-local/frozen/`. Earlier logs under `final/` and root directory precede the last generator correction and do not replace final proof.

Final commands below passed with exit status0 after the last runtime/generator/test-isolation corrections:

```sh
export PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-101/new-package/package/dist/models.generated.js
python3 scripts/test-generate-models-v101.py
./scripts/check-model-regeneration.sh
python3 scripts/test-check-model-regeneration.py
go test ./inference/provider/anthropic ./inference/provider/bedrock ./oauth ./tests -run 'TestV101|TestUpstreamBedrockThinkingPayload|TestV0991OpenAIChatGPT|TestV100AnthropicAsyncBrowserCallback' -count=3
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./inference/provider/anthropic ./inference/provider/bedrock ./oauth ./tests -run 'TestV101|TestUpstreamBedrockThinkingPayload|TestV0991OpenAIChatGPT|TestV100AnthropicAsyncBrowserCallback' -count=3
TMPDIR=/workspace/tmp go test -shuffle=1791064406913863141 ./tests -count=1
go test ./...
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
make check
make test-repro
make sbom-check sbom-self-test vuln-check vuln-self-test license-check
git diff --check
```

`make check` includes deterministic3x tests, vet/staticcheck, logging, older inventory checks, all-kind regeneration/faults, pinned SBOM/security/licence/self-tests. The full 19-path/6-marker/171-row manifests and crosswalks are validated separately; no skipped credential-dependent portable test. Live NVIDIA credential streaming is labelled as a remainder. No live model/catalog fetching.

SLSA is advertised in npm metadata; no signature verification claimed. The precommit base SBOM supplied local gate evidence. Exact runtime provenance is recorded below. Known licence assembly warnings are reviewed, not waived vulnerabilities. Final base-revision SBOM digest `f16ee4dacff469c60b78cac04a95ce1c48437fb0aeacd5b988c10ac4413febfa`, root version `0ad0d8d7e282`, full revision `0ad0d8d7e2827d72db1a88a569e8231af676ab1f`,18components/19dependencyentries/fourrootedges; checksum and validator/self-tests passed. Govulncheck found no reachable vulnerabilities for Go1.26.6. Postcommit security/SBOM generation and validation then passed at the accepted runtime, as recorded below.

Fresh `make check`, `make test-repro`, full tests/race/shuffle, exact failing shuffle seed, focused count3/race3, all-kind regeneration/three corruption gates and twelve hydration faults passed. A pre-existing issue1 HTTP fixture exposed a repeated-run global label-cache collision when httptest reused a port; only its test model ID was isolated with a monotonic atomic counter. Production cache/runtime and malformed-tokenize/zero-hook assertions are unchanged. That case passed count10 and shuffled count10, followed by fresh full/race/shuffle/check/repro/security. Earlier failing logs are superseded by `frozen/` final successful logs; no failures waived.

## Exact runtime and hosted evidence

Postcommit `make sbom-check sbom-self-test vuln-check vuln-self-test license-check` passed with a clean tree. SBOM digest `c68c79a635c9d90848db52795d30384ee60f035bb3c20bbad0775fc5e2237bd4`, root version `55b27b68b23f`, full VCS revision matching the accepted runtime, 18 components / 19 dependency entries / four root edges.

Sole push [CI run `37158302991`](https://github.com/rcarmo/go-ai/actions/runs/37158302991), attempt 1: check job `111306227161` and fuzz job `111306227362` succeeded, all steps passed without skips. Artifact `11287280453`, archive SHA-256 `ae45a30f0ee4dc99fe50f6c0bfa73e0fa61545e9fda95b1650ed95772854f1d6`, inner SBOM digest `d27e97f7fbc50ecac0737218a144c15bc7a58d38783bf3a98cf5d172f0f464ea`. Downloaded archive, sidecar, root identity/full revision and 18/19/4 graph validated independently. Local/hosted JSON differs only in five generator executable hashes; application/root/dependency/licence data match. Builder variation is consistent with the hashes, without proving a sole cause. Hosted vulnerability and licence gates passed.

## Initial native and alias publications

Native Rui annotated tag `v1.0.1` has object `aa21fb898b9982f552ade897645f3c77e798a537`, targeting and peeling to accepted runtime `55b27b68b23f133f69984ba3ca5bae60a88b51f3`. A raw-SHA workflow-ref request failed HTTP 422 and created no publisher run/release. After separate corrected-dispatch authority, one named `ref=v1.0.1` request returned HTTP 204. Failure evidence is preserved; the tag was never moved/recreated.

Native [run `37158803651`](https://github.com/rcarmo/go-ai/actions/runs/37158803651), attempt 1, job `111307689683`, all steps passed. Runtime/tooling both match the accepted SHA. [Release `402718119`](https://github.com/rcarmo/go-ai/releases/tag/v1.0.1), title `go-ai v1.0.1`, public/non-draft/non-prerelease, has two canonical assets:

- `sbom.cdx.json`, ID `608652399`, SHA-256 `96d9662da9225f84698335105626c08ea02938cc48fc5038564ec87dd1a1983a`.
- `sbom.cdx.json.sha256`, ID `608652400`, SHA-256 `d6a09762a956d19171653d2af7d92518a0a2c9c881fabd60bd904dc81770a7f5`.

After independent native acceptance, one upstream alias request used the same verified named tag and returned HTTP 204. [Run `37159011129`](https://github.com/rcarmo/go-ai/actions/runs/37159011129), attempt 1, job `111308312697`, all steps passed at the accepted runtime/tooling SHA. [Release `402719070`](https://github.com/rcarmo/go-ai/releases/tag/upstream-v1.0.1), title `SBOM for @earendil-works/pi-ai v1.0.1`, public/non-draft/non-prerelease, has two canonical assets:

- `sbom.cdx.json`, ID `608658027`, SHA-256 `d27e97f7fbc50ecac0737218a144c15bc7a58d38783bf3a98cf5d172f0f464ea`.
- `sbom.cdx.json.sha256`, ID `608658026`, SHA-256 `ffa3498853af1c4e007bebbd87b11731e776710a210616facedf3a50726a69d2`.

Unauthenticated public downloads, API digests, sidecar and checked-in validators passed for both. Native root version `1.0.1`, alias root `55b27b68b23f`, full VCS revision matching runtime, MIT, 18/19/4. Native/hosted differ only in four version-aligned root fields and compare equal after native normalization; alias/hosted has zero field differences. Lightweight alias targets the accepted runtime. Native object/ref/release and re-downloaded public assets stayed unchanged during alias publication.

Both publications are independently accepted. All 57 older refs and 14 older releases/assets were preserved; final count is 59 refs / 16 releases. Evidence: `/workspace/tmp/go-ai-v101-candidate/`, `/workspace/tmp/go-ai-native-v101-publication/`, `/workspace/tmp/go-ai-upstream-v101-publication/`.

## Documentation and next lane

The final receipt touches only this file and [RELEASE.md](../../RELEASE.md), in one normal Rui commit `Record v1.0.1 native and alias publication [skip ci]`, parented by the accepted runtime. External evidence records its exact docs SHA, guarded push, clean origin synchronisation and zero Actions runs. Only static scope/diff/link/ledger checks run for this receipt; no runtime test loops or SBOM regeneration at docs HEAD. Runtime and tag targets remain at `55b27b68…`.

Rust's local provider lane is active and independently coordinated. Native Go durable implementation needs a separate bounded handoff, foundation → generation → owned tools; accepted design has no coding authority. No durable code is included in v1.0.1's initial provider runtime. The user-authorised later same-v1.0.1 replacement requires a separately accepted durable runtime and coordinated ref/asset authority, preserving the initial evidence; historical v1.0.0 stays immutable.
