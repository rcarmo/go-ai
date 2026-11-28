# OAuth public-helper unchanged originals

This bounded receipt owns nine unchanged AI test files at `fb0fe823539f6d04850f6e34748c01f01f212e1f`: GitHub Copilot, identity, Kimi Code, Meta, shared OAuth auth, callback server, device polling, ChatGPT and Codex OAuth. It is disjoint from provider, Durable, Codemode and Schema packets.

| Upstream test | SHA-256 | Pinned blob |
| --- | --- | --- |
| `packages/ai/test/github-copilot-oauth.test.ts` | `605518d69fae9bf4e8df638a5744c656d35f3b8d4533a994aa70d6206a4b710b` | `7a66bfa62bb2c6a09274f2e676d3d29bc2161ade` |
| `packages/ai/test/identity.test.ts` | `cf6766b2cb95e805cff3c4fd12afc99eb588a6cce6fa9b8472ded0f06def3293` | `6b12d02d8c49bbf69c994c07cde1d3bd84f0eaeb` |
| `packages/ai/test/kimi-coding-oauth.test.ts` | `a0cce566b219ce880c0559f1cede8aec295c694166d595595d2075c4300872f0` | `d64bbf1bba5c3551c2af6171e053c03b039e98ae` |
| `packages/ai/test/meta-oauth.test.ts` | `d78eea9109de93748c6bede3c4a9fec1f0e494013328ee7d54244f5269ea520e` | `3e2ad326844f38c7e3af621169658852fa0a7382` |
| `packages/ai/test/oauth-auth.test.ts` | `cf48a806cd38f76c5cb3a38921672acc8d665fab57d6beb7111ca51e4bc43991` | `6033fb7fb6a4b4631e2b7ef15aa788cd98b463a7` |
| `packages/ai/test/oauth-callback-server.test.ts` | `e61a00bfbd10bdaa3d5b3fa73afdb9542ff51b8920e32f971557e10a4a418906` | `6746220b910b3e97327fe4d0bd59e679e8c7f189` |
| `packages/ai/test/oauth-device-code.test.ts` | `0402a6238bf9ab3be4ce8879f9068e2383475cb2a6a9fad9da33f3acbd8e0f1e` | `2bda9f5e60b64525f31f5f87aef72bb2ae300956` |
| `packages/ai/test/openai-chatgpt-oauth.test.ts` | `9605bc7f0d912af2d2afb9a15cbeca47488f08ae8644a67efc7fbdaa060451f9` | `35ba33aedd598c7b634627ca3a48851779107745` |
| `packages/ai/test/openai-codex-oauth.test.ts` | `a8546d718e3d69e2b42109b9cf61d3bb080767c63be2d9afcdbdf558c963f9c9` | `269e6b85b3da10454e638a1a1ea42b5dc2fb9d5f` |

The detached source checkout matched both every table SHA-256 and `git show fb0fe823539f6d04850f6e34748c01f01f212e1f:<path>`. Vitest 4.1.12 ran all nine originals unchanged on Node 26.13.0, serialized through `/tmp/go-ai/v170-shared-verification.lock`, with one worker and a 400 MiB old-space limit: **82 tests passed**. No live credential, provider network call or secret was used. All requests were intercepted by the original fixtures; no test in this packet is credential-gated.

## Original and native checks

The source-derived controls were first run on unchanged native `cbca8ba81df8a13ee1c88521a078203540a637b1`. They exposed the stale `PiAIVersion`, untrusted and non-normalized GitHub device URLs, absent GitHub classifier availability, no Kimi host override/retry/invalid-grant contract, duplicate callback completion, and the retired Auth0-based Codex device flow. The native root and OAuth baseline otherwise passed before the additions.

The smallest fixes update the client identity version, validate and canonicalize GitHub verification URLs, retain and filter Copilot classifier IDs, expose deterministic device metadata, honor Kimi host and 429/401 refresh behavior, claim an OAuth callback exactly once, and replace Codex device authorization with the v1.7.0 OpenAI account flow. A public generic device-code polling helper now covers immediate and delayed first polls, slow-down intervals, cancellation and expiry. Callback failure HTML remains credential-safe and never reflects token endpoint bodies or authorization credentials.

ChatGPT registration/scopes, Meta mint/refresh, OAuth registry/token derivation, PKCE, callback page escaping and remaining header behavior already passed public native controls. JavaScript credential-store and barrel identity are represented by the native registry and runtime APIs rather than object identity. A live OAuth sign-in is not claimed: every flow test uses an intercepted transport or local listener.

## Verification

With `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2`, `GOFLAGS=-p=1`, Go `1.27.3`, and the shared v1.7 lock, the completed public identity/OAuth selectors pass twice with shuffle. Focused `go vet . ./oauth` and Staticcheck `v0.8.2` for the same package slice pass; `git diff --check` passes. Credential-gated items: **none in this nine-file packet**.

This is a bounded local audit and implementation receipt, not full v1.7.0 acceptance, live login replay, acceptance profiling, publishing or release authorization.
