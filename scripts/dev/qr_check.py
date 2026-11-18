#!/usr/bin/env python3
"""Independent structural check of a QR symbol against ISO/IEC 18004 (level M, byte mode, versions 1-6).

    python3 scripts/dev/qr_check.py SYMBOL.txt              validate the symbol (first line: payload, then 0/1 rows)
    python3 scripts/dev/qr_check.py --self-test SYMBOL.txt  corrupt copies of it and prove each corruption is caught

The decoder is written from the standard and shares no code with the encoder under test: finder, timing and alignment
patterns; both format-information copies with their BCH(15,5) code; the data mask; de-interleaving into blocks; a
Reed-Solomon syndrome check for every block; and the byte-mode payload, which must equal the first line.
Standard library only. Exit status 1 when a check fails.
"""
import subprocess
import sys
import tempfile

MASKS = [lambda r, c: (r + c) % 2 == 0, lambda r, c: r % 2 == 0, lambda r, c: c % 3 == 0, lambda r, c: (r + c) % 3 == 0,
         lambda r, c: (r // 2 + c // 3) % 2 == 0, lambda r, c: (r * c) % 2 + (r * c) % 3 == 0,
         lambda r, c: ((r * c) % 2 + (r * c) % 3) % 2 == 0, lambda r, c: ((r + c) % 2 + (r * c) % 3) % 2 == 0]
# level M: (ecc codewords per block, data codewords per block) for versions 1..6
BLOCKS = {1: (10, [16]), 2: (16, [28]), 3: (26, [44]), 4: (18, [32, 32]), 5: (24, [43, 43]), 6: (16, [27, 27, 27, 27])}
TOTAL = {1: 26, 2: 44, 3: 70, 4: 100, 5: 134, 6: 172}
ALIGN = {1: [], 2: [6, 18], 3: [6, 22], 4: [6, 26], 5: [6, 30], 6: [6, 34]}


def gf_tables():
    exp, log, x = [0] * 512, [0] * 256, 1
    for i in range(255):
        exp[i], log[x] = x, i
        x <<= 1
        if x & 0x100:
            x ^= 0x11D
    for i in range(255, 512):
        exp[i] = exp[i - 255]
    return exp, log


EXP, LOG = gf_tables()


def gmul(a, b):
    return 0 if a == 0 or b == 0 else EXP[LOG[a] + LOG[b]]


def syndrome(msg, i):
    s = 0
    for v in msg:
        s = gmul(s, EXP[i]) ^ v
    return s


def bch_ok(v):
    x = v
    for i in range(14, 9, -1):
        if x >> i & 1:
            x ^= 0x537 << (i - 10)
    return x == 0


def check(payload, rows):
    """Return (list of failures, summary string)."""
    M = [[c == '1' for c in r] for r in rows]
    N = len(M)
    fails = []
    if N < 21 or any(len(r) != N for r in M) or (N - 17) % 4:
        return ["matrix is not a square QR size"], ""
    V = (N - 17) // 4
    if not 1 <= V <= 6:
        return [f"version {V} is outside the supported range 1-6"], ""

    def need(cond, msg):
        if not cond:
            fails.append(msg)

    for r0, c0 in ((0, 0), (0, N - 7), (N - 7, 0)):
        for dr in range(7):
            for dc in range(7):
                ring = max(abs(dr - 3), abs(dc - 3))
                need(M[r0 + dr][c0 + dc] == (ring != 2 and ring != 4), f"finder pattern wrong at {r0 + dr},{c0 + dc}")
    for i in range(8, N - 8):
        need(M[6][i] == (i % 2 == 0), f"horizontal timing wrong at column {i}")
        need(M[i][6] == (i % 2 == 0), f"vertical timing wrong at row {i}")
    need(M[N - 8][8], "dark module missing")

    fn = [[False] * N for _ in range(N)]
    for r in range(9):
        for c in range(9):
            fn[r][c] = True
    for r in range(9):
        for c in range(N - 8, N):
            fn[r][c] = True
    for r in range(N - 8, N):
        for c in range(9):
            fn[r][c] = True
    for i in range(N):
        fn[6][i] = fn[i][6] = True
    last = ALIGN[V][-1:] if V > 1 else []
    for ar in ALIGN[V]:
        for ac in ALIGN[V]:
            if (ar == 6 and ac == 6) or (ar == 6 and last and ac == last[0]) or (last and ar == last[0] and ac == 6):
                continue
            for dr in range(-2, 3):
                for dc in range(-2, 3):
                    need(M[ar + dr][ac + dc] == (max(abs(dr), abs(dc)) != 1), f"alignment pattern wrong at {ar},{ac}")
                    fn[ar + dr][ac + dc] = True

    def read_copy(seq):
        v = 0
        for r, c in seq:
            v = v << 1 | int(M[r][c])
        return v
    seq1 = [(8, c) for c in range(6)] + [(8, 7), (8, 8), (7, 8)] + [(r, 8) for r in range(5, -1, -1)]
    seq2 = [(N - 1 - i, 8) for i in range(7)] + [(8, N - 8 + i) for i in range(8)]
    f1, f2 = read_copy(seq1) ^ 0x5412, read_copy(seq2) ^ 0x5412
    need(f1 == f2, f"the two format-information copies disagree ({f1:015b} / {f2:015b})")
    need(bch_ok(f1), "format information fails its BCH check")
    mask = (f1 >> 10) & 7
    need(f1 >> 13 == 0b00, "error-correction level is not M")

    bits, up, c = [], True, N - 1
    while c > 0:
        if c == 6:
            c -= 1
        for r in (range(N - 1, -1, -1) if up else range(N)):
            for cc in (c, c - 1):
                if not fn[r][cc]:
                    b = M[r][cc] != MASKS[mask](r, cc)
                    bits.append(int(b))
        up, c = not up, c - 2
    total = TOTAL[V]
    need(len(bits) >= total * 8, "not enough data modules for this version")
    cw = [int(''.join(map(str, bits[i:i + 8])), 2) for i in range(0, total * 8, 8)]
    ec, datas = BLOCKS[V]
    blocks, eccs, pos = [[] for _ in datas], [[] for _ in datas], 0
    for i in range(max(datas)):
        for b, d in enumerate(datas):
            if i < d:
                blocks[b].append(cw[pos])
                pos += 1
    for _ in range(ec):
        for b in range(len(datas)):
            eccs[b].append(cw[pos])
            pos += 1
    for b in range(len(datas)):
        bad = [i for i in range(ec) if syndrome(blocks[b] + eccs[b], i)]
        need(not bad, f"Reed-Solomon block {b} has non-zero syndromes {bad}")
    allbits = ''.join(f'{v:08b}' for blk in blocks for v in blk)
    if allbits[:4] != '0100':
        fails.append(f"mode indicator is {allbits[:4]}, not byte mode")
    elif 12 + 8 * int(allbits[4:12], 2) > len(allbits):
        fails.append("the length field claims more data than the symbol holds")
    else:
        n = int(allbits[4:12], 2)
        text = bytes(int(allbits[12 + 8 * i:20 + 8 * i], 2) for i in range(n)).decode('latin-1')
        need(text == payload, f"decoded payload {text!r} differs from {payload!r}")
    return fails, f"version {V} ({N}x{N}), mask {mask}, {len(datas)} RS block(s), payload decodes correctly"


def load(path):
    lines = open(path).read().split('\n')
    return lines[0], [r for r in lines[1:] if r]


def self_test(path):
    payload, rows = load(path)
    base, _ = check(payload, rows)
    if base:
        print("self-test: the untouched symbol already fails:", base[:2])
        return 1
    N = len(rows)
    corruptions = {
        "finder corner": lambda g: g.__setitem__((0, 0), '0' if g[0][0] == '1' else '1'),
        "timing module": lambda g: g.__setitem__((6, 10), '0' if g[6][10] == '1' else '1'),
        "format bit (copy 1)": lambda g: g.__setitem__((8, 2), '0' if g[8][2] == '1' else '1'),
        "format bit (copy 2)": lambda g: g.__setitem__((N - 1, 8), '0' if g[N - 1][8] == '1' else '1'),
        "dark module": lambda g: g.__setitem__((N - 8, 8), '0'),
        "one data module": lambda g: g.__setitem__((N - 3, N - 3), '0' if g[N - 3][N - 3] == '1' else '1'),
    }
    ok = True
    for name, mut in corruptions.items():
        grid = [list(r) for r in rows]
        class G:
            def __getitem__(self, i):
                return grid[i]
            def __setitem__(self, k, v):
                grid[k[0]][k[1]] = v
        mut(G())
        fails, _ = check(payload, [''.join(r) for r in grid])
        caught = bool(fails)
        ok &= caught
        print(f"  {name:22} {'caught' if caught else 'MISSED'}")
    print("self-test:", "ok" if ok else "FAILED: the checker is blind to some corruption")
    return 0 if ok else 1


def main():
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        return 2
    if args[0] == "--self-test":
        return self_test(args[1])
    payload, rows = load(args[0])
    fails, summary = check(payload, rows)
    if fails:
        print(f"qr-check: FAILED ({len(fails)} problem(s)):")
        for f in fails[:10]:
            print("  -", f)
        return 1
    print(f"qr-check: ok — {summary}")
    print("  checked: finder, timing and alignment patterns; format information x2 (BCH); mask; Reed-Solomon; payload")
    return 0


if __name__ == "__main__":
    sys.exit(main())
