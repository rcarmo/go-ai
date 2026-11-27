# Cloudflare Gateway compatibility for OpenAI Completions

All 12 unchanged originals in `openai-completions-empty-tools.test.ts` passed in the locked v1.7.0 AI stage. The release changed three test fixtures to use GLM-5.3 instead of the previous Kimi row. This comparison covers the 12 assertions in those three cases.

The same Go behavior passed on unchanged native code `008caa98822471b58fe2cb03b442dbba99429ef4`. The existing Cloudflare stream suite now checks the request through public dispatch with a per-request HTTP transport. Its explicit compatibility fixture tests conservative request fields, provider auth and all three session affinity headers without depending on a published model ID. The catalog has a separate full-record check.

The Cloudflare stream and Completions empty-tools selection passed twice without skips, with scoped vet and Staticcheck. A compiled control that changed the outgoing `session_id` to `wrong` failed the session-affinity case. No production code changed. The other pinned source cases are not included in this ledger.
