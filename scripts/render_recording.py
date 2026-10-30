#!/usr/bin/env python3
"""Render a recording from record.py; live measurements are left unchanged."""
import argparse
import json
from pathlib import Path
from PIL import Image
from terminal import Renderer, Screen, Stream

parser = argparse.ArgumentParser()
parser.add_argument('recording', type=Path)
parser.add_argument('output', type=Path)
parser.add_argument('--font', default='/System/Library/Fonts/Menlo.ttc')
args = parser.parse_args()
records = iter(args.recording.read_text().splitlines())
metadata = json.loads(next(records))
width, height = metadata['width'], metadata['height']
screen = Screen(width, height)
stream = Stream(screen)
renderer = Renderer(args.font, size=14)
redactions = tuple({metadata.get('host', ''), metadata.get('host', '').split('.')[0], metadata.get('user', '')} - {''})
frames, captures = [], {}
for line in records:
    _, kind, data = json.loads(line)
    if kind == 'o':
        stream.feed(data.encode())
    elif kind == 'r':
        screen.resize(lines=data[1], columns=data[0])
    elif kind in ('f', 'p'):
        image = renderer.render(screen, redact=redactions)
        if kind == 'p':
            captures[data] = image
        else:
            canvas = Image.new('RGB', (renderer.cell_w * width + 32, renderer.cell_h * height + 32), renderer.bg)
            canvas.paste(image, (0, 0))
            frames.append(canvas)
assert frames, 'recording has no frames'
samples = frames[::max(1, len(frames) // 20)]
contact = Image.new('RGB', (240 * len(samples), 160))
for i, frame in enumerate(samples):
    contact.paste(frame.resize((240, 160)), (240 * i, 0))
palette = contact.quantize(colors=256)
indexed = [frame.quantize(palette=palette, dither=Image.Dither.NONE) for frame in frames]
args.output.parent.mkdir(parents=True, exist_ok=True)
indexed[0].save(args.output, save_all=True, append_images=indexed[1:], duration=500, loop=0, optimize=True, disposal=1)
for name, image in captures.items():
    image.save(args.output.parent / (name + '.png'))
print(f'{args.output}: {len(frames)} frames, {args.output.stat().st_size / 1024:.0f} KiB')
