package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/madushas/android-tui/internal/core"
)

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
