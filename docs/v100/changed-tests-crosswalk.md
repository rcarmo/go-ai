# v1.0.0 changed-test crosswalk (3 rows)

Manifest: `docs/v100/changed-tests.txt`, SHA-256 `fbe3c63453261a58352b5e238f5f9488f33a13016485c7e3a49cce17bfdaaca2`.

| upstream test | disposition | Go evidence / rationale |
|---|---|---|
| `packages/ai/test/anthropic-oauth.test.ts` | implemented/adapted | `oauth/anthropic_test.go` covers default browser login, explicit copy-code selection, Anthropic copy-code redirect URI, verifier-as-state validation, JSON authorization/refresh exchanges (exact headers/keys) through the default production endpoint, refresh fallback, zero-value Login/Refresh, official auth/selection parameters, completion-before-success and duplicate exchange prevention. `oauth/anthropic_async_callback_v100_test.go` ports the external fixture and tests repeated async success/failure without EOF under race. Cancellation/selection and redaction controls remain. Live browser credential flow remains N/A. |
| `packages/ai/test/constrained-sampling.test.ts` | implemented | `inference/provider/openairesponses/responses_grammar_replay_v100_test.go` covers capability-gated tool declaration/call/result round-trips, transcript-declared tools, radius#115, foreign raw/fc_/ctc_ grammar ID omission, valid same-source ctc_ controls, function foreign-ID hashing, different-model ID omission and empty missing/null custom input. Existing strict/grammar definitions remain covered. |
| `packages/ai/test/oauth-callback-server.test.ts` | implemented | `oauth/callback_page_v100_test.go` exercises shared callback HTTP output, including the actual Pi SVG geometry/fills, `Signed in to Anthropic.`, HTML content type and malicious-provider escaping. The Anthropic browser/async tests also check completion before success, 502 on token failure, flushed response delivery before Login closes the server, duplicate exchange prevention and deterministic state/cancellation controls. |

Whole upstream test corpus for this release: `docs/v100/test-corpus-171.txt`, SHA-256 `9d24da3ede393a95a7131b1c9ac494f57d8165161d6eb581109c86809131abfc`.
