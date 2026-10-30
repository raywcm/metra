#!/usr/bin/env python3
"""Generate a Homebrew formula from the exact GoReleaser archive checksums."""
import hashlib
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1] if len(sys.argv) > 1 else 'dist')
metadata = json.loads((root / 'metadata.json').read_text())
version = metadata['version']
repo = 'raywcm/metra'
checks = dict(line.split()[::-1] for line in (root / 'checksums.txt').read_text().splitlines())
assert re.fullmatch(r'[0-9A-Za-z.+_-]+', version), 'invalid version'
lines = ['class Metra < Formula', '  desc "Lightweight terminal system monitor"',
         f'  homepage "https://github.com/{repo}"', f'  version "{version}"', '  license "MIT"', '']
for osname, condition in [('darwin', 'macos'), ('linux', 'linux')]:
    lines.append(f'  on_{condition} do')
    for arch, condition in [('arm64', 'arm'), ('amd64', 'intel')]:
        name = f'metra_{version}_{osname}_{arch}.tar.gz'
        digest = hashlib.sha256((root / name).read_bytes()).hexdigest()
        assert checks[name] == digest, f'checksum mismatch: {name}'
        lines += [f'    on_{condition} do', f'      url "https://github.com/{repo}/releases/download/v{version}/{name}"', f'      sha256 "{digest}"', '    end']
    lines += ['  end', '']
lines += ['  def install', '    bin.install "metra"', '  end', '', '  test do',
          '    assert_match "metra", shell_output("#{bin}/metra --version")', '  end', 'end', '']
out = root / 'homebrew' / 'metra.rb'
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text('\n'.join(lines))
print(out)
