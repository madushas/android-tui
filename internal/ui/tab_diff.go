package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type diffRow struct {
	act  int // 0 snapshot, 1 snapshot+props, -1 saved snapshot
	snap *core.SettingsSnap
}

type diffTab struct {
	rowsTab
	snaps []*core.SettingsSnap
	loadT time.Time
	mark  *core.SettingsSnap
}

func newDiff() Tab {
	d := &diffTab{rowsTab: rowsTab{baseTab: baseTab{ident: "diff", name: "Diff"}}}
	d.build = d.buildRows
	return d
}

func (d *diffTab) Reload() { d.loadT = time.Time{} }

func (d *diffTab) Tick(m *Model) tea.Cmd {
	if m.tgt().online() && time.Since(d.loadT) > 3*time.Second {
		d.loadT = time.Now()
		d.snaps = core.ListSnapshots(m.tgt().serial())
	}
	return nil
}

func (d *diffTab) buildRows(m *Model) []row {
	rows := []row{
		{text: "＋ Take snapshot now (settings + device_config)", sty: sAcc.Render("＋ Take snapshot now (settings + device_config)"), d: diffRow{act: 0}},
		{text: "＋ Take snapshot including system properties", sty: sAcc.Render("＋ Take snapshot including system properties"), d: diffRow{act: 1}},
		hdrRow(fmt.Sprintf("Saved snapshots (%d) — Enter compares with the device now · Space marks one as the 'before' for comparing two", len(d.snaps))),
	}
	for _, s := range d.snaps {
		mk := "  "
		if d.mark == s {
			mk = "▶ "
		}
		t := fmt.Sprintf("%s%s  %-34s %5d keys", mk, s.TS.Format("2006-01-02 15:04:05"), clipS(s.Label, 34), len(s.Data))
		rows = append(rows, row{text: t, sty: t, d: diffRow{act: -1, snap: s}})
	}
	return rows
}

func (d *diffTab) Hints() []hint {
	return []hint{{"⏎", "diff vs now"}, {"Space", "mark as before"}, {"x", "delete snapshot"}, {"r", "reload"}}
}

func (d *diffTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	d.head = func(m *Model) string {
		return sDim.Render("Workflow: snapshot → change something in the OEM settings (or a tweak) → Enter on the snapshot to see exactly what changed. A baseline is also taken automatically before the first change android-tui makes.")
	}
	return d.viewRows(m, w, h, focused)
}

func showDiff(m *Model, a, b *core.SettingsSnap) {
	ds := core.DiffSnaps(a, b)
	var ls []line
	ls = append(ls, line{fmt.Sprintf("%s (%s)  →  %s (%s): %d difference(s)", a.Label, a.TS.Format("15:04:05"), b.Label, b.TS.Format("15:04:05"), len(ds)), "bold"}, line{"", ""})
	if len(ds) == 0 {
		ls = append(ls, line{"No differences — nothing changed between the two snapshots.", "ok"})
	}
	for _, x := range ds {
		switch x.Op {
		case "+":
			ls = append(ls, line{fmt.Sprintf("+ %s = %s", x.Key, x.New), "ok"})
		case "-":
			ls = append(ls, line{fmt.Sprintf("- %s = %s", x.Key, x.Old), "err"})
		default:
			ls = append(ls, line{fmt.Sprintf("~ %s: %s → %s", x.Key, x.Old, x.New), "warn"})
		}
		if len(ls) > 6000 {
			ls = append(ls, line{"… truncated", "dim"})
			break
		}
	}
	m.push(newStyledViewer("Settings diff", ls))
}

func (d *diffTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if d.nav(s) {
		return true, nil
	}
	p := m.profile()
	r := d.cur()
	if r == nil {
		return false, nil
	}
	dr := r.d.(diffRow)
	switch s {
	case "enter":
		if p == nil {
			m.say("still probing the device…")
			return true, nil
		}
		if dr.act >= 0 {
			props := dr.act == 1
			m.ask("Snapshot label", "manual", func(m *Model, label string) tea.Cmd {
				return m.job("snapshot", false, func() cont {
					sn, err := core.TakeSnapshot(p, orStr(label, "manual"), props)
					return func(m *Model) tea.Cmd {
						if err != nil {
							m.report(false, err.Error())
						} else {
							m.report(true, fmt.Sprintf("snapshot saved (%d keys): %s", len(sn.Data), sn.File))
						}
						d.loadT = time.Time{}
						return nil
					}
				})
			})
			return true, nil
		}
		a := dr.snap
		before := a
		if d.mark != nil && d.mark != a {
			before = d.mark
			return true, m.job("diff", false, func() cont { return func(m *Model) tea.Cmd { showDiff(m, before, a); return nil } })
		}
		_, hasProps := "", false
		for k := range a.Data {
			if strings.HasPrefix(k, "prop/") {
				hasProps = true
				break
			}
		}
		_ = hasProps
		withProps := false
		for k := range a.Data {
			withProps = withProps || strings.HasPrefix(k, "prop/")
		}
		return true, m.job("diff vs now", false, func() cont {
			now := core.CurrentSnapshot(p, withProps)
			return func(m *Model) tea.Cmd { showDiff(m, a, now); return nil }
		})
	case " ":
		if dr.snap != nil {
			if d.mark == dr.snap {
				d.mark = nil
			} else {
				d.mark = dr.snap
			}
		}
	case "x":
		if dr.snap != nil {
			f := dr.snap.File
			m.confirm("Delete this snapshot file?\n"+f, true, func(m *Model) tea.Cmd { _ = os.Remove(f); d.loadT = time.Time{}; return nil })
		}
	case "r":
		d.loadT = time.Time{}
	default:
		return false, nil
	}
	return true, nil
}
