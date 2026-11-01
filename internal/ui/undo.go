package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

// undoLast (Ctrl-Z) reverts the newest journal group of the selected device, after a confirmation that lists what will change.
func (m *Model) undoLast() tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device to undo its last change")
		return nil
	}
	s, name := t.serial(), t.name()
	es := core.LastGroup(s)
	if len(es) == 0 {
		m.say("nothing to undo on " + name)
		return nil
	}
	var b strings.Builder
	for i, e := range es {
		if i == 6 {
			fmt.Fprintf(&b, "… and %d more\n", len(es)-6)
			break
		}
		b.WriteString("• " + e.Title + " — " + e.Describe() + "\n")
	}
	m.confirm("Undo the last change on "+name+"?\n"+b.String(), false, func(m *Model) tea.Cmd {
		return m.job("undo", false, func() cont {
			done, failed, msgs := core.RevertEntries(es)
			return func(m *Model) tea.Cmd {
				txt := fmt.Sprintf("undid %d change(s)", done)
				if failed > 0 {
					txt += fmt.Sprintf(", %d failed: %s", failed, strings.Join(msgs, "; "))
				}
				m.report(failed == 0, txt)
				for _, id := range []string{"journal", "tweaks", "apps", "control", "system", "debloat"} {
					m.tabs[id].Reload()
				}
				return m.refreshNow()
			}
		})
	})
	return nil
}
