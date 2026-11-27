# Durable attributes input admitted in a task's terminal commit

The unchanged `harness-submit-in-commit.test.ts:583–616` case passed on the pinned Node fixture using `ControlledStorage`. The test, support files and Vitest config matched Git. The other 23 cases were unselected. Five focused native groups passed before the new test.

In the Go SQLite harness, a typed transaction lookup returns the exact user entry with `ByTask` set to the task that admitted it. The terminal task reports the same submission ID as the completed input. Supplemental checks hold the callback before commit, prove there is no stored submission or provider request while it is held, and show that the provider sees the committed task result. One SQLite publication contains the terminal task, the placed submission and its attributed entry at the same commit sequence.

Six focused Go groups passed twice with seed 42, with vet and Staticcheck. Two bounded overlays failed on a wrong terminal result and on lost task attribution. Production code and scheduler ownership did not change.
