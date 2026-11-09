#!/usr/bin/env python3
"""Snapshot animated SVGs through <img> in headless Chromium (as GitHub shows them).

    python3 docs/assets/diagrams/snapshot.py OUT_DIR file.svg ...

Writes <name>-t{1.5,4.5,8.0}.png and <name>-reduced.png (prefers-reduced-motion).
Playwright's reduced-motion emulation does not reach an SVG loaded through <img>,
so the reduced-motion shot loads the SVG as the page itself.
"""
import sys, time
from pathlib import Path
from playwright.sync_api import sync_playwright

out = Path(sys.argv[1]); out.mkdir(parents=True, exist_ok=True)
with sync_playwright() as p:
    b = p.chromium.launch()
    for f in sys.argv[2:]:
        f = Path(f).resolve()
        html = f'<body style="margin:0"><img src="{f.as_uri()}"></body>'
        for motion in ("no-preference", "reduce"):
            pg = b.new_page(viewport={"width": 1100, "height": 460}, reduced_motion=motion)
            tmp = out / "page.html"; tmp.write_text(html)
            t0 = time.time(); pg.goto(f.as_uri() if motion == "reduce" else tmp.as_uri())
            if motion == "reduce":
                pg.wait_for_timeout(2000)
                pg.screenshot(path=str(out / f"{f.stem}-reduced.png"))
            else:
                for t in (1.5, 4.5, 8.0):
                    pg.wait_for_timeout(max(0, (t - (time.time() - t0)) * 1000))
                    pg.screenshot(path=str(out / f"{f.stem}-t{t}.png"))
            pg.close()
    b.close()
