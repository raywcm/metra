#!/usr/bin/env python3
"""Exercise install and checksum failure without network access."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

installer = Path('install.sh').resolve()
with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    fixture, tools, prefix = [root / s for s in ('release', 'tools', 'prefix')]
    fixture.mkdir()
    tools.mkdir()
    import platform
    osname = {'Darwin': 'darwin', 'Linux': 'linux'}[platform.system()]
    arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64'}[platform.machine()]
    name = f'metra_0.1.0_{osname}_{arch}.tar.gz'
    payload = b'#!/bin/sh\necho "metra installer-test"\n'
    with tarfile.open(fixture / name, 'w:gz') as tar:
        info = tarfile.TarInfo('metra')
        info.size = len(payload)
        info.mode = 0o755
        tar.addfile(info, io.BytesIO(payload))
    digest = hashlib.sha256((fixture / name).read_bytes()).hexdigest()
    checks = fixture / 'checksums.txt'
    checks.write_text(f'{digest}  {name}\n')
    curl = tools / 'curl'
    curl.write_text('''#!/usr/bin/env python3
import os, pathlib, shutil, sys
args=sys.argv[1:]
url=next(a for a in args if a.startswith('https://'))
shutil.copyfile(pathlib.Path(os.environ['METRA_FIXTURE'])/url.rsplit('/',1)[-1],args[args.index('-o')+1])
''')
    curl.chmod(0o755)
    env = dict(os.environ, PATH=str(tools)+os.pathsep+os.environ['PATH'], METRA_FIXTURE=str(fixture), METRA_PREFIX=str(prefix))
    subprocess.run(['sh', str(installer)], env=env, check=True, capture_output=True)
    assert subprocess.check_output([str(prefix/'bin/metra')]).strip() == b'metra installer-test'
    checks.write_text(f'{"0"*64}  {name}\n')
    result = subprocess.run(['sh', str(installer)], env=env, capture_output=True)
    assert result.returncode != 0 and b'verification failed' in result.stderr
    assert (prefix/'bin/metra').read_bytes() == payload, 'failed upgrade replaced installed binary'
    assert not list((prefix/'bin').glob('.metra-install.*'))
print('Installer passed: verified install, checksum rejection, existing binary preserved')
