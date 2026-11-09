#!/usr/bin/env python3
"""Turn a frame sequence into an animated WebP, squeezing out long static stretches.

    retime.py OUT.webp FRAME_DIR --fps 20 [--width 900] [--hold 3.0] [--long 4.0]
              [--squeeze 0.7] [--step 0] [--end-hold 3.0] [--min-change 40] [--speed 1]
              [--mp4 OUT.mp4]

Consecutive frames that differ in fewer than --min-change pixels are merged. A merged run
keeps its real duration up to --hold seconds; runs longer than --long seconds
(an agent working, nothing on screen changing) are cut to --squeeze seconds.
--step overrides the duration of every changing frame (for 1 fps screenshot
sequences). The last frame is held for --end-hold seconds.
Encoding goes through img2webp (Homebrew ffmpeg has no libwebp_anim); a key frame
at least every 2 frames stops lossy residue from earlier screens showing through.
"""
import argparse, shutil, subprocess, sys, tempfile
from pathlib import Path
import numpy as np
from PIL import Image

ap = argparse.ArgumentParser()
ap.add_argument("out"); ap.add_argument("frames")
ap.add_argument("--fps", type=float, default=20)
ap.add_argument("--width", type=int, default=900)
ap.add_argument("--hold", type=float, default=3.0)
ap.add_argument("--long", type=float, default=4.0)
ap.add_argument("--squeeze", type=float, default=0.7)
ap.add_argument("--step", type=float, default=0)
ap.add_argument("--end-hold", type=float, default=3.0)
ap.add_argument("--start", type=int, default=0, help="first frame index to use")
ap.add_argument("--stop", type=int, default=0, help="stop before this frame index (0 = all)")
ap.add_argument("--min-change", type=int, default=40,
                help="pixels that must change for a frame to count as new (raise it to ignore ticking timers)")
ap.add_argument("--speed", type=float, default=1.0,
                help="speed-up for short runs (typing, scrolling); holds are unaffected")
ap.add_argument("--quality", type=int, default=70)
ap.add_argument("--mp4")
a = ap.parse_args()

files = sorted(Path(a.frames).glob("*.png"))
files = files[a.start:a.stop or None]
if not files:
    sys.exit("no frames")


def sig(p):
    return np.asarray(Image.open(p).convert("L"), dtype=np.int16)


def same(x, y):
    d = np.abs(x - y) > 40
    if int(d.sum()) < a.min_change:
        return True
    # Ignore sparse video-codec noise: only count dense 4x4 blocks.
    h, w = (d.shape[0] // 4) * 4, (d.shape[1] // 4) * 4
    b = d[:h, :w].reshape(h // 4, 4, w // 4, 4).sum(axis=(1, 3)) >= 6
    if not b.any():
        return True
    ys, xs = np.nonzero(b)  # a lone blinking cursor cell is not a change
    return ys.max() - ys.min() <= 7 and xs.max() - xs.min() <= 4


runs = []  # [first_file, count]
prev = None
for f in files:
    s = sig(f)
    if prev is not None and same(s, prev):
        runs[-1][1] += 1
    else:
        runs.append([f, 1])
    prev = s

dt = 1 / a.fps
out = []
for i, (f, n) in enumerate(runs):
    d = n * dt
    if a.step:
        d = a.step if n == 1 else min(max(d, a.step), a.hold)
    if d > a.long:
        d = a.squeeze
    else:
        d = min(d, a.hold)
        if d < 0.5:
            d /= a.speed
    if i == len(runs) - 1:
        d = a.end_hold
    out.append((f, max(int(round(d * 1000)), 20)))

tmp = Path(tempfile.mkdtemp(prefix="4x-retime-"))
args = ["img2webp", "-loop", "0", "-kmin", "1", "-kmax", "2", "-lossy", "-q", str(a.quality), "-m", "6", "-exact"]
for i, (f, ms) in enumerate(out):
    g = tmp / f"{i:05d}.png"
    im = Image.open(f).convert("RGB")
    if a.width and im.width != a.width:
        im = im.resize((a.width, round(im.height * a.width / im.width)), Image.LANCZOS)
    im.save(g)
    args += ["-d", str(ms), str(g)]
subprocess.run(args + ["-o", a.out], check=True, stdout=subprocess.DEVNULL)
total = sum(ms for _, ms in out) / 1000
if a.mp4:
    lst = tmp / "list.txt"
    with open(lst, "w") as fh:
        for i, (_, ms) in enumerate(out):
            fh.write(f"file '{tmp / f'{i:05d}.png'}'\nduration {ms / 1000:.3f}\n")
        fh.write(f"file '{tmp / f'{len(out) - 1:05d}.png'}'\n")
    subprocess.run(["ffmpeg", "-y", "-v", "error", "-f", "concat", "-safe", "0", "-i", str(lst),
                    "-vf", "fps=20,format=yuv420p,scale=trunc(iw/2)*2:trunc(ih/2)*2", "-c:v", "libx264",
                    "-crf", "23", "-movflags", "+faststart", a.mp4], check=True)
shutil.rmtree(tmp)
print(f"{a.out}: {len(out)} frames, {total:.1f}s, {Path(a.out).stat().st_size / 1e6:.2f} MB")
