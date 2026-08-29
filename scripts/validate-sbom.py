#!/usr/bin/env python3
"""Validate the generated CycloneDX SBOM has required Go module content."""
from __future__ import annotations
import hashlib
import json
import pathlib
import sys


def fail(msg: str) -> int:
    print(f"sbom validation failed: {msg}", file=sys.stderr)
    return 1


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("usage: scripts/validate-sbom.py artifacts/sbom.cdx.json artifacts/sbom.cdx.json.sha256", file=sys.stderr)
        return 2
    sbom_path = pathlib.Path(argv[1])
    sha_path = pathlib.Path(argv[2])
    if not sbom_path.is_file():
        return fail(f"missing SBOM {sbom_path}")
    if not sha_path.is_file():
        return fail(f"missing checksum {sha_path}")
    raw = sbom_path.read_bytes()
    if b"/workspace" in raw or b"/home/" in raw:
        return fail("SBOM contains local absolute paths")
    got_sha = hashlib.sha256(raw).hexdigest()
    recorded = sha_path.read_text(encoding="utf-8").strip().split()[0]
    if got_sha != recorded:
        return fail(f"checksum mismatch: got {got_sha}, recorded {recorded}")
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        return fail(f"invalid JSON: {exc}")
    if data.get("bomFormat") != "CycloneDX":
        return fail("bomFormat is not CycloneDX")
    if not str(data.get("specVersion", "")).startswith("1."):
        return fail("missing CycloneDX specVersion")
    metadata = data.get("metadata") or {}
    component = metadata.get("component") or {}
    if component.get("name") != "github.com/rcarmo/go-ai":
        return fail(f"unexpected root component name {component.get('name')!r}")
    if component.get("type") not in {"application", "library"}:
        return fail(f"unexpected root component type {component.get('type')!r}")
    components = data.get("components")
    if not isinstance(components, list) or not components:
        return fail("empty components list")
    names = {c.get("name") for c in components if isinstance(c, dict)}
    required = {
        "github.com/aws/aws-sdk-go-v2/config",
        "github.com/aws/aws-sdk-go-v2/service/bedrockruntime",
        "github.com/coder/websocket",
    }
    missing = sorted(required - names)
    if missing:
        return fail("missing expected resolved dependencies: " + ", ".join(missing))
    deps = data.get("dependencies")
    if not isinstance(deps, list) or not deps:
        return fail("empty dependency graph")
    print(f"SBOM valid: {len(components)} components, sha256 {got_sha}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
