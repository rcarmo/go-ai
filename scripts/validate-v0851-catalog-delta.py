#!/usr/bin/env python3
"""Validate exact full-record catalog deltas for the v0.85.0 -> v0.85.1 audit."""
from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
EXPECTED_FILES = {
    "text-v0850.jsonl": (1336, "d654dcaaf752ce55a42691fe1291498bb09d7538b362a8db77fbb5ec31b1b661"),
    "text-v0851.jsonl": (1354, "430b342c0e721a3eefaa782e050af1ba5fe7abfa96b09cbec37e14a17a976d56"),
    "images-v0850.jsonl": (50, "2b74be7dd9438fdb21a15bfde7e8cddd8ffc04525addb837731d2e56a3f85f54"),
    "images-v0851.jsonl": (52, "d92cfc65b8450383fa7714b212192cd12cff02f4f0e275ba17767f4b59381179"),
}
EXPECTED_DELTAS = {"text": (20, 2, 18), "images": (2, 0, 0)}
EXPECTED_CURRENT = {"text": (1354, 39, 9), "images": (52, 1, 1)}


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def canonical(record: dict) -> str:
    return json.dumps(record, sort_keys=True, separators=(",", ":"))


def record_key(record: dict) -> str:
    provider = record.get("provider")
    model_id = record.get("id")
    if not isinstance(provider, str) or not isinstance(model_id, str):
        raise ValueError(f"record missing provider/id: {record!r}")
    return provider + "\t" + model_id


def load(path: pathlib.Path) -> dict[str, dict]:
    records: dict[str, dict] = {}
    for line_no, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        record = json.loads(line)
        key = record.get("_key")
        if not isinstance(key, str):
            key = record_key(record)
        if key in records:
            raise ValueError(f"{path}:{line_no}: duplicate key {key}")
        records[key] = record
    return records


def fail(message: str) -> int:
    print(f"v0.85.1 catalog delta validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0851"
    for name, (want_count, want_hash) in EXPECTED_FILES.items():
        path = docs / name
        if not path.is_file():
            return fail(f"missing {path}")
        rows = [line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
        got_hash = sha256(path)
        if len(rows) != want_count:
            return fail(f"{name} rows {len(rows)}, want {want_count}")
        if got_hash != want_hash:
            return fail(f"{name} sha256 {got_hash}, want {want_hash}")
    try:
        text_old = load(docs / "text-v0850.jsonl")
        text_new = load(docs / "text-v0851.jsonl")
        images_old = load(docs / "images-v0850.jsonl")
        images_new = load(docs / "images-v0851.jsonl")
    except Exception as exc:  # noqa: BLE001 - CLI diagnostic
        return fail(str(exc))
    for label, old, new in [("text", text_old, text_new), ("images", images_old, images_new)]:
        added = set(new) - set(old)
        removed = set(old) - set(new)
        changed = {key for key in set(old) & set(new) if canonical(old[key]) != canonical(new[key])}
        got = (len(added), len(removed), len(changed))
        want = EXPECTED_DELTAS[label]
        if got != want:
            return fail(f"{label} delta +{got[0]}/-{got[1]}/{got[2]}, want +{want[0]}/-{want[1]}/{want[2]}")
        count = len(new)
        providers = len({record.get("provider") for record in new.values()})
        apis = len({record.get("api") for record in new.values()})
        want_current = EXPECTED_CURRENT[label]
        if (count, providers, apis) != want_current:
            return fail(f"{label} current counts {(count, providers, apis)}, want {want_current}")
        print(f"{label} full-record delta: +{got[0]}/-{got[1]}/{got[2]}; current count/providers/apis: {count}/{providers}/{apis}")
    print("v0.85.1 catalog delta validation passed")
    return 0


def mutate_first_metadata(path: pathlib.Path) -> None:
    lines = path.read_text(encoding="utf-8").splitlines()
    record = json.loads(lines[0])
    record["_mutation"] = "non-id metadata drift"
    lines[0] = canonical(record)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0851-catalog-delta-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied catalog deltas did not validate before corruption")
        for name in ["text-v0850.jsonl", "text-v0851.jsonl", "images-v0850.jsonl", "images-v0851.jsonl"]:
            copy = pathlib.Path(td) / f"repo-{name}"
            shutil.copytree(tmp, copy)
            mutate_first_metadata(copy / "docs" / "v0851" / name)
            proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0851-catalog-delta.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if proc.returncode == 0:
                return fail(f"corrupted {name} unexpectedly validated")
    print("v0.85.1 catalog delta negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    return self_test() if args.self_test else validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
