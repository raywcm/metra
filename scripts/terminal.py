"""Render captured terminal cells; optional development dependencies: pyte, Pillow."""
from pathlib import Path
import pyte
from PIL import Image, ImageDraw, ImageFont


class Screen(pyte.Screen):
    def back_tab(self, count=1, **kwargs):
        for _ in range(max(1, count)):
            self.cursor.x = max((x for x in self.tabstops if x < self.cursor.x), default=0)

    def scroll_up(self, count=1, **kwargs):
        top, bottom = self.margins or (0, self.lines - 1)
        saved = self.cursor.y
        self.cursor.y = bottom
        for _ in range(min(count or 1, bottom - top + 1)):
            self.index()
        self.cursor.y = saved

    def scroll_down(self, count=1, **kwargs):
        top, bottom = self.margins or (0, self.lines - 1)
        saved = self.cursor.y
        self.cursor.y = top
        for _ in range(min(count or 1, bottom - top + 1)):
            self.reverse_index()
        self.cursor.y = saved


class Stream(pyte.ByteStream):
    csi = dict(pyte.ByteStream.csi, Z='back_tab', S='scroll_up', T='scroll_down')
    events = pyte.ByteStream.events | {'back_tab', 'scroll_up', 'scroll_down'}


class Renderer:
    colors = {'black': '#111827', 'red': '#F87171', 'green': '#34D399',
              'brown': '#FBBF24', 'blue': '#7DD3FC', 'magenta': '#A78BFA',
              'cyan': '#67E8F9', 'white': '#E2E8F0'}

    def __init__(self, font='/System/Library/Fonts/Menlo.ttc', size=18,
                 background='dark', fallback='/System/Library/Fonts/STHeiti Light.ttc'):
        self.font = ImageFont.truetype(font, size)
        self.fallback = ImageFont.truetype(fallback, size) if Path(fallback).is_file() else self.font
        self.cell_w, self.cell_h = round(self.font.getlength('M')), round(size * 1.5)
        self.margin = 16
        self.bg, self.fg = ('#FFFFFF', '#182234') if background == 'light' else ('#101724', '#E2E8F0')
        self.cache = {}

    def color(self, value, default):
        if value == 'default':
            return default
        if value in self.colors:
            return self.colors[value]
        return '#' + value if len(value) == 6 else default

    def cell(self, char):
        key = (char.data, char.fg, char.bg, char.reverse)
        if key in self.cache:
            return self.cache[key]
        fg, bg = self.color(char.fg, self.fg), self.color(char.bg, self.bg)
        if char.reverse:
            fg, bg = bg, fg
        w, h = self.cell_w, self.cell_h
        wide = any(ord(ch) >= 0x2e80 and ch not in '█▆▄▂▀▌▐' for ch in char.data)
        image = Image.new('RGB', (w * (2 if wide else 1), h), bg)
        draw = ImageDraw.Draw(image)
        connections = {'─': 'lr', '│': 'ud', '┌': 'rd', '┐': 'ld',
                       '└': 'ru', '┘': 'lu', '├': 'urd', '┤': 'uld',
                       '┬': 'lrd', '┴': 'lru', '┼': 'lrud'}
        if char.data in connections:
            cx, cy = w // 2, h // 2
            for direction in connections[char.data]:
                end = {'l': (0, cy), 'r': (w - 1, cy),
                       'u': (cx, 0), 'd': (cx, h - 1)}[direction]
                draw.line((cx, cy, *end), fill=fg)
        elif char.data in ('█', '▆', '▄', '▂'):
            fraction = {'█': 1, '▆': .75, '▄': .5, '▂': .25}[char.data]
            draw.rectangle((0, round(h * (1 - fraction)), w - 1, h - 1), fill=fg)
        elif char.data == '▀':
            draw.rectangle((0, 0, w - 1, h // 2 - 1), fill=fg)
        elif char.data in ('▌', '▐'):
            begin = 0 if char.data == '▌' else w // 2
            draw.rectangle((begin, 0, begin + w // 2 - 1, h - 1), fill=fg)
        elif char.data.strip():
            draw.text((0, 0), char.data, font=self.fallback if wide else self.font, fill=fg)
        if len(self.cache) > 24000:
            self.cache.clear()
        self.cache[key] = image
        return image

    def render(self, screen, redact=()):
        w, h, margin = self.cell_w, self.cell_h, self.margin
        image = Image.new('RGB', (w * screen.columns + 2 * margin, h * screen.lines + 2 * margin), self.bg)
        for y in range(screen.lines):
            for x in range(screen.columns):
                char = screen.buffer[y][x]
                if char.data:
                    image.paste(self.cell(char), (margin + x * w, margin + y * h))
            # Hide only host/user labels; sampled values and graphs are unchanged.
            line = ''.join(screen.buffer[y][x].data or ' ' for x in range(screen.columns))
            for text in redact:
                start = line.find(text)
                if start >= 0:
                    draw = ImageDraw.Draw(image)
                    px = margin + start * w
                    draw.rectangle((px, margin + y * h, px + len(text) * w - 1, margin + (y + 1) * h - 1), fill=self.bg)
                    replacement = 'local'[:len(text)].ljust(len(text))
                    draw.text((px, margin + y * h), replacement, font=self.font, fill=self.fg)
        return image
