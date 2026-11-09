#!/usr/bin/env python3
"""Capture the 4x Live web dashboard while tour.tape drives a real run.

Starts `4x live` on the throwaway demo project (/tmp/4xdemo), opens the feature
in headless Chromium (Playwright) and screenshots two views once per second
until the run reaches pending-review:

  /tmp/4xdemo/live/board/NNNNN.png     project overview (sidebar cards + stats)
  /tmp/4xdemo/live/messages/NNNNN.png  feature detail, Messages tab

Usage: render.sh tour starts it next to `vhs docs/assets/record/tour.tape`. Frames are turned into clips by render.sh.
"""
import json, os, re, shutil, subprocess, sys, time
from pathlib import Path
from playwright.sync_api import sync_playwright

D = Path("/tmp/4xdemo")
PROJ = D / "work/greet"
FEATURE = "F001-greet-by-name"
OUT = D / "live"
END_PHASES = {"pending-review", "done", "needs-attention", "blocked"}

env = dict(os.environ, HOME=str(D / "home"), PATH=f"{D/'bin'}:{os.environ['PATH']}")


def phase():
    try:
        return json.load(open(PROJ / ".4x/run" / FEATURE / "state.json")).get("phase", "")
    except (OSError, ValueError):
        return ""


def main():
    deadline = time.time() + 600
    while not (PROJ / ".4x/features" / f"{FEATURE}.yaml").exists():
        if time.time() > deadline:
            sys.exit("feature never created")
        time.sleep(0.5)
    log = open(D / "live.log", "w")
    srv = subprocess.Popen([str(D / "bin/4x"), "live", "-p", "4599", str(PROJ)],
                           env=env, stdout=log, stderr=subprocess.STDOUT)
    url = None
    while not url:
        time.sleep(0.3)
        m = re.search(r"http://\S+", (D / "live.log").read_text())
        url = m and m.group(0)
    shutil.rmtree(OUT, ignore_errors=True)
    for v in ("board", "messages"):
        (OUT / v).mkdir(parents=True)
    try:
        with sync_playwright() as p:
            b = p.chromium.launch()
            pages = {}
            for v in ("board", "messages"):
                pg = b.new_page(viewport={"width": 1280, "height": 720},
                                color_scheme="dark", device_scale_factor=1)
                pg.goto(url)
                pg.wait_for_timeout(1500)
                if v == "messages":
                    pg.get_by_text("F001: Greet by name").first.click()
                    pg.wait_for_timeout(500)
                    pg.get_by_text("Messages", exact=True).first.click()
                pages[v] = pg
            n, done_at = 0, None
            while True:
                for v, pg in pages.items():
                    pg.screenshot(path=str(OUT / v / f"{n:05d}.png"))
                n += 1
                if phase() in END_PHASES and done_at is None:
                    done_at = n
                if done_at is not None and n - done_at > 8:
                    break
                if n > 3600:
                    break
                time.sleep(1)
            b.close()
    finally:
        srv.terminate()
    print(f"captured {n} frames, final phase {phase()!r}")


if __name__ == "__main__":
    main()
