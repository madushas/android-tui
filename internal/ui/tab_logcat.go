package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/madushas/android-tui/internal/core"
)

const lvls = "VDIWEFS"

var lvlStyle = []lipgloss.Style{sDim, sAcc, sOK, sWarn, sErr, sErrB, sDim}

type logStream struct {
	mu     sync.Mutex
	ch     chan string
	cmd    *exec.Cmd
	serial string
	alive  bool
}

func (l *logStream) start(serial, pid string) {
	l.stop()
	argv := []string{"-s", serial, "logcat", "-v", "threadtime"}
	if pid != "" {
		argv = append(argv, "--pid="+pid)
	}
	core.Logf("cmd", "$ adb %s", strings.Join(argv, " "))
	cmd := exec.Command("adb", argv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return
	}
	ch := make(chan string, 16384)
	l.mu.Lock()
	l.cmd, l.ch, l.serial, l.alive = cmd, ch, serial, true
	l.mu.Unlock()
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			select {
			case ch <- strings.TrimRight(sc.Text(), "\r"):
			default: // UI is behind: drop rather than block adb
			}
		}
		_ = cmd.Wait()
		l.mu.Lock()
		if l.cmd == cmd {
			l.alive = false
		}
		l.mu.Unlock()
	}()
}

func (l *logStream) stop() {
	l.mu.Lock()
	c := l.cmd
	l.cmd, l.serial, l.alive = nil, "", false
	l.mu.Unlock()
	if c != nil && c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
	}
}

func (l *logStream) drain(limit int) []string {
	l.mu.Lock()
	ch := l.ch
	l.mu.Unlock()
	var out []string
	for len(out) < limit {
		select {
		case s := <-ch:
			out = append(out, s)
		default:
			return out
		}
	}
	return out
}

type logcatTab struct {
	baseTab
	st     logStream
	all    []core.LogLine
	vw     []core.LogLine
	min    int
	rx     *regexp.Regexp
	follow bool
	paused bool
	pkg    string
	pid    string
	preset string // name of the active core.LogPresets entry ("" = none / manual filter)
	top    int
	page   int
}

func newLogcat() Tab {
	return &logcatTab{baseTab: baseTab{ident: "logcat", name: "Logcat"}, follow: true}
}

func (l *logcatTab) stop() { l.st.stop() }

func (l *logcatTab) match(e core.LogLine) bool {
	if e.Lvl >= 0 && e.Lvl < l.min {
		return false
	}
	if l.pid != "" && e.PID != "" && e.PID != l.pid {
		return false
	}
	return l.rx == nil || l.rx.MatchString(e.Raw)
}

func (l *logcatTab) rebuild() {
	l.vw = l.vw[:0]
	for _, e := range l.all {
		if l.match(e) {
			l.vw = append(l.vw, e)
		}
	}
}

func (l *logcatTab) Bg(m *Model) {
	t := m.tgt()
	if l.st.serial != "" && (!t.online() || l.st.serial != t.serial()) {
		l.st.stop()
		l.all, l.vw = nil, nil
	}
	if lines := l.st.drain(3000); len(lines) > 0 {
		for _, s := range lines {
			e := core.ParseLogLine(s)
			l.all = append(l.all, e)
			if !l.paused && l.match(e) {
				l.vw = append(l.vw, e)
			}
		}
		if c := core.Cfg.LogLines; len(l.all) > c*5/4 {
			l.all = append([]core.LogLine(nil), l.all[len(l.all)-c:]...)
		}
		if c := core.Cfg.LogLines; len(l.vw) > c*5/4 {
			l.vw = append([]core.LogLine(nil), l.vw[len(l.vw)-c:]...)
		}
	}
}

func (l *logcatTab) Tick(m *Model) tea.Cmd {
	if t := m.tgt(); t.online() && m.settled() && l.st.serial != t.serial() {
		l.all, l.vw = nil, nil
		l.st.start(t.serial(), l.pid)
	}
	return nil
}

func (l *logcatTab) restart(m *Model) {
	if t := m.tgt(); t.online() {
		l.all, l.vw = nil, nil
		l.st.start(t.serial(), l.pid)
	}
}

func (l *logcatTab) setApp(m *Model, pkg string) tea.Cmd {
	s := m.tgt().serial()
	if pkg == "" {
		l.pkg, l.pid = "", ""
		l.restart(m)
		return nil
	}
	return m.job("pidof", false, func() cont {
		r := core.Sh(s, "pidof -s "+core.Quote(pkg), core.Opt{Timeout: 5 * time.Second})
		return func(m *Model) tea.Cmd {
			if p := r.Text(); regexp.MustCompile(`^\d+$`).MatchString(p) {
				l.pkg, l.pid = pkg, p
				l.restart(m)
			} else {
				m.report(false, pkg+" is not running")
			}
			return nil
		}
	})
}

func (l *logcatTab) Hints() []hint {
	return []hint{{"v/d/i/w/e", "level"}, {"/", "filter"}, {"P", "presets"}, {"p", "pause"}, {"c", "clear"}, {"a", "app"}, {"s", "save"}, {"⏎", "full line"}, {"G", "follow"}}
}

func (l *logcatTab) Footer() string {
	s := fmt.Sprintf("%d lines", len(l.vw))
	switch {
	case l.paused:
		s += " · paused"
	case l.follow:
		s += " · following"
	}
	return s
}

func (l *logcatTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	head := " " + sDim.Render("level ") + sAccB.Render(string(lvls[l.min])+"+")
	if l.rx != nil {
		f := l.rx.String()
		if l.preset != "" {
			f = "preset " + l.preset
		}
		head += sWarn.Render("   filter: " + f)
	}
	hit := sWarnB.Reverse(true) // regex matches inside the message are drawn inverted
	if l.pkg != "" {
		head += sMag.Render("   app: " + l.pkg)
	}
	if l.paused {
		head += sWarn.Render("   ⏸ paused")
	}
	l.mu()
	rows := h - 1
	l.page = rows
	n := len(l.vw)
	if l.follow && !l.paused {
		l.top = max(0, n-rows)
	}
	l.top = max(0, min(l.top, max(0, n-rows)))
	tw := max(8, min(22, w/6))
	out := []string{head}
	for i := 0; i < rows; i++ {
		k := l.top + i
		if k >= n {
			break
		}
		e := l.vw[k]
		if e.Lvl < 0 {
			out = append(out, " "+sDim.Render(clipS(e.Raw, w-2)))
			continue
		}
		st := lvlStyle[e.Lvl]
		tag := sAcc
		if e.Lvl >= 3 {
			tag = st
		}
		msgSt := sNone
		if e.Lvl >= 3 {
			msgSt = st
		}
		out = append(out, " "+sDim.Render(padR(clipS(e.TS, 12), 12))+" "+st.Bold(true).Render(string(lvls[e.Lvl]))+" "+tag.Render(padL(clipS(e.Tag, tw), tw))+" "+highlight(clipS(e.Msg, w-tw-19), l.rx, msgSt, hit))
	}
	return out
}

func (l *logcatTab) mu() {}

// highlight renders s in the base style with every regex match drawn in the hit style.
func highlight(s string, rx *regexp.Regexp, base, hit lipgloss.Style) string {
	if rx == nil || s == "" {
		return base.Render(s)
	}
	var sb strings.Builder
	pos := 0
	for _, r := range rx.FindAllStringIndex(s, -1) {
		if r[1] <= r[0] || r[0] < pos {
			continue // skip empty or overlapping matches
		}
		sb.WriteString(base.Render(s[pos:r[0]]))
		sb.WriteString(hit.Render(s[r[0]:r[1]]))
		pos = r[1]
	}
	sb.WriteString(base.Render(s[pos:]))
	return sb.String()
}

func (l *logcatTab) Scroll(d int) {
	l.follow = false
	l.top = max(0, l.top+d)
}

func (l *logcatTab) Typing() bool { return false }

func (l *logcatTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	n, page := len(l.vw), max(1, l.page)
	switch s {
	case "up", "k":
		l.follow, l.top = false, max(0, l.top-1)
	case "down", "j":
		l.top = min(max(0, n-page), l.top+1)
	case "pgup":
		l.follow, l.top = false, max(0, l.top-page)
	case "pgdown":
		l.top = min(max(0, n-page), l.top+page)
	case "home", "g":
		l.follow, l.top = false, 0
	case "end", "G":
		l.follow, l.paused = true, false
		l.rebuild()
	case "v", "d", "i", "w", "e":
		l.min = strings.Index("vdiwef", s)
		l.rebuild()
	case "/":
		cur := ""
		if l.rx != nil {
			cur = l.rx.String()
		}
		m.ask("Filter logcat (text or regex, empty clears)", cur, func(m *Model, v string) tea.Cmd {
			l.rx, l.preset = nil, ""
			if v != "" {
				re, err := regexp.Compile("(?i)" + v)
				if err != nil {
					re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(v))
				}
				l.rx = re
			}
			l.rebuild()
			return nil
		})
	case "P":
		// preset picker: first row clears; the rest come from core.LogPresets (data only)
		names := []string{"(clear preset)"}
		for _, pr := range core.LogPresets {
			names = append(names, fmt.Sprintf("%s  (≥%s)", pr.Name, string(lvls[pr.MinLevel])))
		}
		m.choose("Logcat preset", names, func(m *Model, i int) tea.Cmd {
			l.rx, l.preset, l.min = nil, "", 0
			if i > 0 {
				pr := core.LogPresets[i-1]
				l.preset, l.min = pr.Name, pr.MinLevel
				if pr.Regex != "." { // "." = level-only preset, nothing to highlight
					if re, err := regexp.Compile("(?i)" + pr.Regex); err == nil {
						l.rx = re
					}
				}
			}
			l.rebuild()
			return nil
		})
	case "p":
		l.paused = !l.paused
		if !l.paused {
			l.rebuild()
			l.follow = true
		}
	case "c":
		if t := m.tgt(); t.online() {
			core.Adb(t.serial(), core.Opt{Timeout: 10 * time.Second}, "logcat", "-c")
		}
		l.all, l.vw = nil, nil
	case "r":
		l.restart(m)
	case "a":
		m.ask("Only this app's process (package name, empty = all)", l.pkg, func(m *Model, v string) tea.Cmd { return l.setApp(m, strings.TrimSpace(v)) })
	case "s":
		core.MkdirP(core.Cfg.ShotDir)
		f := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("logcat-%s-%s.txt", m.tgt().name(), time.Now().Format("20060102-150405")))
		var sb strings.Builder
		for _, e := range l.vw {
			sb.WriteString(e.Raw + "\n")
		}
		_ = os.WriteFile(f, []byte(sb.String()), 0o644)
		m.report(true, fmt.Sprintf("saved %d lines to %s", len(l.vw), f))
	case "enter":
		if n > 0 {
			i := l.top
			if l.follow {
				i = min(n-1, l.top+page-1)
			}
			m.viewText("Logcat line", wrap(l.vw[i].Raw, 110))
		}
	default:
		return false, nil
	}
	return true, nil
}
