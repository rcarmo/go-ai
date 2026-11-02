# Third-party notices

## TypeBox format checks

`schema_formats_generated.go` adapts the email, URI, UUID and IPv4 regular
expressions from TypeBox, carried by the pinned upstream pi-schema source.
`scripts/generate-schema-formats.ts` records the source paths and revision.
`schema_validation_temporal.go` adapts the date, time and date-time algorithms
from the same pinned source (`formats/typebox/{date,time,date_time}.ts`).
`schema_validation_hostname.go` adapts its hostname/IDNA and punycode rules
(`formats/typebox/idna/`).

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

`tests/testdata/upstream-main/hostname.json` is copied from the pinned upstream
JSON Schema Test Suite fixture. Its copyright and the MIT permission/disclaimer
printed above apply to this fixture:

Copyright (c) 2012 Julian Berman
