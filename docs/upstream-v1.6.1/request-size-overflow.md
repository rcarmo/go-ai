# Request-size overflow requires a complete error code

All 41 unchanged `overflow.test.ts` originals passed after byte verification. The unchanged native overflow group passed. With the new exact cases, Go recognised existing HTTP 413 and complete-code positives but incorrectly classified all five longer identifiers as overflow.

The Go pattern now requires ASCII word boundaries around `request_too_large`. It still accepts case-insensitive complete codes and preserves provider-independent HTTP 413 handling. A supplemental pinned probe distinguishes non-ASCII delimiters from the long s (U+017F), which is not an ASCII S in the upstream expression.

The 31 focused Go tests passed twice without skips. Compiled controls caught removing the code boundaries, broadening Unicode case-folding and dropping the independent colon-delimited 413 status match. Vet and Staticcheck v0.8.2 passed.
