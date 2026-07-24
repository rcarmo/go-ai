#!/usr/bin/env python3
"""Compare generated Go model provider/id pairs against exact upstream pi-ai provider maps.

Usage:
  scripts/compare-upstream-models.py /path/to/pi/packages/ai/src/providers

The script intentionally reads upstream `*.models.ts` files directly rather than
another port's generated catalog. It compares unique `(provider, id)` pairs only;
metadata parity remains covered by generator/tests.
"""
from __future__ import annotations

import json
import os
import pathlib
import re
import sys
from typing import Iterable


def upstream_pairs(providers_dir: pathlib.Path) -> set[tuple[str, str]]:
    if not providers_dir.is_dir():
        raise SystemExit(f"providers dir not found: {providers_dir}")
    pairs: set[tuple[str, str]] = set()
    inline_pattern = re.compile(r'id:\s*"([^"]+)".*?provider:\s*"([^"]+)"', re.S)
    json_import_pattern = re.compile(r'import\s+values\s+from\s+"([^"]+\.json)"')
    for path in sorted(providers_dir.glob("*.models.ts")):
        text = path.read_text(encoding="utf-8")
        json_match = json_import_pattern.search(text)
        if json_match:
            data_path = path.parent / json_match.group(1).removeprefix("./")
            if not data_path.exists() and (data_dir := os.environ.get("PI_AI_MODEL_DATA_DIR")):
                data_path = pathlib.Path(data_dir) / pathlib.Path(json_match.group(1)).name
            data = json.loads(data_path.read_text(encoding="utf-8"))
            values = []
            for value in data.values():
                if isinstance(value, dict) and "id" in value and "provider" in value:
                    values.append(value)
                elif isinstance(value, dict):
                    values.extend(value.values())
            for value in values:
                pairs.add((value["provider"], value["id"]))
            continue
        for model_id, provider in inline_pattern.findall(text):
            pairs.add((provider, model_id))
    return pairs


def generated_pairs(generated_go: pathlib.Path) -> set[tuple[str, str]]:
    text = generated_go.read_text(encoding="utf-8")
    pattern = re.compile(r'ID:\s*"([^"]+)".*?Provider:\s*"([^"]+)"', re.S)
    return {(provider, model_id) for model_id, provider in pattern.findall(text)}


def format_pairs(pairs: Iterable[tuple[str, str]]) -> str:
    return "\n".join(f"  {provider}/{model_id}" for provider, model_id in sorted(pairs))


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    repo = pathlib.Path(__file__).resolve().parents[1]
    upstream = upstream_pairs(pathlib.Path(argv[1]))
    generated = generated_pairs(repo / "models_generated.go")
    missing = upstream - generated
    extra = generated - upstream
    print(f"upstream pairs: {len(upstream)}")
    print(f"generated pairs: {len(generated)}")
    if missing or extra:
        if missing:
            print("missing from generated:")
            print(format_pairs(missing))
        if extra:
            print("extra in generated:")
            print(format_pairs(extra))
        return 1
    print("model provider/id pairs match exactly")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
