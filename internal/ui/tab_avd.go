package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type avdTab struct{ rowsTab }

func newAVD() Tab {
	a := &avdTab{rowsTab: rowsTab{baseTab: baseTab{ident: "avd", name: "AVD"}}}
	a.build = a.buildRows
	return a
}

type avdField struct{ label, key, kind string }

func avdFields() []avdField {
	fs := []avdField{{"Display name", "avd.ini.displayname", "text"}}
	for _, f := range core.HWFields {
		fs = append(fs, avdField{f.Label, f.Key, f.Kind})
	}
	return append(fs, avdField{"System image", "@image", ""}, avdField{"Launch flags", "@flags", ""})
}

func (a *avdTab) buildRows(m *Model) []row {
	t := m.tgt()
	if !t.isEmu() {
		return nil
	}
	c := core.CfgPath(t.emu.Name)
	var rows []row
	for _, f := range avdFields() {
		var v string
		switch f.key {
		case "@image":
			v = t.emu.Info.String()
			if t.emu.Custom {
				v += "   ★ custom ROM"
			}
		case "@flags":
			v = core.Cfg.AVDFlags[t.emu.Name]
			if v == "" {
				v = "(none)  — presets: hardware GPU, no Vulkan, writable system…"
			}
		default:
			v = orStr(core.IniGet(c, f.key), "—")
		}
		lab := "  " + padR(f.label, 30)
		rows = append(rows, row{text: lab + v, sty: lab + sAcc.Render(v), d: f})
	}
	return rows
}

func (a *avdTab) Hints() []hint { return []hint{{"⏎", "edit field"}, {"F", "launch flags"}} }

func (a *avdTab) View(m *Model, w, h int, focused bool) []string {
	t := m.tgt()
	if !t.isEmu() {
		return []string{"", "  " + sDim.Render("Select an emulator — this tab edits its hardware, system image and launch flags.")}
	}
	a.head = func(m *Model) string {
		s := sAccB.Render(t.emu.Name) + sDim.Render("  "+t.emu.Info.String())
		if t.emu.Active() {
			s += sWarn.Render("   running — stop it to edit")
		}
		return s
	}
	return a.viewRows(m, w, h, focused)
}

func (a *avdTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if a.nav(s) {
		return true, nil
	}
	t := m.tgt()
	if !t.isEmu() {
		return false, nil
	}
	if s == "F" {
		return true, m.editFlags(t.emu.Name)
	}
	if s != "enter" {
		return false, nil
	}
	r := a.cur()
	if r == nil {
		return true, nil
	}
	f := r.d.(avdField)
	name := t.emu.Name
	if f.key == "@flags" {
		return true, m.editFlags(name)
	}
	if t.emu.Active() {
		m.say(name + " is running — stop it first (x)")
		return true, nil
	}
	if f.key == "@image" {
		return true, m.changeImage(name)
	}
	cur := core.IniGet(core.CfgPath(name), f.key)
	apply := func(m *Model, v string) tea.Cmd {
		m.report(true, fmt.Sprintf("%s = %s (next boot is cold)%s", f.label, v, map[bool]string{true: " · storage size needs a data wipe to apply"}[f.kind == "size:wipe"]))
		core.ApplyHW(name, f.key, v)
		return nil
	}
	if strings.HasPrefix(f.kind, "enum:") {
		opts := strings.Split(f.kind[5:], ",")
		m.choose(fmt.Sprintf("%s (now: %s)", f.label, orStr(cur, "unset")), opts, func(m *Model, i int) tea.Cmd { return apply(m, opts[i]) })
		return true, nil
	}
	m.ask(f.label, cur, func(m *Model, v string) tea.Cmd {
		if msg := core.ValidateHW(f.kind, v); msg != "" {
			m.report(false, msg)
			return nil
		}
		return apply(m, v)
	})
	return true, nil
}

// ───────────── tools: the whole palette as a browsable toolbox ─────────────

type toolsTab struct{ rowsTab }

func newTools() Tab {
	t := &toolsTab{rowsTab: rowsTab{baseTab: baseTab{ident: "tools", name: "Tools"}}}
	t.build = t.buildRows
	return t
}

func (t *toolsTab) buildRows(m *Model) []row {
	var rows []row
	last := ""
	for _, a := range m.actions() {
		if a.group == "Go to" || a.group == "Help" || !t.match(a.group+" "+a.title) {
			continue
		}
		if a.group != last {
			rows = append(rows, hdrRow(a.group))
			last = a.group
		}
		txt := a.title
		if a.key != "" {
			txt += "   (" + a.key + ")"
		}
		if why := m.unmet(a.req); why != "" {
			rows = append(rows, row{text: txt + "   — " + why, sty: sDim.Render(txt + "   — " + why), d: a})
		} else {
			rows = append(rows, row{text: txt, sty: txt, d: a})
		}
	}
	return rows
}

func (t *toolsTab) Hints() []hint { return []hint{{"⏎", "run"}, {"/", "filter"}} }

func (t *toolsTab) View(m *Model, w, h int, focused bool) []string {
	t.head = func(m *Model) string {
		s := sDim.Render("toolbox for ") + sAccB.Render(orStr(m.tgt().name(), "—")) + sDim.Render("   dimmed = unavailable for the selected target")
		if t.flt != "" {
			s += sWarn.Render("   filter: " + t.flt)
		}
		return s
	}
	return t.viewRows(m, w, h, focused)
}

func (t *toolsTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if t.nav(s) {
		return true, nil
	}
	switch s {
	case "/":
		t.askFilter(m)
	case "enter":
		if r := t.cur(); r != nil {
			a := r.d.(action)
			if why := m.unmet(a.req); why != "" {
				m.say(a.title + ": " + why)
				return true, nil
			}
			return true, a.run(m)
		}
	case "esc":
		if t.flt != "" {
			t.flt = ""
			return true, nil
		}
		return false, nil
	default:
		return false, nil
	}
	return true, nil
}
