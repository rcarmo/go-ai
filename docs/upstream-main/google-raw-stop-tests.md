## Google raw stops and observers

Target: `81f3cd412758ebdd92324c7d63b669a189408d0a`. `inference/provider/google/raw_stop_main_test.go` translates the ten expanded cases in `packages/ai/test/google-raw-stop-reason.test.ts` through registered production Google/Vertex streams and loopback HTTP/SSE.

| Upstream case | Cases | Native test | Disposition |
| --- | ---: | --- | --- |
| Google MALFORMED_FUNCTION_CALL error and exact diagnostic | 1 | `TestUpstreamMainGoogleRawStopErrors/google-generative-ai` | Runtime assertions pass |
| Vertex SAFETY error and exact diagnostic | 1 | `.../google-vertex` | Runtime assertions pass |
| MAX_TOKENS plus tool call remains length, both adapters | 2 | `TestUpstreamMainGoogleRawStopTools/{api}/MAX_TOKENS` | Pass |
| STOP plus tool call becomes toolUse, both adapters | 2 | `.../{api}/STOP` | Pass |
| Ordered chunks, callback models, response ID and exact text content | 2 | `TestUpstreamMainGoogleProviderEvents/{api}` | Partial: four SDK reference-identity assertions unrepresented |
| Default and explicit User-Agent | 2 | `TestUpstreamMainGoogleUserAgent/explicit={false,true}` | Pass |

The SDK mock becomes real REST/SSE transport. Vertex uses an explicit fake API key plus project/location, so no ADC or live credential path is claimed. User-Agent expectations use the native identity formatter, retaining its Go runtime token. Fixture model construction is local and independent of global catalogue registration.

After correcting a test-harness attempt to use interface assertions on native tagged content structs, the unchanged stream path passed eight cases and failed both observer cases: no callbacks were invoked. The budget-only edit described below does not affect these non-reasoning stream inputs. Native observers now receive each decoded chunk before response-ID/content/finish/usage normalisation, with callback errors terminating the stream. A separately decoded map preserves unknown fields and cannot mutate typed/lossless stream data. This differs from JavaScript's shared mutable SDK chunk; the four upstream reference-identity assertions are not ported. Callback projection/mutation semantics need Rui's disposition, as with Mistral. The file remains partial.

## Source-derived and native regressions

`budget_family_main_test.go` translates the case-sensitive family substring logic in both adapters' `getGoogleBudget`: 22 production Simple-path cases, including lowercase, uppercase/mixed-case suffixes, an uppercase prefix with lowercase family, and a zero custom override. Twelve cases fail on unchanged `2559859`; native lowercasing incorrectly selects a fixed budget instead of adaptive `-1`. Matching the original ID fixes those failures. Custom overrides retain precedence.

`observer_control_main_test.go` adds two source-derived rejection cases: the second hook fails before processing its response ID, accumulated text survives, and later chunks are not observed. Both failed before hook wiring. Two separately labelled native ownership tests require detached mutation isolation while retaining unknown fields; both initially failed because the hook was absent. They protect the native authority boundary without claiming upstream mutation parity.

`terminal_kind_main_test.go` adds ten source-derived terminal-kind cases from both adapters' post-stream throw/catch. Six error cases failed before changes: native emitted done with reason error. Provider failures now emit one error and no done; STOP/toolUse and MAX_TOKENS/length remain successful terminals. The two older raw-stop helper tests now require error events and retain all previous message assertions. Hook rejection also cancels the SSE parser's channel producer; the transport retains body ownership and closure, and scanner bounds are unchanged.

## Response metadata and diagnostic lifetime

`metadata_state_main_test.go` adds sixteen source-derived production HTTP/SSE cases at the same pin. Six cases cover first-nonempty response-ID retention, a late metadata-only ID and later empty IDs. Ten cover SAFETY followed by STOP, MAX_TOKENS, MALFORMED_FUNCTION_CALL or an omitted finish reason, plus STOP followed by SAFETY. They check final raw/normalised reasons and diagnostics, exactly one terminal event, and diagnostic-free publication-time text-delta/block-end snapshots through the native snapshot API.

On unchanged `c1e8fc1`, twelve cases fail and four pass: two failures replace the first response ID; all ten diagnostic cases expose premature block-end diagnostics or stale diagnostics after a later success. The first ID is now retained, and provider diagnostics are created only after block closure when the final outcome is an error, matching the pinned adapters' post-stream throw/catch. Unlike Mistral, these Google adapters do not attach diagnostics while processing finish-reason chunks. All sixteen cases and the earlier Google regressions pass.

The snapshot checks are an explicit native adaptation. Pinned `utils/event-stream.ts` queues shared output references; JavaScript consumers can observe later mutations, including the diagnostic assigned just after block-end publication. Native events retain detached publication-time snapshots. These tests establish the source control-flow state mapped to that native contract, not JavaScript consumer identity/timing equivalence. Keep detachment/authority fences; snapshot/reference semantics need an explicit disposition alongside observer mutation.

## Development validation and remaining work

With `scripts/project-env.sh`, Go 1.27.3 and `GOMAXPROCS=4`: full root normal tests, Google normal/race tests, root vet, Google staticcheck v0.8.2 and `git diff --check` pass. Disposable logs were removed. The 23 previously recorded root staticcheck findings outside Google remain open; no root-staticcheck pass is claimed. No acceptance profiling was run.

The [cancellation/syntax follow-up](google-cancellation-admission.md) covers ten cancellation/body-lifetime cases and eight syntax-admission cases with a locked SDK reference. Remaining review includes other preflight/deadline paths, SDK framing/shape/enrichment, valid-but-unsupported DTO admission, provider-only options, Vertex API-key/ADC crosswalks and live disable/signature tests. Observer identity/mutation and detached-event snapshot/reference semantics require explicit dispositions. Broader convergence, dependency review and acceptance/profiling are incomplete; v1.4.0 and generated catalogues are unchanged.
