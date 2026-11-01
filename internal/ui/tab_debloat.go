package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type debloatTab struct {
	rowsTab
	ents    []core.DebloatEntry
	loaded  bool
	loading bool
	marks   map[string]bool
	prof    *core.Profile
}

func newDebloat() Tab {
	d := &debloatTab{rowsTab: rowsTab{baseTab: baseTab{ident: "debloat", name: "Debloat"}}, marks: map[string]bool{}}
	d.build = d.buildRows
	return d
}

func (d *debloatTab) Reload() { d.loaded = false }

func (d *debloatTab) Tick(m *Model) tea.Cmd {
	p := m.profile()
	if !m.tgt().online() || p == nil {
		d.loaded, d.prof = false, nil
		return nil
	}
	if p != d.prof {
		d.prof, d.loaded = p, false
	}
	if !d.loaded && !d.loading {
		d.loading = true
		return m.job("debloat catalog", true, func() cont {
			fresh := core.ProbeDevice(p.Serial) // package states may have changed
			_, ents, _ := core.Catalog(fresh)
			return func(m *Model) tea.Cmd {
				m.profiles[p.Serial] = fresh
				d.prof = fresh
				d.ents, d.loaded, d.loading = ents, true, false
				return nil
			}
		})
	}
	return nil
}

func (d *debloatTab) state(m *Model, pkg string) string {
	p := m.profile()
	switch {
	case p == nil || !p.Installed[pkg]:
		return "removed"
	case p.Disabled[pkg]:
		return "disabled"
	}
	return "installed"
}

func (d *debloatTab) buildRows(m *Model) []row {
	var rows []row
	last := ""
	for i, e := range d.ents {
		if !d.match(e.Pkg + " " + e.Label + " " + e.Pack) {
			continue
		}
		if e.Pack != last {
			rows = append(rows, hdrRow(e.Pack+" pack"))
			last = e.Pack
		}
		st := d.state(m, e.Pkg)
		mark := "   "
		if d.marks[e.Pkg] {
			mark = "[x]"
		}
		g := map[string]string{"installed": "●", "disabled": "○", "removed": "✗"}[st]
		plain := fmt.Sprintf("%s %s %s %-8s %-9s %s", mark, g, padR(clipS(e.Label, 36), 36), e.Risk, st, e.Why)
		sty := sOKB.Render(mark) + " " + map[string]lipglossStyle{"installed": sOK}[st].Render(g) + " " + padR(clipS(e.Label, 36), 36) + " " + riskStyle(e.Risk).Render(padR(e.Risk, 8)) + " " + sDim.Render(padR(st, 9)+" "+e.Why)
		rows = append(rows, row{text: plain, sty: sty, d: i})
	}
	return rows
}

func (d *debloatTab) Hints() []hint {
	return []hint{{"Space", "mark"}, {"a", "mark all safe"}, {"d", "disable"}, {"u", "uninstall (user 0)"}, {"e", "restore"}, {"S", "snapshot"}, {"R", "restore snapshot"}, {"/", "filter"}}
}

func (d *debloatTab) Footer() string {
	return fmt.Sprintf("%d known bloat packages installed", len(d.ents))
}

func (d *debloatTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	d.head = func(m *Model) string {
		s := sDim.Render("Only packages that exist on this device and match its brand/SoC/form factor are listed. Disable is reversible; a snapshot is saved before every batch.")
		if d.loading {
			s = sDim.Render(spinner(m.frame) + " reading packages…")
		}
		return s
	}
	out := d.viewRows(m, w, h, focused)
	if d.loaded && len(d.rows) == 0 {
		out = append(out, "  "+sDim.Render("None of the known bloat packages are installed here. Use the Apps tab for anything else."))
	}
	return out
}

func (d *debloatTab) targets(m *Model) []core.DebloatEntry {
	var out []core.DebloatEntry
	for _, e := range d.ents {
		if d.marks[e.Pkg] {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		if r := d.cur(); r != nil {
			out = append(out, d.ents[r.d.(int)])
		}
	}
	return out
}

func (d *debloatTab) batch(m *Model, verb, state string, ents []core.DebloatEntry) tea.Cmd {
	p := m.profile()
	if p == nil || len(ents) == 0 {
		return nil
	}
	run := func(m *Model) tea.Cmd {
		return m.job(verb, false, func() cont {
			var pk []string
			for _, e := range ents {
				pk = append(pk, e.Pkg)
			}
			core.EnsureBaseline(p)
			snap, serr := core.SaveDebloatSnapshot(p, pk, verb)
			group := fmt.Sprintf("debloat@%d", time.Now().Unix())
			ok, bad, last := 0, 0, ""
			for _, e := range ents {
				r := core.ApplyOp(p, group, strings.Title(verb)+" "+e.Pkg, core.Op{Kind: "pkg", Pkg: e.Pkg}, state, false) //nolint:staticcheck
				if r.Err != "" {
					bad++
					last = r.Err
				} else {
					ok++
				}
			}
			return func(m *Model) tea.Cmd {
				d.marks, d.loaded = map[string]bool{}, false
				where := ""
				if serr == nil {
					where = "  snapshot: " + snap.File
				}
				m.report(bad == 0, fmt.Sprintf("%s: %d ok, %d failed %s%s", verb, ok, bad, last, where))
				return nil
			}
		})
	}
	risky := false
	for _, e := range ents {
		risky = risky || e.Risk == "risky"
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Label+" ["+e.Risk+"]")
	}
	m.confirm(fmt.Sprintf("%s %d package(s)?\n%s\nA snapshot is saved first and every change is journalled.", strings.Title(verb), len(ents), strings.Join(names, ", ")), risky, run) //nolint:staticcheck
	return nil
}

func (d *debloatTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if d.nav(s) {
		return true, nil
	}
	p := m.profile()
	switch s {
	case "/":
		d.askFilter(m)
	case " ":
		if r := d.cur(); r != nil {
			e := d.ents[r.d.(int)]
			d.marks[e.Pkg] = !d.marks[e.Pkg]
			d.step(1)
		}
	case "a":
		for _, e := range d.ents {
			if e.Risk == "safe" && d.state(m, e.Pkg) == "installed" {
				d.marks[e.Pkg] = true
			}
		}
	case "d":
		return true, d.batch(m, "disable", "disabled", d.targets(m))
	case "u":
		return true, d.batch(m, "uninstall", "removed", d.targets(m))
	case "e":
		return true, d.batch(m, "restore", "installed", d.targets(m))
	case "S":
		if p == nil {
			return true, nil
		}
		var pk []string
		for _, e := range d.ents {
			pk = append(pk, e.Pkg)
		}
		snap, err := core.SaveDebloatSnapshot(p, pk, "manual")
		if err != nil {
			m.report(false, err.Error())
		} else {
			m.report(true, "saved "+snap.File)
		}
	case "R":
		if p == nil {
			return true, nil
		}
		snaps := core.ListDebloatSnapshots(p.Serial)
		if len(snaps) == 0 {
			m.say("no debloat snapshots for this device yet")
			return true, nil
		}
		var names []string
		for _, sn := range snaps {
			names = append(names, fmt.Sprintf("%s · %s · %d packages", sn.TS.Format("2006-01-02 15:04:05"), sn.Reason, len(sn.Packages)))
		}
		m.choose("Restore packages to the state in snapshot", names, func(m *Model, i int) tea.Cmd {
			sn := snaps[i]
			return m.job("restore snapshot", false, func() cont {
				n, errs := core.RestoreDebloat(p, sn)
				return func(m *Model) tea.Cmd {
					d.loaded = false
					m.report(len(errs) == 0, fmt.Sprintf("restored %d package(s) %s", n, strings.Join(errs, "; ")))
					return nil
				}
			})
		})
	default:
		return false, nil
	}
	return true, nil
}
