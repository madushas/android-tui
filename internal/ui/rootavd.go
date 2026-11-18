package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

const rootAVDHelp = ` rootAVD is a third-party script (by "newbit") that patches an AVD's ramdisk with Magisk.
 android-tui can run it for you, but does not bundle or modify it — this is a power-user feature.

 SET IT UP
   1. get your own copy of rootAVD (search for "rootAVD newbit", e.g. https://gitlab.com/newbit/rootAVD)
   2. tell android-tui where it is, any one of:
        ROOTAVD="$HOME/tools/rootAVD/rootAVD.sh"        in ~/.config/android-tui/config
        ANDROID_TUI_ROOTAVD=/path/to/rootAVD.sh          in the environment
        android-tui rootavd <avd> --script /path/to/rootAVD.sh        on the command line

 HOW IT WORKS HERE
   · the emulator must be RUNNING (rootAVD patches through adb)
   · android-tui first keeps a one-time backup of the stock ramdisk.img next to it
   · rootAVD then runs on your terminal (it asks its own questions) and android-tui checks that the ramdisk changed
   · afterwards cold-boot the AVD; "Restore stock ramdisk" puts the original back

 CAUTION  the ramdisk belongs to the SYSTEM IMAGE: every AVD created from the same image is affected.
          Emulator menu / palette: "Root AVD with rootAVD…" and "Restore stock ramdisk".`

// rootAVD starts the optional rootAVD flow for the selected emulator (see core/rootavd.go).
func (m *Model) rootAVD() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		m.say("select an emulator first")
		return nil
	}
	script := core.RootAVDScript()
	if script == "" {
		m.push(newViewer("rootAVD — power users", strings.Split(rootAVDHelp, "\n")))
		return nil
	}
	if !e.Online() {
		m.say("start " + e.Name + " first — rootAVD patches the ramdisk through adb while it runs")
		return nil
	}
	plan, err := core.PlanRootAVD(e.Name)
	if err == nil {
		err = plan.UseScript(script)
	}
	if err != nil {
		m.report(false, err.Error())
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Patch the ramdisk of %s with rootAVD (Magisk)?\n\n", e.Name)
	fmt.Fprintf(&b, "This rewrites the system image's ramdisk.img (%s).\n", plan.Rel)
	if len(plan.Shared) > 0 {
		fmt.Fprintf(&b, "Also affected, because they use the same image: %s.\n", strings.Join(plan.Shared, ", "))
	}
	b.WriteString("A one-time backup of the stock ramdisk is kept so you can undo it.\nrootAVD is interactive: the TUI hands you the terminal while it runs.")
	m.confirmTyped(b.String(), "root", func(m *Model) tea.Cmd { return m.runRootAVD(plan) })
	return nil
}

// runRootAVD backs the stock ramdisk up, hands the terminal to rootAVD, then judges the result by the ramdisk itself.
func (m *Model) runRootAVD(plan *core.RootAVDPlan) tea.Cmd {
	return m.job("back up the stock ramdisk", false, func() cont {
		before, note, err := plan.Prepare()
		return func(m *Model) tea.Cmd {
			if err != nil {
				m.report(false, err.Error())
				return nil
			}
			if note != "" {
				core.Logf("info", "%s", note)
			}
			cmd := plan.Command(nil, true)
			core.Logf("cmd", "$ %s", core.ShellJoin(cmd.Args))
			return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
				return execThen{err: runErr, then: func(m *Model, _ error) tea.Cmd {
					ok, msg := plan.Finish(before, runErr)
					m.report(ok, msg)
					return nil
				}}
			})
		}
	})
}

// restoreRamdisk puts the stock ramdisk back from android-tui's own backup.
func (m *Model) restoreRamdisk() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		m.say("select an emulator first")
		return nil
	}
	plan, err := core.PlanRootAVD(e.Name)
	if err != nil {
		m.report(false, err.Error())
		return nil
	}
	switch plan.Status() {
	case "no backup yet":
		m.say("nothing to restore: android-tui has no backup of the stock ramdisk for this image")
		return nil
	case "stock":
		m.say("the ramdisk is already the stock one")
		return nil
	}
	var running []string
	for _, x := range m.sc.Emus {
		if x.Active() && (x.Name == e.Name || slices.Contains(plan.Shared, x.Name)) {
			running = append(running, x.Name)
		}
	}
	if len(running) > 0 {
		m.report(false, "stop "+strings.Join(running, ", ")+" first — they boot from this image")
		return nil
	}
	m.confirm("Restore the stock ramdisk for "+plan.Rel+"?\nThis undoes rootAVD for every AVD that uses this image.", true, func(m *Model) tea.Cmd {
		return m.job("restore ramdisk", false, func() cont {
			ok, msg := plan.Restore()
			return func(m *Model) tea.Cmd { m.report(ok, msg); return nil }
		})
	})
	return nil
}
