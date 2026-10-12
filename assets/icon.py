#!/usr/bin/env python3
"""Foray's icon: a mushroom cap over a ticked box — the forage, done.
Writes assets/icon.png and basecamp/view/icon.png (512x512), and the Android app's
mobile/assets/icon.png (1024, the same tile) and adaptive-icon.png (1024, the art alone on
transparent, inside the adaptive icon's safe circle; the background is app.json's #0b0d0f)."""
import os
from PIL import Image, ImageDraw, ImageFilter

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def render(S=512, tile=True, art=1.0):
    """The design is drawn on a 512 grid; `art` shrinks the mushroom and box about the centre."""
    k = S / 512 * art
    o = S / 2 - 256 * k
    P = lambda *xy: tuple(o + v * k for v in xy)
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    if tile:
        d.rounded_rectangle((16 * S / 512, 16 * S / 512, S - 16 * S / 512, S - 16 * S / 512), radius=110 * S / 512, fill=(11, 13, 15, 255))
    glow = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    ImageDraw.Draw(glow).ellipse(P(120, 60, 392, 300), fill=(154, 123, 255, 90))
    img.alpha_composite(glow.filter(ImageFilter.GaussianBlur(40 * k)))
    d = ImageDraw.Draw(img)
    # the cap
    d.pieslice(P(116, 92, 396, 330), 180, 360, fill=(154, 123, 255, 255))
    for x, y, r in [(190, 160, 16), (262, 132, 13), (320, 172, 15)]:
        d.ellipse(P(x - r, y - r, x + r, y + r), fill=(214, 221, 227, 230))
    # the stem
    d.rounded_rectangle(P(226, 206, 286, 300), radius=18 * k, fill=(214, 221, 227, 255))
    # the box, and the tick
    d.rounded_rectangle(P(150, 300, 362, 440), radius=26 * k, outline=(53, 240, 160, 255), width=round(14 * k))
    d.line([P(196, 370), P(240, 410), P(318, 330)], fill=(53, 240, 160, 255), width=round(20 * k), joint="curve")
    return img


for out, img in (("assets/icon.png", render()), ("basecamp/view/icon.png", render()),
                 ("mobile/assets/icon.png", render(1024)),
                 # The art spans ~0.82 of the tile; the adaptive safe circle is 66/108 ≈ 0.61 of the layer.
                 ("mobile/assets/adaptive-icon.png", render(1024, tile=False, art=0.70))):
    img.save(os.path.join(ROOT, out))
    print("wrote", out)
