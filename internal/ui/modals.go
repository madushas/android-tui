package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// cont is a continuation that runs on the UI goroutine.
type cont func(m *Model) tea.Cmd

type modal interface {
	update(m *Model, msg tea.Msg) (closed bool, then cont)
	view(w, h int) []string
}

// frame draws a bordered modal box.
func frame(title string, body []string, w int, footer string, danger bool) []string {
	t := sAccB.Render(title)
	if danger {
		t = sErrB.Render(title)
	}
	b := box(t, body, w, len(body)+2, true, footer)
	return b
}

func centerXY(boxLines []string, W, H int) (int, int) {
	w := 0
	if len(boxLines) > 0 {
		w = sw(boxLines[0])
	}
	return max(0, (W-w)/2), max(0, (H-len(boxLines))/2)
}

// ───────────────────────────── line editor ─────────────────────────────

func padAll(ls []string, w int) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = fit(l, w)
	}
	return out
}

func wrap(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		if cur != "" && sw(cur)+1+sw(word) > w {
			out = append(out, cur)
			cur = ""
		}
		if cur != "" {
			cur += " "
		}
		cur += word
	}
	if cur != "" || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// ───────────────────────────── input ─────────────────────────────
