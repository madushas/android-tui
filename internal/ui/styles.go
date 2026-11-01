// Package ui is the Bubble Tea front end.
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Theme uses the terminal's own 16 colours so it follows your palette.
var (
	cOK    = lipgloss.Color("2")
	cWarn  = lipgloss.Color("3")
	cErr   = lipgloss.Color("1")
	cAcc   = lipgloss.Color("6")
	cBlue  = lipgloss.Color("4")
	cMag   = lipgloss.Color("5")
	cGrey  = lipgloss.Color("8")
	sOK    = lipgloss.NewStyle().Foreground(cOK)
	sOKB   = sOK.Bold(true)
	sWarn  = lipgloss.NewStyle().Foreground(cWarn)
	sWarnB = sWarn.Bold(true)
	sErr   = lipgloss.NewStyle().Foreground(cErr)
	sErrB  = sErr.Bold(true)
	sAcc   = lipgloss.NewStyle().Foreground(cAcc)
	sAccB  = sAcc.Bold(true)
	sBlue  = lipgloss.NewStyle().Foreground(cBlue)
	sMag   = lipgloss.NewStyle().Foreground(cMag)
	sDim   = lipgloss.NewStyle().Foreground(cGrey)
	sBold  = lipgloss.NewStyle().Bold(true)
	sSel   = lipgloss.NewStyle().Background(cBlue).Foreground(lipgloss.Color("15")).Bold(true)
	sRev   = lipgloss.NewStyle().Reverse(true)
	sQR    = lipgloss.NewStyle().Background(lipgloss.Color("15")).Foreground(lipgloss.Color("0"))
	sNone  = lipgloss.NewStyle()
)

var styleByName = map[string]lipgloss.Style{"": sNone, "dim": sDim, "ok": sOK, "okb": sOKB, "err": sErr, "errb": sErrB, "warn": sWarn,
	"acc": sAcc, "accb": sAccB, "bold": sBold, "blue": sBlue, "mag": sMag}

func sty(name string) lipgloss.Style { return styleByName[name] }

const spinFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

func spinner(frame int) string { r := []rune(spinFrames); return string(r[frame%len(r)]) }

// ───────────── width-aware string helpers ─────────────

func sw(s string) int { return ansi.StringWidth(s) }

// fit truncates/pads a (possibly styled) string to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if sw(s) > w {
		s = ansi.Truncate(s, w, "") + "\x1b[0m"
	}
	if p := w - sw(s); p > 0 {
		s += strings.Repeat(" ", p)
	}
	return s
}

func clipS(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= n {
		return s
	}
	return runewidth.Truncate(s, n, "…")
}

func padL(s string, n int) string {
	if d := n - runewidth.StringWidth(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func padR(s string, n int) string {
	if d := n - runewidth.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// cutLeft drops the first n cells of a styled string, keeping the active SGR state.
func cutLeft(s string, n int) string {
	var out strings.Builder
	cur := ""
	skipped := 0
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
				j++
			}
			seq := string(rs[i : j+1])
			if j < len(rs) && rs[j] == 'm' {
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					cur = ""
				} else {
					cur += seq
				}
			}
			if skipped >= n {
				out.WriteString(seq)
			}
			i = j
			continue
		}
		w := runewidth.RuneWidth(rs[i])
		if skipped < n {
			skipped += w
			if skipped > n { // wide rune straddles the cut
				out.WriteString(cur + " ")
			}
			if skipped >= n {
				out.WriteString(cur)
			}
			continue
		}
		out.WriteRune(rs[i])
	}
	return out.String()
}

// overlay draws box lines on top of bg at (x,y).
func overlay(bg []string, box []string, x, y int) []string {
	for i, bl := range box {
		yy := y + i
		if yy < 0 || yy >= len(bg) {
			continue
		}
		w := sw(bl)
		left := bg[yy]
		if sw(left) < x {
			left += strings.Repeat(" ", x-sw(left))
		} else {
			left = ansi.Truncate(left, x, "")
		}
		bg[yy] = left + "\x1b[0m" + bl + "\x1b[0m" + cutLeft(bg[yy], x+w)
	}
	return bg
}

// ───────────── boxes ─────────────

// box draws a hairline panel with the title embedded in the top border.
func box(title string, body []string, w, h int, focused bool, footer string) []string {
	if w < 4 || h < 2 {
		return make([]string, max(h, 0))
	}
	bs := sDim
	if focused {
		bs = sAcc
	}
	iw := w - 2
	top := bs.Render("┌─") + " " + title + " "
	if rest := w - 1 - sw(top); rest > 0 {
		top += bs.Render(strings.Repeat("─", rest))
	} else {
		top = ansi.Truncate(top, w-1, "") + "\x1b[0m"
	}
	top += bs.Render("┐")
	out := []string{top}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		out = append(out, bs.Render("│")+fit(l, iw)+bs.Render("│"))
	}
	bot := ""
	if footer != "" {
		f := " " + clipS(footer, w-8) + " "
		bot = bs.Render(strings.Repeat("─", max(0, iw-sw(f)-1))) + sDim.Render(f) + bs.Render("─")
	} else {
		bot = bs.Render(strings.Repeat("─", iw))
	}
	return append(out, bs.Render("└")+bot+bs.Render("┘"))
}

func hjoin(a, b []string) []string {
	n := max(len(a), len(b))
	out := make([]string, n)
	for i := range out {
		if i < len(a) {
			out[i] = a[i]
		}
		if i < len(b) {
			out[i] += b[i]
		}
	}
	return out
}

// ───────────── charts ─────────────

const blocks = " ▁▂▃▄▅▆▇█"

func loadStyle(p float64) lipgloss.Style {
	switch {
	case p < 60:
		return sOK
	case p < 85:
		return sWarn
	}
	return sErr
}

func bar(frac float64, w int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := int(frac*float64(w) + 0.5)
	return strings.Repeat("█", n) + strings.Repeat("░", w-n)
}

// graph draws a btop-style block area graph (newest value at the right).
func graph(vals []float64, w, h int, vmax float64) []string {
	if len(vals) > w {
		vals = vals[len(vals)-w:]
	}
	pad := make([]float64, w-len(vals))
	vals = append(pad, vals...)
	if vmax <= 0 {
		vmax = 1
	}
	rb := []rune(blocks)
	rows := make([]string, 0, h)
	for r := h - 1; r >= 0; r-- {
		var sb strings.Builder
		for _, v := range vals {
			f := v / vmax
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			lvl := int(f*float64(h*8)+0.5) - r*8
			sb.WriteRune(rb[max(0, min(8, lvl))])
		}
		rows = append(rows, sb.String())
	}
	return rows
}

func spark(vals []float64, w int, vmax float64) string { return graph(vals, w, 1, vmax)[0] }

func human(n float64) string {
	u := []string{"B", "K", "M", "G", "T"}
	i := 0
	for n >= 1024 && i < len(u)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", int(n))
	}
	return fmt.Sprintf("%.1f%s", n, u[i])
}

type lipglossStyle = lipgloss.Style
