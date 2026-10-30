#!/usr/bin/env python3
"""Verify expected release artifacts and archive contents/checksums."""
import hashlib
import json
from pathlib import Path
import sys
import tarfile
root = Path(sys.argv[1] if len(sys.argv) > 1 else 'dist')
version = json.loads((root / 'metadata.json').read_text())['version']
checks = dict(line.split()[::-1] for line in (root / 'checksums.txt').read_text().splitlines())
for name, expected in checks.items():
    file = root / name
    assert file.is_file() and hashlib.sha256(file.read_bytes()).hexdigest() == expected, name
archives = list(root.glob('metra_*.tar.gz'))
assert len(archives) == 4, 'need Linux/macOS amd64/arm64 archives'
for archive in archives:
    with tarfile.open(archive) as tar:
        names = tar.getnames()
        for member in tar.getmembers():
            assert not member.name.startswith('/') and '..' not in Path(member.name).parts
        assert all(n in names for n in ['metra', 'README.md', 'LICENSE', 'THIRD_PARTY_NOTICES.md', 'internal/buildinfo/VERSION']), archive
        assert tar.extractfile('internal/buildinfo/VERSION').read().decode().strip() == version, archive
for suffix in ['.deb', '.rpm', '.pkg.tar.zst']:
    assert len(list(root.glob('*' + suffix))) == 2, f'need amd64/arm64 {suffix} packages'
assert (root / 'homebrew/metra.rb').is_file()
print('Verified 4 platform archives, 6 Linux packages, checksums and Homebrew formula')
