#!/usr/bin/env python3
"""Render a real captured PTY frame. Optional dev dependencies: pyte, Pillow."""
import argparse
from pathlib import Path
from terminal import Renderer, Screen, Stream

parser = argparse.ArgumentParser()
parser.add_argument('capture', type=Path)
parser.add_argument('output', type=Path)
parser.add_argument('--columns', type=int, default=120)
parser.add_argument('--rows', type=int, default=36)
parser.add_argument('--fallback-font', default='/System/Library/Fonts/STHeiti Light.ttc')
parser.add_argument('--background', choices=['dark', 'light'], default='dark')
parser.add_argument('--font', default='/System/Library/Fonts/Menlo.ttc')
args = parser.parse_args()
screen = Screen(args.columns, args.rows)
Stream(screen).feed(args.capture.read_bytes())
renderer = Renderer(args.font, background=args.background, fallback=args.fallback_font)
args.output.parent.mkdir(parents=True, exist_ok=True)
renderer.render(screen).save(args.output)
print(args.output)
