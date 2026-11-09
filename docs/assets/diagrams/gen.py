#!/usr/bin/env python3
"""Generate the README diagrams as animated SVG (light + dark).

    python3 docs/assets/diagrams/gen.py

Writes docs/assets/{architecture,loop}-{light,dark}.svg. Animation is pure CSS
(@keyframes) in an overlay group: packets travel the edges in order, the
active edge is highlighted and boxes pulse on arrival. prefers-reduced-motion
stops it, leaving the static diagram.
"""
import math
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent

THEMES = {
    "light": dict(bg="#FFFFFF", text="#1F2328", muted="#59636E", border="#D0D7DE",
                  box="#FFFFFF", soft="#F6F8FA", accent="#8250DF", accent_soft="#F3EEFF",
                  accent_border="#C9B3F5", roles=dict(designer="#8250DF", coder="#0A7EA4",
                  reviewer="#1A7F37", tester="#BC4C00", accept="#9A6700", done="#1A7F37")),
    "dark": dict(bg="#0D1117", text="#E6EDF3", muted="#8B949E", border="#30363D",
                 box="#161B22", soft="#161B22", accent="#BC8CFF", accent_soft="#1E1A33",
                 accent_border="#5B4B8A", roles=dict(designer="#BC8CFF", coder="#56D4DD",
                 reviewer="#56D364", tester="#FFA657", accept="#E3B341", done="#56D364")),
}

FONT = ("<style>.sans{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif}"
        ".mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}.b{font-weight:600}"
        ".cap{font-size:13px;letter-spacing:1.2px;font-weight:600}</style>")


def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


class Svg:
    def __init__(self, w, h, title, desc, t):
        self.w, self.h, self.t = w, h, t
        self.parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" role="img" aria-labelledby="title desc">',
                      f'<title id="title">{esc(title)}</title>', f'<desc id="desc">{esc(desc)}</desc>', FONT,
                      '<defs>' + self.marker("ah", t["muted"]) + self.marker("aha", t["accent"]) + '</defs>',
                      f'<rect width="{w}" height="{h}" fill="{t["bg"]}"/>']
        self.fx, self.css, self.n = [], [], 0

    @staticmethod
    def marker(i, c):
        return (f'<marker id="{i}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9" markerHeight="9" '
                f'markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M0,1 L9,5 L0,9 Z" fill="{c}"/></marker>')

    def text(self, x, y, s, cls="sans", fill=None, size=14, anchor="start"):
        fill = fill or self.t["text"]
        self.parts.append(f'<text x="{x}" y="{y}" class="{cls}" fill="{fill}" font-size="{size}" text-anchor="{anchor}">{esc(s)}</text>')

    def box(self, x, y, w, h, fill=None, stroke=None, rx=10, sw=1.5):
        self.parts.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill or self.t["box"]}" stroke="{stroke or self.t["border"]}" stroke-width="{sw}"/>')
        return (x, y, w, h, rx)

    def path(self, pts, accent=False, dashed=False, arrow=True):
        d = "M" + " L".join(f"{x},{y}" for x, y in pts)
        c = self.t["accent"] if accent else self.t["muted"]
        extra = ' stroke-dasharray="6 5"' if dashed else ""
        m = f' marker-end="url(#{"aha" if accent else "ah"})"' if arrow else ""
        self.parts.append(f'<path d="{d}" fill="none" stroke="{c}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"{extra}{m}/>')
        return pts

    # ---- animation overlay -------------------------------------------------
    def _id(self, p):
        self.n += 1
        return f"{p}{self.n}"

    def anim_edge(self, pts, t0, t1, T, optional=False):
        """Highlight edge pts and move a packet along it between t0 and t1 (seconds)."""
        pct = lambda s: f"{100 * s / T:.2f}%"
        e = self._id("e")
        hold = min(t1 + 1.2, T)
        self.css.append(f"@keyframes {e}{{0%{{opacity:0}}{pct(max(t0 - .1, 0))}{{opacity:0}}{pct(t0)}{{opacity:.5}}"
                        f"{pct(t1)}{{opacity:.5}}{pct(hold)}{{opacity:0}}100%{{opacity:0}}}}.{e}{{animation:{e} {T}s linear infinite}}")
        d = "M" + " L".join(f"{x},{y}" for x, y in pts)
        self.fx.append(f'<path class="{"ego" if optional else "eg"} {e}" d="{d}" opacity="0"/>')
        p = self._id("p")
        L = [0.0]
        for (x0, y0), (x1, y1) in zip(pts, pts[1:]):
            L.append(L[-1] + math.hypot(x1 - x0, y1 - y0))
        frames = [f"0%{{opacity:0;transform:translate({pts[0][0]}px,{pts[0][1]}px)}}",
                  f"{pct(max(t0 - .05, 0))}{{opacity:0;transform:translate({pts[0][0]}px,{pts[0][1]}px)}}"]
        for (x, y), l in zip(pts, L):
            s = t0 + (t1 - t0) * l / L[-1]
            op = 1 if l < L[-1] else 0
            frames.append(f"{pct(s)}{{opacity:{op};transform:translate({x}px,{y}px)}}")
        frames.append(f"100%{{opacity:0;transform:translate({pts[-1][0]}px,{pts[-1][1]}px)}}")
        self.css.append(f"@keyframes {p}{{{''.join(frames)}}}.{p}{{animation:{p} {T}s linear infinite}}")
        self.fx.append(f'<g class="{"pko" if optional else "pk"} {p}" opacity="0"><circle r="7" class="h"/><circle r="3.5" class="c"/></g>')

    def anim_box(self, b, t0, T):
        pct = lambda s: f"{100 * s / T:.2f}%"
        x, y, w, h, rx = b
        i = self._id("b")
        self.css.append(f"@keyframes {i}{{0%{{opacity:0}}{pct(t0)}{{opacity:0}}{pct(t0 + .12)}{{opacity:.85}}"
                        f"{pct(min(t0 + .7, T))}{{opacity:.85}}{pct(min(t0 + 1.3, T))}{{opacity:0}}100%{{opacity:0}}}}"
                        f".{i}{{animation:{i} {T}s ease-in-out infinite}}")
        self.fx.append(f'<rect class="bx {i}" opacity="0" x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}"/>')

    def done(self):
        a = self.t["accent"]
        base = (f".fx .h{{fill:{a};opacity:.28}}.fx .c{{fill:{a}}}.fx .pko .h{{opacity:.14}}.fx .pko .c{{opacity:.6}}"
                f".fx .eg{{fill:none;stroke:{a};stroke-width:4;stroke-linecap:round;stroke-linejoin:round}}"
                f".fx .ego{{fill:none;stroke:{a};stroke-width:2;stroke-dasharray:6 5;stroke-linecap:round}}"
                f".fx .bx{{fill:none;stroke:{a};stroke-width:2.5}}"
                "@media (prefers-reduced-motion:reduce){.fx *{animation:none!important;opacity:0!important}}")
        self.parts.append("<style>" + base + "".join(self.css) + "</style>")
        self.parts.append('<g class="fx" aria-hidden="true">' + "".join(self.fx) + "</g>")
        self.parts.append("</svg>")
        return "\n".join(self.parts) + "\n"


def architecture(t):
    W, H, T = 1100, 400, 10.0
    s = Svg(W, H, "4x architecture",
            "You drive the 4x CLI (Go), which enforces deterministic guardrails and never calls an LLM. "
            "It hands work to runners (Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor, OpenCode) "
            "through the .4x/ file protocol. 4x Live streams the same state over SSE to a multi-project dashboard.", t)
    for x, lab in ((40, "OPERATOR"), (180, "LAYER 1 · CLI"), (500, "LAYER 2 · RUNNERS"), (820, "LAYER 3 · LIVE")):
        s.text(x, 40, lab, "sans cap", t["muted"], 13)
    you = s.box(40, 150, 100, 72)
    s.text(90, 192, "You", "sans b", size=17, anchor="middle")
    cli = s.box(180, 92, 270, 188, fill=t["accent_soft"], stroke=t["accent_border"])
    s.text(200, 124, "4x CLI (Go)", "sans b", t["accent"], 18)
    s.text(200, 148, "Deterministic guardrails.", "sans", t["text"], 14)
    s.text(200, 168, "No LLM calls.", "sans", t["text"], 14)
    for i, c in enumerate(("scope checks", "protocol", "state machine", "dependencies")):
        bx, by = 200 + (i % 2) * 120, 190 + (i // 2) * 38
        s.box(bx, by, 112, 28, fill=t["box"], rx=14)
        s.text(bx + 56, by + 19, c, "sans", t["accent"], 13, "middle")
    run = s.box(500, 92, 270, 188)
    s.text(520, 124, "Runners", "sans b", size=18)
    s.text(520, 148, "Each uses native platform capabilities", "sans", t["muted"], 13)
    names = ("Claude Code", "Codex", "Gemini", "Antigravity", "Copilot", "Cursor", "OpenCode")
    for i, c in enumerate(names):
        bx, by = 520 + (i % 2) * 120, 166 + (i // 2) * 27
        s.text(bx, by + 17, "▸ " + c, "mono", t["text"], 13)
    live = s.box(820, 92, 240, 188)
    s.text(840, 124, "4x Live", "sans b", size=18)
    s.text(840, 148, "Dashboard", "sans", t["muted"], 13)
    for i, c in enumerate(("multi-project", "real-time monitoring", "phase transitions", "log streaming")):
        s.text(840, 180 + i * 24, "• " + c, "sans", t["text"], 14)
    e1 = s.path([(140, 186), (178, 186)])
    e2 = s.path([(450, 186), (498, 186)], accent=True)
    s.text(475, 312, ".4x/ directory", "mono", t["accent"], 13, "middle")
    s.text(475, 330, "file-based protocol", "sans", t["muted"], 13, "middle")
    s.path([(475, 196), (475, 296)], arrow=False, dashed=True)
    e3 = s.path([(770, 186), (818, 186)], accent=True)
    s.text(795, 312, "SSE events", "mono", t["accent"], 13, "middle")
    s.path([(795, 196), (795, 296)], arrow=False, dashed=True)
    s.text(40, 376, "The CLI owns state and guardrails; runners only read and write files under .4x/; REST + SSE API for the dashboard.", "sans", t["muted"], 14)
    s.anim_edge(e1, 0.3, 1.0, T); s.anim_box(cli, 1.0, T)
    s.anim_edge(e2, 2.2, 3.0, T); s.anim_box(run, 3.0, T)
    s.anim_edge([(770, 206), (800, 206)], 4.4, 4.8, T, optional=True)
    s.anim_edge(e3, 5.0, 5.8, T); s.anim_box(live, 5.8, T)
    s.anim_edge(list(reversed(e2)), 7.2, 8.0, T, optional=True); s.anim_box(cli, 8.0, T)
    return s.done()


def loop(t):
    W, H, T = 1100, 330, 11.0
    s = Svg(W, H, "How the 4x loop works",
            "Designer, Coder, Reviewer, Tester, Accept, Pending Review, Done. A Reviewer FAIL or a failing test "
            "sends the code to amending, which goes back to the Reviewer. Escalations route back to the Designer. "
            "A human signs off before Done.", t)
    r = t["roles"]
    labels = [("Designer", r["designer"]), ("Coder", r["coder"]), ("Reviewer", r["reviewer"]),
              ("Tester", r["tester"]), ("Accept", r["accept"]), ("Pending Review", t["muted"]), ("Done", r["done"])]
    bw, gap, x0, y0, bh = 128, 22, 36, 70, 64
    boxes = []
    for i, (lab, c) in enumerate(labels):
        x = x0 + i * (bw + gap)
        boxes.append(s.box(x, y0, bw, bh))
        s.parts.append(f'<rect x="{x}" y="{y0}" width="5" height="{bh}" rx="2" fill="{c}"/>')
        s.text(x + bw / 2 + 2, y0 + 38, lab, "sans b", c if c != t["muted"] else t["text"], 15 if len(lab) < 10 else 14, "middle")
    s.text(36, 40, "ROLES RUN ISOLATED, IN ORDER", "sans cap", t["muted"], 13)
    cy = y0 + bh / 2
    edges = []
    for i in range(6):
        a = x0 + i * (bw + gap) + bw
        edges.append(s.path([(a, cy), (a + gap - 2, cy)]))
    cx = lambda i: x0 + i * (bw + gap) + bw / 2
    am = s.box(cx(2) + 10, 214, 150, 52, fill=t["soft"])
    s.text(cx(2) + 85, 246, "amending", "mono b", t["text"], 15, "middle")
    rv_am = s.path([(cx(2) - 20, y0 + bh), (cx(2) - 20, 240), (cx(2) + 8, 240)], accent=True)
    s.text(cx(2) - 26, 168, "FAIL", "mono", t["accent"], 12, "end")
    ts_am = s.path([(cx(3), y0 + bh), (cx(3), 240), (cx(2) + 162, 240)], accent=True)
    s.text(cx(3) + 6, 168, "verify failed", "mono", t["accent"], 12, "start")
    am_rv = s.path([(cx(2) + 40, 214), (cx(2) + 40, y0 + bh + 2)])
    s.text(cx(2) + 46, 196, "re-review", "sans", t["muted"], 12)
    s.path([(cx(2) + 85, 266), (cx(2) + 85, 294), (cx(0), 294), (cx(0), y0 + bh + 2)], dashed=True)
    s.text(cx(0) + 10, 286, "escalation → Designer (spec-mismatch, criteria-wrong)", "sans", t["muted"], 12)
    s.path([(cx(6), 204), (cx(6), y0 + bh + 2)])
    s.text(cx(6), 224, "human sign-off", "sans b", t["text"], 13, "middle")
    s.text(cx(6), 242, "4x done", "mono", t["muted"], 12, "middle")
    s.text(W - 36, 316, "Round budget (default 5) stops endless amend loops.", "sans", t["muted"], 13, "end")
    # animation: happy path with one review round-trip
    tt = 0.3
    s.anim_box(boxes[0], tt, T)
    s.anim_edge(edges[0], tt + .5, tt + .9, T); s.anim_box(boxes[1], tt + .9, T)
    s.anim_edge(edges[1], tt + 1.4, tt + 1.8, T); s.anim_box(boxes[2], tt + 1.8, T)
    s.anim_edge(rv_am, tt + 2.4, tt + 3.1, T); s.anim_box(am, tt + 3.1, T)
    s.anim_edge(am_rv, tt + 3.7, tt + 4.1, T); s.anim_box(boxes[2], tt + 4.1, T)
    s.anim_edge(edges[2], tt + 4.7, tt + 5.1, T); s.anim_box(boxes[3], tt + 5.1, T)
    s.anim_edge(edges[3], tt + 5.7, tt + 6.1, T); s.anim_box(boxes[4], tt + 6.1, T)
    s.anim_edge(edges[4], tt + 6.7, tt + 7.1, T); s.anim_box(boxes[5], tt + 7.1, T)
    s.anim_edge([(cx(6), 204), (cx(6), y0 + bh + 2)], tt + 7.7, tt + 8.1, T, optional=True)
    s.anim_edge(edges[5], tt + 8.2, tt + 8.6, T); s.anim_box(boxes[6], tt + 8.6, T)
    return s.done()


for name, fn in (("architecture", architecture), ("loop", loop)):
    for theme, t in THEMES.items():
        p = OUT / f"{name}-{theme}.svg"
        p.write_text(fn(t))
        print(f"{p.relative_to(OUT.parent.parent)}: {p.stat().st_size} bytes")
