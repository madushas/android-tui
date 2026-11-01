package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type systemTab struct {
	rowsTab
	mode     int
	settings []core.Setting
	procs    []core.Proc
	services []string
	loaded   [3]bool
	loading  bool
	last     time.Time
}

var sysModes = []string{"settings", "processes", "services"}

func newSystem() Tab {
	s := &systemTab{rowsTab: rowsTab{baseTab: baseTab{ident: "system", name: "System"}}}
	s.build = s.buildRows
	return s
}

func (s *systemTab) Tick(m *Model) tea.Cmd {
	if s.stale(m) {
		s.loaded = [3]bool{}
		s.settings, s.procs, s.services = nil, nil, nil
	}
	t := m.tgt()
	if !t.online() || s.loading {
		return nil
	}
	if !s.loaded[s.mode] || (s.mode == 1 && time.Since(s.last) > 5*time.Second) {
		return s.load(m, s.loaded[s.mode])
	}
	return nil
}

func (s *systemTab) load(m *Model, quiet bool) tea.Cmd {
	mode, gen, ser := s.mode, s.gen, m.tgt().serial()
	script := []string{`for ns in global system secure; do echo "@@$ns"; settings list $ns; done`, "ps -A -o PID,USER,RSS,NAME", "dumpsys -l"}[mode]
	s.loading, s.last = true, time.Now()
	return m.job("system "+sysModes[mode], quiet, func() cont {
		r := core.Sh(ser, script, core.Opt{Timeout: 20 * time.Second, Quiet: quiet})
		return func(m *Model) tea.Cmd {
			s.loading = false
			if gen != s.gen {
				return nil
			}
			switch mode {
			case 0:
				s.settings = core.ParseSettings(r.Out)
			case 1:
				s.procs = core.ParseProcs(r.Out)
			default:
				s.services = nil
				for _, l := range splitLines(r.Out) {
					if len(l) > 2 && l[:2] == "  " {
						s.services = append(s.services, trimS(l))
					}
				}
			}
			s.loaded[mode] = true
			return nil
		}
	})
}

func (s *systemTab) buildRows(m *Model) []row {
	if !m.tgt().online() {
		return nil
	}
	if !s.loaded[s.mode] {
		return []row{hdrRow("loading…")}
	}
	var rows []row
	switch s.mode {
	case 0:
		last := ""
		for _, x := range s.settings {
			if !s.match(x.NS + " " + x.K + " " + x.V) {
				continue
			}
			if x.NS != last {
				rows = append(rows, hdrRow(x.NS))
				last = x.NS
			}
			rows = append(rows, itemRow(x.K+" = "+x.V, "", x))
		}
	case 1:
		for _, p := range s.procs {
			if s.match(p.Name + p.User) {
				rows = append(rows, itemRow(fmt.Sprintf("%6d  %-10s %8s  %s", p.PID, p.User, human(float64(p.RSS)*1024), p.Name), "", p))
			}
		}
	default:
		for _, n := range s.services {
			if s.match(n) {
				rows = append(rows, itemRow(n, "", n))
			}
		}
	}
	if len(rows) == 0 {
		rows = []row{hdrRow("no matches")}
	}
	return rows
}

func (s *systemTab) Hints() []hint {
	switch s.mode {
	case 0:
		return []hint{{"⏎", "edit"}, {"a", "add"}, {"d", "delete"}, {"y", "copy"}, {"m", "view"}, {"/", "filter"}}
	case 1:
		return []hint{{"x", "kill -9"}, {"s", "force-stop"}, {"m", "view"}, {"/", "filter"}, {"r", "reload"}}
	}
	return []hint{{"⏎", "dumpsys"}, {"m", "view"}, {"/", "filter"}}
}

func (s *systemTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	s.head = func(m *Model) string {
		out := ""
		for i, n := range sysModes {
			st := sDim
			if i == s.mode {
				st = sSel
			}
			out += st.Render(" " + n + " ")
		}
		out += sDim.Render("   m switch view")
		if s.flt != "" {
			out += sWarn.Render("   filter: " + s.flt)
		}
		if s.loading {
			out += sDim.Render("   " + spinner(m.frame))
		}
		return out
	}
	return s.viewRows(m, w, h, focused)
}

func (s *systemTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	key := k.String()
	if s.nav(key) {
		return true, nil
	}
	r := s.cur()
	switch key {
	case "m":
		s.mode, s.sel, s.top = (s.mode+1)%3, 0, 0
		return true, nil
	case "/":
		s.askFilter(m)
		return true, nil
	case "r":
		s.loaded[s.mode] = false
		return true, nil
	case "esc":
		if s.flt != "" {
			s.flt = ""
			return true, nil
		}
		return false, nil
	}
	if r == nil || !m.tgt().online() {
		return false, nil
	}
	reload := func(m *Model) tea.Cmd { s.loaded[s.mode] = false; return nil }
	switch s.mode {
	case 0:
		x := r.d.(core.Setting)
		switch key {
		case "enter":
			m.ask(x.NS+" "+x.K, x.V, func(m *Model, v string) tea.Cmd {
				if v == x.V {
					return nil
				}
				return m.apply("Setting "+x.NS+"/"+x.K, core.Op{Kind: "setting", NS: x.NS, Key: x.K}, v, false, func(m *Model, _ core.JEntry) tea.Cmd { return reload(m) })
			})
		case "d":
			m.confirm("Delete setting "+x.NS+"/"+x.K+"?", true, func(m *Model) tea.Cmd {
				return m.apply("Delete setting "+x.NS+"/"+x.K, core.Op{Kind: "setting", NS: x.NS, Key: x.K}, "", true, func(m *Model, _ core.JEntry) tea.Cmd { return reload(m) })
			})
		case "a":
			m.ask("namespace key value  (e.g. global stay_on_while_plugged_in 3)", "", func(m *Model, v string) tea.Cmd {
				f := fieldsN(v, 3)
				if len(f) != 3 {
					return nil
				}
				if f[0] != "global" && f[0] != "system" && f[0] != "secure" {
					m.say("namespace must be global, system or secure")
					return nil
				}
				return m.apply("Setting "+f[0]+"/"+f[1], core.Op{Kind: "setting", NS: f[0], Key: f[1]}, f[2], false, func(m *Model, _ core.JEntry) tea.Cmd { return reload(m) })
			})
		case "y":
			m.say(map[bool]string{true: "copied", false: "no clipboard tool found"}[core.CopyText(x.K+"="+x.V)])
		default:
			return false, nil
		}
	case 1:
		p := r.d.(core.Proc)
		switch key {
		case "x":
			m.confirm(fmt.Sprintf("kill -9 %d (%s)?", p.PID, p.Name), true, func(m *Model) tea.Cmd {
				return m.adbDo("kill", fmt.Sprintf("kill -9 %d", p.PID), reload)
			})
		case "s":
			return true, m.adbDo("force-stop", "am force-stop "+core.Quote(p.Name), nil)
		default:
			return false, nil
		}
	default:
		if key != "enter" {
			return false, nil
		}
		n := r.d.(string)
		return true, m.showAdb("dumpsys "+n, "dumpsys "+core.Quote(n))
	}
	return true, nil
}
