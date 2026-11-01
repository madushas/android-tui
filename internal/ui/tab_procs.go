package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

var procSorts = []string{"cpu", "memory", "pid", "name"}

type procsTab struct {
	rowsTab
	procs   []core.TopProc
	sortBy  int
	loading bool
	last    time.Time
	n       int
}

func newProcs() Tab {
	p := &procsTab{rowsTab: rowsTab{baseTab: baseTab{ident: "procs", name: "Procs"}}}
	p.build = p.buildRows
	return p
}

func (p *procsTab) Tick(m *Model) tea.Cmd {
	if p.stale(m) {
		p.procs, p.last = nil, time.Time{}
	}
	t := m.tgt()
	if !t.online() || p.loading || time.Since(p.last) < 4*time.Second || !m.settled() {
		return nil
	}
	p.loading, p.last = true, time.Now()
	s, gen := t.serial(), p.gen
	withMem := p.n%2 == 0
	p.n++
	return m.job("processes", true, func() cont {
		top := core.Sh(s, "top -b -n 1 -m 80", core.Opt{Timeout: 20 * time.Second, Quiet: true})
		procs := core.ParseTop(top.Out)
		if len(procs) == 0 { // very old toolbox: fall back to ps
			procs = psFallback(s)
		}
		var pss map[int]int64
		if withMem {
			pss = core.ParsePSS(core.Sh(s, "dumpsys meminfo | head -n 600", core.Opt{Timeout: 30 * time.Second, Quiet: true}).Out)
		}
		return func(m *Model) tea.Cmd {
			p.loading = false
			if gen != p.gen {
				return nil
			}
			old := map[int]int64{}
			for _, x := range p.procs {
				old[x.PID] = x.PSS
			}
			for i := range procs {
				if pss != nil {
					procs[i].PSS = pss[procs[i].PID]
				} else {
					procs[i].PSS = old[procs[i].PID]
				}
			}
			p.procs = procs
			return nil
		}
	})
}

func psFallback(s string) []core.TopProc {
	var out []core.TopProc
	for _, x := range core.ParseProcs(core.Sh(s, "ps -A -o PID,USER,RSS,NAME", core.Opt{Quiet: true}).Out) {
		out = append(out, core.TopProc{PID: x.PID, User: x.User, RSS: x.RSS, Name: x.Name})
	}
	return out
}

func pkgOf(name string) string {
	if i := strings.Index(name, ":"); i > 0 {
		name = name[:i]
	}
	if strings.Contains(name, ".") && !strings.HasPrefix(name, "/") && !strings.Contains(name, " ") {
		return name
	}
	return ""
}

func (p *procsTab) sorted() []core.TopProc {
	v := append([]core.TopProc(nil), p.procs...)
	switch procSorts[p.sortBy] {
	case "cpu":
		sort.SliceStable(v, func(i, j int) bool { return v[i].CPU > v[j].CPU })
	case "memory":
		sort.SliceStable(v, func(i, j int) bool { return max(v[i].PSS, v[i].RSS) > max(v[j].PSS, v[j].RSS) })
	case "pid":
		sort.SliceStable(v, func(i, j int) bool { return v[i].PID < v[j].PID })
	default:
		sort.SliceStable(v, func(i, j int) bool { return v[i].Name < v[j].Name })
	}
	return v
}

func (p *procsTab) buildRows(m *Model) []row {
	var rows []row
	for _, x := range p.sorted() {
		if !p.match(x.Name + x.User) {
			continue
		}
		mem := "-"
		if x.PSS > 0 {
			mem = human(float64(x.PSS) * 1024)
		}
		rss := "-"
		if x.RSS > 0 {
			rss = human(float64(x.RSS) * 1024)
		}
		cpu := fmt.Sprintf("%5.1f", x.CPU)
		plain := fmt.Sprintf("%6d %-10s %s%% %8s %8s  %s", x.PID, clipS(x.User, 10), cpu, mem, rss, x.Name)
		cs := loadStyle(x.CPU * 4) // a core at 25% is already busy on a phone
		rows = append(rows, row{text: plain, sty: sDim.Render(fmt.Sprintf("%6d %-10s ", x.PID, clipS(x.User, 10))) + cs.Render(cpu+"%") + fmt.Sprintf(" %8s %8s  ", mem, rss) + x.Name, d: x})
	}
	return rows
}

func (p *procsTab) Hints() []hint {
	return []hint{{"o", "sort"}, {"k", "kill -9"}, {"f", "force-stop"}, {"d", "disable app"}, {"b", "bucket"}, {"w", "un-whitelist doze"}, {"j", "cancel jobs"}, {"l", "logcat"}, {"/", "filter"}}
}

func (p *procsTab) Footer() string { return fmt.Sprintf("%d processes", len(p.procs)) }

func (p *procsTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	p.head = func(m *Model) string {
		s := sDim.Render(fmt.Sprintf("%6s %-10s %6s %8s %8s  ", "PID", "USER", "CPU", "PSS", "RSS")) + sAccB.Render("sorted by "+procSorts[p.sortBy])
		if st := m.stat(); st != nil && st.MemTotal > 0 {
			s += sDim.Render(fmt.Sprintf("   RAM %s / %s", human(float64(st.MemUsed)), human(float64(st.MemTotal))))
		}
		if p.loading {
			s += sDim.Render("  " + spinner(m.frame))
		}
		return s
	}
	return p.viewRows(m, w, h, focused)
}

func (p *procsTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if p.nav(s) {
		return true, nil
	}
	if s == "o" {
		p.sortBy = (p.sortBy + 1) % len(procSorts)
		return true, nil
	}
	if s == "/" {
		p.askFilter(m)
		return true, nil
	}
	if s == "r" {
		p.last = time.Time{}
		return true, nil
	}
	r := p.cur()
	if r == nil {
		return false, nil
	}
	x := r.d.(core.TopProc)
	pkg := pkgOf(x.Name)
	ser := m.tgt().serial()
	needPkg := func() bool {
		if pkg == "" {
			m.say(x.Name + " isn't an app process")
		}
		return pkg != ""
	}
	switch s {
	case "k":
		m.confirm(fmt.Sprintf("kill -9 %d (%s)?", x.PID, x.Name), true, func(m *Model) tea.Cmd {
			return m.adbDo("kill", fmt.Sprintf("kill -9 %d", x.PID), func(m *Model) tea.Cmd { p.last = time.Time{}; return nil })
		})
	case "f":
		if needPkg() {
			return true, m.adbDo("force-stop "+pkg, "am force-stop "+core.Quote(pkg), func(m *Model) tea.Cmd { p.last = time.Time{}; return nil })
		}
	case "d":
		if needPkg() {
			m.confirm("Disable "+pkg+" for user 0? (journalled, revert from the Journal tab)", false, func(m *Model) tea.Cmd {
				return m.apply("Disable "+pkg, core.Op{Kind: "pkg", Pkg: pkg}, "disabled", false, func(m *Model, e core.JEntry) tea.Cmd { p.last = time.Time{}; return nil })
			})
		}
	case "b":
		if needPkg() {
			b := []string{"active", "working_set", "frequent", "rare", "restricted"}
			m.choose("Standby bucket for "+pkg, b, func(m *Model, i int) tea.Cmd {
				return m.apply("Bucket "+pkg+" → "+b[i], core.Op{Kind: "bucket", Pkg: pkg}, b[i], false, nil)
			})
		}
	case "w":
		if needPkg() {
			return true, m.apply("Doze whitelist −"+pkg, core.Op{Kind: "doze", Pkg: pkg}, "none", false, nil)
		}
	case "j":
		if needPkg() {
			return true, m.job("cancel jobs", false, func() cont {
				res := core.Sh(ser, "cmd jobscheduler cancel "+core.Quote(pkg), core.Opt{Timeout: 20 * time.Second})
				return func(m *Model) tea.Cmd {
					m.report(res.OK(), "jobscheduler cancel "+pkg+": "+core.AdbMsg(res))
					return nil
				}
			})
		}
	case "l":
		if needPkg() {
			return true, m.logcatFor(pkg)
		}
	default:
		return false, nil
	}
	return true, nil
}
