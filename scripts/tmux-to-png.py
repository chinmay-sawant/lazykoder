#!/usr/bin/env python3
"""Render a tmux capture-pane -e dump to a PNG.

Used to snapshot the live lazyKoder TUI when a compositor grab is unavailable.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

CSI = re.compile(r"\x1b\[([0-9;]*)m")

DEFAULT_FG = (236, 234, 230)
DEFAULT_BG = (0, 0, 0)

# DejaVu covers box-drawing, diamonds, and triangles that the TUI uses.
FONT_REGULAR = Path("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf")
FONT_BOLD = Path("/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf")


def parse_sgr(seq: str, fg, bg, bold, reverse):
    if seq == "" or seq == "0":
        return DEFAULT_FG, DEFAULT_BG, False, False
    parts = [int(p) if p else 0 for p in seq.split(";")]
    i = 0
    while i < len(parts):
        n = parts[i]
        if n == 0:
            fg, bg, bold, reverse = DEFAULT_FG, DEFAULT_BG, False, False
        elif n == 1:
            bold = True
        elif n == 7:
            reverse = True
        elif n == 22:
            bold = False
        elif n == 27:
            reverse = False
        elif n == 39:
            fg = DEFAULT_FG
        elif n == 49:
            bg = DEFAULT_BG
        elif n == 38 and i + 1 < len(parts):
            kind = parts[i + 1]
            if kind == 2 and i + 4 < len(parts):
                fg = (parts[i + 2], parts[i + 3], parts[i + 4])
                i += 4
            elif kind == 5 and i + 2 < len(parts):
                fg = xterm256(parts[i + 2])
                i += 2
        elif n == 48 and i + 1 < len(parts):
            kind = parts[i + 1]
            if kind == 2 and i + 4 < len(parts):
                bg = (parts[i + 2], parts[i + 3], parts[i + 4])
                i += 4
            elif kind == 5 and i + 2 < len(parts):
                bg = xterm256(parts[i + 2])
                i += 2
        i += 1
    return fg, bg, bold, reverse


def xterm256(n: int):
    if n < 16:
        table = [
            (0, 0, 0), (205, 0, 0), (0, 205, 0), (205, 205, 0),
            (0, 0, 238), (205, 0, 205), (0, 205, 205), (229, 229, 229),
            (127, 127, 127), (255, 0, 0), (0, 255, 0), (255, 255, 0),
            (92, 92, 255), (255, 0, 255), (0, 255, 255), (255, 255, 255),
        ]
        return table[n]
    if n < 232:
        n -= 16
        levels = [0, 95, 135, 175, 215, 255]
        return (levels[n // 36], levels[(n // 6) % 6], levels[n % 6])
    v = 8 + (n - 232) * 10
    return (v, v, v)


def strip_other_escapes(text: str) -> str:
    text = re.sub(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)", "", text)
    text = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", lambda m: m.group(0) if m.group(0).endswith("m") else "", text)
    return text.replace("\r", "")


def render(text: str, out: Path, size=15, pad=20) -> None:
    lines = text.splitlines()
    if lines and lines[-1] == "":
        lines = lines[:-1]
    cols = max((visible_width(line) for line in lines), default=80)
    rows = max(len(lines), 1)

    regular = ImageFont.truetype(str(FONT_REGULAR), size)
    bold = ImageFont.truetype(str(FONT_BOLD), size)
    cell_w = max(int(regular.getlength("M")) + 1, 9)
    cell_h = size + 8

    img = Image.new("RGB", (cols * cell_w + pad * 2, rows * cell_h + pad * 2), DEFAULT_BG)
    draw = ImageDraw.Draw(img)

    for y, raw in enumerate(lines):
        raw = strip_other_escapes(raw)
        fg, bg, is_bold, reverse = DEFAULT_FG, DEFAULT_BG, False, False
        x = 0
        pos = 0
        while pos < len(raw):
            m = CSI.match(raw, pos)
            if m:
                fg, bg, is_bold, reverse = parse_sgr(m.group(1), fg, bg, is_bold, reverse)
                pos = m.end()
                continue
            ch = raw[pos]
            pos += 1
            if ch == "\x1b":
                nxt = raw.find("\x1b", pos)
                pos = len(raw) if nxt < 0 else nxt
                continue
            cell_fg, cell_bg = (bg, fg) if reverse else (fg, bg)
            px = pad + x * cell_w
            py = pad + y * cell_h
            draw.rectangle([px, py, px + cell_w - 1, py + cell_h - 1], fill=cell_bg)
            font = bold if is_bold else regular
            draw.text((px, py + 1), ch, font=font, fill=cell_fg, anchor="lt")
            x += 1
    img.save(out, "PNG")
    print(f"wrote {out} ({img.size[0]}x{img.size[1]})")


def visible_width(line: str) -> int:
    return len(CSI.sub("", strip_other_escapes(line)))


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("ansi")
    p.add_argument("png")
    args = p.parse_args()
    text = Path(args.ansi).read_text(encoding="utf-8", errors="replace")
    render(text, Path(args.png))
    return 0


if __name__ == "__main__":
    sys.exit(main())
