package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

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
		d := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("packages-%s-%s.txt", core.SafeName(m.tgt().name()), time.Now().Format("20060102-150405")))
		core.MkdirP(core.Cfg.ShotDir)
		var sb strings.Builder
		for _, e := range vis {
			sy := "user"
			if e.System {
				sy = "system"
			}
			fmt.Fprintf(&sb, "%s\t%d\t%s\t%s\n", e.Name, e.VC, orStr(e.Installer, "-"), sy)
		}
		_ = core.WritePrivate(d, []byte(sb.String()))
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
