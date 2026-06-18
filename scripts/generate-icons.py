#!/usr/bin/env python3
from __future__ import annotations

import math
import os
import shutil
import subprocess
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont


ROOT = Path(__file__).resolve().parents[1]
ICON_DIR = ROOT / "assets" / "icons"
TAURI_ICON_DIR = ROOT / "dashboard" / "tauri" / "src-tauri" / "icons"
WEB_DIR = ROOT / "dashboard" / "web"
MACOS_RESOURCE_DIR = ROOT / "dashboard" / "macos" / "Resources"


def font(size: int, weight: str = "Bold") -> ImageFont.FreeTypeFont:
    candidates = [
        f"/System/Library/Fonts/SFNSMono{weight}.otf",
        f"/System/Library/Fonts/Supplemental/Menlo {weight}.ttf",
        "/System/Library/Fonts/Supplemental/Menlo.ttc",
        "/System/Library/Fonts/Supplemental/Arial Bold.ttf",
    ]
    for path in candidates:
        if Path(path).exists():
            return ImageFont.truetype(path, size=size)
    return ImageFont.load_default()


def rounded_mask(size: int, radius: int) -> Image.Image:
    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, size - 1, size - 1), radius=radius, fill=255)
    return mask


def interpolate(a: tuple[int, int, int], b: tuple[int, int, int], t: float) -> tuple[int, int, int]:
    return tuple(round(a[i] + (b[i] - a[i]) * t) for i in range(3))


def background(size: int) -> Image.Image:
    top = (18, 23, 34)
    bottom = (5, 8, 13)
    img = Image.new("RGBA", (size, size))
    px = img.load()
    for y in range(size):
        t = y / max(size - 1, 1)
        for x in range(size):
            dx = (x - size * 0.22) / size
            dy = (y - size * 0.18) / size
            glow = max(0, 1 - math.sqrt(dx * dx + dy * dy) * 2.1)
            base = interpolate(top, bottom, t)
            color = (
                min(255, round(base[0] + 20 * glow)),
                min(255, round(base[1] + 72 * glow)),
                min(255, round(base[2] + 56 * glow)),
                255,
            )
            px[x, y] = color
    return img


def draw_app_tile(size: int) -> Image.Image:
    scale = 4
    canvas = Image.new("RGBA", (size * scale, size * scale), (0, 0, 0, 0))
    s = canvas.size[0]
    mask = rounded_mask(s, round(s * 0.205))
    bg = background(s)
    canvas.alpha_composite(Image.composite(bg, Image.new("RGBA", (s, s), (0, 0, 0, 0)), mask))

    draw = ImageDraw.Draw(canvas)
    pad = s * 0.185
    center = (s / 2, s / 2)
    nodes = [
        (pad, pad, (16, 185, 129)),
        (s - pad, pad, (59, 130, 246)),
        (pad, s - pad, (245, 158, 11)),
        (s - pad, s - pad, (244, 63, 94)),
    ]

    shadow = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    sd = ImageDraw.Draw(shadow)
    for x, y, _ in nodes:
        sd.line((x, y, center[0], center[1]), fill=(0, 0, 0, 120), width=round(s * 0.065))
    shadow = shadow.filter(ImageFilter.GaussianBlur(round(s * 0.012)))
    canvas.alpha_composite(shadow)

    for x, y, color in nodes:
        draw.line((x, y, center[0], center[1]), fill=(*color, 210), width=round(s * 0.044))

    ring_r = s * 0.255
    draw.ellipse(
        (center[0] - ring_r, center[1] - ring_r, center[0] + ring_r, center[1] + ring_r),
        fill=(8, 12, 20, 238),
        outline=(255, 255, 255, 34),
        width=round(s * 0.018),
    )

    node_r = s * 0.063
    for x, y, color in nodes:
        draw.ellipse((x - node_r, y - node_r, x + node_r, y + node_r), fill=(*color, 255))
        draw.ellipse(
            (x - node_r, y - node_r, x + node_r, y + node_r),
            outline=(255, 255, 255, 105),
            width=round(s * 0.012),
        )

    f = font(round(s * 0.255))
    text = "4x"
    bbox = draw.textbbox((0, 0), text, font=f)
    tw = bbox[2] - bbox[0]
    th = bbox[3] - bbox[1]
    draw.text(
        (center[0] - tw / 2, center[1] - th / 2 - s * 0.018),
        text,
        font=f,
        fill=(244, 244, 245, 255),
    )

    highlight = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    hd = ImageDraw.Draw(highlight)
    hd.rounded_rectangle(
        (s * 0.08, s * 0.055, s * 0.92, s * 0.46),
        radius=round(s * 0.16),
        fill=(255, 255, 255, 18),
    )
    canvas.alpha_composite(Image.composite(highlight, Image.new("RGBA", (s, s), (0, 0, 0, 0)), mask))

    return canvas.resize((size, size), Image.Resampling.LANCZOS)


def draw_app_icon(size: int) -> Image.Image:
    canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    tile_size = round(size * 0.88)
    if tile_size % 2 != size % 2:
        tile_size -= 1
    tile = draw_app_tile(tile_size)
    offset = ((size - tile_size) // 2, (size - tile_size) // 2)
    canvas.alpha_composite(tile, offset)
    return canvas


def draw_menu_icon(width: int, height: int, state: str = "idle") -> Image.Image:
    scale = 4
    w = width * scale
    h = height * scale
    img = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    f = font(round(h * 0.70))
    text = "4x"
    bbox = draw.textbbox((0, 0), text, font=f)
    text_w = bbox[2] - bbox[0]
    text_h = bbox[3] - bbox[1]
    text_x = w * 0.08 if state == "running" else (w - text_w) / 2
    draw.text((text_x, h * 0.50 - text_h / 2 - h * 0.08), text, font=f, fill=(0, 0, 0, 255))
    if state == "running":
        draw.polygon(
            [
                (w * 0.75, h * 0.22),
                (w * 0.75, h * 0.80),
                (w * 0.94, h * 0.51),
            ],
            fill=(0, 0, 0, 255),
        )
    return img.resize((width, height), Image.Resampling.LANCZOS)


def write_svg_source() -> None:
    svg = """<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#12362e"/>
      <stop offset=".48" stop-color="#111827"/>
      <stop offset="1" stop-color="#05080d"/>
    </linearGradient>
  </defs>
  <rect width="1024" height="1024" rx="212" fill="url(#bg)"/>
  <g fill="none" stroke-linecap="round" stroke-width="46">
    <path d="M198 198 512 512" stroke="#10b981"/>
    <path d="M826 198 512 512" stroke="#3b82f6"/>
    <path d="M198 826 512 512" stroke="#f59e0b"/>
    <path d="M826 826 512 512" stroke="#f43f5e"/>
  </g>
  <circle cx="512" cy="512" r="260" fill="#080c14" opacity=".94"/>
  <circle cx="512" cy="512" r="260" fill="none" stroke="#fff" stroke-opacity=".14" stroke-width="18"/>
  <g stroke="#fff" stroke-opacity=".42" stroke-width="12">
    <circle cx="198" cy="198" r="62" fill="#10b981"/>
    <circle cx="826" cy="198" r="62" fill="#3b82f6"/>
    <circle cx="198" cy="826" r="62" fill="#f59e0b"/>
    <circle cx="826" cy="826" r="62" fill="#f43f5e"/>
  </g>
  <text x="512" y="589" text-anchor="middle" font-family="Menlo, SF Mono, monospace" font-size="252" font-weight="800" fill="#f4f4f5">4x</text>
</svg>
"""
    (ICON_DIR / "4x-icon.svg").write_text(svg)


def make_icns(source: Path, dest: Path) -> None:
    iconset = ICON_DIR / "4x-live.iconset"
    if iconset.exists():
        shutil.rmtree(iconset)
    iconset.mkdir(parents=True)
    sizes = [
        (16, "icon_16x16.png"),
        (32, "icon_16x16@2x.png"),
        (32, "icon_32x32.png"),
        (64, "icon_32x32@2x.png"),
        (128, "icon_128x128.png"),
        (256, "icon_128x128@2x.png"),
        (256, "icon_256x256.png"),
        (512, "icon_256x256@2x.png"),
        (512, "icon_512x512.png"),
        (1024, "icon_512x512@2x.png"),
    ]
    base = Image.open(source)
    for px, name in sizes:
        base.resize((px, px), Image.Resampling.LANCZOS).save(iconset / name)
    subprocess.run(["iconutil", "-c", "icns", str(iconset), "-o", str(dest)], check=True)
    shutil.rmtree(iconset)


def main() -> None:
    for path in (ICON_DIR, TAURI_ICON_DIR, WEB_DIR, MACOS_RESOURCE_DIR):
        path.mkdir(parents=True, exist_ok=True)

    app_1024 = draw_app_icon(1024)
    app_1024.save(ICON_DIR / "4x-app-icon.png")
    app_1024.save(TAURI_ICON_DIR / "icon.png")
    app_1024.save(MACOS_RESOURCE_DIR / "AppIcon.png")

    favicon_sizes = [(16, 16), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    app_1024.save(WEB_DIR / "favicon.ico", sizes=favicon_sizes)

    draw_app_icon(180).save(WEB_DIR / "apple-touch-icon.png")
    draw_app_icon(192).save(WEB_DIR / "icon-192.png")
    draw_app_icon(512).save(WEB_DIR / "icon-512.png")

    menu_states = {
        "idle": ("4x-menubar-template.png", "MenuBarIconTemplate"),
        "running": ("4x-menubar-running-template.png", "MenuBarIconRunningTemplate"),
        "stopped": ("4x-menubar-stopped-template.png", "MenuBarIconStoppedTemplate"),
    }
    for state, (asset_name, resource_name) in menu_states.items():
        menu_1x = draw_menu_icon(32, 18, state)
        menu_2x = draw_menu_icon(64, 36, state)
        menu_1x.save(ICON_DIR / asset_name)
        menu_1x.save(MACOS_RESOURCE_DIR / f"{resource_name}.png")
        menu_2x.save(MACOS_RESOURCE_DIR / f"{resource_name}@2x.png")

    write_svg_source()
    make_icns(ICON_DIR / "4x-app-icon.png", MACOS_RESOURCE_DIR / "AppIcon.icns")


if __name__ == "__main__":
    os.chdir(ROOT)
    main()
