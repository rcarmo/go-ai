# Pi Messages validates content indexes before using them

The unchanged `pi-messages.test.ts:287–313` original passed with the other ten pinned originals. The test, adapter, shared decoder, runner config and lock matched Git. On the previous native checkpoint, a deliberately small forward gap returned success and created three blocks. The very large original index was not sent through unbounded native code.

The Go converter now checks the numeric index while it is still in wire form. Start events may append the next block or reuse an existing block. Other content events must name an existing block. All four pinned invalid variants return an `Invalid contentIndex` error without filling the gap. A positive control appends and reuses a block. Go has no JavaScript Array prototype, so the prototype assertion is unmatched.

The complete Go provider package passed twice with no skips. A compiled control that permits a small forward gap failed the expected error and content-bound checks. Vet and Staticcheck passed.
