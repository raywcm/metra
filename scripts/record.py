#!/usr/bin/env python3
"""Record a live terminal on a remote host using only the Python standard library."""
import argparse
import codecs
import errno
import fcntl
import getpass
import json
import os
from pathlib import Path
import pty
import select
import signal
import socket
import struct
import tempfile
import termios
import time

parser = argparse.ArgumentParser()
parser.add_argument('binary')
parser.add_argument('output', type=Path)
parser.add_argument('--warmup', type=float, default=62)
args = parser.parse_args()
binary = str(Path(args.binary).resolve())
args.output.parent.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='metra-record-') as temporary, args.output.open('w') as output:
    output.write(json.dumps({'version': 2, 'width': 120, 'height': 36,
                             'host': socket.gethostname(), 'user': getpass.getuser()}) + '\n')
    start = time.monotonic()
    decoder = codecs.getincrementaldecoder('utf-8')('replace')
    pid, fd = pty.fork()
    if pid == 0:
        output.close()
        os.environ.update(TERM='xterm-256color', COLORTERM='truecolor', LANG='en_US.UTF-8', LC_ALL='en_US.UTF-8')
        os.environ.pop('NO_COLOR', None)
        os.execv(binary, [binary, '--config', str(Path(temporary) / 'settings.json'), '--language', 'en', '--theme', 'dark'])

    def event(kind, data=''):
        output.write(json.dumps([round(time.monotonic() - start, 4), kind, data]) + '\n')

    def resize(width, height):
        event('r', [width, height])
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', height, width, 0, 0))
        os.kill(pid, signal.SIGWINCH)

    def drain(seconds, record=False):
        end, next_frame = time.monotonic() + seconds, time.monotonic()
        collected = bytearray()
        while time.monotonic() < end:
            if select.select([fd], [], [], .05)[0]:
                try:
                    chunk = os.read(fd, 65536)
                except OSError as error:
                    if error.errno == errno.EIO:
                        break
                    raise
                if not chunk:
                    break
                collected.extend(chunk)
                event('o', decoder.decode(chunk))
                if b'\x1b]11;?' in chunk:
                    os.write(fd, b'\x1b]11;rgb:1010/1717/2424\x1b\\')
            if record and time.monotonic() >= next_frame:
                event('f')
                next_frame += .5
        return bytes(collected)

    def send(keys, seconds=1, record=False):
        os.write(fd, keys)
        return drain(seconds, record)

    try:
        resize(120, 36)
        drain(args.warmup)
        drain(6, True)
        event('p', 'server-overview')
        send(b'\x1b[C', 4, True)
        send(b'/metra\r', 2, True)
        send(b'\x1b', .5)
        send(b'\x1b[C', 4, True)
        event('p', 'server-cpu')
        send(b'\x1b[C', 4, True)
        send(b'\x1b[C', 3, True)
        for _ in range(20):
            data = send(b'\x1b[C', .7)
            if b'Overall' in data and b'NVIDIA' in data:
                drain(7, True)
                event('p', 'server-gpu')
                resize(80, 24)
                drain(3, True)
                resize(120, 36)
                drain(2, True)
                break
        else:
            raise RuntimeError('NVIDIA page not found; refusing to publish an incomplete server recording')
        send(b',', 3, True)
        event('p', 'server-settings')
        send(b'\x1b', .2)
        send(b'q', .5)
        _, status = os.waitpid(pid, 0)
        assert os.waitstatus_to_exitcode(status) == 0, 'Metra exited with an error'
    finally:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        os.close(fd)
print(args.output)
