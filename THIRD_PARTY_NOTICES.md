# Third-party notices

## TypeBox validation rules

`schema_formats_generated.go` adapts the email, URI, UUID and IPv4 regular
expressions from TypeBox, carried by the pinned upstream pi-schema source.
`scripts/generate-schema-formats.ts` records the source paths and revision.
`schema_validation_temporal.go` adapts the date, time and date-time algorithms
from the same pinned source (`formats/typebox/{date,time,date_time}.ts`).
`schema_validation_hostname.go` adapts its hostname/IDNA and punycode rules
(`formats/typebox/idna/`). The Unicode-mode constructor rule in
`schema_validation_formats.go` and `internal/quickjs/regexp.go` follows
`formats/typebox/regex.ts`. Primitive conversion ordering in `context.go` and
`schema_validation_primitives.go` follows `packages/schema/src/coerce.ts`, whose
conversion pass derives from TypeBox's `Value.Convert`. Selected original
assertions are retained in `tests/testdata/upstream-main/validation-strings.json`
and `tests/testdata/upstream-main/validation-primitives.json`.

The MIT License (MIT)

Copyright (c) 2017-2026 Haydn Paterson

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.

## JSON Schema Test Suite

`tests/testdata/upstream-main/hostname.json`, the regex assertions in
`tests/testdata/upstream-main/validation-regex.json` and the selected
reference/anchor assertions in `tests/testdata/upstream-main/validation-resources.json` come from
the pinned upstream JSON Schema Test Suite. Its copyright and the MIT
permission/disclaimer printed above apply to these fixtures:

Copyright (c) 2012 Julian Berman

## QuickJS syntax checks

The regex format checker uses the same bundled QuickJS-WASI 3.6.2 asset and Go
host as codemode. Asset provenance and full MIT notices are retained in
`internal/quickjs/guest/PROVENANCE.md`, `guest/LICENSE.quickjs-wasi` and
`internal/quickjs/LICENSE.gi` (guest paths relative to `internal/quickjs/`).
The QuickJS-WASI notice is Copyright (c) 2026 Vercel, Inc.; the adapted Go host
notice is Copyright (c) 2026 Rui Carmo. No guest asset was changed.

## pi-schema validation and reference resolution

`schema_validation_resources.go` adapts resource indexing, scope and URI fallback
rules from `packages/schema/src/resolver.ts` at `c9f4c7b9`.
`schema_validation.go` adapts the overlapping-oneOf diagnostic from
`packages/schema/src/explain.ts` at the same pin. `schema_validation_null.go`
and optional-null normalisation follow the null-domain compiler/coercion rules
at that pin. The MIT permission and
disclaimer printed above apply:

Copyright (c) 2025 Mario Zechner

## WHATWG URL parsing

URI resolution and URL format validation use `github.com/nlnwa/whatwg-url`
v0.6.2, Copyright 2020 National
Library of Norway, under the Apache License 2.0:
<https://github.com/nlnwa/whatwg-url/blob/v0.6.2/LICENSE>.
The dependency source and licence remain in its module; this repository imports
the parser and adds a measured opaque-path compatibility rule. The URL host
adapter directly uses `golang.org/x/net/idna` v0.62.0 (already a transitive
dependency), Copyright 2009 The Go Authors, under its BSD-3-Clause licence:
<https://cs.opensource.google/go/x/net/+/refs/tags/v0.62.0:LICENSE>.
It selects the WHATWG UTS46 options and checks the mapped empty-A-label case
before canonical hosts enter the parser.
