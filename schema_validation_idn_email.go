package goai

// The literal is from pinned pi-schema's formats/typebox/idn_email.ts
// (TypeBox, MIT; Copyright (c) 2017-2026 Haydn Paterson; see THIRD_PARTY_NOTICES.md).
const schemaIDNEmailPattern = "^(?:[A-Za-z0-9!#$%&'*+/=?^_`{|}~\\u{0080}-\\u{10FFFF}-]+(?:\\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~\\u{0080}-\\u{10FFFF}-]+)*|\"(?:[^\"\\\\]|\\\\.)*\")@[\\p{L}\\p{N}](?:[\\p{L}\\p{N}-]{0,62})(?<!-)(?:\\.[\\p{L}\\p{N}](?:[\\p{L}\\p{N}-]{0,62})(?<!-))*$"
