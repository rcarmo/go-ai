#!/usr/bin/env python3
"""Validate the generated CycloneDX SBOM has required Go module content."""
from __future__ import annotations
import argparse
import hashlib
import json
import pathlib
import sys
from typing import Any

ROOT_MODULE = "github.com/rcarmo/go-ai"
REQUIRED_DEPENDENCIES = {
    "github.com/aws/aws-sdk-go-v2/config",
    "github.com/aws/aws-sdk-go-v2/service/bedrockruntime",
    "github.com/coder/websocket",
}


def fail(msg: str) -> int:
    print(f"sbom validation failed: {msg}", file=sys.stderr)
    return 1


def component_name(component: dict[str, Any]) -> str | None:
    name = component.get("name")
    return name if isinstance(name, str) else None


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sbom", type=pathlib.Path)
    parser.add_argument("checksum", type=pathlib.Path)
    parser.add_argument("--expected-revision", required=True, help="expected root component revision/version, usually git rev-parse --short=12 HEAD")
    args = parser.parse_args(argv[1:])

    sbom_path = args.sbom
    sha_path = args.checksum
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
    if not isinstance(component, dict):
        return fail("missing metadata component")
    if component_name(component) != ROOT_MODULE:
        return fail(f"unexpected root component name {component.get('name')!r}")
    if component.get("version") != args.expected_revision:
        return fail(f"unexpected root component revision {component.get('version')!r}, want {args.expected_revision!r}")
    if component.get("type") not in {"application", "library"}:
        return fail(f"unexpected root component type {component.get('type')!r}")
    root_ref = component.get("bom-ref")
    if not isinstance(root_ref, str) or ROOT_MODULE not in root_ref:
        return fail(f"unexpected root component bom-ref {root_ref!r}")
    components = data.get("components")
    if not isinstance(components, list) or not components:
        return fail("empty components list")
    names = {component_name(c) for c in components if isinstance(c, dict)}
    missing = sorted(REQUIRED_DEPENDENCIES - names)
    if missing:
        return fail("missing expected resolved dependencies: " + ", ".join(missing))
    deps = data.get("dependencies")
    if not isinstance(deps, list) or not deps:
        return fail("empty dependency graph")
    root_dep = next((dep for dep in deps if isinstance(dep, dict) and dep.get("ref") == root_ref), None)
    if root_dep is None:
        return fail(f"missing root dependency graph ref {root_ref!r}")
    depends_on = root_dep.get("dependsOn")
    if not isinstance(depends_on, list) or not depends_on:
        return fail("root dependency graph has empty dependsOn")
    for required in sorted(REQUIRED_DEPENDENCIES):
        if not any(isinstance(ref, str) and required in ref for ref in depends_on):
            return fail(f"root dependency graph missing dependency ref for {required}")
    print(f"SBOM valid: {len(components)} components, sha256 {got_sha}, revision {args.expected_revision}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
