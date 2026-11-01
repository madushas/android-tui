// Package qr is a small dependency-free QR encoder (byte mode, ECC level M, versions 1-6) —
// enough for an adb "pair with QR code" payload.
package qr

import (
	"errors"
	"strings"
)

// ec level M: ecc codewords per block, number of blocks, data codewords per block.
var tbl = map[int][3]int{1: {10, 1, 16}, 2: {16, 1, 28}, 3: {26, 1, 44}, 4: {18, 2, 32}, 5: {24, 2, 43}, 6: {16, 4, 27}}
var align = map[int][]int{2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34}}
var gfExp [512]int
var gfLog [256]int

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i], gfLog[x] = x, i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func mul(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

func rsECC(data []int, n int) []int {
	gen := []int{1}
	for i := 0; i < n; i++ {
		nxt := make([]int, len(gen)+1)
		for j, c := range gen {
			nxt[j] ^= c
			nxt[j+1] ^= mul(c, gfExp[i])
		}
		gen = nxt
	}
	rem := make([]int, n)
	for _, b := range data {
		f := b ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for i := 0; i < n; i++ {
			rem[i] ^= mul(gen[i+1], f)
		}
	}
	return rem
}

// Encode returns the module matrix (true = dark).
func Encode(text string) ([][]bool, error) {
	raw := []byte(text)
	ver := 0
	for v := 1; v <= 6; v++ {
		if t := tbl[v]; len(raw) <= t[1]*t[2]-2 {
			ver = v
			break
		}
	}
	if ver == 0 {
		return nil, errors.New("payload too long for the built-in QR encoder")
	}
	ecn, nb, dpb := tbl[ver][0], tbl[ver][1], tbl[ver][2]
	capw := nb * dpb
	var bits strings.Builder
	w := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			bits.WriteByte(byte('0' + (v>>i)&1))
		}
	}
	w(4, 4)
	w(len(raw), 8)
	for _, c := range raw {
		w(int(c), 8)
	}
	for i := 0; i < 4 && bits.Len() < capw*8; i++ {
		bits.WriteByte('0')
	}
	for bits.Len()%8 != 0 {
		bits.WriteByte('0')
	}
	bs := bits.String()
	var cw []int
	for i := 0; i < len(bs); i += 8 {
		v := 0
		for _, ch := range bs[i : i+8] {
			v = v<<1 | int(ch-'0')
		}
		cw = append(cw, v)
	}
	for k := 0; len(cw) < capw; k++ {
		cw = append(cw, []int{0xEC, 0x11}[k%2])
	}
	blocks := make([][]int, nb)
	eccs := make([][]int, nb)
	for i := range blocks {
		blocks[i] = cw[i*dpb : (i+1)*dpb]
		eccs[i] = rsECC(blocks[i], ecn)
	}
	var final []int
	for i := 0; i < dpb; i++ {
		for _, b := range blocks {
			final = append(final, b[i])
		}
	}
	for i := 0; i < ecn; i++ {
		for _, e := range eccs {
			final = append(final, e[i])
		}
	}
	size := 17 + 4*ver
	mod, fn := grid(size), grid(size)
	setf := func(x, y int, v bool) { mod[y][x], fn[y][x] = v, true }
	for i := 0; i < size; i++ {
		setf(6, i, i%2 == 0)
		setf(i, 6, i%2 == 0)
	}
	finder := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := cx+dx, cy+dy
				if x >= 0 && x < size && y >= 0 && y < size {
					d := max(abs(dx), abs(dy))
					setf(x, y, d != 2 && d != 4)
				}
			}
		}
	}
	finder(3, 3)
	finder(size-4, 3)
	finder(3, size-4)
	pos := align[ver]
	for _, cy := range pos {
		for _, cx := range pos {
			last := pos[len(pos)-1]
			if (cx == 6 && cy == 6) || (cx == 6 && cy == last) || (cx == last && cy == 6) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					setf(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	format := func(mask int) {
		rem := mask
		for i := 0; i < 10; i++ {
			rem = (rem << 1) ^ ((rem >> 9) * 0x537)
		}
		b := (mask<<10 | rem) ^ 0x5412
		g := func(i int) bool { return (b>>i)&1 == 1 }
		for i := 0; i < 6; i++ {
			setf(8, i, g(i))
		}
		setf(8, 7, g(6))
		setf(8, 8, g(7))
		setf(7, 8, g(8))
		for i := 9; i < 15; i++ {
			setf(14-i, 8, g(i))
		}
		for i := 0; i < 8; i++ {
			setf(size-1-i, 8, g(i))
		}
		for i := 8; i < 15; i++ {
			setf(8, size-15+i, g(i))
		}
		setf(8, size-8, true)
	}
	format(0)
	var data []bool
	for _, c := range final {
		for i := 7; i >= 0; i-- {
			data = append(data, (c>>i)&1 == 1)
		}
	}
	k, up := 0, true
	for x := size - 1; x > 0; x -= 2 {
		if x == 6 {
			x--
		}
		for j := 0; j < size; j++ {
			y := j
			if up {
				y = size - 1 - j
			}
			for _, xx := range []int{x, x - 1} {
				if !fn[y][xx] {
					mod[y][xx] = k < len(data) && data[k]
					k++
				}
			}
		}
		up = !up
	}
	masks := []func(x, y int) bool{
		func(x, y int) bool { return (x+y)%2 == 0 }, func(x, y int) bool { return y%2 == 0 },
		func(x, y int) bool { return x%3 == 0 }, func(x, y int) bool { return (x+y)%3 == 0 },
		func(x, y int) bool { return (x/3+y/2)%2 == 0 }, func(x, y int) bool { return x*y%2+x*y%3 == 0 },
		func(x, y int) bool { return (x*y%2+x*y%3)%2 == 0 }, func(x, y int) bool { return ((x+y)%2+x*y%3)%2 == 0 },
	}
	var best [][]bool
	bestP := -1
	for mk := 0; mk < 8; mk++ {
		m := grid(size)
		for y := 0; y < size; y++ {
			copy(m[y], mod[y])
			for x := 0; x < size; x++ {
				if !fn[y][x] && masks[mk](x, y) {
					m[y][x] = !m[y][x]
				}
			}
		}
		saved := mod
		mod = m
		format(mk)
		cand := mod
		mod = saved
		if p := penalty(cand); bestP < 0 || p < bestP {
			best, bestP = cand, p
		}
	}
	return best, nil
}

func grid(n int) [][]bool {
	g := make([][]bool, n)
	for i := range g {
		g[i] = make([]bool, n)
	}
	return g
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func penalty(m [][]bool) int {
	n := len(m)
	p := 0
	line := func(get func(i int) bool) {
		run := 1
		var sb strings.Builder
		for i := 0; i < n; i++ {
			if i > 0 {
				if get(i) == get(i-1) {
					run++
				} else {
					if run >= 5 {
						p += 3 + run - 5
					}
					run = 1
				}
			}
			if get(i) {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
		if run >= 5 {
			p += 3 + run - 5
		}
		s := sb.String()
		for _, pat := range []string{"10111010000", "00001011101"} {
			for i := strings.Index(s, pat); i != -1; {
				p += 40
				j := strings.Index(s[i+1:], pat)
				if j == -1 {
					break
				}
				i += 1 + j
			}
		}
	}
	for a := 0; a < n; a++ {
		a := a
		line(func(i int) bool { return m[a][i] })
		line(func(i int) bool { return m[i][a] })
	}
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			if m[y][x] == m[y][x+1] && m[y][x] == m[y+1][x] && m[y][x] == m[y+1][x+1] {
				p += 3
			}
		}
	}
	dark := 0
	for _, r := range m {
		for _, v := range r {
			if v {
				dark++
			}
		}
	}
	total := n * n
	p += ((abs(dark*20-total*10)+total-1)/total - 1) * 10
	return p
}

// Lines renders the matrix with half-block characters, quiet zone included (dark module = black).
func Lines(m [][]bool, quiet int) []string {
	n := len(m)
	get := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < n && y < n && m[y][x]
	}
	tot := n + 2*quiet
	var out []string
	for y := 0; y < tot; y += 2 {
		var sb strings.Builder
		for x := 0; x < tot; x++ {
			t, b := get(x, y), get(x, y+1)
			switch {
			case t && b:
				sb.WriteString("█")
			case t:
				sb.WriteString("▀")
			case b:
				sb.WriteString("▄")
			default:
				sb.WriteString(" ")
			}
		}
		out = append(out, sb.String())
	}
	return out
}
