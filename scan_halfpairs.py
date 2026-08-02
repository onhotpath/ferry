#!/usr/bin/env python3
"""Half-pair census over an entire Go source tree.

The in-process census (P1) can only see types somebody thought to name. This
scans declarations instead, so the denominator is every type in the tree.

A "half pair" is a type declaring MarshalText or AppendText without
UnmarshalText, or the reverse. Under a paired chain those types are the ones
whose treatment changes, so the count is the blast radius of making an
incomplete pair a schema-compile error.
"""
import os
import re
import sys
from collections import defaultdict

METHOD = re.compile(
    r'^func \((?:\w+ )?\*?(\w+)\) '
    r'(MarshalText|AppendText|UnmarshalText|MarshalJSON|UnmarshalJSON|'
    r'MarshalBinary|AppendBinary|UnmarshalBinary|GobEncode|GobDecode)\('
)

ARMS = {
    'text':   (('MarshalText', 'AppendText'), ('UnmarshalText',)),
    'json':   (('MarshalJSON',), ('UnmarshalJSON',)),
    'binary': (('MarshalBinary', 'AppendBinary'), ('UnmarshalBinary',)),
    'gob':    (('GobEncode',), ('GobDecode',)),
}


def scan(root, skip_tests=True, exclude=None):
    methods = defaultdict(set)   # (pkgdir, type) -> {method}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in ('testdata', '.git', 'vendor')]
        rel = os.path.relpath(dirpath, root)
        if exclude and (rel.split(os.sep)[0] in exclude or '/internal/' in '/' + rel + '/'):
            continue
        for fn in filenames:
            if not fn.endswith('.go'):
                continue
            if skip_tests and fn.endswith('_test.go'):
                continue
            path = os.path.join(dirpath, fn)
            try:
                with open(path, encoding='utf-8', errors='replace') as f:
                    for line in f:
                        m = METHOD.match(line)
                        if m:
                            methods[(dirpath, m.group(1))].add(m.group(2))
            except OSError:
                pass
    return methods


def report(label, methods):
    print(f'\n=== {label} ===')
    print(f'  types declaring any of the eight methods: {len(methods)}')
    for arm, (encs, decs) in ARMS.items():
        pair = enc_only = dec_only = 0
        enc_only_names, dec_only_names = [], []
        for (pkg, typ), ms in methods.items():
            e = any(x in ms for x in encs)
            d = any(x in ms for x in decs)
            if e and d:
                pair += 1
            elif e:
                enc_only += 1
                enc_only_names.append(f'{os.path.basename(pkg)}.{typ}')
            elif d:
                dec_only += 1
                dec_only_names.append(f'{os.path.basename(pkg)}.{typ}')
        print(f'  arm {arm:<7} complete pairs {pair:4d}   '
              f'encoder only {enc_only:3d}   decoder only {dec_only:3d}')
        if enc_only_names:
            print(f'      encoder only: {", ".join(sorted(enc_only_names)[:14])}'
                  f'{" ..." if len(enc_only_names) > 14 else ""}')
        if dec_only_names:
            print(f'      decoder only: {", ".join(sorted(dec_only_names)[:14])}'
                  f'{" ..." if len(dec_only_names) > 14 else ""}')


if __name__ == '__main__':
    for label, root in zip(sys.argv[1::2], sys.argv[2::2]):
        report(label, scan(root, exclude={'cmd', 'internal'} if 'public' in label else None))
