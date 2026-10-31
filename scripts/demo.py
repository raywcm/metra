#!/usr/bin/env python3
"""Record a real Metra session as a GIF. Requires pyte and Pillow."""
import argparse
import errno
import fcntl
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
from PIL import Image
from terminal import Renderer, Screen, Stream

parser = argparse.ArgumentParser()
parser.add_argument('binary', nargs='?', default='bin/metra')
parser.add_argument('--output', type=Path, default=Path('docs/assets/demo-macos.gif'))
parser.add_argument('--font', default='/System/Library/Fonts/Menlo.ttc')
args = parser.parse_args()
binary = str(Path(args.binary).resolve())
columns, rows = 120, 36
screen = Screen(columns, rows)
stream = Stream(screen)
renderer = Renderer(args.font, size=14)
frames = []
captures = {}
redactions = tuple({socket.gethostname(), socket.gethostname().split('.')[0], os.environ.get('USER', '')} - {''})

with tempfile.TemporaryDirectory(prefix='metra-demo-') as temporary:
    pid, fd = pty.fork()
    if pid == 0:
        os.environ.update(TERM='xterm-256color', COLORTERM='truecolor', LANG='en_US.UTF-8', LC_ALL='en_US.UTF-8')
        os.environ.pop('NO_COLOR', None)
        os.execv(binary, [binary, '--config', str(Path(temporary) / 'settings.json'), '--language', 'en', '--theme', 'dark'])

    def resize(width, height):
        screen.resize(lines=height, columns=width)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', height, width, 0, 0))
        os.kill(pid, signal.SIGWINCH)

    def drain(seconds, record=False):
        end = time.monotonic() + seconds
        next_frame = time.monotonic()
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
                if b'\x1b]11;?' in chunk:
                    os.write(fd, b'\x1b]11;rgb:1010/1717/2424\x1b\\')
                stream.feed(chunk)
            if record and time.monotonic() >= next_frame:
                image = renderer.render(screen, redact=redactions)
                canvas = Image.new('RGB', (renderer.cell_w * columns + 32, renderer.cell_h * rows + 32), renderer.bg)
                canvas.paste(image, (0, 0))
                frames.append(canvas)
                next_frame += .5

    def send(keys, seconds=1.0, record=False):
        os.write(fd, keys)
        drain(seconds, record)

    def capture(name):
        captures[name] = renderer.render(screen, redact=redactions)

    def go_to(title):
        for _ in range(24):
            send(b'\x1b[C', .5)
            if any(line.strip().startswith(title) for line in screen.display[3:6]):
                return True
        return False

    try:
        resize(columns, rows)
        drain(62)
        drain(5, True)
        capture('performance')
        send(b'\x1b[C', 3, True)
        send(b'\x1b[B\x1b[B', 1, True)
        send(b'/metra\r', 2, True)
        send(b'\x1b', 1)
        send(b'\x1b[H', 1)
        capture('processes')
        send(b'\x1b[C', 3, True)
        send(b'c', 3, True)
        send(b'c', .5)
        capture('cpu')
        send(b'\x1b[C', 4, True)
        capture('memory')
        send(b'\x1b[C', 3, True)
        capture('disk')
        if go_to('GPU0'):
            drain(5, True)
            capture('gpu')
        send(b',', 3, True)
        send(b'\x1b', .5)
        capture('settings')
        # Show a narrow terminal inside the same canvas, then restore it.
        if go_to('CPU'):
            resize(60, 20)
            drain(3, True)
            resize(columns, rows)
            drain(2, True)
        send(b'q', .4)
        _, status = os.waitpid(pid, 0)
        assert os.waitstatus_to_exitcode(status) == 0, 'Metra exited with an error'
    finally:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        os.close(fd)

args.output.parent.mkdir(parents=True, exist_ok=True)
# One shared palette keeps text and chart colors stable between frames.
samples = frames[::max(1, len(frames) // 20)]
contact = Image.new('RGB', (240 * len(samples), 160))
for i, frame in enumerate(samples):
    contact.paste(frame.resize((240, 160)), (240 * i, 0))
palette = contact.quantize(colors=256)
indexed = [frame.quantize(palette=palette, dither=Image.Dither.NONE) for frame in frames]
indexed[0].save(args.output, save_all=True, append_images=indexed[1:], duration=500, loop=0, optimize=True, disposal=1)
for name, image in captures.items():
    image.save(args.output.parent / (name + '.png'))
print(f'{args.output}: {len(frames)} frames, {args.output.stat().st_size / 1024:.0f} KiB')
