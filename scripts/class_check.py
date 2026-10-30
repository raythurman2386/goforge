#!/usr/bin/env python3
"""Fail when a static class token in templ/handler files is missing from the compiled CSS.

Tailwind silently drops classes it does not know (typos, removed component
classes, unsupported variants). This lists every literal token in a
class="..." attribute that has no matching selector in output.css.

Usage (from the repo root):
    tailwindcss -i static/css/input.css -o static/css/output.css
    python3 scripts/class_check.py static/css/output.css

Exits 1 and prints "token [files]" lines when anything is undefined.
"""
import collections
import glob
import re
import sys

css = open(sys.argv[1] if len(sys.argv) > 1 else 'static/css/output.css').read()
sel = {m.group(1).replace('\\', '') for m in re.finditer(r'\.((?:\\.|[A-Za-z0-9_-])+)', css)}
missing = collections.defaultdict(set)
files = glob.glob('internal/web/templates/**/*.templ', recursive=True) + \
    [f for f in glob.glob('internal/web/handlers/*.go') if not f.endswith('_test.go')]
for f in files:
    for m in re.finditer(r'class="([^"{}]*)"', open(f).read()):
        for t in m.group(1).split():
            if t not in sel:
                missing[t].add(f)
for k, v in sorted(missing.items()):
    print(k, sorted(v))
if missing:
    print(f'class_check: {len(missing)} undefined class token(s)', file=sys.stderr)
    sys.exit(1)
print(f'class_check: 0 undefined class tokens across {len(files)} files')
