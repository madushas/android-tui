#!/usr/bin/env python3
"""Drive the TUI in a pty, render with pyte. steps separated by ';' :
 w:SECS wait · snap:LABEL print screen · <KEY,KEY> special keys · anything else = literal text
 mouse: click|dclick|rclick@TEXT (cell where TEXT first appears on screen; suffix |L or |R = left/right pane only)
        or click|dclick|rclick:X,Y (0-based)
        wheelup|wheeldown@TEXT or :X,Y   (SGR mouse reports; the app must have mouse support on)"""
import os, pty, re, sys, time, select, fcntl, termios, struct, pyte
cols, rows = int(os.environ.get("COLS", 140)), int(os.environ.get("ROWS", 42))
KEYS = {"ENTER": "\r", "TAB": "\t", "ESC": "\x1b", "UP": "\x1b[A", "DOWN": "\x1b[B", "RIGHT": "\x1b[C", "LEFT": "\x1b[D",
        "BTAB": "\x1b[Z", "PGUP": "\x1b[5~", "HOME": "\x1b[H", "END": "\x1b[F", "PGDN": "\x1b[6~", "C-p": "\x10", "C-n": "\x0e", "C-o": "\x0f", "C-z": "\x1a", "C-b": "\x02", "C-t": "\x14", "C-l": "\x0c", "BS": "\x7f", "SPACE": " "}
pid, fd = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    os.environ.update(TERM=os.environ.get("DTERM","xterm-256color"),COLORTERM="truecolor", LANG="C.UTF-8", LC_ALL="C.UTF-8")
    os.execv(os.environ["ATUI_BIN"], [os.environ["ATUI_BIN"]])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
screen = pyte.Screen(cols, rows); stream = pyte.ByteStream(screen)
alive = True
def pump(t):
    global alive
    end = time.time() + t
    while time.time() < end and alive:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try: d = os.read(fd, 65536)
            except OSError: alive = False; return
            if not d: alive = False; return
            if b"\x1b]11;?" in d: os.write(fd, b"\x1b]11;rgb:0000/0000/0000\x1b\\")
            if b"\x1b[6n" in d: os.write(fd, b"\x1b[1;1R")
            stream.feed(d)
def sel_rows(x0=0):
    """Rows (0-based) that carry a highlight (reverse video or a non-default background) right of column x0."""
    out = []
    for y in range(rows):
        row = screen.buffer[y]
        if any((row[x].reverse or row[x].bg != "default") and row[x].data.strip() or (row[x].reverse and True) for x in range(x0, cols) if x in row):
            out.append(y)
    return out
def dump(label):
    print(f"\n──── {label} " + "─" * 60)
    for l in screen.display: print(l.rstrip())
    print("__highlight_rows__ " + ",".join(map(str, sel_rows(41))))
def mouse(btn, x, y):
    os.write(fd, f"\x1b[<{btn};{x+1};{y+1}M".encode()); pump(0.05)
    os.write(fd, f"\x1b[<{btn};{x+1};{y+1}m".encode()); pump(0.25)
def where(text):
    """Cell of the first occurrence of TEXT. A '|L' / '|R' suffix restricts the search to the left (x<41) or right pane."""
    side = None
    if text[-2:] in ("|L", "|R"): text, side = text[:-2], text[-1]
    for y, l in enumerate(screen.display):
        lo, hi = (0, 41) if side == "L" else (41, cols) if side == "R" else (0, cols)
        i = l.find(text, lo, hi)
        if i >= 0: return i + min(2, len(text) // 2), y
    print(f"mouse target not on screen: {text!r}"); sys.exit(3)
MOUSE = re.compile(r"^(click|dclick|rclick|wheelup|wheeldown)(?:@(.+)|:(\d+),(\d+))$")
pump(1.5)
for step in sys.argv[1].split(";"):
    mm = MOUSE.match(step)
    if mm:
        kind, text, X, Y = mm.groups()
        x, y = where(text) if text else (int(X), int(Y))
        if kind == "click": mouse(0, x, y)
        elif kind == "rclick": mouse(2, x, y)
        elif kind == "dclick": mouse(0, x, y); mouse(0, x, y)
        elif kind == "wheelup": os.write(fd, f"\x1b[<64;{x+1};{y+1}M".encode()); pump(0.25)
        else: os.write(fd, f"\x1b[<65;{x+1};{y+1}M".encode()); pump(0.25)
        continue
    if step.startswith("w:"): pump(float(step[2:])); continue
    if step.startswith("snap"): dump(step); continue
    if step.startswith("<") and step.endswith(">"):
        for k in step[1:-1].split(","): os.write(fd, KEYS.get(k,k).encode()); pump(0.25)
        continue
    os.write(fd, step.encode()); pump(0.3)
print("process alive:", alive)
try: os.kill(pid, 15)
except OSError: pass
