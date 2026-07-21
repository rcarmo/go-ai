#!/usr/bin/env python3
"""Generate images/models_generated.go from pi-ai image-models.generated.ts."""
from __future__ import annotations
import json, pathlib, re, sys


def js_object_to_json(text: str) -> str:
    text = '\n'.join(line for line in text.splitlines() if not line.lstrip().startswith('//'))
    text = re.sub(r'\}\s+satisfies\s+ImagesModel<[^>]+>', '}', text)
    text = re.sub(r'\s+as const', '', text)
    marker = 'export const IMAGE_MODELS ='
    marker_pos = text.find(marker)
    start = text.find('{', marker_pos if marker_pos >= 0 else 0)
    end = text.rfind('}')
    obj = text[start:end+1]
    obj = re.sub(r'(?m)^(\s+)([A-Za-z_][A-Za-z0-9_]*)\s*:', r'\1"\2":', obj)
    obj = re.sub(r',\s*([}\]])', r'\1', obj)
    return obj


def go_slice(values: list[str]) -> str:
    return '[]string{' + ', '.join(json.dumps(v) for v in values) + '}'


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print('usage: scripts/generate-image-models.py /path/to/image-models.generated.ts images/models_generated.go', file=sys.stderr)
        return 2
    data = json.loads(js_object_to_json(pathlib.Path(argv[1]).read_text()))
    models = []
    for provider_models in data.values():
        models.extend(provider_models.values())
    models.sort(key=lambda m: (m['provider'], m['id']))
    lines = [
        '// Code generated from @earendil-works/pi-ai image-models.generated.ts. DO NOT EDIT.',
        '//',
        f'// Source: image-models.generated.ts ({len(models)} image models, {len(data)} provider)',
        '',
        'package images',
        '',
        'import goai "github.com/rcarmo/go-ai"',
        '',
        'func RegisterBuiltinImageModels() {',
        '\tfor i := range builtinImageModels {',
        '\t\tRegisterImageModel(&builtinImageModels[i])',
        '\t}',
        '}',
        '',
        'var builtinImageModels = []ImagesModel{',
    ]
    for m in models:
        cost = m.get('cost', {})
        lines += [
            '\t{',
            f'\t\tID:       {json.dumps(m["id"])},',
            f'\t\tName:     {json.dumps(m["name"])},',
            f'\t\tApi:      ImagesApi({json.dumps(m["api"])}),',
            f'\t\tProvider: ImagesProvider({json.dumps(m["provider"])}),',
            f'\t\tBaseURL:  {json.dumps(m["baseUrl"])},',
            f'\t\tInput:    {go_slice(m.get("input", []))},',
            f'\t\tOutput:   {go_slice(m.get("output", []))},',
            f'\t\tCost:     goai.ModelCost{{Input: {cost.get("input",0)!r}, Output: {cost.get("output",0)!r}, CacheRead: {cost.get("cacheRead",0)!r}, CacheWrite: {cost.get("cacheWrite",0)!r}}},',
            '\t},',
        ]
    lines += ['}', '']
    pathlib.Path(argv[2]).write_text('\n'.join(lines))
    print(f'wrote {argv[2]} with {len(models)} image models')
    return 0

if __name__ == '__main__':
    raise SystemExit(main(sys.argv))
