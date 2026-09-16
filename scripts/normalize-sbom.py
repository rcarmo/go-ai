#!/usr/bin/env python3
"""Normalize CycloneDX SBOM JSON for stable artifact hashing."""
from __future__ import annotations
import json
import pathlib
import sys

ROOT_MODULE = "github.com/rcarmo/go-ai"


def replace_purl_version(value: str, revision: str) -> str:
    if ROOT_MODULE not in value:
        return value
    base, sep, qualifiers = value.partition("?")
    if "@" in base:
        prefix = base.rsplit("@", 1)[0]
    else:
        prefix = base
    out = f"{prefix}@{revision}"
    if sep:
        out += sep + qualifiers
    return out


def rewrite_dependency_refs(dependencies: object, old_root_ref: str | None, new_root_ref: str) -> None:
    if not isinstance(dependencies, list):
        return
    for dep in dependencies:
        if not isinstance(dep, dict):
            continue
        if dep.get("ref") == old_root_ref or (isinstance(dep.get("ref"), str) and ROOT_MODULE in dep["ref"]):
            dep["ref"] = new_root_ref
        depends_on = dep.get("dependsOn")
        if isinstance(depends_on, list):
            for index, ref in enumerate(depends_on):
                if ref == old_root_ref or (isinstance(ref, str) and ROOT_MODULE in ref):
                    depends_on[index] = new_root_ref


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
        old_root_ref = component.get("bom-ref") if isinstance(component.get("bom-ref"), str) else None
        component["version"] = revision
        if isinstance(component.get("bom-ref"), str):
            component["bom-ref"] = replace_purl_version(component["bom-ref"], revision)
        if isinstance(component.get("purl"), str):
            component["purl"] = replace_purl_version(component["purl"], revision)
        new_root_ref = component.get("bom-ref")
        if isinstance(new_root_ref, str):
            rewrite_dependency_refs(data.get("dependencies"), old_root_ref, new_root_ref)
    data["metadata"] = metadata
    path.write_text(json.dumps(data, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
