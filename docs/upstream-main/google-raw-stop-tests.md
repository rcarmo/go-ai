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

## Development validation and remaining work

With `scripts/project-env.sh`, Go 1.27.3 and `GOMAXPROCS=4`: full root normal tests, Google normal/race tests, root vet, Google staticcheck v0.8.2 and `git diff --check` pass. Disposable logs were removed. The 23 previously recorded root staticcheck findings outside Google remain open; no root-staticcheck pass is claimed. No acceptance profiling was run.

Remaining review includes first-nonempty response-ID retention (native currently replaces later nonempty IDs), diagnostic lifetime across mixed finish reasons, cancellation/error classification, malformed-frame/observer admission, provider-only options, Vertex API-key/ADC crosswalks and live disable/signature tests. The observer identity/mutation difference requires an explicit disposition. Broader convergence, dependency review and acceptance/profiling are incomplete; v1.4.0 and generated catalogues are unchanged.
