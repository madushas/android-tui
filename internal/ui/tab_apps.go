package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

var scopes = []string{"user", "system", "enabled", "disabled", "removed", "all"}
var sorts = []string{"name", "installer", "version"}
var installers = map[string]string{"com.android.vending": "play", "com.android.shell": "adb", "org.fdroid.fdroid": "f-droid", "com.aurora.store": "aurora",
	"com.google.android.packageinstaller": "pkginst", "com.android.packageinstaller": "pkginst", "com.amazon.venezia": "amazon",
	"com.sec.android.app.samsungapps": "galaxy", "null": "—"}

type appsTab struct {
	rowsTab
	items   []core.Pkg
	scope   int
	sort    int
	loading bool
	err     string
	detail  map[string][]string
	want    string
	marks   map[string]bool
	lw      int
}

func newApps() Tab {
	a := &appsTab{rowsTab: rowsTab{baseTab: baseTab{ident: "apps", name: "Apps"}}, detail: map[string][]string{}, marks: map[string]bool{}}
	a.build = a.buildRows
	return a
}

func (a *appsTab) reset() {
	a.items, a.detail, a.marks = nil, map[string][]string{}, map[string]bool{}
	a.sel, a.top = 0, 0
}

func (a *appsTab) load(m *Model) tea.Cmd {
	s, gen := m.tgt().serial(), a.gen
	a.loading = true
	return m.job("list packages", false, func() cont {
		r := core.Sh(s, core.PMScript, core.Opt{Timeout: 40 * time.Second})
		return func(m *Model) tea.Cmd {
			if gen != a.gen {
				return nil
			}
			a.loading = false
			if strings.TrimSpace(r.Out) == "" && !r.OK() {
				a.err = r.Why()
				return nil
			}
			a.err, a.items = "", core.ParsePM(r.Out)
			return nil
		}
	})
}

func (a *appsTab) visible() []core.Pkg {
	sc := scopes[a.scope]
	var v []core.Pkg
	for _, p := range a.items {
		var keep bool
		switch {
		case sc == "removed":
			keep = p.Removed
		case p.Removed:
			keep = false
		default:
			keep = map[string]bool{"user": !p.System, "system": p.System, "enabled": !p.Disabled, "disabled": p.Disabled, "all": true}[sc]
		}
		if keep && a.match(p.Name) {
			v = append(v, p)
		}
	}
	switch sorts[a.sort] {
	case "installer":
		sort.SliceStable(v, func(i, j int) bool {
			return v[i].Installer < v[j].Installer || (v[i].Installer == v[j].Installer && v[i].Name < v[j].Name)
		})
	case "version":
		sort.SliceStable(v, func(i, j int) bool { return v[i].VC > v[j].VC })
	default:
		sort.Slice(v, func(i, j int) bool { return v[i].Name < v[j].Name })
	}
	return v
}

func (a *appsTab) buildRows(m *Model) []row {
	var rows []row
	for _, p := range a.visible() {
		inst := installers[p.Installer]
		if inst == "" && p.Installer != "" {
			inst = p.Installer[strings.LastIndex(p.Installer, ".")+1:]
		}
		inst = clipS(inst, 8)
		flags := " "
		if p.System {
			flags = "S"
		}
		if p.Disabled {
			flags += "D"
		}
		vc := ""
		if p.VC > 0 {
			vc = fmt.Sprint(p.VC)
		}
		mark := "   "
		if a.marks[p.Name] {
			mark = "[x]"
		}
		right := fmt.Sprintf("%-8s %7s %-2s", inst, vc, flags)
		w := max(16, a.lw-2-4-1-19-2)
		name := clipS(p.Name, w)
		plain := mark + " " + padR(name, w) + " " + right
		st := sNone
		if p.Removed {
			st = sDim
		}
		rows = append(rows, row{text: plain, sty: sOKB.Render(mark) + " " + st.Render(padR(name, w)) + " " + sDim.Render(right), d: p})
	}
	return rows
}

func (a *appsTab) curPkg() *core.Pkg {
	if r := a.cur(); r != nil {
		p := r.d.(core.Pkg)
		return &p
	}
	return nil
}

func (a *appsTab) targets() []string {
	vis := map[string]bool{}
	for _, p := range a.visible() {
		vis[p.Name] = true
	}
	var out []string
	for k := range a.marks {
		if vis[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	if len(out) > 0 {
		return out
	}
	if p := a.curPkg(); p != nil {
		return []string{p.Name}
	}
	return nil
}

func (a *appsTab) Tick(m *Model) tea.Cmd {
	if a.stale(m) {
		a.reset()
		if m.tgt().online() {
			return a.load(m)
		}
		return nil
	}
	p := a.curPkg()
	t := m.tgt()
	if t.online() && p != nil && !p.Removed && m.settled() && a.want != p.Name && a.detail[p.Name] == nil {
		name, gen, s := p.Name, a.gen, t.serial()
		a.want = name
		q := core.Quote(name)
		script := fmt.Sprintf(`dumpsys package %s | grep -E 'versionName|targetSdk|firstInstallTime|lastUpdateTime|codePath' | head -n 8; echo "pid=$(pidof %s)"`, q, q)
		return m.job("details "+name, true, func() cont {
			r := core.Sh(s, script, core.Opt{Timeout: 15 * time.Second, Quiet: true})
			return func(m *Model) tea.Cmd {
				a.want = ""
				if gen == a.gen {
					var ls []string
					for _, l := range strings.Split(r.Out, "\n") {
						if l = strings.TrimSpace(l); l != "" {
							ls = append(ls, l)
						}
					}
					a.detail[name] = ls
				}
				return nil
			}
		})
	}
	return nil
}

func (a *appsTab) Hints() []hint {
	if scopes[a.scope] == "removed" {
		return []hint{{"⏎", "restore"}, {"Space", "mark"}, {"f", "scope"}, {"/", "filter"}}
	}
	return []hint{{"⏎", "launch"}, {"Space", "mark"}, {"S", "stop"}, {"C", "clear"}, {"u", "uninstall"}, {"d/e", "disable/enable"}, {"p", "perms"}, {"m", "more"},
		{"P", "pull"}, {"i/U", "install file/url"}, {"f", "scope"}, {"s", "sort"}, {"/", "filter"}}
}

func (a *appsTab) Footer() string {
	n := 0
	for _, p := range a.visible() {
		if a.marks[p.Name] {
			n++
		}
	}
	s := fmt.Sprintf("%d of %d", min(a.sel+1, len(a.rows)), len(a.rows))
	if n > 0 {
		s += fmt.Sprintf(" · %d marked", n)
	}
	return s
}

func (a *appsTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	a.lw = w
	vis := a.visible()
	a.head = func(m *Model) string {
		s := sDim.Render("scope ") + sAccB.Render(scopes[a.scope]) + sDim.Render(fmt.Sprintf("  sort %s  %d/%d", sorts[a.sort], len(vis), len(a.items)))
		if a.flt != "" {
			s += sWarn.Render("   filter: " + a.flt)
		}
		if a.loading {
			s += sDim.Render("   " + spinner(m.frame) + " loading")
		}
		if a.err != "" {
			s += sErr.Render("   " + a.err)
		}
		return s
	}
	wide := w >= 100
	lw := w
	if wide {
		lw = w * 62 / 100
	}
	a.lw = lw
	list := a.viewRows(m, lw, h, focused)
	if !wide {
		return list
	}
	detail := make([]string, h)
	if p := a.curPkg(); p != nil {
		dw := w - lw - 3
		detail[1] = sBold.Render(clipS(p.Name, dw))
		sysS := "user"
		if p.System {
			sysS = "system"
		}
		detail[2] = sDim.Render(clipS(fmt.Sprintf("uid %s · versionCode %d · %s", orStr(p.UID, "?"), p.VC, sysS), dw))
		switch det := a.detail[p.Name]; {
		case p.Removed:
			detail[4] = sWarn.Render("uninstalled for user 0 — Enter restores it")
		case det == nil:
			detail[4] = sDim.Render(spinner(m.frame) + " …")
		default:
			for i, l := range det {
				if 4+i >= h-2 {
					break
				}
				if strings.HasPrefix(l, "pid=") {
					if strings.TrimSpace(l[4:]) != "" {
						l = sOK.Render("running · pid " + l[4:])
					} else {
						l = "not running"
					}
				}
				detail[4+i] = clipS(l, dw)
			}
		}
		if h > 2 {
			detail[h-2] = sDim.Render(clipS(p.Path, dw))
		}
	}
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(list) {
			l = list[i]
		}
		out[i] = fit(l, lw) + sDim.Render("│") + " " + detail[i]
	}
	return out
}
