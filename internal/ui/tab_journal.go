package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type journalTab struct {
	rowsTab
	es  []core.JEntry
	all bool
	t   time.Time
}

func newJournal() Tab {
	j := &journalTab{rowsTab: rowsTab{baseTab: baseTab{ident: "journal", name: "Journal"}}}
	j.build = j.buildRows
	return j
}

func (j *journalTab) Reload() { j.t = time.Time{} }

func (j *journalTab) Tick(m *Model) tea.Cmd {
	if time.Since(j.t) > 2*time.Second {
		j.t = time.Now()
		j.es = core.LoadJournal()
		sort.SliceStable(j.es, func(a, b int) bool { return j.es[a].TS.After(j.es[b].TS) })
	}
	return nil
}

func (j *journalTab) buildRows(m *Model) []row {
	s := m.tgt().serial()
	var rows []row
	for i, e := range j.es {
		if !j.all && e.Serial != s {
			continue
		}
		text := e.Describe()
		if !j.match(e.Title + " " + text) {
			continue
		}
		mark, st := "✓", sOK
		switch {
		case e.Reverted:
			mark, st = "↩", sDim
		case e.Irrev:
			mark, st = "•", sDim
		case !e.Verified:
			mark, st = "!", sWarn
		}
		dev := ""
		if j.all {
			dev = padR(clipS(e.Model, 12), 12) + " "
		}
		plain := fmt.Sprintf("%s %s %s%s  %s", e.TS.Format("01-02 15:04"), mark, dev, padR(clipS(e.Title, 34), 34), text)
		rows = append(rows, row{text: plain, sty: sDim.Render(e.TS.Format("01-02 15:04")) + " " + st.Render(mark) + " " + dev + st.Render(padR(clipS(e.Title, 34), 34)) + "  " + sDim.Render(text), d: i})
	}
	return rows
}

func (j *journalTab) Hints() []hint {
	return []hint{{"⏎", "revert this"}, {"R", "REVERT ALL"}, {"a", "all devices"}, {"x", "export"}, {"/", "filter"}}
}

func (j *journalTab) Footer() string { return fmt.Sprintf("%d entries", len(j.rows)) }

func (j *journalTab) View(m *Model, w, h int, focused bool) []string {
	j.head = func(m *Model) string {
		pending := 0
		for _, e := range j.es {
			if e.Serial == m.tgt().serial() && !e.Reverted && !e.Irrev {
				pending++
			}
		}
		s := sDim.Render("✓ applied · ↩ reverted · ! didn't stick · • one-shot   ") + sAccB.Render(fmt.Sprintf("%d revertible on this device", pending))
		if j.all {
			s += sWarn.Render("   [all devices]")
		}
		return s
	}
	out := j.viewRows(m, w, h, focused)
	if len(j.rows) == 0 {
		out = append(out, "  "+sDim.Render("No changes recorded yet. Everything android-tui changes on a device lands here, with the value it replaced."))
	}
	return out
}

func (j *journalTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if j.nav(s) {
		return true, nil
	}
	serial := m.tgt().serial()
	switch s {
	case "a":
		j.all = !j.all
	case "/":
		j.askFilter(m)
	case "x":
		core.MkdirP(core.Cfg.ShotDir)
		f := filepath.Join(core.Cfg.ShotDir, "journal-"+time.Now().Format("20060102-150405")+".json")
		b, _ := json.MarshalIndent(j.es, "", " ")
		_ = os.WriteFile(f, b, 0o644)
		m.report(true, "exported journal to "+f)
	case "enter":
		r := j.cur()
		if r == nil {
			return true, nil
		}
		e := j.es[r.d.(int)]
		if e.Reverted || e.Irrev {
			m.say("nothing to revert for this entry")
			return true, nil
		}
		m.confirm("Revert?\n"+e.Title+"\n"+e.Describe(), false, func(m *Model) tea.Cmd {
			return m.job("revert", false, func() cont {
				ok, msg := core.RevertEntry(e)
				return func(m *Model) tea.Cmd { m.report(ok, msg); j.t = time.Time{}; return m.refreshNow() }
			})
		})
	case "R":
		if !m.tgt().online() {
			m.say("select a running device first")
			return true, nil
		}
		m.confirmTyped("Revert ALL optimizations on "+m.tgt().name()+"?\nEvery journalled change is rolled back, newest first.", "revert", func(m *Model) tea.Cmd {
			return m.job("revert all", false, func() cont {
				done, failed, msgs := core.RevertAll(serial)
				return func(m *Model) tea.Cmd {
					m.report(failed == 0, fmt.Sprintf("reverted %d change(s), %d failed %s", done, failed, strings.Join(msgs, "; ")))
					j.t = time.Time{}
					m.tabs["tweaks"].Reload()
					return m.refreshNow()
				}
			})
		})
	default:
		return false, nil
	}
	return true, nil
}
