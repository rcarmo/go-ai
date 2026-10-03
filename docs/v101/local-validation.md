# v1.0.1 local candidate validation

Local-only candidate from `0ad0d8d7e2827d72db1a88a569e8231af676ab1f`. No candidate commit/push/hosted CI/tag/release is authorised yet. Accepted v1.0.0 runtime561/tag/assets and original6795 receipts remain unchanged.

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

SLSA is advertised in npm metadata; no signature verification claimed. SBOM at uncommitted base is gate evidence, not candidate release provenance. Final exact runtime/SBOM/CI SHA requires later authorisation. Known licence assembly warnings are reviewed, not waived vulnerabilities. Final base-revision SBOM digest `f16ee4dacff469c60b78cac04a95ce1c48437fb0aeacd5b988c10ac4413febfa`, root version `0ad0d8d7e282`, full revision `0ad0d8d7e2827d72db1a88a569e8231af676ab1f`,18components/19dependencyentries/fourrootedges; checksum and validator/self-tests passed. Govulncheck found no reachable vulnerabilities for Go1.26.6. Candidate provenance still requires postcommit regeneration after separate authorisation.

Fresh `make check`, `make test-repro`, full tests/race/shuffle, exact failing shuffle seed, focused count3/race3, all-kind regeneration/three corruption gates and twelve hydration faults passed. A pre-existing issue1 HTTP fixture exposed a repeated-run global label-cache collision when httptest reused a port; only its test model ID was isolated with a monotonic atomic counter. Production cache/runtime and malformed-tokenize/zero-hook assertions are unchanged. That case passed count10 and shuffled count10, followed by fresh full/race/shuffle/check/repro/security. Earlier failing logs are superseded by `frozen/` final successful logs; no failures waived.

Durable native port is required next in separate feature commits/milestones after this provider boundary. No durable code enters this candidate. User-authorised future v1.0.1 replacement for accepted durable runtime is a separate audited target/ref/asset decision; historical v1.0.0 is immutable. No refs/releases are changed by local work.
