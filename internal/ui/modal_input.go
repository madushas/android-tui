package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type inputModal struct {
	title, hint string
	ed          *lineEdit
	complete    func(string) []string
	cands       []string
	ci          int
	ok          func(m *Model, v string) tea.Cmd
}

func completePath(text string) []string {
	t := core.NormPath(text)
	m, _ := filepath.Glob(t + "*")
	sort.Strings(m)
	for i, p := range m {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			m[i] = p + "/"
		}
	}
	return m
}

func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

func (i *inputModal) update(m *Model, msg tea.Msg) (bool, cont) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "enter":
		v := i.ed.text()
		return true, func(m *Model) tea.Cmd { return i.ok(m, v) }
	case "esc", "ctrl+c":
		return true, nil
	case "tab":
		if i.complete == nil {
			return false, nil
		}
		if len(i.cands) > 0 && i.ci >= 0 && i.ed.text() == i.cands[i.ci] {
			i.ci = (i.ci + 1) % len(i.cands)
			i.ed.set(i.cands[i.ci])
			return false, nil
		}
		i.cands, i.ci = i.complete(i.ed.text()), -1
		if len(i.cands) > 0 {
			if cp := commonPrefix(i.cands); len(cp) > len(strings.TrimSpace(i.ed.text())) || len(i.cands) == 1 {
				i.ed.set(cp)
			} else {
				i.ci = 0
				i.ed.set(i.cands[0])
			}
		}
		return false, nil
	}
	if i.ed.key(k) {
		i.cands, i.ci = nil, -1
	}
	return false, nil
}

func (i *inputModal) view(w, h int) []string {
	iw := min(w-6, max(58, sw(i.title)+8))
	body := []string{" " + i.ed.render(iw-2, true), " " + sDim.Render(clipS(i.hint, iw-2))}
	for n, c := range i.cands {
		if n >= 6 {
			break
		}
		s := sDim
		if n == i.ci {
			s = sAcc
		}
		body = append(body, " "+s.Render(clipS(c, iw-2)))
	}
	body = append(body, " "+sDim.Render("Enter ok · Esc cancel"+map[bool]string{true: " · Tab complete"}[i.complete != nil]))
	return frame(" "+i.title+" ", padAll(body, iw), iw+2, "", false)
}

// ───────────────────────────── choose ─────────────────────────────
