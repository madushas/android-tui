package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

func (a *appsTab) pull(m *Model) tea.Cmd {
	s, ps := m.tgt().serial(), a.targets()
	m.push(newWork(fmt.Sprintf("Pull APKs (%d)", len(ps)), true, func(emit core.Emit, cancelled func() bool) any {
		n := 0
		for _, p := range ps {
			if cancelled() {
				break
			}
			r := core.Sh(s, "pm path "+core.Quote(p), core.Opt{Timeout: 15 * time.Second})
			if !core.IsPackageName(p) {
				emit(fmt.Sprintf("skipped unsafe package name %q from the device", p), false)
				continue
			}
			dest, err := core.JoinWithin(core.Cfg.PullDir, p)
			if err != nil {
				emit(fmt.Sprintf("skipped %q: %v", p, err), false)
				continue
			}
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
