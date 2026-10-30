#!/usr/bin/env python3
"""Fail when a class token used by the web UI is missing from the compiled CSS.

Tailwind silently drops classes it does not know (typos, removed component
classes, unsupported variants). This lists every class token below that has no
matching selector in output.css.

Checked sources:
  1. Literal class="..." attributes in internal/web/templates/**/*.templ and
     internal/web/handlers/*.go (not tests).
  2. Dynamic templ class expressions, class={ ... }: every "..." string literal
     inside the braces (the static prefix, templ.KV("...", cond) keys).
  3. Go class helpers in internal/web/templates/** (.templ and non-test .go):
     every `return "..."` inside a func whose name ends in Class, such as
     statusBadgeClass, alertClass, logLevelBadgeClass, backupLineClass.
  4. Literal string arguments to the Skeleton components
     (components.Skeleton("h-5 w-28"), statCardSkeleton("h-9")).
  5. components/ui merge inputs in .templ and non-test .go files under
     internal/web/templates: string literals passed to Class(...) or
     ui.Class(...), and "class": "..." entries in templ.Attributes literals.
  6. static/js/app.js and static/js/ui.js: the string values of the
     LEVEL_CLASSES object, string literals assigned to .className, and
     classList.add/remove/toggle args.

Known gap (intentional): a class built from a runtime value that never appears
as a literal (for example "w-" + width) cannot be checked statically. Keep such
classes as complete literal strings in one of the sources above; Tailwind's
content globs (tailwind.config.js) also need the full literal to compile it.

Usage (from the repo root):
    make css-check
or
    npx --yes tailwindcss@3.4.17 -i static/css/input.css -o static/css/output.css
    python3 scripts/class_check.py static/css/output.css

Exits 1 and prints "token [sources]" lines when anything is undefined.
"""
import collections
import glob
import re
import sys

STRING = re.compile(r'"((?:[^"\\\n]|\\.)*)"')
# Fewer selectors than this means output.css is a placeholder or a failed build.
MIN_SELECTORS = 200


def read(path):
    with open(path, encoding='utf-8') as f:
        return f.read()


def brace_block(text, start):
    """Return text[start:end] where text[start-1] is '{' and end is its match."""
    depth = 1
    i = start
    while i < len(text) and depth:
        c = text[i]
        if c == '"':
            m = STRING.match(text, i)
            if m:
                i = m.end()
                continue
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
        i += 1
    return text[start:i - 1]


def literal_attrs(text):
    for m in re.finditer(r'class="([^"{}]*)"', text):
        yield 'class=""', m.group(1)


def templ_class_exprs(text):
    for m in re.finditer(r'class=\{', text):
        block = brace_block(text, m.end())
        for s in STRING.finditer(block):
            # Skip comparison operands such as level == "success".
            if re.search(r'[=!]=\s*$', block[:s.start()]) or re.match(r'\s*[=!]=', block[s.end():]):
                continue
            yield 'class={}', s.group(1)


def class_helpers(text):
    for m in re.finditer(r'^func (\w+Class)\([^)]*\)[^{\n]*\{', text, re.M):
        body = brace_block(text, m.end())
        for r in re.finditer(r'return\s+"((?:[^"\\\n]|\\.)*)"', body):
            yield m.group(1) + '()', r.group(1)


def skeleton_args(text):
    for m in re.finditer(r'\b(?:components\.)?(?:Skeleton|statCardSkeleton)\(', text):
        args = text[m.end():text.find(')', m.end())]
        for s in STRING.finditer(args):
            yield 'Skeleton()', s.group(1)


def paren_block(text, start):
    """Return text[start:end] where text[start-1] is '(' and end is its match."""
    depth = 1
    i = start
    while i < len(text) and depth:
        c = text[i]
        if c == '"':
            m = STRING.match(text, i)
            if m:
                i = m.end()
                continue
        if c == '(':
            depth += 1
        elif c == ')':
            depth -= 1
        i += 1
    return text[start:i - 1]


def ui_merge_args(text):
    # Class("a b", fooClass(x)) and ui.Class(...): every literal argument.
    for m in re.finditer(r'(?<![\w.])(?:ui\.)?Class\(', text):
        for s in STRING.finditer(paren_block(text, m.end())):
            yield 'ui.Class()', s.group(1)
    # templ.Attributes{"class": "..."} defaults handed to ui.Attrs.
    for m in re.finditer(r'"class"\s*:\s*"((?:[^"\\\n]|\\.)*)"', text):
        yield '"class":', m.group(1)


def js_classes(text):
    m = re.search(r'LEVEL_CLASSES\s*=\s*\{', text)
    if m:
        for s in STRING.finditer(brace_block(text, m.end())):
            yield 'LEVEL_CLASSES', s.group(1)
    for m in re.finditer(r'\.className\s*=\s*((?:\s*"(?:[^"\\\n]|\\.)*"\s*\+?)+)', text):
        for s in STRING.finditer(m.group(1)):
            yield '.className', s.group(1)
    for m in re.finditer(r'classList\.(?:add|remove|toggle)\(([^)]*)\)', text):
        for s in STRING.finditer(m.group(1)):
            yield 'classList', s.group(1)


def main():
    css = read(sys.argv[1] if len(sys.argv) > 1 else 'static/css/output.css')
    sel = {m.group(1).replace('\\', '') for m in re.finditer(r'\.((?:\\.|[A-Za-z0-9_-])+)', css)}
    if len(sel) < MIN_SELECTORS:
        print(f'class_check: output.css has only {len(sel)} selectors; '
              'it is a placeholder or a failed build. Rebuild with Tailwind v3.4.17 (make css).',
              file=sys.stderr)
        sys.exit(1)

    templ_files = glob.glob('internal/web/templates/**/*.templ', recursive=True)
    tmpl_go = [f for f in glob.glob('internal/web/templates/**/*.go', recursive=True)
               if not f.endswith(('_test.go', '_templ.go'))]
    handler_go = [f for f in glob.glob('internal/web/handlers/*.go') if not f.endswith('_test.go')]
    js_files = ['static/js/app.js', 'static/js/ui.js']

    scanners = {}
    for f in templ_files:
        scanners[f] = (literal_attrs, templ_class_exprs, class_helpers, skeleton_args, ui_merge_args)
    for f in tmpl_go:
        scanners[f] = (class_helpers, ui_merge_args)
    for f in handler_go:
        scanners[f] = (literal_attrs,)
    for f in js_files:
        scanners[f] = (js_classes,)

    missing = collections.defaultdict(set)
    counts = collections.Counter()
    for f, fns in scanners.items():
        text = read(f)
        for fn in fns:
            for kind, value in fn(text):
                for t in value.split():
                    counts[kind] += 1
                    if t not in sel:
                        missing[t].add(f'{f} ({kind})')
    for k, v in sorted(missing.items()):
        print(k, sorted(v))
    if missing:
        print(f'class_check: {len(missing)} undefined class token(s)', file=sys.stderr)
        sys.exit(1)
    detail = ', '.join(f'{k} {v}' for k, v in sorted(counts.items()))
    print(f'class_check: 0 undefined class tokens across {len(scanners)} files ({detail})')


if __name__ == '__main__':
    main()
