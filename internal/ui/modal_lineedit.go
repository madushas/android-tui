package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type lineEdit struct {
	t    []rune
	p    int
	hist []string
	hi   int
}

func newEdit(s string) *lineEdit { return &lineEdit{t: []rune(s), p: len([]rune(s))} }

func (e *lineEdit) text() string { return string(e.t) }

func (e *lineEdit) set(s string) { e.t, e.p = []rune(s), len([]rune(s)) }

func (e *lineEdit) key(k tea.KeyMsg) bool {
	switch k.String() {
	case "left":
		e.p = max(0, e.p-1)
	case "right":
		e.p = min(len(e.t), e.p+1)
	case "home", "ctrl+a":
		e.p = 0
	case "end", "ctrl+e":
		e.p = len(e.t)
	case "backspace":
		if e.p > 0 {
			e.t = append(e.t[:e.p-1], e.t[e.p:]...)
			e.p--
		}
	case "delete", "ctrl+d":
		if e.p < len(e.t) {
			e.t = append(e.t[:e.p], e.t[e.p+1:]...)
		}
	case "ctrl+u":
		e.t, e.p = append([]rune(nil), e.t[e.p:]...), 0
	case "ctrl+k":
		e.t = e.t[:e.p]
	case "ctrl+w":
		q := e.p
		for q > 0 && e.t[q-1] == ' ' {
			q--
		}
		for q > 0 && e.t[q-1] != ' ' {
			q--
		}
		e.t = append(append([]rune(nil), e.t[:q]...), e.t[e.p:]...)
		e.p = q
	case "up":
		if len(e.hist) == 0 {
			return false
		}
		e.hi = max(0, e.hi-1)
		e.set(e.hist[e.hi])
	case "down":
		if len(e.hist) == 0 {
			return false
		}
		e.hi = min(len(e.hist), e.hi+1)
		if e.hi < len(e.hist) {
			e.set(e.hist[e.hi])
		} else {
			e.set("")
		}
	default:
		switch k.Type {
		case tea.KeyRunes:
			r := []rune(strings.ReplaceAll(string(k.Runes), "\n", " "))
			e.t = append(e.t[:e.p], append(r, e.t[e.p:]...)...)
			e.p += len(r)
		case tea.KeySpace:
			e.t = append(e.t[:e.p], append([]rune{' '}, e.t[e.p:]...)...)
			e.p++
		default:
			return false
		}
	}
	return true
}

// render shows the text with a block cursor, scrolled to fit w cells.
func (e *lineEdit) render(w int, cursor bool) string {
	if w < 2 {
		return ""
	}
	start := 0
	for e.p-start >= w-1 {
		start++
	}
	end := min(len(e.t), start+w-1)
	before, at, after := string(e.t[start:min(e.p, end)]), " ", ""
	if e.p < len(e.t) {
		at = string(e.t[e.p])
		after = string(e.t[min(e.p+1, end):end])
	}
	if !cursor {
		return before + at + after
	}
	return before + sRev.Render(at) + after
}

// ───────────────────────────── confirm ─────────────────────────────
