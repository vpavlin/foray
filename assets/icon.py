#!/usr/bin/env python3
"""Foray's icon: a mushroom cap over a ticked box — the forage, done.
Writes assets/icon.png and basecamp/view/icon.png (512x512)."""
import os
from PIL import Image, ImageDraw, ImageFilter

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
S = 512
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)
d.rounded_rectangle((16, 16, S - 16, S - 16), radius=110, fill=(11, 13, 15, 255))
glow = Image.new("RGBA", (S, S), (0, 0, 0, 0))
ImageDraw.Draw(glow).ellipse((120, 60, 392, 300), fill=(154, 123, 255, 90))
img.alpha_composite(glow.filter(ImageFilter.GaussianBlur(40)))
d = ImageDraw.Draw(img)
# the cap
d.pieslice((116, 92, 396, 330), 180, 360, fill=(154, 123, 255, 255))
for x, y, r in [(190, 160, 16), (262, 132, 13), (320, 172, 15)]:
    d.ellipse((x - r, y - r, x + r, y + r), fill=(214, 221, 227, 230))
# the stem
d.rounded_rectangle((226, 206, 286, 300), radius=18, fill=(214, 221, 227, 255))
# the box, and the tick
d.rounded_rectangle((150, 300, 362, 440), radius=26, outline=(53, 240, 160, 255), width=14)
d.line([(196, 370), (240, 410), (318, 330)], fill=(53, 240, 160, 255), width=20, joint="curve")
for out in ("assets/icon.png", "basecamp/view/icon.png"):
    img.save(os.path.join(ROOT, out))
    print("wrote", out)
