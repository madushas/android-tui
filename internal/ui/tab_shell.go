package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type shellTab struct {
	baseTab
	ed      *lineEdit
	out     []line
	top     int
	follow  bool
	running bool
	histF   string
}

func newShell() Tab {
	s := &shellTab{baseTab: baseTab{ident: "shell", name: "Shell"}, follow: true, histF: filepath.Join(core.CacheDir, "shell_history")}
	var hist []string
	if b, err := os.ReadFile(s.histF); err == nil {
		hist = strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(hist) > 500 {
			hist = hist[len(hist)-500:]
		}
	}
	s.ed = newEdit("")
	s.ed.hist, s.ed.hi = hist, len(hist)
	s.out = []line{{"Type a command and press Enter — it runs as `adb shell <cmd>`.  Ctrl-T opens a real interactive shell.", "dim"}}
	return s
}

func (s *shellTab) Typing() bool { return true }
func (s *shellTab) Hints() []hint {
	return []hint{{"⏎", "run"}, {"↑/↓", "history"}, {"PgUp/PgDn", "scroll"}, {"C-l", "clear"}, {"C-t", "real shell"}, {"C-n/C-b", "next/prev tab"}}
}
func (s *shellTab) Footer() string { return fmt.Sprintf("%d lines", len(s.out)) }
func (s *shellTab) Scroll(d int)   { s.follow, s.top = false, max(0, s.top+d) }

func (s *shellTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	rows := h - 2
	n := len(s.out)
	if s.follow {
		s.top = max(0, n-rows)
	}
	s.top = max(0, min(s.top, max(0, n-rows)))
	var out []string
	for i := 0; i < rows; i++ {
		if k := s.top + i; k < n {
			out = append(out, " "+sty(s.out[k].s).Render(clipS(s.out[k].t, w-2)))
		} else {
			out = append(out, "")
		}
	}
	prompt := sAccB.Render("$")
	if s.running {
		prompt = sAcc.Render(spinner(m.frame))
	}
	out = append(out, sDim.Render(strings.Repeat("─", w)), " "+prompt+" "+s.ed.render(w-4, focused))
	return out
}

func (s *shellTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "enter":
		if c := strings.TrimSpace(s.ed.text()); c != "" && !s.running {
			return true, s.run(m, c)
		}
	case "pgup":
		s.follow, s.top = false, max(0, s.top-10)
	case "pgdown":
		s.top += 10
	case "ctrl+l":
		s.out = nil
	case "ctrl+t":
		if t := m.tgt(); t.online() {
			return true, m.ext(false, "adb", "-s", t.serial(), "shell")
		}
	case "esc", "tab", "shift+tab":
		return false, nil
	default:
		s.ed.key(k)
	}
	return true, nil
}

func (s *shellTab) run(m *Model, cmd string) tea.Cmd {
	ser := m.tgt().serial()
	s.ed.hist = append(s.ed.hist, cmd)
	s.ed.hi = len(s.ed.hist)
	s.ed.set("")
	core.MkdirP(core.CacheDir)
	h := s.ed.hist
	if len(h) > 500 {
		h = h[len(h)-500:]
	}
	_ = os.WriteFile(s.histF, []byte(strings.Join(h, "\n")+"\n"), 0o644)
	s.out = append(s.out, line{"$ " + cmd, "accb"})
	s.follow, s.running = true, true
	return m.job("shell", false, func() cont {
		r := core.Sh(ser, cmd, core.Opt{Timeout: 60 * time.Second})
		return func(m *Model) tea.Cmd {
			s.running = false
			for _, l := range strings.Split(strings.TrimRight(r.Out+r.Err, "\n"), "\n") {
				s.out = append(s.out, line{l, ""})
			}
			if !r.OK() {
				s.out = append(s.out, line{fmt.Sprintf("[exit %d]", r.RC), "err"})
			}
			if len(s.out) > 5000 {
				s.out = s.out[len(s.out)-5000:]
			}
			return nil
		}
	})
}
