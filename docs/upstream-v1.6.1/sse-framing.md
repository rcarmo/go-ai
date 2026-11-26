# SSE framing across byte boundaries

All four unchanged `packages/ai/test/sse.test.ts` cases passed on Node 26.13.0 and locked Vitest 4.1.12, without network access. Before running them, the upstream lockfile, Vitest config, test file and decoder matched the pinned Git bytes. The unchanged Go SSE package passed at `87cacdd3` before the new comparisons were added.

The new Go tests use the public `sse.Parse` reader path. They check every chunk size for the three line endings, including splits within `é`, a preserved leading space and a final event with no blank line before EOF. A gated reader checks that no event arrives after the first CR-ended read, then that the rest of the split CRLF produces exactly one event. Empty event names and comment-only frames leave the next event's raw data intact. Go calls its default event `message`; the TypeScript decoder represents the absent name as `null`.

The whole-event assertion for the chunk matrix is partial: the source decoder keeps a comment in the raw lines of a data-bearing frame, whereas the Go `Raw` field is documented to omit comments. That field and its production users were not changed. The other four sites map to framing and default dispatch, not identical Go and TypeScript event shapes.

All 18 Go package entries (including the existing fuzz seeds, not a fuzzing run) passed twice, with no skips. Vet and Staticcheck v0.8.2 passed. Three compiled scratch controls were rejected: trimming all leading spaces changed the payload; consuming a chunk-ending CR without waiting for a possible LF split one event into two; dispatching empty frames produced an extra event.
