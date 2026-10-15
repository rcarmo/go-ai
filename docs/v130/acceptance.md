# 1.3.0 port acceptance

Target official `71023aec1546d6d677a65669c5665c0927ce0dd0`, relative to official1.2.0 `06a466935536f8c9bcacb2d5efc0fc021be1bdff`. Native1.2.0 tag/assets remain unchanged. [Changed paths](changed-paths.json) inventories78 relevant paths from226-file compare; [provenance](provenance.json) records verified npm artifacts.

## Changed contracts

| Upstream family | Native disposition/evidence |
| --- | --- |
| Completed tool arguments, JSON repair and validation | `internal/jsonparse/tool_arguments.go`; ArgumentsError DTO/wire/event/checkpoint propagation; parser and durable zero-effect error-result tests. Streaming partials remain separate from completed object parsing. |
| Cross-model thinking | `transform.go` drops other-model thinking; same-model signed/redacted replay retained, whitespace-only unsigned dropped. Identity/isolation matrix and model-selection tests. |
| Anthropic thinking-off and error-result images | Explicit off suppresses managed effort markers/binding; omitted settings retain adaptive defaults. Error images/captions follow all adjacent tool results; successful/deferred images preserved. Request/order/isolation tests. |
| Bedrock Claude error images | Successful images retained in result; Claude error images extracted after grouped results. Existing empty/sanitisation cases and new order fixture pass. |
| Responses mid-conversation effort | Compat-gated same-model historical/current configuration_update items, stable medium request effort, terminal providerThinkingLevel; sign-in/no-option/disabled gate exclusions. Generator emits capability; payload/terminal tests. |
| OpenAI combined reasoning/content and Google4+ | Reasoning/details ordered before combined answer; Gemini4+ selects thinkingLevel. Provider payload/stream regressions. |
| Kimi/OpenCode/model schema | Regenerated pinned JSON schema-v6:1614chat/60image/40classifier. Kimi Chat Completions /coding/v1; OpenCode endpoints/protocol/compat and pricing from official source. OpenCode Google already uses common session headers. Exact regeneration, negative drift and12 offline atomic failure tests pass. |
| Retry phrases | Service-is-temporarily-unavailable and unable-to-forward-upstream matched, existing billing-limit exclusions retained. |
| Removed assistant-frame API | Removed isolated AssistantMessageFrame/Encoder/reducer and dedicated tests as upstream breaking removal; regular provider/SSE API remains. |
| Durable baseline | Explicit system baseline leads imported history; later system deltas remain positional; cached and historical effective contributions respected. Context race tests. |
| SQLite namespace | Validated optional native table prefix; independent sequential stores/reopen/reclaim, invalid prefix before filesystem mutation. Exclusive native file locking retained; Cloudflare shared-DO adapter coordination has no native deployed counterpart. Native format remains distinct. |
| Codemode worker lifecycle | AcquireWorker/OnWorkerStarted actual-exit hooks; module close precedes release/notification, including start failure/panic/cancellation. Native close joins workers; Bun nonwaiting terminate is unnecessary. Official1.3VM integration passes; unchanged prelude and quickjs-wasi3.6.2 verified. |
| Durable output helper imports/base64 | Chord output import moves and JS8KiB base64 allocation optimisation map to existing native UTF8/base64 implementations; no artificial JS allocation port. |

## Verification

Go1.27.2/Staticcheck0.8.2/GOMAXPROCS2. Full normal seed1792082342786540542: durable115.239s/tools4.900s/QuickJS17.382s/consumers6.905s. Final production root race same seed: durable755.637s/tools34.944s/QuickJS132.136s/consumers29.387s. Later terminal-effort addition is test-only, focused race count3 passes. Vet/staticcheck/logging/security/licence/local SBOM+publisher self-tests pass; security has no exceptions. Existing assembly-import inspection warnings remain tool limitations.

Official pi-codemode1.3.0 Node integration executes all3top-level suites without skips, race count3 passes3.239s. Native process/transport, ownership/cancellation and SQLite conformance remain covered. No live paid-provider or deployed Cloudflare service acceptance is claimed.

CPU/alloc_space/alloc_objects captured and inspected for1.3changed paths. Initial parser/provider captures had0CPU samples; repeated2000-fixture workloads produced40ms/230ms/310ms CPU samples for parser/Anthropic/Responses. JSON parsing/repair, request DTO construction and reflection dominate small-path allocations. Durable140ms CPU samples/45.25MiB/563170objects: transactional prepareWithReferences46.43% allocation space. QuickJS95.51MiB/175779objects, engine compilation/initialisation dominates. No equivalent-workload optimisation comparison or speedup claim; ownership validation retained. Raw captures/binaries/diagnostic logs disposed after analysis.

All3npm packages pass registry signatures and publish/SLSA attestations using TUF-authenticated npm keys/Sigstore roots, exact GitHub workflow/tag/source revision and tampered-DSSE rejection. Public native release assets and standard module download must still be verified after publication.

## Native scope

Sensible bounded native storage/schema/output/callback policies remain as accepted in1.2.0. SQLite prefixes do not relax cross-process file ownership or implement a Cloudflare adapter. Lifecycle hooks do not pretend script settlement is VM exit. These platform adaptations are documented, not silently classified as upstream deployment tests.

Release1.3.0 is authorised, but tag/publication waits for final source/SBOM/reproducibility and release-record checks.
