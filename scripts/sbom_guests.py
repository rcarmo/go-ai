#!/usr/bin/env python3
"""Add intentional embedded guest inputs to an existing CycloneDX SBOM."""
import hashlib
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
ASSETS = (
    ("quickjs-wasi", "3.6.2", "pkg:npm/quickjs-wasi@3.6.2", "quickjs.wasm", "LICENSE.quickjs-wasi"),
    ("@earendil-works/pi-codemode", "1.3.0", "pkg:npm/%40earendil-works/pi-codemode@1.3.0", "prelude.js", "LICENSE.pi-codemode"),
)


def guest_components():
    result = []
    for name, version, purl, asset, licence in ASSETS:
        base = ROOT / "internal/quickjs/guest"
        if "Permission is hereby granted" not in (base / licence).read_text():
            raise ValueError("missing embedded guest MIT notice")
        result.append({"type": "library", "name": name, "version": version,
                       "bom-ref": purl, "purl": purl,
                       "licenses": [{"license": {"id": "MIT"}}],
                       "hashes": [{"alg": "SHA-256", "content": hashlib.sha256((base / asset).read_bytes()).hexdigest()}],
                       "properties": [{"name": "go-ai.embedded.asset", "value": "internal/quickjs/guest/" + asset},
                                      {"name": "go-ai.embedded.hash.scope", "value": "embedded file, not complete npm package"}]})
    return result


def augment(data):
    guests = guest_components()
    refs = {guest["bom-ref"] for guest in guests}
    data["components"] = [c for c in data.get("components", []) if c.get("bom-ref") not in refs] + guests
    dependencies = data.setdefault("dependencies", [])
    root = data["metadata"]["component"]["bom-ref"]
    edge = next((d for d in dependencies if d.get("ref") == root), None)
    if edge is None:
        raise ValueError("missing root dependency edge")
    edge["dependsOn"] = sorted(set(edge.get("dependsOn", [])) | refs)
    for ref in sorted(refs):
        if not any(d.get("ref") == ref for d in dependencies):
            dependencies.append({"ref": ref, "dependsOn": []})
    return data


if __name__ == "__main__":
    path = pathlib.Path(sys.argv[1])
    data = augment(json.loads(path.read_text()))
    path.write_text(json.dumps(data, sort_keys=True, separators=(",", ":")) + "\n")
