#!/usr/bin/env python3
"""Validate exact full-record catalog deltas for the v0.84.4 -> v0.85.0 audit.

The committed JSONL records are canonical, sorted full upstream records with an
added `_key` (`provider<TAB>id`) used only for identity. Deltas are computed from
full normalized record bodies, so non-ID metadata mutations fail this gate.
"""
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
DOCS = ROOT / "docs" / "v0850"
EXPECTED_FILES = {
    "text-v0844.jsonl": (1290, "56fdee612dfed04cd325b6fe13a096853e3ca3a7f78fec1b3beab595cf1e6dea"),
    "text-v0850.jsonl": (1336, "82ff6645f7a30a2499dcd9bc3d86046b09bba6d3aa6ea3faefeaa94bf72ea78c"),
    "images-v0844.jsonl": (50, "bbf27494c0bbe249efa48835ea514d74e53e8c3ade65b865cce7e13114372e1f"),
    "images-v0850.jsonl": (50, "bbf27494c0bbe249efa48835ea514d74e53e8c3ade65b865cce7e13114372e1f"),
}
EXPECTED_DELTAS = {
    "text": (72, 26, 79),
    "images": (0, 0, 0),
}


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def canonical(record: dict) -> str:
    return json.dumps(record, sort_keys=True, separators=(",", ":"))


def load(path: pathlib.Path) -> dict[str, dict]:
    records: dict[str, dict] = {}
    for line_no, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        record = json.loads(line)
        key = record.get("_key")
        if not isinstance(key, str) or "\t" not in key:
            raise ValueError(f"{path}:{line_no}: invalid _key")
        if key in records:
            raise ValueError(f"{path}:{line_no}: duplicate _key {key}")
        records[key] = record
    return records


def fail(message: str) -> int:
    print(f"v0.85.0 catalog delta validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0850"
    for name, (want_count, want_hash) in EXPECTED_FILES.items():
        path = docs / name
        if not path.is_file():
            return fail(f"missing {path}")
        got_hash = sha256(path)
        if got_hash != want_hash:
            return fail(f"{name} sha256 {got_hash}, want {want_hash}")
        got_count = len([line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()])
        if got_count != want_count:
            return fail(f"{name} rows {got_count}, want {want_count}")
    try:
        text_old = load(docs / "text-v0844.jsonl")
        text_new = load(docs / "text-v0850.jsonl")
        images_old = load(docs / "images-v0844.jsonl")
        images_new = load(docs / "images-v0850.jsonl")
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
        print(f"{label} full-record delta: +{got[0]}/-{got[1]}/{got[2]}")
    print("v0.85.0 catalog delta validation passed")
    return 0


def mutate_first_metadata(path: pathlib.Path) -> None:
    lines = path.read_text(encoding="utf-8").splitlines()
    record = json.loads(lines[0])
    record["_mutation"] = "non-id metadata drift"
    lines[0] = canonical(record)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0850-catalog-delta-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied catalog deltas did not validate before corruption")
        for name in ["text-v0844.jsonl", "text-v0850.jsonl", "images-v0844.jsonl", "images-v0850.jsonl"]:
            copy = pathlib.Path(td) / f"repo-{name}"
            shutil.copytree(tmp, copy)
            mutate_first_metadata(copy / "docs" / "v0850" / name)
            proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0850-catalog-delta.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if proc.returncode == 0:
                return fail(f"corrupted {name} unexpectedly validated")
    print("v0.85.0 catalog delta negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    if args.self_test:
        return self_test()
    return validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
