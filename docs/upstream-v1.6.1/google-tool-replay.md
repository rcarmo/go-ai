# Google tool replay recognizes prefixed Gemini model IDs

All 13 variants of the unchanged `google-shared-gemini3-unsigned-tool-call.test.ts` passed on Node 26.13.0 and locked Vitest 4.1.12. The original, shared converter, transcript helper, Vitest config and npm lock matched the pinned Git bytes. The unchanged full native Google package passed before the new wire fixture was added.

Through a public completion and an inert HTTP transport, the native fixture captured exact function-call and function-response IDs, signature presence, and raw model turn JSON. The red test failed only for `randomprefix-gemini-4-argon`: both sets of IDs were missing. The parser now recognises the first version-bearing Gemini name anywhere in a gateway model ID. The prefixed Gemini 2.5 case still omits IDs; native tests also cover both adapters, same-model signatures, and the absence of synthetic unsigned history.

The full Go Google package passed all 104 tests without skips. Two compiled controls failed as intended when the function-response IDs or the same-model signature were removed. Vet and Staticcheck passed. The version helper is also used by Google's strict-tool and multimedia-routing checks, which stayed green in the full package run. No SDK constructor, live request, or new per-model compatibility override is claimed.
