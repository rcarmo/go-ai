# Schema equality and unique-item validation

## Pinned original and receipt

This lane is locked to native base `59c6ed7e4cd8c0ff73236615d345d8d974060d4d` and upstream pi v1.7.0 commit `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The unchanged `packages/schema/test/equal.test.ts` Git blob is `79164af87738bbfae43ad28c516453544a2ffad5`, with SHA-256 `591a1a7a347f537bcf9fb5fedae96455eacd85eb20bda0a415ff4de5353768f3`. Its bytes were verified before and after the original Vitest run; the pinned original passed one file and one test.

The original ten-pair assertion compares the exported `equal` operation with TypeBox `Value.Equal`. `TestSchemaEqualityOriginalCases` ports that complete table, including both explicit-undefined inputs, in source order and reversed order. The same table is exercised through `CompiledJSONSchema.Check`: JSON-representable literals use `const` and `enum`; all pairs use `uniqueItems` as the production equality path. Object member order remains irrelevant, array order remains significant, and object, array, primitive and missing-property boundaries remain distinct.

The unchanged native `SchemaEqual` control passed. The first production-path run exposed two concrete mismatches: `uniqueItems` rejected the `null`/explicit-undefined pair and the present-undefined-property/empty-map pair even though those values are structurally distinct. Runtime commit `a61f5bbc53d9c4a21cfbf38b799ddfe2c735a194` adds a narrow undefined-containing-item fallback to the existing bounded canonicalizer. Follow-up `a61f5bbc53d9c4a21cfbf38b799ddfe2c735a194` keeps the full structural traversal active before that fallback. It therefore delegates only validated native-only undefined containers to `SchemaEqual`; JSON values still use the canonical hash path, and cyclic/non-JSON failures remain unchanged.

## Verification and scope

The complete original test, `const`, `enum`, and `uniqueItems` production paths passed twice with `-shuffle=42 -count=2`. Relevant literal and unique-item controls also passed. `go vet ./...` and Staticcheck v0.8.2 passed with `CGO_ENABLED=0`. No full acceptance, provider, profile, publication, or root-repository work was performed.
