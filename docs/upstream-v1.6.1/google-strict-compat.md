# Google model overrides select strict tool mode

All 23 unchanged `google-shared-convert-tools.test.ts` cases passed on Node 26.13.0 and locked Vitest 4.1.12. The test, three Google adapter sources, compatibility schema, Vitest config and lock matched pinned Git bytes. The verified release catalog has no `supportsStrictMode` override on a Google API model, so no generated record changed.

The unchanged Go baseline already detected prefixed Gemini 4 IDs. With the normal Gen AI and Vertex request builders, explicit overrides were ignored: the alias did not send `VALIDATED`, and the disabled Gemini model still did. Go now reads the optional boolean field `Model.GoogleCompat.SupportsStrictMode`. An explicit false disables strict mode, an explicit true enables it, and a missing boolean leaves model-ID detection in force. The provider-specific JSON field is `googleCompat`; cloned models own their boolean.

The new Go comparisons stop at the public payload hook before any HTTP attempt and check both adapters, including a missing boolean within an otherwise present compatibility object. All 105 Google package tests and the focused model tests passed without skips, followed by vet and Staticcheck. Two compiled controls failed when treating false as unset or sharing the boolean between model copies.
