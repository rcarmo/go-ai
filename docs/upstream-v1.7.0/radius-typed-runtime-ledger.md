# Radius typed runtime ledger

| Contract | Implementation | Regression evidence | Status |
| --- | --- | --- | --- |
| Test compilation | `oauth/radius_runtime_test.go` uses the `ModelThinkingLevel` key type | Radius suite compiles and passes twice | complete |
| Credential ownership | `ModelsStoreEntry.OwnerFingerprint`, `DynamicModelCacheProvider`, Radius JWT `iss` + `sub` fingerprint | foreign owner blocked; opaque/missing owner denied; access-token hash exclusion asserted | complete |
| Validator isolation | Radius reads validators only from an owned typed entry | foreign owner receives no `If-None-Match` or `If-Modified-Since` | complete |
| Legacy typed recovery | nil `ImageModels` or `ClassifierModels` disables validators and retries unconditional 304 | source-derived legacy-typed test passes | complete |
| Atomic ETag publication | 200 and 304 paths retain `ETag`, `LastModified` and `CheckedAt` in the entry written under provider publication ordering | original 200/304 tests and ETag preservation control pass | complete |
| Legacy chat cache | Non-ownership-aware stores/providers retain existing restore behavior | existing `ModelsStoreEntry` and runtime cache tests pass | complete |
| Typed-only refresh trigger | `hasDynamicModelCatalog` includes image/decision entries | helper regression covers image-only and decision-only entries | complete |
| No ambient registration/auth change | Radius registration, OAuth config, and credential methods remain on existing paths | OAuth/runtime controls pass | complete |

Uncovered in this lane: release-level acceptance profiling, hosted CI, live Radius credentials, and unrelated v1.7.0 gaps.
