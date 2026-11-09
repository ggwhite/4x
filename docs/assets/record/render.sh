#!/usr/bin/env bash
# Rebuild the README clips from the real 4x binary. Headless: vhs for the
# terminal, Playwright (headless Chromium) for the 4x Live dashboard.
#
#   docs/assets/record/render.sh tour    # setup + ONE real agent run (spends tokens):
#                                        # tour.webp/.mp4, feature-loop.webp,
#                                        # feature-live.webp, feature-guardrails.webp,
#                                        # feature-cost.webp
#   docs/assets/record/render.sh recut   # re-encode every clip from the frames the last
#                                        # `tour` left in /tmp/4xdemo (no agent run)
#
# Requires: go, vhs, ffmpeg, img2webp (libwebp), python3 with playwright, numpy,
# pillow, and a logged-in `claude` CLI for `tour`. Everything runs under /tmp/4xdemo.
set -euo pipefail
cd "$(dirname "$0")/../../.."
REC=docs/assets/record
OUT=docs/assets
V=/tmp/4xdemo/vhs

frames() { # mp4 -> png dir
	rm -rf "$2" && mkdir -p "$2"
	ffmpeg -y -v error -i "$1" -vf fps=20 "$2/%05d.png"
}

record() {
	"$REC/setup-demo.sh"
	mkdir -p "$V"
	python3 "$REC/capture-live.py" >/tmp/4xdemo/capture.log 2>&1 &
	local cap=$!
	vhs "$REC/tour.tape" # the real run
	wait "$cap" || true
	cat /tmp/4xdemo/capture.log
	# Feature clips need the feature still in pending-review: record them before sign-off.
	for name in guardrails cost; do
		vhs "$REC/$name.tape"
		frames "$V/$name-raw.mp4" "$V/$name-frames"
	done
	vhs "$REC/tour-end.tape"
	rm -rf "$V/tour-frames" && mkdir -p "$V/tour-frames"
	frames "$V/tour-raw.mp4" "$V/a" && frames "$V/tour-end-raw.mp4" "$V/b"
	for f in "$V"/a/*.png; do mv "$f" "$V/tour-frames/a-$(basename "$f")"; done
	for f in "$V"/b/*.png; do mv "$f" "$V/tour-frames/b-$(basename "$f")"; done
}

encode() {
	# Long agent waits (> 4 s with nothing new on screen) shrink to 0.5 s; typing runs 1.6x.
	python3 "$REC/retime.py" "$OUT/tour.webp" "$V/tour-frames" --hold 2 --long 4 --squeeze 0.5 \
		--speed 1.6 --end-hold 3 --mp4 "$OUT/tour.mp4"
	for name in guardrails cost; do
		python3 "$REC/retime.py" "$OUT/feature-$name.webp" "$V/$name-frames" --hold 2.4 --speed 1.5 --end-hold 2.5
	done
	# Dashboard: 1 screenshot per second; --min-change skips frames where only a timer ticked.
	python3 "$REC/retime.py" "$OUT/feature-loop.webp" /tmp/4xdemo/live/messages --fps 1 --step 0.6 \
		--hold 0.9 --long 3 --squeeze 0.5 --end-hold 2.5 --min-change 800
	python3 "$REC/retime.py" "$OUT/feature-live.webp" /tmp/4xdemo/live/board --fps 1 --step 0.7 \
		--hold 0.9 --long 3 --squeeze 0.6 --end-hold 2.5 --min-change 250
}

case "${1:-}" in
tour) record && encode ;;
recut) encode ;;
*)
	echo "usage: $0 tour|recut" >&2
	exit 2
	;;
esac
