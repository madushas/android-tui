package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type action struct {
	group, title, key, req string
	run                    func(m *Model) tea.Cmd
}

// unmet returns why an action isn't available for the current target ("" = available).
func (m *Model) unmet(req string) string {
	t := m.tgt()
	switch req {
	case "any", "":
		return ""
	case "emu":
		if !t.isEmu() {
			return "select an emulator"
		}
	case "stopped":
		if !t.isEmu() || t.emu.State != "stopped" {
			return "the emulator must be stopped"
		}
	case "active":
		if !t.isEmu() || !t.emu.Active() {
			return "the emulator isn't running"
		}
	case "online":
		if !t.online() {
			return "the target must be booted and online"
		}
	case "console":
		if !t.console() {
			return "needs a running emulator"
		}
	}
	return ""
}

func (m *Model) act(label string, fn func() (bool, string)) tea.Cmd {
	return m.job(label, false, func() cont {
		ok, msg := fn()
		return func(m *Model) tea.Cmd { m.report(ok, msg); return m.refreshNow() }
	})
}

func (m *Model) emuTarget() *core.Emu {
	if t := m.tgt(); t.isEmu() {
		e := *t.emu
		return &e
	}
	return nil
}

func (m *Model) emuCmd(label string, args ...string) tea.Cmd {
	s := m.tgt().serial()
	return m.job(label, false, func() cont {
		r := core.Adb(s, core.Opt{Timeout: 15 * time.Second}, append([]string{"emu"}, args...)...)
		return func(m *Model) tea.Cmd {
			m.report(r.OK() && !strings.Contains(r.Out, "KO"), label+": "+core.AdbMsg(r))
			return nil
		}
	})
}

func (m *Model) hostAdb(label string, args ...string) tea.Cmd {
	s := m.tgt().serial()
	return m.job(label, false, func() cont {
		r := core.Adb(s, core.Opt{Timeout: 60 * time.Second}, args...)
		return func(m *Model) tea.Cmd { m.report(r.OK(), label+": "+core.AdbMsg(r)); return m.refreshNow() }
	})
}

// palette opens the fuzzy action launcher.
func (m *Model) palette() tea.Cmd {
	acts := m.actions()
	var ok, no []action
	for _, a := range acts {
		if m.unmet(a.req) == "" {
			ok = append(ok, a)
		} else {
			no = append(no, a)
		}
	}
	use := core.UsageCounts()
	actKey := func(a action) string { return a.group + "/" + a.title }
	sort.SliceStable(ok, func(i, j int) bool { return use[actKey(ok[i])] > use[actKey(ok[j])] }) // most-used first
	rows := append(ok, no...)
	var items []item
	for _, a := range rows {
		key := ""
		if a.key != "" {
			key = "  (" + a.key + ")"
		}
		t := fmt.Sprintf("%-9s %s%s", a.group, a.title, key)
		why := m.unmet(a.req)
		if why != "" {
			t += "   — " + why
		}
		items = append(items, item{text: t, dim: why != ""})
	}
	c := &chooseModal{title: "Actions — " + orStr(m.tgt().name(), "no target"), items: items, flt: newEdit(""), typeahead: true, filtering: true, marks: map[int]bool{}, fuzzy: true,
		boost: func(i int) int { return min(use[actKey(rows[i])], 10) * 3 }}
	c.pick = func(m *Model, i int) tea.Cmd {
		if why := m.unmet(rows[i].req); why != "" {
			m.say(rows[i].title + ": " + why)
			return nil
		}
		core.BumpUsage(actKey(rows[i]))
		return rows[i].run(m)
	}
	m.push(c)
	return nil
}

// ───────────── emulator flows ─────────────
