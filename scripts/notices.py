#!/usr/bin/env python3
"""Collect pinned dependency licenses; --check verifies the committed manifest."""
import argparse
import json
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
raw = subprocess.check_output([os.environ.get('GO', 'go'), 'list', '-m', '-json', 'all'], text=True)
decoder = json.JSONDecoder()
modules = []
while raw.strip():
    obj, idx = decoder.raw_decode(raw.lstrip())
    raw = raw.lstrip()[idx:]
    if not obj.get('Main'):
        modules.append(obj)
lines = ['# Third-party notices', '', 'metra uses the following pinned Go modules. Their license texts are included below `licenses/`.', '']
for m in modules:
    path, version = m['Path'], m['Version']
    if 'Dir' not in m:
        downloaded=json.loads(subprocess.check_output([os.environ.get('GO','go'),'mod','download','-json',path+'@'+version],text=True))
        m['Dir']=downloaded['Dir']
    directory = Path(m['Dir'])
    files = [p for p in directory.iterdir() if p.is_file() and p.name.lower().startswith(('license', 'copying', 'notice'))]
    assert files, f'No license found for {path}'
    dest = Path('licenses') / f'{path.replace("/", "_")}@{version}'
    for p in sorted(files):
        target = dest / p.name
        if args.check:
            assert target.read_bytes() == p.read_bytes(), f'Stale license: {target}'
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(p.read_bytes())
    lines.append(f'- `{path} {version}` — [{files[0].name}]({dest.as_posix()}/{files[0].name})')
text = '\n'.join(lines) + '\n'
manifest = Path('THIRD_PARTY_NOTICES.md')
if args.check:
    assert manifest.read_text() == text, 'Third-party notices need regeneration'
else:
    manifest.write_text(text)
print(f'{len(modules)} dependency licenses verified' if args.check else f'{len(modules)} dependency licenses collected')
