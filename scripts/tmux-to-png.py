#!/usr/bin/env python3
"""Render a tmux capture-pane -e dump to a PNG.

Primary path: ANSI -> HTML (DejaVu Sans Mono) -> wkhtmltoimage.
That keeps box-drawing and figlet/ASCII aligned instead of turning
underscores into loose dashes.
"""

from __future__ import annotations

import argparse
import html
import re
import subprocess
import sys
import tempfile
from pathlib import Path

CSI = re.compile(r"\x1b\[([0-9;]*)m")

DEFAULT_FG = (236, 234, 230)
DEFAULT_BG = (0, 0, 0)
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
FONT_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
WKHTML = "/usr/local/bin/wkhtmltoimage"


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
    text = re.sub(
        r"\x1b\[[0-9;?]*[A-Za-z]",
        lambda m: m.group(0) if m.group(0).endswith("m") else "",
        text,
    )
    return text.replace("\r", "")


def css_color(rgb) -> str:
    return f"rgb({rgb[0]},{rgb[1]},{rgb[2]})"


def line_to_html(raw: str) -> str:
    raw = strip_other_escapes(raw)
    fg, bg, bold, reverse = DEFAULT_FG, DEFAULT_BG, False, False
    out = []
    buf = []
    cur = None

    def flush():
        if not buf:
            return
        text = html.escape("".join(buf))
        buf.clear()
        style = f"color:{css_color(cur[0])};background:{css_color(cur[1])}"
        if cur[2]:
            style += ";font-weight:700"
        out.append(f'<span style="{style}">{text}</span>')

    pos = 0
    while pos < len(raw):
        m = CSI.match(raw, pos)
        if m:
            flush()
            fg, bg, bold, reverse = parse_sgr(m.group(1), fg, bg, bold, reverse)
            pos = m.end()
            continue
        ch = raw[pos]
        pos += 1
        if ch == "\x1b":
            nxt = raw.find("\x1b", pos)
            pos = len(raw) if nxt < 0 else nxt
            continue
        cell_fg, cell_bg = (bg, fg) if reverse else (fg, bg)
        key = (cell_fg, cell_bg, bold)
        if cur != key:
            flush()
            cur = key
        buf.append(ch)
    flush()
    return "".join(out) if out else "&nbsp;"


def ansi_to_html(text: str) -> str:
    lines = text.splitlines()
    if lines and lines[-1] == "":
        lines = lines[:-1]
    cols = max((len(CSI.sub("", strip_other_escapes(line))) for line in lines), default=80)
    body = "\n".join(line_to_html(line) for line in lines)
    # 9.6px per DejaVu 16px cell, plus padding.
    width_px = max(cols * 10 + 40, 800)
    return f"""<!DOCTYPE html>
<html><head><meta charset="utf-8">
<style>
@font-face {{
  font-family: Term;
  src: url('file://{FONT}');
  font-weight: 400;
}}
@font-face {{
  font-family: Term;
  src: url('file://{FONT_BOLD}');
  font-weight: 700;
}}
html, body {{
  margin: 0;
  background: #000;
}}
pre {{
  margin: 16px;
  padding: 0;
  background: #000;
  color: rgb(236,234,230);
  font-family: Term, "DejaVu Sans Mono", monospace;
  font-size: 15px;
  line-height: 1.25;
  letter-spacing: 0;
  white-space: pre;
  tab-size: 8;
}}
</style></head>
<body>
<pre>{body}</pre>
</body></html>
"""


def render(text: str, out: Path) -> None:
    page = ansi_to_html(text)
    cols = max(
        (len(CSI.sub("", strip_other_escapes(line))) for line in text.splitlines()),
        default=80,
    )
    width_px = max(cols * 10 + 48, 900)
    with tempfile.TemporaryDirectory() as tmp:
        html_path = Path(tmp) / "pane.html"
        html_path.write_text(page, encoding="utf-8")
        cmd = [
            WKHTML,
            "--quiet",
            "--format", "png",
            "--quality", "100",
            "--width", str(width_px),
            "--disable-smart-width",
            "--enable-local-file-access",
            str(html_path),
            str(out),
        ]
        subprocess.run(cmd, check=True)
    print(f"wrote {out} ({out.stat().st_size} bytes, width={width_px})")


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
