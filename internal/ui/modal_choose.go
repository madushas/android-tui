package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type item struct {
	text string
	dim  bool
}

type chooseModal struct {
	title      string
	items      []item
	multi      bool
	marks      map[int]bool
	typeahead  bool
	allowEmpty bool
	flt        *lineEdit
	filtering  bool
	sel, top   int
	page       int
	pick       func(m *Model, idx int) tea.Cmd
	picks      func(m *Model, idx []int) tea.Cmd
	listRow    int             // box row of the first list item (set by view; for mouse hit-testing)
	vrows      int             // visible list rows (set by view)
	fuzzy      bool            // rank by fuzzy score instead of AND-substring filtering (the action palette)
	boost      func(i int) int // extra score for item i (frecency)
}

func (c *chooseModal) vis() []int {
	if q := c.flt.text(); c.fuzzy && strings.TrimSpace(q) != "" {
		type scored struct{ i, s int }
		var ss []scored
		for i, it := range c.items {
			if s, ok := fuzzy(q, it.text); ok {
				if c.boost != nil {
					s += c.boost(i)
				}
				if it.dim {
					s -= 60 // unavailable actions rank below available ones
				}
				ss = append(ss, scored{i, s})
			}
		}
		sort.SliceStable(ss, func(a, b int) bool { return ss[a].s > ss[b].s })
		out := make([]int, len(ss))
		for k, x := range ss {
			out[k] = x.i
		}
		return out
	}
	toks := strings.Fields(strings.ToLower(c.flt.text()))
	var out []int
	for i, it := range c.items {
		l := strings.ToLower(it.text)
		okk := true
		for _, t := range toks {
			okk = okk && strings.Contains(l, t)
		}
		if okk {
			out = append(out, i)
		}
	}
	return out
}

// choose confirms the current selection (or the marked set, for multi-select lists).
func (c *chooseModal) choose(vis []int) (bool, cont) {
	if len(vis) == 0 && (!c.multi || !c.allowEmpty) {
		return false, nil
	}
	if c.multi {
		var idx []int
		for i := range c.marks {
			if c.marks[i] {
				idx = append(idx, i)
			}
		}
		sort.Ints(idx)
		if len(idx) == 0 && !c.allowEmpty {
			idx = []int{vis[c.sel]}
		}
		return true, func(m *Model) tea.Cmd { return c.picks(m, idx) }
	}
	i := vis[c.sel]
	return true, func(m *Model) tea.Cmd { return c.pick(m, i) }
}

// mouse: wheel scrolls, click selects (toggles in multi-select lists), double-click confirms, click outside closes.
func (c *chooseModal) mouse(m *Model, ev mouseEv, box rect) (bool, cont) {
	vis := c.vis()
	in := box.in(ev.x, ev.y)
	switch ev.kind {
	case mWheel:
		c.sel = max(0, min(c.sel+ev.dy*wheelStep, len(vis)-1))
		return false, nil
	case mRight:
		return !in, nil
	}
	if !in {
		return true, nil
	}
	row := ev.y - box.y - c.listRow
	if row < 0 || row >= c.vrows || c.top+row >= len(vis) {
		return false, nil
	}
	c.sel = c.top + row
	switch {
	case ev.kind == mDouble:
		return c.choose(vis)
	case c.multi:
		c.marks[vis[c.sel]] = !c.marks[vis[c.sel]]
	}
	return false, nil
}

func (c *chooseModal) update(m *Model, msg tea.Msg) (bool, cont) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	vis := c.vis()
	s := k.String()
	choose := func() (bool, cont) { return c.choose(vis) }
	switch {
	case s == "up" || s == "ctrl+p" || (!c.filtering && s == "k"):
		c.sel--
	case s == "down" || s == "ctrl+n" || (!c.filtering && s == "j"):
		c.sel++
	case s == "pgup":
		c.sel -= c.page
	case s == "pgdown":
		c.sel += c.page
	case s == "enter":
		return choose()
	case s == "esc" || s == "ctrl+c":
		if c.flt.text() != "" {
			c.flt.set("")
			c.sel = 0
			c.filtering = c.typeahead
		} else if c.filtering && !c.typeahead {
			c.filtering = false
		} else {
			return true, nil
		}
	case c.filtering:
		if c.flt.key(k) {
			c.sel, c.top = 0, 0
		}
	case s == "q":
		return true, nil
	case s == "/":
		c.filtering = true
	case s == "g" || s == "home":
		c.sel = 0
	case s == "G" || s == "end":
		c.sel = len(vis) - 1
	case c.multi && s == " " && len(vis) > 0:
		c.marks[vis[c.sel]] = !c.marks[vis[c.sel]]
		c.sel++
	case c.multi && s == "a":
		all := true
		for _, i := range vis {
			all = all && c.marks[i]
		}
		for _, i := range vis {
			c.marks[i] = !all
		}
	}
	c.sel = max(0, min(c.sel, len(vis)-1))
	return false, nil
}

func (c *chooseModal) view(w, h int) []string {
	vis := c.vis()
	iw := 46
	for _, it := range c.items {
		iw = max(iw, min(100, sw(it.text)+8))
	}
	iw = min(w-6, max(iw, sw(c.title)+8))
	rows := min(h-8, max(3, len(vis)))
	var body []string
	if c.filtering || c.flt.text() != "" {
		body = append(body, " "+sAcc.Render("/ ")+c.flt.render(iw-5, c.filtering))
	}
	c.page, c.vrows, c.listRow = rows, rows, len(body)+1 // +1: the frame's top border
	c.sel = max(0, min(c.sel, len(vis)-1))
	if c.sel < c.top {
		c.top = c.sel
	}
	if c.sel >= c.top+rows {
		c.top = c.sel - rows + 1
	}
	for r := 0; r < rows; r++ {
		k := c.top + r
		if k >= len(vis) {
			body = append(body, "")
			continue
		}
		i := vis[k]
		pre := " "
		if c.multi {
			pre = " [ ] "
			if c.marks[i] {
				pre = " [x] "
			}
		}
		line := fit(clipS(pre+c.items[i].text, iw), iw)
		switch {
		case k == c.sel:
			line = sSel.Render(line)
		case c.items[i].dim:
			line = sDim.Render(line)
		}
		body = append(body, line)
	}
	foot := fmt.Sprintf("%d/%d", min(c.sel+1, len(vis)), len(vis))
	if c.multi {
		foot = "Space toggle · a all · " + foot
	}
	return frame(" "+c.title+" ", padAll(body, iw), iw+2, foot, false)
}

// ───────────────────────────── viewer / streams ─────────────────────────────
