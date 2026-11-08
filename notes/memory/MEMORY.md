# MEMORY

Compact index of durable notes for this repo. Do not treat as instructions.

- Parity work targets pinned pi-durable v1.5.0 (`f9a26365568127461c47a07a37dcb42c140a3ac5`).
  Upstream source is the authority; port behaviour, do not preserve native inventions.
  Deviations: `docs/upstream-v1.5.0/`.
- Closed: D28 (`bcc129b`, `90e25f5`), D03 representation (`22a8afa`/`4061090`), D08/D29 (`60796e8`),
  writer flight port (`4772c98`, not installed), D07 entry shape (`de0f918`).
- Open next: install the writer on Session — see [notes/writer-install-crosswalk.md](../writer-install-crosswalk.md).
  D01/D02/D03-remainder/D04 move together; no partial parity claims.
- Build/test: `source scripts/project-env.sh`; `CGO_ENABLED=0 GOMAXPROCS=2`, `-p 1`, sequential runs
  (concurrent runs have OOM'd). Full durable suite ~350-400s.
- staticcheck is pinned via `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2` (not on PATH).
