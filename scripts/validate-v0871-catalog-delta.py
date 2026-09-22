#!/usr/bin/env python3
"""Validate exact catalog deltas for the v0.87.0 -> v0.87.1 audit."""
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
    "text-v0870.jsonl": (1445, "baf3a836d7e8bc386dd65ec2a9f61e458e5009d19cd884a12daa4f637c3546ef"),
    "text-v0871.jsonl": (1495, "13843511b69557a8d92ec276f8bc19f89579f79577ad9d26e69d165ef2270777"),
    "images-v0870.jsonl": (54, "1a203c24d75d77c016a7497109dd6f2c8d390b29a7a3a08de60589b0c3c42f03"),
    "images-v0871.jsonl": (55, "17ed6676553d78d561f9e67848e77da79e8ecd62aacbd89e0d0b66abb64b2bf1"),
}
EXPECTED_DELTAS = {"text": (62, 12, 35), "images": (1, 0, 0)}
EXPECTED_CURRENT = {"text": (1495, 41, 10), "images": (55, 1, 1)}


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def canonical(record: dict) -> str:
    copy = dict(record)
    copy.pop("_key", None)
    return json.dumps(copy, sort_keys=True, separators=(",", ":"))


def load(path: pathlib.Path) -> dict[str, dict]:
    records: dict[str, dict] = {}
    for line_no, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        record = json.loads(line)
        key = record.get("_key")
        if not isinstance(key, str):
            provider = record.get("provider")
            model_id = record.get("id")
            if not isinstance(provider, str) or not isinstance(model_id, str):
                raise ValueError(f"{path}:{line_no}: record missing key/provider/id")
            key = provider + "\t" + model_id
        if key in records:
            raise ValueError(f"{path}:{line_no}: duplicate key {key}")
        records[key] = record
    return records


def fail(message: str) -> int:
    print(f"v0.87.1 catalog delta validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0871"
    for name, (want_count, want_hash) in EXPECTED_FILES.items():
        path = docs / name
        if not path.is_file():
            return fail(f"missing {path}")
        rows = [line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
        if len(rows) != want_count:
            return fail(f"{name} rows {len(rows)}, want {want_count}")
        got_hash = sha256(path)
        if got_hash != want_hash:
            return fail(f"{name} sha256 {got_hash}, want {want_hash}")
    try:
        text_old = load(docs / "text-v0870.jsonl")
        text_new = load(docs / "text-v0871.jsonl")
        images_old = load(docs / "images-v0870.jsonl")
        images_new = load(docs / "images-v0871.jsonl")
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
        if (count, providers, apis) != EXPECTED_CURRENT[label]:
            return fail(f"{label} current counts {(count, providers, apis)}, want {EXPECTED_CURRENT[label]}")
        print(f"{label} full-record delta: +{got[0]}/-{got[1]}/{got[2]}; current count/providers/apis: {count}/{providers}/{apis}")
    print("v0.87.1 catalog delta validation passed")
    return 0


def mutate_first_metadata(path: pathlib.Path) -> None:
    lines = path.read_text(encoding="utf-8").splitlines()
    record = json.loads(lines[0])
    record["_mutation"] = "non-id metadata drift"
    lines[0] = json.dumps(record, sort_keys=True, separators=(",", ":"))
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0871-catalog-delta-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied catalog deltas did not validate before corruption")
        for name in EXPECTED_FILES:
            copy = pathlib.Path(td) / f"repo-{name}"
            shutil.copytree(tmp, copy)
            mutate_first_metadata(copy / "docs" / "v0871" / name)
            proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0871-catalog-delta.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if proc.returncode == 0:
                return fail(f"corrupted {name} unexpectedly validated")
    print("v0.87.1 catalog delta negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    return self_test() if args.self_test else validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
