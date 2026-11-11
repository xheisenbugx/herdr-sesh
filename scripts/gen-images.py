#!/usr/bin/env python3
"""Draws the README images for herdr-sesh: mockups of the herdr-sesh
layout with demo data (no real spaces or terminal content), in its theme."""
import os, re, subprocess, sys
from xml.sax.saxutils import escape

ICONS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "mods", "sesh-sidebar")
OUT = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "docs", "images")
os.makedirs(OUT, exist_ok=True)

C = dict(bg="#1e1d2d", canvas="#1a1927", chrome="#1e1d2d", pop="#232236", raised="#2a2940", hover="#28263b",
         sel="#3b3363", border="#3a3852", fg="#cdd6f4", muted="#a6adc8", dim="#6c7086", accent="#a48cf2",
         working="#89b4fa", blocked="#f9e2af", done="#a6e3a1", red="#f38ba8", teal="#94e2d5", orange="#fab387")
FONT = "BlexMono Nerd Font Mono"
FS = 13
CW = FS * 0.6  # monospace advance


class S:
    def __init__(s, w, h):
        s.w, s.h, s.parts = w, h, []

    def add(s, x):
        s.parts.append(x)

    def rect(s, x, y, w, h, fill, rx=0, stroke=None, sw=1, op=1):
        st = f' stroke="{stroke}" stroke-width="{sw}"' if stroke else ""
        s.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" fill-opacity="{op}"{st}/>')

    def line(s, x1, y1, x2, y2, c=C["border"], w=1):
        s.add(f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="{c}" stroke-width="{w}"/>')

    def text(s, x, y, t, c=C["fg"], bold=False, size=FS, anchor="start", op=1):
        b = ' font-weight="bold"' if bold else ""
        s.add(f'<text x="{x}" y="{y}" fill="{c}" font-size="{size}"{b} text-anchor="{anchor}" fill-opacity="{op}" xml:space="preserve">{escape(t)}</text>')
        return x + len(t) * size * 0.6

    def icon(s, name, x, y, size, c):
        body = open(f"{ICONS}/{name}.svg").read()
        inner = re.sub(r"^<svg[^>]*>|</svg>$", "", body.strip())
        attrs = re.search(r"<svg([^>]*)>", body).group(1)
        attrs = re.sub(r'\s(xmlns|width|height)="[^"]*"', "", attrs)
        s.add(f'<svg x="{x}" y="{y}" width="{size}" height="{size}" color="{c}"{attrs}>{inner}</svg>')

    def save(s, name, scale=2):
        svg = (f'<svg xmlns="http://www.w3.org/2000/svg" width="{s.w}" height="{s.h}" viewBox="0 0 {s.w} {s.h}" '
               f'font-family="{FONT}">' + "".join(s.parts) + "</svg>")
        p = f"{OUT}/{name}"
        open(p + ".svg", "w").write(svg)
        subprocess.run(["rsvg-convert", "-z", str(scale), p + ".svg", "-o", p + ".png"], check=True)
        os.remove(p + ".svg")


GLYPH = {"blocked": ("bell", C["blocked"]), "done": ("check", C["done"]), "working": ("working", C["working"]), "idle": ("circle", C["dim"])}

THREADS = {
    "pinned": [dict(st="idle", title="Refactor the auth middleware", age="1h", space="api", detail="idle", dc=C["dim"])],
    "inbox": [
        dict(st="blocked", title="Fix the login redirect", age="2m", space="api", detail="Allow Bash: npm test", dc=C["blocked"], branch="fix/login", git="↑2", sel=True),
        dict(st="done", title="Write the getting started guide", age="5m", space="docs", detail="finished", dc=C["done"], branch="docs/guide", git="↑3", pr="#42", unread=True),
        dict(st="idle", title="Bump the test dependencies", age="3h", space="web", detail="idle", dc=C["dim"]),
    ],
    "working": [dict(st="working", title="Add rate limiting", age="42s", space="api", detail="› working 42s", dc=C["muted"], branch="feat/rate-limit", git="↑4 ↓1")],
}


def hatch(s, x, y, w, title, right="+"):
    e = s.text(x, y, title, C["accent"], bold=True)
    n = int((w - (e - x) - 16 - (len(right) + 1) * CW) / CW) if right else int((w - (e - x) - 10) / CW)
    s.text(e + 8, y, "/" * max(0, n), C["accent"], op=0.45)
    if right:
        s.text(x + w, y, right, C["accent"], anchor="end", bold=True)


def section(s, x, y, w, icon, label, n):
    s.icon(icon, x, y - 10, 12, C["muted"])
    e = s.text(x + 18, y, label, C["muted"], bold=True)
    s.line(e + 8, y - 4, x + w - 20, y - 4)
    s.text(x + w, y, str(n), C["muted"], anchor="end")


def thread(s, x, y, w, th, wide=False):
    if th.get("sel"):
        s.rect(x - 10, y - 15, w + 18, 44, C["sel"])
        s.rect(x - 10, y - 15, 3, 44, C["accent"])
    if th.get("unread"):
        s.text(x, y, "•", C["accent"], bold=True)
    ic, col = GLYPH[th["st"]]
    s.icon(ic, x + 8, y - 10, 12, col)
    bold = th.get("sel") or th.get("unread") or th["st"] == "blocked"
    s.text(x + 26, y, th["title"], C["fg"], bold=bold)
    s.text(x + w - 16, y, th["age"], C["dim"], anchor="end")
    if th.get("unread") or th.get("sel"):
        s.text(x + w, y, "●", C["accent"], anchor="end")
    y2 = y + 20
    e = s.text(x + 26, y2, th["space"], C["muted"])
    e = s.text(e + 6, y2, "·", C["dim"])
    s.text(e + 6, y2, th["detail"], th["dc"])
    right = ""
    if wide and th.get("branch"):
        right = th["branch"]
    if th.get("git"):
        right += ("  " if right else "") + th["git"]
    rx = x + w
    if th.get("pr"):
        s.text(rx, y2, th["pr"], C["done"], anchor="end")
        rx -= len(th["pr"]) * CW + 14
        s.icon("pr", rx, y2 - 10, 11, C["done"])
        rx -= 10
    if right:
        s.text(rx, y2, right, C["dim"], anchor="end")
    return y + 48


def sidebar(s, x, y, w, h, footer=True):
    s.rect(x, y, w, h, C["chrome"])
    pad = x + 14
    iw = w - 28
    yy = y + 52
    hatch(s, pad, yy, iw, "Threads", "⋯  +")
    yy += 22
    s.text(pad, yy, "all spaces · 7", C["muted"])
    bx = pad + iw
    for ic, col, n in (("check", C["done"], 1), ("working", C["working"], 1), ("bell", C["blocked"], 1)):
        s.text(bx, yy, str(n), col, anchor="end")
        bx -= len(str(n)) * CW + 15
        s.icon(ic, bx, yy - 10, 11, col)
        bx -= 14
    yy += 36
    section(s, pad, yy, iw, "pin", "PINNED", 1)
    yy += 24
    for th in THREADS["pinned"]:
        yy = thread(s, pad, yy, iw, th)
    yy += 8
    section(s, pad, yy, iw, "inbox", "INBOX", 3)
    yy += 24
    for th in THREADS["inbox"]:
        yy = thread(s, pad, yy, iw, th)
    sy = y + h - (150 if footer else 110)
    s.line(pad, sy, pad + iw, sy)
    sy += 24
    for ic, label, n, col in (("working", "Working", 1, C["working"]), ("clock", "Snoozed", 1, C["muted"]), ("box", "Settled", 2, C["muted"])):
        s.text(pad + 4, sy, "▸", C["dim"])
        s.icon(ic, pad + 20, sy - 10, 12, col)
        s.text(pad + 40, sy, label)
        s.text(pad + iw, sy, str(n), C["muted"], anchor="end")
        sy += 24
    if footer:
        fy = y + h - 22
        s.line(pad, fy - 20, pad + iw, fy - 20)
        fx = pad
        for i, (k, d) in enumerate((("a", "approve"), ("x", "deny"), ("r", "reply"), ("m", "more"), ("?", "keys"))):
            if i:
                fx = s.text(fx + 6, fy, "·", C["dim"]) + 6
            fx = s.text(fx, fy, k, C["accent"], bold=True)
            fx = s.text(fx + 5, fy, d, C["muted"])


CLAUDE = [
    ("● ", C["orange"], "The redirect drops the page you came from, so after logging in", C["fg"]),
    ("  ", None, "everyone lands on the dashboard. I'll keep it in a next parameter.", C["fg"]),
    ("", None, "", None),
    ("● ", C["done"], "Update(src/auth/redirect.ts)", C["fg"]),
    ("  ⎿ ", C["dim"], "Updated src/auth/redirect.ts with 3 additions and 1 removal", C["muted"]),
    ("", None, "", None),
    ("● ", C["orange"], "Now let me run the tests.", C["fg"]),
]


def claude_box(s, x, y, w):
    s.rect(x, y, w, 200, "none", rx=6, stroke=C["accent"])
    lines = [("Bash command", C["fg"], True), ("", None, False), ("  npm test -- redirect", C["fg"], False),
             ("  Run the redirect tests", C["muted"], False), ("", None, False), ("Do you want to proceed?", C["fg"], False)]
    yy = y + 26
    for t, c, b in lines:
        if t:
            s.text(x + 16, yy, t, c, bold=b)
        yy += 19
    s.text(x + 16, yy, "❯ 1. Yes", C["working"])
    s.text(x + 16, yy + 19, "  2. Yes, and don't ask again for npm test", C["fg"])
    s.text(x + 16, yy + 38, "  3. No, and tell Claude what to do differently", C["fg"])


def screen(s, x, y):
    yy = y
    for p, pc, t, tc in CLAUDE:
        if p:
            e = s.text(x, yy, p, pc)
            s.text(e, yy, t, tc)
        yy += 20
    claude_box(s, x, yy + 6, 520)
    return yy + 220


def window(s, W, H):
    s.rect(0, 0, W, H, C["canvas"], rx=12)
    for i, c in enumerate(("#ff5f57", "#febc2e", "#28c840")):
        s.add(f'<circle cx="{20 + i * 20}" cy="20" r="6" fill="{c}"/>')


def hero():
    W, H, SB = 1600, 900, 380
    s = S(W, H)
    window(s, W, H)
    sidebar(s, 0, 0, SB, H)
    s.rect(0, 0, W, H, "none", rx=12)
    for i, c in enumerate(("#ff5f57", "#febc2e", "#28c840")):
        s.add(f'<circle cx="{20 + i * 20}" cy="20" r="6" fill="{c}"/>')
    s.line(SB, 0, SB, H)
    # tabs row
    s.rect(SB + 1, 0, W - SB - 1, 40, C["chrome"])
    tx = SB + 14
    for name, act, st in (("api", True, "blocked"), ("server", False, None), ("+", False, None)):
        tw = len(name) * CW + (40 if st else 20)
        if act:
            s.rect(tx - 6, 8, tw + 10, 24, C["raised"], rx=5)
        if st:
            ic, col = GLYPH[st]
            s.icon(ic, tx, 14, 12, col)
            s.text(tx + 18, 25, name, C["fg"])
        else:
            s.text(tx, 25, name, C["muted"])
        tx += tw + 18
    ax = W - 16
    for nm, st in (("docs", "done"),):
        ax = s.text(ax, 25, nm, C["done"], anchor="end") - len(nm) * CW * 2 - 18
        s.icon("check", ax + 2, 14, 11, C["done"])
    # pane
    px, py, pw, ph = SB + 8, 44, W - SB - 16, H - 52
    s.rect(px, py, pw, ph, C["bg"], rx=6, stroke=C["accent"])
    yy = py + 30
    s.text(px + 16, yy, "> ", C["dim"])
    s.text(px + 34, yy, "after login, take people back to the page they came from", C["fg"], bold=True)
    screen(s, px + 16, yy + 36)
    s.save("hero")


def overlay_base(W, H, OX, OY, OW, OH):
    s = S(W, H)
    window(s, W, H)
    sidebar(s, 0, 0, 380, H)
    s.line(380, 0, 380, H)
    s.rect(0, 0, W, H, "#000000", rx=12, op=0.45)
    s.rect(OX, OY, OW, OH, C["bg"], rx=10, stroke=C["border"])
    return s


def footer(s, x, y, w, hints):
    s.line(x, y - 22, x + w, y - 22)
    fx = x
    for i, (k, d) in enumerate(hints):
        if i:
            fx = s.text(fx + 8, y, "·", C["dim"]) + 8
        fx = s.text(fx, y, k, C["accent"], bold=True)
        fx = s.text(fx + 6, y, d, C["muted"])


def agents():
    W, H = 1600, 900
    OX, OY, OW, OH = 48, 44, W - 96, H - 88
    s = overlay_base(W, H, OX, OY, OW, OH)
    x, w = OX + 26, OW - 52
    yy = OY + 36
    hatch(s, x, yy, w, "Agents")
    yy += 22
    s.text(x, yy, "all spaces · 7", C["muted"])
    bx = x + w
    for ic, col, n in (("check", C["done"], 1), ("working", C["working"], 1), ("bell", C["blocked"], 1)):
        s.text(bx, yy, str(n), col, anchor="end")
        bx -= len(str(n)) * CW + 15
        s.icon(ic, bx, yy - 10, 11, col)
        bx -= 14
    yy += 22
    s.text(x, yy, "›", C["accent"], bold=True)
    s.rect(x + 16, yy - 13, 8, 17, C["fg"])
    s.text(x + 26, yy, "ilter by title, space or branch", C["dim"])
    lw = int(w * 0.46)
    top = yy + 34
    s.line(x + lw + 10, top - 14, x + lw + 10, OY + OH - 70)
    y2 = top
    section(s, x, y2, lw - 10, "pin", "PINNED", 1); y2 += 24
    for th in THREADS["pinned"]:
        y2 = thread(s, x, y2, lw - 10, th, wide=True)
    y2 += 8
    section(s, x, y2, lw - 10, "inbox", "INBOX", 3); y2 += 24
    for th in THREADS["inbox"]:
        y2 = thread(s, x, y2, lw - 10, th, wide=True)
    sy = OY + OH - 200
    s.line(x, sy, x + lw - 10, sy)
    sy += 24
    s.text(x + 4, sy, "▾", C["dim"]); s.icon("working", x + 20, sy - 10, 12, C["working"]); s.text(x + 40, sy, "Working"); s.text(x + lw - 10, sy, "1", C["muted"], anchor="end")
    sy += 24
    thread(s, x, sy, lw - 10, THREADS["working"][0], wide=True)
    sy += 48
    for ic, label, n in (("clock", "Snoozed", 1), ("box", "Settled", 2)):
        s.text(x + 4, sy, "▸", C["dim"]); s.icon(ic, x + 20, sy - 10, 12, C["muted"]); s.text(x + 40, sy, label); s.text(x + lw - 10, sy, str(n), C["muted"], anchor="end")
        sy += 24
    # preview
    px = x + lw + 28
    pw = w - lw - 28
    e = s.text(px, top, "SCREEN · FIX THE LOGIN REDIRECT", C["muted"], bold=True)
    s.line(e + 10, top - 4, px + pw, top - 4)
    s.icon("bell", px, top + 12, 12, C["blocked"])
    e = s.text(px + 18, top + 24, "waiting", C["blocked"])
    s.text(e + 6, top + 24, "· 2m · api › Fix the login redirect · Claude Code", C["dim"])
    e = s.text(px, top + 46, "~/code/api-login", C["dim"])
    e = s.text(e + 14, top + 46, "fix/login", C["muted"])
    s.text(e + 14, top + 46, "↑2", C["dim"])
    s.text(px, top + 68, "Allow Bash: npm test", C["blocked"])
    s.line(px, top + 82, px + pw, top + 82)
    screen(s, px, top + 108)
    footer(s, x, OY + OH - 24, w, (("^y", "approve"), ("^x", "deny"), ("^o", "reply"), ("↵", "go"), ("alt+z", "snooze"), ("^g", "diff"), ("f1", "keys")))
    s.save("agents")


def sessions():
    W, H = 1600, 900
    OX, OY, OW, OH = 112, 126, W - 224, int(H * 0.72)
    s = overlay_base(W, H, OX, OY, OW, OH)
    x, w = OX + 26, OW - 52
    yy = OY + 36
    hatch(s, x, yy, w, "Sessions", None)
    yy += 22
    fx = x
    for i, (ic, label) in enumerate(((None, "all"), ("grid", "spaces"), ("gear", "configs"), ("box", "session files"), ("folder", "zoxide"), ("branch", "machines"))):
        if i:
            fx = s.text(fx + 6, yy, "·", C["dim"]) + 6
        col = C["accent"] if i == 0 else C["muted"]
        if ic:
            s.icon(ic, fx, yy - 10, 11, col); fx += 16
        fx = s.text(fx, yy, label, col, bold=i == 0)
    s.text(x + w, yy, "11", C["muted"], anchor="end")
    yy += 22
    s.text(x + 12, yy, "›", C["accent"], bold=True)
    s.rect(x + 28, yy - 13, 8, 17, C["fg"])
    s.text(x + 38, yy, "ilter sessions...", C["dim"])
    lw = int(w * 0.4)
    top = yy + 34
    s.line(x + lw, top - 16, x + lw, OY + OH - 70)
    rows = [("bell", C["blocked"], "api", True), ("check", C["done"], "docs", False), ("grid", C["working"], "web", False),
            ("grid", C["working"], "dotfiles", False), ("gear", C["muted"], "notes", False), ("box", C["muted"], "monitoring", False),
            ("folder", C["teal"], "~/code/mobile", False), ("folder", C["teal"], "~/code/infra/terraform", False),
            ("folder", C["teal"], "~/code/design-system", False), ("folder", C["teal"], "~/Downloads", False), ("branch", C["muted"], "vps:~/apps/blog", False)]
    ry = top
    for ic, col, name, sel in rows:
        if sel:
            s.rect(x - 4, ry - 15, lw - 8, 22, C["sel"]); s.rect(x - 4, ry - 15, 3, 22, C["accent"])
        s.icon(ic, x + 8, ry - 10, 12, col)
        s.text(x + 28, ry, name, C["fg"], bold=sel)
        if sel:
            s.text(x + lw - 18, ry, "needs you", C["blocked"], size=11, anchor="end")
        ry += 24
    px = x + lw + 22
    code = [("import", " { Router } ", "from", " 'express'"), ("import", " { loginRedirect } ", "from", " './auth/redirect'"), None,
            ("export const", " routes = Router()", "", ""), None, ("", "routes.get('/account', requireLogin, (req, res) => {", "", ""),
            ("", "  res.render('account', { user: req.user })", "", ""), ("", "})", "", ""), None,
            ("function", " requireLogin(req, res, next) {", "", ""), ("  if", " (req.user) ", "return", " next()"),
            ("", "  res.redirect(loginRedirect(req))", "", ""), ("", "}", "", "")]
    cy = top
    for i, c in enumerate(code):
        s.text(px + 18, cy, str(i + 1), C["dim"], anchor="end")
        if c:
            e = s.text(px + 30, cy, c[0], C["accent"])
            e = s.text(e, cy, c[1], C["fg"])
            e = s.text(e, cy, c[2], C["accent"])
            s.text(e, cy, c[3], C["done"] if "'" in c[3] else C["fg"])
        cy += 22
    s.text(px + 30, cy + 16, "NORMAL  src/routes.ts", C["muted"])
    s.text(px + 30 + 46 * CW, cy + 16, "12:3", C["muted"])
    footer(s, x, OY + OH - 24, w, (("↵", "connect"), ("^o", "hide preview"), ("^a ^t ^g ^s ^z ^r", "source"), ("^x", "kill"), ("esc", "quit")))
    s.save("sessions")


def menu():
    W, H = 760, 620
    s = S(W, H)
    window(s, W, H)
    sidebar(s, 0, 0, 380, H, footer=False)
    s.line(380, 0, 380, H)
    mx, my, mw = 300, 170, 280
    items = [("bell", "Approve", "a"), ("x", "Deny", "x"), ("reply", "Reply…", "r"), None, ("pin", "Pin", "p"), ("box", "Settle", "s"),
             ("clock", "Snooze", "z"), ("eye", "Mark seen", "e"), None, ("branch", "Review changes", "d"), ("pr", "Open pull request", "P"),
             ("pencil", "Rename", "R"), ("plus", "New thread here", "n"), None, ("trash", "Close", "X")]
    mh = 46 + sum(9 if it is None else 26 for it in items) + 8
    s.add(f'<rect x="{mx + 4}" y="{my + 8}" width="{mw}" height="{mh}" rx="8" fill="#000" fill-opacity=".35"/>')
    s.rect(mx, my, mw, mh, C["pop"], rx=8, stroke=C["border"])
    s.text(mx + 12, my + 24, "Fix the login redirect", C["accent"], bold=True)
    s.icon("bell", mx + mw - 24, my + 13, 12, C["blocked"])
    s.line(mx, my + 36, mx + mw, my + 36)
    yy = my + 46
    for i, it in enumerate(items):
        if it is None:
            s.line(mx + 8, yy + 4, mx + mw - 8, yy + 4); yy += 9; continue
        ic, label, k = it
        danger = label == "Close"
        if i == 0:
            s.rect(mx + 1, yy, mw - 2, 26, C["sel"])
        s.icon(ic, mx + 12, yy + 6, 13, C["red"] if danger else (C["accent"] if i == 0 else C["muted"]))
        s.text(mx + 34, yy + 17, label, C["red"] if danger else C["fg"])
        s.text(mx + mw - 14, yy + 17, k, C["accent"], bold=True, anchor="end")
        yy += 26
    s.save("menu")


def sidebar_only():
    s = S(400, 760)
    s.rect(0, 0, 400, 760, C["chrome"], rx=12)
    for i, c in enumerate(("#ff5f57", "#febc2e", "#28c840")):
        s.add(f'<circle cx="{20 + i * 20}" cy="20" r="6" fill="{c}"/>')
    sidebar(s, 0, 0, 400, 760)
    s.save("sidebar")


hero(); agents(); sessions(); menu(); sidebar_only()
print("ok")
