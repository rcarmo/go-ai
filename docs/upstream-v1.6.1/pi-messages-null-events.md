# Pi Messages observes parsed events and skips null frames

All 11 unchanged pinned `pi-messages.test.ts` originals passed offline on Node 26.13.0 and locked Vitest 4.1.12. The test, provider, shared SSE decoder, config and lock matched the pinned Git bytes. The unchanged Go provider package passed before the public comparison was added.

The native stream originally omitted both valid raw hook notifications and converted a `null` frame into an extra start. With the current converter, a null frame is a no-op. The public hook sees the exact start and done objects in wire order before conversion. The original assertion checks that terminal stop remains `stop`. The CRLF and final-tail case and the hook refusal case are supplemental. The parser stops its channel sends when the stream terminates early.

The full Go provider package passed twice without skips. Two compiled overlays independently caught removing the hook and treating null as an event. Vet and Staticcheck passed. No other adapter or shared decoder changed.
