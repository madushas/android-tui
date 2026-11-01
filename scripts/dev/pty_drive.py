#!/usr/bin/env python3
"""Drive the emu TUI in a pty, render with pyte. steps separated by ';' :
 w:SECS wait · snap:LABEL print screen · <KEY,KEY> special keys · anything else = literal text"""
import os, pty, sys, time, select, fcntl, termios, struct, pyte
cols, rows = int(os.environ.get("COLS", 140)), int(os.environ.get("ROWS", 42))
KEYS = {"ENTER": "\r", "TAB": "\t", "ESC": "\x1b", "UP": "\x1b[A", "DOWN": "\x1b[B", "RIGHT": "\x1b[C", "LEFT": "\x1b[D",
        "BTAB": "\x1b[Z", "PGUP": "\x1b[5~", "PGDN": "\x1b[6~", "C-p": "\x10", "C-n": "\x0e", "C-z": "\x1a", "C-b": "\x02", "C-t": "\x14", "C-l": "\x0c", "BS": "\x7f", "SPACE": " "}
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
def dump(label):
    print(f"\n──── {label} " + "─" * 60)
    for l in screen.display: print(l.rstrip())
pump(1.5)
for step in sys.argv[1].split(";"):
    if step.startswith("w:"): pump(float(step[2:])); continue
    if step.startswith("snap"): dump(step); continue
    if step.startswith("<") and step.endswith(">"):
        for k in step[1:-1].split(","): os.write(fd, KEYS.get(k,k).encode()); pump(0.25)
        continue
    os.write(fd, step.encode()); pump(0.3)
print("process alive:", alive)
try: os.kill(pid, 15)
except OSError: pass
