# Provider retry fallback backoff

The fallback in `packages/ai/src/utils/provider-retry.ts:67–68` at upstream `f9a26365568127461c47a07a37dcb42c140a3ac5` caps `0.5 * 2 ** retryIndex` at eight seconds, then applies up to 25% downward jitter. The pinned provider retry tests cover retry, server delay limits and cancellation; these fallback controls derive from the implementation.

At local parent `d0cf691`, `TestMainProviderRetryFallbackBackoffBounds` failed at index 64 with a zero delay, and `TestMainProviderRetryFallbackHasDownwardJitter` found no jitter. The former overflowed the integer multiplier before applying the cap. `provider_retry.go` now caps before shifting and applies jitter using Go's `math/rand/v2`. Negative retry indexes cannot arise from the public retry loop; the helper clamps them to zero.

The two source-derived root tests passed (`make test PACKAGES=. TEST_FLAGS="-v -run 'TestMainProviderRetryFallback' -count=1 -timeout=45s"`, 0.004s). Six provider controls passed (`make test PACKAGES=./tests TEST_FLAGS="-v -run 'TestMainProviderRetrySourceNoRetryStatuses|TestRetryProviderRequest|TestDoProviderRequestWithRetry' -count=1 -timeout=45s"`, 0.798s). Restoring the parent implementation via a temporary compiler overlay caused both root tests to fail. `go vet . ./tests`, formatting and `git diff --check` passed with `CGO_ENABLED=0`; overlay scratch was removed.

The bounds test samples the production calculation; the jitter test checks for at least one sub-base sample in 64 draws. They do not prove the random distribution. Server header parsing, extreme requested delays and provider-specific status exclusions require separate controls. Full provider retry parity, the full suite, profiling, hosted CI and release remain unverified.
