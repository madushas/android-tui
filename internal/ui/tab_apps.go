package ui

import (
	"fmt"
	"os"
	"path/filepath"
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

// pkgState sets each package to state ("disabled" | "installed" | "removed") through the journal:
// one group, so the whole selection can be undone from the Journal tab.
func (a *appsTab) pkgState(m *Model, verb, state string, pkgs []string) tea.Cmd {
	t := m.tgt()
	if !t.online() || len(pkgs) == 0 {
		return nil
	}
	s, prof := t.serial(), m.profile()
	return m.job(verb, false, func() cont {
		p := prof
		if p == nil {
			p = core.ProbeDevice(s)
		}
		core.EnsureBaseline(p)
		group := fmt.Sprintf("apps@%d", time.Now().UnixNano())
		ok, last := 0, ""
		for _, pk := range pkgs {
			e := core.ApplyOp(p, group, verb+" "+pk, core.Op{Kind: "pkg", Pkg: pk}, state, false)
			if e.Err != "" {
				last = e.Err
			} else {
				ok++
			}
		}
		return func(m *Model) tea.Cmd {
			m.profiles[s] = p
			what := pkgs[0]
			if len(pkgs) > 1 {
				what = fmt.Sprintf("%d/%d apps", ok, len(pkgs))
			}
			msg := verb + ": " + what
			if ok != len(pkgs) && last != "" {
				msg += " — " + last
			}
			m.report(ok == len(pkgs), msg+"  (journalled — Ctrl-Z to undo)")
			a.marks = map[string]bool{}
			a.Reload()
			return nil
		}
	})
}

func (a *appsTab) batch(m *Model, label, script, confirmMsg string, danger, reload bool, pkgs []string) tea.Cmd {
	s := m.tgt().serial()
	if s == "" || len(pkgs) == 0 {
		return nil
	}
	run := func(m *Model) tea.Cmd {
		return m.job(label, false, func() cont {
			ok, last := 0, ""
			for _, p := range pkgs {
				r := core.Sh(s, strings.ReplaceAll(script, "{p}", core.Quote(p)), core.Opt{Timeout: 60 * time.Second})
				out := strings.TrimSpace(r.Out)
				bad := !r.OK() || strings.HasPrefix(out, "Error") || strings.HasPrefix(out, "Failure") || strings.HasPrefix(out, "Exception")
				if bad {
					l := strings.Split(out, "\n")
					last = l[len(l)-1]
				} else {
					ok++
				}
			}
			return func(m *Model) tea.Cmd {
				what := pkgs[0]
				if len(pkgs) > 1 {
					what = fmt.Sprintf("%d/%d apps", ok, len(pkgs))
				}
				msg := label + ": " + what
				if ok != len(pkgs) && last != "" {
					msg += " — " + last
				}
				m.report(ok == len(pkgs), msg)
				if reload {
					a.marks = map[string]bool{}
					a.Reload()
				}
				return nil
			}
		})
	}
	if confirmMsg != "" {
		what := pkgs[0]
		if len(pkgs) > 1 {
			what = fmt.Sprintf("%d apps", len(pkgs))
		}
		m.confirm(strings.ReplaceAll(confirmMsg, "{p}", what), danger, run)
		return nil
	}
	return run(m)
}

func (a *appsTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if a.nav(s) {
		return true, nil
	}
	p := a.curPkg()
	vis := a.visible()
	switch s {
	case "/":
		a.askFilter(m)
	case "f":
		a.scope, a.sel, a.top = (a.scope+1)%len(scopes), 0, 0
	case "s":
		a.sort = (a.sort + 1) % len(sorts)
	case "r":
		a.Reload()
	case "i":
		return true, m.installAPK("")
	case "U":
		return true, m.installURL()
	case "x":
		d := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("packages-%s-%s.txt", m.tgt().name(), time.Now().Format("20060102-150405")))
		core.MkdirP(core.Cfg.ShotDir)
		var sb strings.Builder
		for _, e := range vis {
			sy := "user"
			if e.System {
				sy = "system"
			}
			fmt.Fprintf(&sb, "%s\t%d\t%s\t%s\n", e.Name, e.VC, orStr(e.Installer, "-"), sy)
		}
		_ = os.WriteFile(d, []byte(sb.String()), 0o644)
		m.report(true, fmt.Sprintf("exported %d packages to %s", len(vis), d))
	case "a":
		all := true
		for _, e := range vis {
			all = all && a.marks[e.Name]
		}
		for _, e := range vis {
			a.marks[e.Name] = !all
		}
	case "esc":
		if len(a.marks) > 0 {
			a.marks = map[string]bool{}
			return true, nil
		}
		if a.flt != "" {
			a.flt = ""
			return true, nil
		}
		return false, nil
	default:
		if p == nil {
			return false, nil
		}
		return a.pkgKey(m, s, *p)
	}
	return true, nil
}

func (a *appsTab) pkgKey(m *Model, s string, p core.Pkg) (bool, tea.Cmd) {
	switch s {
	case " ":
		a.marks[p.Name] = !a.marks[p.Name]
		a.step(1)
	case "enter":
		if p.Removed {
			return true, a.batch(m, "restored", "pm install-existing --user 0 {p}", "", false, true, a.targets())
		}
		return true, a.batch(m, "launched", `c=$(cmd package resolve-activity --brief {p} | tail -n1); case "$c" in */*) am start -n "$c";; *) monkey -p {p} -c android.intent.category.LAUNCHER 1;; esac`, "", false, false, []string{p.Name})
	case "S":
		return true, a.batch(m, "force-stopped", "am force-stop {p}", "", false, false, a.targets())
	case "C":
		return true, a.batch(m, "cleared data of", "pm clear {p}", "Clear ALL data of {p}?", true, false, a.targets())
	case "u":
		flag, msg := "", "Uninstall {p}?"
		if p.System {
			// system apps: journalled removal for user 0 (restorable from the Journal tab)
			pk := a.targets()
			m.confirm("Remove system app(s) for user 0? (restorable from the Journal tab)", true, func(m *Model) tea.Cmd {
				return a.pkgState(m, "Remove", "removed", pk)
			})
			return true, nil
		}
		return true, a.batch(m, "uninstalled", "pm uninstall "+flag+"{p}", msg, true, true, a.targets())
	case "d":
		return true, a.pkgState(m, "Disable", "disabled", a.targets())
	case "e":
		return true, a.pkgState(m, "Enable", "installed", a.targets())
	case "o":
		return true, a.batch(m, "opened settings for", "am start -a android.settings.APPLICATION_DETAILS_SETTINGS -d package:{p}", "", false, false, []string{p.Name})
	case "p":
		return true, a.perms(m, p.Name, 0)
	case "P":
		return true, a.pull(m)
	case "l":
		return true, m.logcatFor(p.Name)
	case "y":
		m.say(map[bool]string{true: "copied package name", false: "no clipboard tool found"}[core.CopyText(p.Name)])
	case "m":
		a.more(m, p.Name)
	default:
		return false, nil
	}
	return true, nil
}

func (a *appsTab) pull(m *Model) tea.Cmd {
	s, ps := m.tgt().serial(), a.targets()
	m.push(newWork(fmt.Sprintf("Pull APKs (%d)", len(ps)), true, func(emit core.Emit, cancelled func() bool) any {
		n := 0
		for _, p := range ps {
			if cancelled() {
				break
			}
			r := core.Sh(s, "pm path "+core.Quote(p), core.Opt{Timeout: 15 * time.Second})
			dest := filepath.Join(core.Cfg.PullDir, p)
			core.MkdirP(dest)
			for _, l := range strings.Split(r.Out, "\n") {
				if strings.HasPrefix(l, "package:") {
					pr := core.Adb(s, core.Opt{Timeout: 300 * time.Second}, "pull", strings.TrimSpace(l[8:]), dest+"/")
					emit(p+": "+core.AdbMsg(pr), false)
					if pr.OK() {
						n++
					}
				}
			}
		}
		return n
	}, func(m *Model, v any) tea.Cmd {
		m.report(true, fmt.Sprintf("pulled %v file(s) to %s/<package>/", v, core.Cfg.PullDir))
		return nil
	}))
	return nil
}

func (a *appsTab) perms(m *Model, pkg string, sel int) tea.Cmd {
	s := m.tgt().serial()
	return m.job("permissions", false, func() cont {
		r := core.Sh(s, "dumpsys package "+core.Quote(pkg), core.Opt{Timeout: 20 * time.Second})
		perms := core.ParsePerms(r.Out)
		return func(m *Model) tea.Cmd {
			if len(perms) == 0 {
				m.say("no permission info found")
				return nil
			}
			names := make([]string, 0, len(perms))
			for n := range perms {
				names = append(names, n)
			}
			sort.Slice(names, func(i, j int) bool {
				ri, rj := perms[names[i]].Kind == "runtime", perms[names[j]].Kind == "runtime"
				if ri != rj {
					return ri
				}
				return names[i] < names[j]
			})
			var items []string
			for _, n := range names {
				p := perms[n]
				box := "[-]"
				if p.Granted != nil {
					box = map[bool]string{true: "[x]", false: "[ ]"}[*p.Granted]
				}
				items = append(items, fmt.Sprintf("%s %s  (%s)", box, strings.TrimPrefix(n, "android.permission."), p.Kind))
			}
			c := m.choose("Permissions — "+pkg+"   Enter toggles runtime ones", items, func(m *Model, i int) tea.Cmd {
				n, p := names[i], perms[names[i]]
				if p.Kind != "runtime" {
					m.say("only runtime permissions can be granted/revoked")
					return a.perms(m, pkg, i)
				}
				verb := "grant"
				if p.Granted != nil && *p.Granted {
					verb = "revoke"
				}
				return m.job(verb, false, func() cont {
					rr := core.Sh(s, fmt.Sprintf("pm %s %s %s", verb, core.Quote(pkg), n), core.Opt{Timeout: 15 * time.Second})
					return func(m *Model) tea.Cmd {
						if !rr.OK() {
							m.report(false, n+": "+rr.Why())
						}
						return a.perms(m, pkg, i)
					}
				})
			})
			c.sel = sel
			return nil
		}
	})
}

func (a *appsTab) more(m *Model, pkg string) {
	q := core.Quote(pkg)
	type opt struct {
		t string
		f func() tea.Cmd
	}
	opts := []opt{
		{"Standby bucket…", func() tea.Cmd {
			b := []string{"active", "working_set", "frequent", "rare", "restricted"}
			m.choose("Standby bucket (how aggressively Android limits background work)", b, func(m *Model, i int) tea.Cmd {
				return m.apply("Bucket "+pkg+" → "+b[i], core.Op{Kind: "bucket", Pkg: pkg}, b[i], false, nil)
			})
			return nil
		}},
		{"Allow in Doze (battery whitelist)", func() tea.Cmd {
			return m.apply("Doze whitelist +"+pkg, core.Op{Kind: "doze", Pkg: pkg}, "whitelisted", false, nil)
		}},
		{"Remove from Doze whitelist", func() tea.Cmd {
			return m.apply("Doze whitelist −"+pkg, core.Op{Kind: "doze", Pkg: pkg}, "none", false, nil)
		}},
		{"Reset runtime permissions", func() tea.Cmd { return m.adbDo("reset permissions", "pm reset-permissions", nil) }},
		{"AppOps (view)", func() tea.Cmd { return m.showAdb("appops — "+pkg, "cmd appops get "+q) }},
		{"AppOps (set…)", func() tea.Cmd {
			m.ask("op and mode, e.g.  CAMERA deny   (allow deny ignore default foreground)", "", func(m *Model, v string) tea.Cmd {
				if f := strings.Fields(v); len(f) == 2 {
					return m.apply("AppOp "+pkg+" "+f[0]+" → "+f[1], core.Op{Kind: "appops", Pkg: pkg, Op: f[0]}, f[1], false, nil)
				}
				return nil
			})
			return nil
		}},
		{"Full package info (dumpsys package)", func() tea.Cmd { return m.showAdb("dumpsys package "+pkg, "dumpsys package "+q) }},
		{"Activities, services, receivers", func() tea.Cmd {
			return m.showAdb("components — "+pkg, "dumpsys package "+q+" | grep -E 'Activity|Service|Receiver|Provider' | head -n 120")
		}},
		{"Compile (speed) — faster startup", func() tea.Cmd { return m.adbDo("compile", "cmd package compile -m speed -f "+q, nil) }},
		{"Trim caches of all apps", func() tea.Cmd { return m.adbDo("trim caches", "pm trim-caches 999G", nil) }},
		{"Open Play Store page", func() tea.Cmd {
			return m.adbDo("play store", "am start -a android.intent.action.VIEW -d market://details?id="+pkg, nil)
		}},
		{"Restore if removed (install-existing)", func() tea.Cmd {
			return a.batch(m, "restored", "pm install-existing --user 0 {p}", "", false, true, []string{pkg})
		}},
		{"Copy package name", func() tea.Cmd {
			m.say(map[bool]string{true: "copied", false: "no clipboard tool found"}[core.CopyText(pkg)])
			return nil
		}},
	}
	var names []string
	for _, o := range opts {
		names = append(names, o.t)
	}
	m.choose("More — "+pkg, names, func(m *Model, i int) tea.Cmd { return opts[i].f() })
}
