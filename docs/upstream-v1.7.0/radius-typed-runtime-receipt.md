# Radius typed runtime receipt

## Scope

Base: `15d9040d37157d03fc739d82b89d8a9ad25aa5be` on the isolated `port/v170-radius-typed` lane.

This slice completes credential-owned Radius chat/image/decision runtime catalogs. It does not change ambient provider registration, OAuth endpoint selection, authentication policy, or the legacy non-Radius chat cache contract.

## Behavior covered

- Typed image and classifier snapshots are detached at write/read and public lookup boundaries.
- Runtime image/classifier keys override compatibility registries per provider, including authoritative empty catalogs.
- Radius cache validators and catalog rows require an owner fingerprint derived from JWT `iss` + `sub`, hashed together. No access/refresh token hash is stored; opaque or missing credentials cannot own a cache entry.
- A credential owner can reuse its validators. A different, invalid, or missing owner starts from a fresh response without inherited validators.
- A legacy chat-only Radius entry bypasses validators and a stale 304 is retried without conditional headers. Nil typed lists therefore cannot hide typed discoveries.
- Successful 200 and 304 publication retains validators through the cache/runtime write boundary.
- OAuth token refresh is triggered for typed-only image or decision catalogs, not only for a non-empty chat list.
- Generic legacy chat cache restore remains unchanged for providers without an ownership contract.

## Verification

Environment: Go `1.27.3`, `CGO_ENABLED=0`, `GOTOOLCHAIN=go1.27.3`, bounded to two workers and one build package with `GOMEMLIMIT=650MiB`.

The final focused run acquired `/tmp/go-ai/v170-radius-typed.lock` with a 90-second wait and passed:

```sh
go test ./oauth -run 'Test(Radius|DynamicModelCatalogPresence)' -count=2 -shuffle=42 -timeout=90s
go test ./tests -run 'Test(ModelRuntime|ModelsStoreEntry)' -count=2 -shuffle=42 -timeout=90s
go test ./oauth -run 'Test(GetAPIKey|RuntimeForProvider|Radius)' -count=1 -timeout=90s
go test ./tests -run 'TestV0821ModelCatalogRevalidation|TestModelsStoreEntryPreservesETagAndLastModified' -count=1 -timeout=90s
```

Earlier compile/build failures in the new detached-catalog test were fixed by using `ModelThinkingLevel` conversions. The final focused and control commands above passed. The same working tree also passed:

```sh
make vet
make staticcheck
```

Staticcheck used its pinned `v0.8.2`; the first attempt reported capitalized error text in the new stale-304 error, which was corrected before the passing rerun.

## Limits

This is a scoped local runtime/cache port receipt, not release acceptance. It does not include acceptance profiling, hosted CI, live-credential replay, dependency/security publication, or a whole-repository test claim.
