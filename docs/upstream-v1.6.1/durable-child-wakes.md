# Durable collects owned child results across separate wakes

The pinned `harness-park-children.test.ts:107–156` case passed unchanged with the official Node fixture; the nine other cases were unselected. The test, helper files and Vitest config matched Git. The five focused native baseline groups passed before the new assertions.

The public SQLite test parks the caller on two owned children with `any`. When B finishes, the caller sees only B in the wake, then waits for A. Between wakes, A remains sleeping, B's ownership is unchanged, the original input is still pending, and no additional provider request has run. After A finishes, the result is exactly `x: ay + y: bee`. The committed tool-result entry is attributed to the original caller. The three pinned assertions cover normal settlement, exact output and nil/B/A wake order. The public live-state and persistence witnesses are supplemental; the historical reopen case is separate.

The five final focused groups passed twice with seed 42. Vet and Staticcheck passed. Removing either the repark checkpoint or the caller attribution in isolated overlays caused the intended bounded failure. No production change was needed.
