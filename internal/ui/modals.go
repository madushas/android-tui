package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/madushas/android-tui/internal/core"
	"github.com/madushas/android-tui/internal/qr"
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

type confirmModal struct {
	msg    string
	danger bool
	yes    cont
}

func (c *confirmModal) update(m *Model, msg tea.Msg) (bool, cont) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch s := k.String(); {
		case s == "y" || s == "Y" || (s == "enter" && !c.danger):
			return true, c.yes
		case s == "n" || s == "N" || s == "esc" || s == "q" || s == "enter" || s == "ctrl+c":
			return true, nil
		}
	}
	return false, nil
}

func (c *confirmModal) view(w, h int) []string {
	iw := min(w-6, 62)
	var body []string
	for _, para := range strings.Split(c.msg, "\n") {
		for _, l := range wrap(para, iw-2) {
			body = append(body, " "+l)
		}
	}
	if c.danger && len(body) > 0 {
		body[0] = sErr.Render(body[0])
	}
	body = append(body, "", " "+sDim.Render("y yes   n no"+map[bool]string{false: "   Enter = yes"}[c.danger]))
	t := " Confirm "
	if c.danger {
		t = " Danger "
	}
	return frame(t, padAll(body, iw), iw+2, "", c.danger)
}

// typedConfirmModal is for catastrophic actions (wipe, delete, revert-all): the word must be typed before Enter works.
type typedConfirmModal struct {
	msg, word, buf string
	yes            cont
}

func (c *typedConfirmModal) update(m *Model, msg tea.Msg) (bool, cont) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch s := k.String(); {
	case s == "esc" || s == "ctrl+c":
		return true, nil
	case s == "enter":
		if strings.EqualFold(strings.TrimSpace(c.buf), c.word) {
			return true, c.yes
		}
	case s == "backspace":
		if r := []rune(c.buf); len(r) > 0 {
			c.buf = string(r[:len(r)-1])
		}
	case k.Type == tea.KeyRunes:
		c.buf += string(k.Runes)
	}
	return false, nil
}

func (c *typedConfirmModal) view(w, h int) []string {
	iw := min(w-6, 62)
	var body []string
	for _, para := range strings.Split(c.msg, "\n") {
		for _, l := range wrap(para, iw-2) {
			body = append(body, " "+l)
		}
	}
	if len(body) > 0 {
		body[0] = sErr.Render(body[0])
	}
	state := sDim.Render("(type it to enable Enter)")
	if strings.EqualFold(strings.TrimSpace(c.buf), c.word) {
		state = sOK.Render("✓ Enter to confirm")
	}
	body = append(body, "", " Type "+sErrB.Render(strings.ToUpper(c.word))+" to confirm:", " > "+c.buf+sRev.Render(" ")+"  "+state, "", " "+sDim.Render("Esc cancels"))
	return frame(" Danger ", padAll(body, iw), iw+2, "", true)
}

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

func (c *chooseModal) update(m *Model, msg tea.Msg) (bool, cont) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	vis := c.vis()
	s := k.String()
	choose := func() (bool, cont) {
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
	c.page = rows
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

type line struct{ t, s string }

type stream struct {
	mu     sync.Mutex
	lines  []line
	rep    bool
	done   bool
	val    any
	cancel bool
	proc   *os.Process
}

func (s *stream) emit(t string, replace bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := line{t: ansi.Strip(t)}
	if replace && s.rep && len(s.lines) > 0 {
		s.lines[len(s.lines)-1] = l
	} else {
		s.lines = append(s.lines, l)
	}
	s.rep = replace
}

func (s *stream) addStyled(t, st string) {
	s.mu.Lock()
	s.lines = append(s.lines, line{t, st})
	s.rep = false
	s.mu.Unlock()
}

func (s *stream) snapshot() ([]line, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]line(nil), s.lines...), s.done
}

func (s *stream) cancelled() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.cancel }

func (s *stream) requestCancel() {
	s.mu.Lock()
	s.cancel = true
	p := s.proc
	s.mu.Unlock()
	if p != nil {
		_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	}
}

type viewerModal struct {
	title     string
	static    []line
	src       *stream // live source (work/task) — nil for static viewers
	top, left int
	follow    bool
	page      int
	search    string
	searching *lineEdit
	note      string
	autoclose bool
	then      func(m *Model, v any) tea.Cmd
}

func newViewer(title string, lines []string) *viewerModal {
	v := &viewerModal{title: title}
	for _, l := range lines {
		v.static = append(v.static, line{t: l})
	}
	return v
}

func newStyledViewer(title string, lines []line) *viewerModal {
	return &viewerModal{title: title, static: lines}
}

func (v *viewerModal) all() ([]line, bool) {
	if v.src != nil {
		return v.src.snapshot()
	}
	return v.static, true
}

func (v *viewerModal) update(m *Model, msg tea.Msg) (bool, cont) {
	lines, done := v.all()
	if _, ok := msg.(tickMsg); ok {
		if v.src != nil && done && v.autoclose && v.src.val != nil {
			return true, v.finish()
		}
		return false, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		if mm, ok := msg.(tea.MouseMsg); ok {
			switch mm.Button {
			case tea.MouseButtonWheelUp:
				v.top, v.follow = max(0, v.top-3), false
			case tea.MouseButtonWheelDown:
				v.top += 3
			}
		}
		return false, nil
	}
	n := len(lines)
	if v.searching != nil {
		switch k.String() {
		case "enter":
			v.search = v.searching.text()
			v.searching = nil
			v.find(lines, v.top-1, 1)
		case "esc":
			v.searching = nil
		default:
			v.searching.key(k)
		}
		return false, nil
	}
	v.note = ""
	s := k.String()
	switch s {
	case "q", "esc", "ctrl+c":
		if v.src != nil && !done {
			v.src.requestCancel()
			return false, nil
		}
		return true, v.finish()
	case "j", "down":
		v.top++
	case "k", "up":
		v.top, v.follow = v.top-1, false
	case "pgdown", " ":
		v.top += v.page
	case "pgup":
		v.top, v.follow = v.top-v.page, false
	case "g", "home":
		v.top, v.follow = 0, false
	case "G", "end":
		v.top, v.follow = max(0, n-v.page), v.src != nil
	case "right":
		v.left += 8
	case "left":
		v.left = max(0, v.left-8)
	case "/":
		v.searching = newEdit(v.search)
	case "n":
		v.find(lines, v.top, 1)
	case "N":
		v.find(lines, v.top, -1)
	case "y":
		var sb strings.Builder
		for _, l := range lines {
			sb.WriteString(l.t + "\n")
		}
		v.note = map[bool]string{true: "copied", false: "no clipboard tool found"}[core.CopyText(sb.String())]
	}
	return false, nil
}

func (v *viewerModal) finish() cont {
	if v.then == nil {
		return nil
	}
	var val any
	if v.src != nil {
		val = v.src.val
	}
	return func(m *Model) tea.Cmd { return v.then(m, val) }
}

func (v *viewerModal) find(lines []line, start, step int) {
	q := strings.ToLower(v.search)
	if q == "" || len(lines) == 0 {
		return
	}
	for d := 1; d <= len(lines); d++ {
		k := ((start+step*d)%len(lines) + len(lines)) % len(lines)
		if strings.Contains(strings.ToLower(lines[k].t), q) {
			v.top, v.follow = k, false
			return
		}
	}
	v.note = "no match for '" + v.search + "'"
}

func (v *viewerModal) view(w, h int) []string {
	lines, done := v.all()
	iw, ih := w-6, h-4
	v.page = max(1, ih-3)
	n := len(lines)
	if v.follow || (v.src != nil && !done && v.top >= n-v.page-1) {
		v.top = max(0, n-v.page)
	}
	v.top = max(0, min(v.top, max(0, n-v.page)))
	sq := strings.ToLower(v.search)
	var body []string
	for i := 0; i < v.page; i++ {
		k := v.top + i
		if k >= n {
			body = append(body, "")
			continue
		}
		t := lines[k].t
		if v.left > 0 {
			rs := []rune(t)
			if v.left < len(rs) {
				t = string(rs[v.left:])
			} else {
				t = ""
			}
		}
		t = clipS(strings.ReplaceAll(t, "\t", "    "), iw-2)
		st := sty(lines[k].s)
		out := st.Render(t)
		if sq != "" {
			if j := strings.Index(strings.ToLower(t), sq); j >= 0 {
				out = st.Render(t[:j]) + sRev.Render(t[j:j+len(sq)]) + st.Render(t[j+len(sq):])
			}
		}
		body = append(body, " "+out)
	}
	foot := ""
	switch {
	case v.searching != nil:
		foot = sAcc.Render("/") + v.searching.render(iw-4, true)
	case v.note != "":
		foot = sWarn.Render(v.note)
	case v.src != nil && !done:
		foot = sAcc.Render(spinner(int(time.Now().UnixMilli()/100))) + sDim.Render(" running · Esc cancels")
	case v.src != nil:
		foot = sDim.Render("done · q closes · / search · y copy")
	default:
		foot = sDim.Render("j/k scroll · / search · n/N next · y copy · q close")
	}
	body = append(body, "", " "+foot)
	return box(sAccB.Render(" "+v.title+" "), padAll(body, iw), iw+2, len(body)+2, true, fmt.Sprintf("%d/%d", min(n, v.top+v.page), n))
}

// ───────────────────────────── background work ─────────────────────────────

// newWork runs fn in a goroutine and streams its output into a viewer.
func newWork(title string, autoclose bool, fn func(emit core.Emit, cancelled func() bool) any, then func(m *Model, v any) tea.Cmd) *viewerModal {
	s := &stream{}
	v := &viewerModal{title: title, src: s, follow: true, autoclose: autoclose, then: then}
	go func() {
		val := fn(s.emit, s.cancelled)
		s.mu.Lock()
		s.val, s.done = val, true
		s.mu.Unlock()
	}()
	return v
}

var crRE = regexp.MustCompile(`[\r\n]`)

// newTask streams one or more commands (stdout+stderr) into a viewer; value is the final exit code.
func newTask(title string, argvs [][]string, then func(m *Model, rc int, lines []string) tea.Cmd) *viewerModal {
	var s *stream
	v := newWork(title, false, func(emit core.Emit, cancelled func() bool) any {
		rc := 0
		for _, argv := range argvs {
			emitStyled := func(t, st string) { s.addStyled(t, st) }
			emitStyled("$ "+core.ShellJoin(argv), "dim")
			core.Logf("cmd", "$ %s", core.ShellJoin(argv))
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			pr, pw, err := os.Pipe()
			if err != nil {
				return 1
			}
			cmd.Stdout, cmd.Stderr = pw, pw
			if err := cmd.Start(); err != nil {
				_ = pw.Close()
				_ = pr.Close()
				emitStyled(err.Error(), "err")
				return 127
			}
			_ = pw.Close() // the child holds its own copy of the write end
			s.mu.Lock()
			s.proc = cmd.Process
			s.mu.Unlock()
			buf := make([]byte, 4096)
			var acc []byte
			flush := func(seg []byte, sep byte) {
				if len(strings.TrimSpace(string(seg))) > 0 {
					emit(string(seg), sep == '\r') // a trailing \r means "progress line: overwrite it"
				}
			}
			for {
				n, err := pr.Read(buf)
				acc = append(acc, buf[:n]...)
				for {
					loc := crRE.FindIndex(acc)
					if loc == nil {
						break
					}
					flush(acc[:loc[0]], acc[loc[0]])
					acc = acc[loc[1]:]
				}
				if err != nil {
					break
				}
			}
			_ = pr.Close()
			if len(acc) > 0 {
				flush(acc, '\n')
			}
			if err := cmd.Wait(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					rc = ee.ExitCode()
				} else {
					rc = 1
				}
			}
			if cancelled() {
				break
			}
		}
		st := "ok"
		if rc != 0 {
			st = "err"
		}
		s.addStyled(fmt.Sprintf("── finished (exit %d) ──", rc), st)
		return rc
	}, nil)
	s = v.src
	v.then = func(m *Model, val any) tea.Cmd {
		if then == nil {
			return nil
		}
		ls, _ := s.snapshot()
		var txt []string
		for _, l := range ls {
			txt = append(txt, l.t)
		}
		rc, _ := val.(int)
		return then(m, rc, txt)
	}
	return v
}

// ───────────────────────────── QR pairing ─────────────────────────────

type qrModal struct {
	q       *core.QRPair
	qr      []string
	then    func(m *Model, ok bool) tea.Cmd
	lines   []string
	fin, ok bool
}

func newQRModal(then func(m *Model, ok bool) tea.Cmd) (*qrModal, error) {
	q, err := core.NewQRPair()
	if err != nil {
		return nil, err
	}
	mat, err := qr.Encode(q.Payload)
	if err != nil {
		return nil, err
	}
	go q.Run()
	return &qrModal{q: q, qr: qr.Lines(mat, 2), then: then}, nil
}

func (q *qrModal) update(m *Model, msg tea.Msg) (bool, cont) {
	switch msg := msg.(type) {
	case tickMsg:
		q.lines, q.fin, q.ok = q.q.State()
	case tea.KeyMsg:
		if q.fin || msg.String() == "esc" || msg.String() == "ctrl+c" {
			q.q.Cancel()
			ok := q.ok
			return true, func(m *Model) tea.Cmd { return q.then(m, ok) }
		}
	}
	return false, nil
}

func (q *qrModal) view(w, h int) []string {
	qw := sw(q.qr[0])
	iw := min(w-4, max(qw+6, 62))
	need := len(q.qr) + 9
	if h < need+2 {
		return frame(" QR pairing ", padAll([]string{" " + sErr.Render(fmt.Sprintf("terminal too short — need %d rows (have %d). Esc cancels.", need+2, h))}, iw), iw+2, "", false)
	}
	body := []string{" " + sDim.Render("Phone: Settings → Developer options → Wireless debugging →"), " " + sDim.Render("Pair device with QR code, then scan:")}
	for _, l := range q.qr {
		body = append(body, fit(strings.Repeat(" ", (iw-qw)/2)+sQR.Render(l), iw))
	}
	ls := q.lines
	if len(ls) > 4 {
		ls = ls[len(ls)-4:]
	}
	for _, l := range ls {
		st := sNone
		if strings.Contains(l, "✓") {
			st = sOKB
		} else if strings.Contains(l, "timed out") {
			st = sErr
		}
		body = append(body, " "+st.Render(clipS(l, iw-2)))
	}
	for len(body) < len(q.qr)+8 {
		body = append(body, "")
	}
	if q.fin {
		body = append(body, " "+sDim.Render("any key closes"))
	} else {
		body = append(body, " "+sAcc.Render(spinner(int(time.Now().UnixMilli()/100)))+sDim.Render(" listening via mDNS · Esc cancels"))
	}
	return frame(" Pair with QR code ", padAll(body, iw), iw+2, "", false)
}

var _ = io.EOF
