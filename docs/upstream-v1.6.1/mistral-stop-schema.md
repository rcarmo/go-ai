# Mistral preserves raw stops and routes the published model correctly

The three unchanged `mistral-raw-stop-reason.test.ts` cases and two unchanged `mistral-tool-schema.test.ts` cases passed on pinned v1.6.1 inputs. The test files, adapter, retry helper, config and lockfile matched Git. The staged Mistral catalog shard matched the verified release source. Node 26.13.0 and Vitest 4.1.12 ran both files offline.

The Go fixture now captures public requests for the same catalog model, `mistral-medium-latest`. On unchanged production, these requests lacked `/v1` and the `error` finish reason reported an extra `(server error)` suffix. A separate pinned fetch witness captured `https://api.mistral.ai/v1/chat/completions`; the original checks the terminal result, not this URL. Go now uses that endpoint for the canonical service origin and retains existing custom Go base paths.

Both known and unknown error stops preserve the raw reason. The original `error` is retryable with exactly `Provider stopped with: error`; `unmapped_error` remains non-retryable. The retry classifier recognises the exact upstream phrase instead of relying on the old suffix. Removing just that rule in a compiled control made the known error non-retryable without changing its URL or diagnostic. This is classification, not evidence of a second stream attempt.

The unchanged full Go package baseline also exposed a retired model in the strict-tool test. The source switched that case to `mistral-medium-latest`; the Go fixture now does likewise without reviving the old model. The exact strict and non-strict tool JSON and terminal transport error still match the visible behavior. Three TypeBox symbol checks remain unmatched because Go's JSON-backed schema values cannot carry JavaScript keys of that type.

The full Go Mistral package passed all 54 tests without skips, and three focused shared retry tests also passed. Vet and Staticcheck passed. Partial stream tests keep their original state and ordering checks with the new exact diagnostic.
