#!/usr/bin/env python3
"""Normalize CycloneDX SBOM JSON for stable artifact hashing."""
from __future__ import annotations
import json
import pathlib
import sys


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("usage: scripts/normalize-sbom.py artifacts/sbom.cdx.json REVISION", file=sys.stderr)
        return 2
    path = pathlib.Path(argv[1])
    revision = argv[2]
    data = json.loads(path.read_text(encoding="utf-8"))
    data.pop("serialNumber", None)
    metadata = data.get("metadata") or {}
    metadata.pop("timestamp", None)
    component = metadata.get("component")
    if isinstance(component, dict):
        component["version"] = revision
    data["metadata"] = metadata
    path.write_text(json.dumps(data, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
