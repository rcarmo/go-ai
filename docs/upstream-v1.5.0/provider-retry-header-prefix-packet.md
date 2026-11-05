# Provider retry numeric prefixes

At upstream `f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/ai/src/utils/provider-retry.ts:55–65` uses `Number.parseFloat` for both retry headers. At parent `773d073`, Go rejected numeric prefixes with trailing text and fell back to jitter. A production-path control using `retry-after: 277403 seconds` made a second request instead of rejecting the requested delay against the one-second cap.

The provider header parser now accepts a signed decimal prefix, optional complete exponent, leading whitespace and `Infinity`. It distinguishes a non-number from infinity when deciding whether to try an HTTP date. It rejects non-finite numeric delays and preserves header precedence. Go's whitespace and HTTP date parsing are not an exhaustive implementation of ECMAScript whitespace or `Date.parse`; those differences remain open.

The source-derived prefix, non-finite, cap, duration-bound and fallback controls passed (`make test PACKAGES=. TEST_FLAGS="-v -run 'TestMainProviderRetry(Header|ServerDelayBounds|HugeServerDelay|Fallback)' -count=1 -timeout=45s"`). Six provider retry controls passed in `./tests`. Restoring the parent implementation via a compiler overlay caused the production-path cap control to fail with two requests. `go vet . ./tests`, formatting and `git diff --check` passed with `CGO_ENABLED=0`; overlay scratch was removed. These controls derive from source; they do not represent the entire upstream test file.

Date parsing, exact whitespace sets, fractional duration rounding and provider-specific exclusion wiring need separate examination. Full provider retry parity, the full suite, profiling, hosted CI and release remain unverified.
