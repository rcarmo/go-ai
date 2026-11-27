# AI changed-test group 3 checkpoint

- Native base was isolated at exact `016c041fb016d866273fbbb09bf4d0dfe6830ef5`, matching fetched `origin/main` before edits.
- Upstream `v1.7.0` pin `fb0fe823539f6d04850f6e34748c01f01f212e1f` was byte-verified for the three requested test files by Git blob and SHA-256.
- Unchanged originals ran in an isolated empty HOME: 160 credential-gated cases skipped, no provider calls. Generated data came from the checksum-pinned npm artifact.
- No runtime difference was demonstrated. Native production controls pass for orphan tool-result synthesis, production usage, exact Unicode/0xD83D sanitization, and finite-JSON rejection.
- An over-broad selected-package run was stopped after completion by scope/memory direction. It exposed unrelated baseline failures in retired OpenCode catalogue fixtures, Durable Codemode timing/output, a Mistral timeout selector, and a QuickJS collision test. None is in this three-file audit and none was changed.
- Pending: twice-run the exact new controls and narrow relevant selectors, then inspect and commit locally only.
