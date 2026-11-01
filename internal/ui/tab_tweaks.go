package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type twRow struct {
	core.CatalogTweak
	state, cur string
}

type tweaksTab struct {
	rowsTab
	items   []twRow
	loading bool
	showAll bool
	marks   map[string]bool
	prof    *core.Profile
}

func newTweaks() Tab {
	t := &tweaksTab{rowsTab: rowsTab{baseTab: baseTab{ident: "tweaks", name: "Tweaks"}}, marks: map[string]bool{}}
	t.build = t.buildRows
	return t
}

func (t *tweaksTab) Tick(m *Model) tea.Cmd {
	p := m.profile()
	if p != t.prof {
		t.prof, t.items = p, nil
	}
	if !m.tgt().online() {
		t.items, t.prof = nil, nil
		return nil
	}
	if p != nil && t.items == nil && !t.loading {
		return t.load(m, p)
	}
	return nil
}

func (t *tweaksTab) Reload() { t.items = nil }

func (t *tweaksTab) load(m *Model, p *core.Profile) tea.Cmd {
	t.loading = true
	return m.job("read tweak states", false, func() cont {
		cat, _, _ := core.Catalog(p)
		// read every applicable tweak's ops in a few batched adb calls (was one call per op)
		var ts []core.Tweak
		for _, c := range cat {
			if c.OK {
				ts = append(ts, c.Tweak)
			}
		}
		sts := core.TweakStates(p, ts)
		var rows []twRow
		k := 0
		for _, c := range cat {
			r := twRow{CatalogTweak: c}
			if c.OK {
				r.state, r.cur = sts[k][0], sts[k][1]
				k++
			}
			rows = append(rows, r)
		}
		return func(m *Model) tea.Cmd { t.items, t.loading = rows, false; return nil }
	})
}

var stateGlyph = map[string]string{"on": "●", "off": "○", "partial": "◐", "unknown": "?", "action": "▶", "preset": "◆"}

func riskStyle(r string) lipglossStyle {
	switch r {
	case "safe":
		return sOK
	case "risky":
		return sErr
	}
	return sWarn
}

func (t *tweaksTab) buildRows(m *Model) []row {
	var rows []row
	last := ""
	for i := range t.items {
		it := t.items[i]
		if !it.OK && !t.showAll {
			continue
		}
		if !t.match(it.Title + " " + it.Category + " " + it.Pack) {
			continue
		}
		if it.Category != last {
			rows = append(rows, hdrRow(it.Category))
			last = it.Category
		}
		mark := "   "
		if t.marks[it.ID] {
			mark = "[x]"
		}
		g := stateGlyph[it.state]
		if !it.OK {
			g = "✗"
		}
		tag := it.Risk
		if it.Reboot {
			tag += " · reboot"
		}
		if it.Pack != "universal" {
			tag += " · " + it.Pack
		}
		extra := ""
		switch {
		case !it.OK:
			extra = it.Reason
		case it.state == "preset" || (it.state != "on" && it.state != "off" && it.state != "action" && it.cur != ""):
			extra = "now: " + it.cur
		}
		w := 46
		plain := fmt.Sprintf("%s %s %s %s %s", mark, g, padR(clipS(it.Title, w), w), padR(tag, 22), extra)
		gs := map[string]lipglossStyle{"on": sOKB, "partial": sWarn}[it.state]
		if !it.OK {
			plain = fmt.Sprintf("%s %s %s %s", mark, g, padR(clipS(it.Title, w), w), "— "+extra)
			rows = append(rows, row{text: plain, sty: sDim.Render(plain), d: i})
			continue
		}
		sty := sOKB.Render(mark) + " " + gs.Render(g) + " " + padR(clipS(it.Title, w), w) + " " + riskStyle(it.Risk).Render(padR(tag, 22)) + " " + sDim.Render(extra)
		rows = append(rows, row{text: plain, sty: sty, d: i})
	}
	return rows
}

func (t *tweaksTab) cur_() *twRow {
	if r := t.cur(); r != nil {
		return &t.items[r.d.(int)]
	}
	return nil
}

func (t *tweaksTab) Hints() []hint {
	return []hint{{"⏎", "apply/toggle"}, {"p", "preview"}, {"i", "info"}, {"v", "revert"}, {"Space", "mark"}, {"A", "apply marked"}, {"a", "show unavailable"}, {"/", "filter"}, {"r", "reload"}}
}

func (t *tweaksTab) Footer() string {
	n, ok := len(t.items), 0
	for _, i := range t.items {
		if i.OK {
			ok++
		}
	}
	return fmt.Sprintf("%d applicable / %d", ok, n)
}

func (t *tweaksTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	p := m.profile()
	t.head = func(m *Model) string {
		if p == nil {
			return sDim.Render(spinner(m.frame) + " probing device…")
		}
		s := sAccB.Render(p.Summary()) + sDim.Render("  ·  tweaks that don't fit this device are hidden (a shows them)")
		if !p.Root && p.ShizukuInstalled && !p.ShizukuRunning {
			s += sWarn.Render("  ! Shizuku not running (palette: Start Shizuku)")
		}
		if t.loading {
			s += sDim.Render("  " + spinner(m.frame))
		}
		if t.flt != "" {
			s += sWarn.Render("   filter: " + t.flt)
		}
		return s
	}
	return t.viewRows(m, w, h, focused)
}

func opLine(o core.Op, serial string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings put %s %s", orStr(o.NS, "?"), o.Key)
	case "device_config":
		return fmt.Sprintf("device_config put %s %s", o.NS, o.Key)
	case "prop":
		return "setprop " + o.Key
	case "shell":
		return o.Cmd
	case "cleanup":
		return "rm -rf " + o.Key
	case "auto":
		return "set " + o.Key + "  (setting or property, detected on the device)"
	}
	return o.Kind + " " + o.Key
}

func (t *tweaksTab) info(m *Model, it *twRow) {
	ls := []string{it.Title, strings.Repeat("═", min(60, sw(it.Title))), "", it.Desc, "",
		fmt.Sprintf("pack: %s · category: %s · risk: %s%s", it.Pack, it.Category, it.Risk, map[bool]string{true: " · needs reboot"}[it.Reboot])}
	if !it.OK {
		ls = append(ls, "NOT AVAILABLE HERE: "+it.Reason)
	}
	r := it.Requires
	var req []string
	if r.MinSDK > 0 {
		req = append(req, fmt.Sprintf("API ≥ %d", r.MinSDK))
	}
	for _, x := range [][]string{r.Form, r.SoC, r.Brand} {
		if len(x) > 0 {
			req = append(req, strings.Join(x, "/"))
		}
	}
	if r.Root {
		req = append(req, "root")
	}
	if len(req) > 0 {
		ls = append(ls, "requires: "+strings.Join(req, " · "))
	}
	ls = append(ls, "", "Operations")
	for _, o := range it.Ops {
		ls = append(ls, "  • "+opLine(o, ""))
		if o.On != "" || o.Off != "" || o.OffUnset {
			off := o.Off
			if o.OffUnset {
				off = "(delete)"
			}
			ls = append(ls, fmt.Sprintf("      on = %s   off = %s", o.On, off))
		}
	}
	m.viewText("Tweak — "+it.Title, ls)
}

func (t *tweaksTab) preview(m *Model, it *twRow, mode string) {
	p := m.profile()
	s := p.Serial
	m.push(newWork("Preview — "+it.Title, false, func(emit core.Emit, _ func() bool) any {
		emit(fmt.Sprintf("mode: %s   (nothing is changed by a preview)", mode), false)
		for _, o := range it.Ops {
			prev, ex, readable := o.Read(s)
			val := o.On
			if mode == "off" {
				val = o.Off
				if o.OffUnset {
					val = "(delete)"
				}
			} else if mode != "on" {
				val = strings.ReplaceAll(o.On, "{v}", mode)
			}
			cur := "(not readable)"
			if readable {
				cur = prev
				if !ex {
					cur = "∅ (unset)"
				}
			}
			emit(fmt.Sprintf("  %-58s  %s → %s", opLine(o, s), cur, orStr(val, "—")), false)
		}
		return true
	}, nil))
}

func (t *tweaksTab) run(m *Model, it twRow, mode string) tea.Cmd {
	p := m.profile()
	title := it.Title
	return m.job("apply "+title, false, func() cont {
		core.EnsureBaseline(p)
		es := core.ApplyTweak(p, it.Tweak, mode)
		return func(m *Model) tea.Cmd {
			bad, unver := "", 0
			for _, e := range es {
				if e.Err != "" {
					bad = e.Err
				} else if !e.Verified {
					unver++
				}
			}
			switch {
			case bad != "":
				m.report(false, title+": "+bad)
			case unver > 0:
				m.report(false, fmt.Sprintf("%s: applied, but %d value(s) did not stick — the OEM or a Google flag sync may reset them", title, unver))
			default:
				m.report(true, title+" ✓  (journalled — Ctrl-Z to undo)"+map[bool]string{true: " — reboot to take effect"}[it.Reboot])
			}
			t.items = nil
			return nil
		}
	})
}

func (t *tweaksTab) activate(m *Model, it twRow) tea.Cmd {
	p := m.profile()
	if p == nil {
		m.say("still probing the device…")
		return nil
	}
	confirmThen := func(mode string, danger bool) tea.Cmd {
		var ops []string
		for _, o := range it.Ops {
			ops = append(ops, "  • "+opLine(o, ""))
		}
		msg := fmt.Sprintf("%s\n%s\nRisk: %s. Every change is journalled and can be reverted.\n%s", it.Title, it.Desc, it.Risk, strings.Join(ops, "\n"))
		if it.Risk == "safe" && !it.Action {
			return t.run(m, it, mode)
		}
		m.confirm(msg, danger || it.Risk == "risky", func(m *Model) tea.Cmd { return t.run(m, it, mode) })
		return nil
	}
	switch {
	case it.Special == "home_picker":
		return m.job("launchers", false, func() cont {
			cands, cur := core.HomeActivities(p.Serial)
			return func(m *Model) tea.Cmd {
				if len(cands) == 0 {
					m.say("no launchers found")
					return nil
				}
				items := make([]string, len(cands))
				for i, c := range cands {
					items[i] = c + map[bool]string{true: "   ← current"}[c == cur]
				}
				m.choose("Default launcher", items, func(m *Model, i int) tea.Cmd {
					return m.apply("Default launcher → "+cands[i], core.Op{Kind: "home"}, cands[i], false, func(m *Model, e core.JEntry) tea.Cmd { t.items = nil; return nil })
				})
				return nil
			}
		})
	case it.Special == "value":
		m.ask(it.Title+" — new value (current: "+orStr(it.cur, "unset")+"; empty = restore default)", it.cur, func(m *Model, v string) tea.Cmd {
			if strings.TrimSpace(v) == "" {
				return t.run(m, it, "off")
			}
			return t.run(m, it, strings.TrimSpace(v))
		})
	case it.Special == "prop_glob":
		return m.job("find loggers", false, func() cont {
			plan := core.GlobPlan(p.Serial, strings.Split(it.Cmd, ","))
			return func(m *Model) tea.Cmd {
				if len(plan) == 0 {
					m.say("no enabled loggers found matching " + it.Cmd)
					return nil
				}
				var ls []string
				for _, c := range plan {
					ls = append(ls, fmt.Sprintf("  %s: %s → %s", c.Key, c.Cur, c.New))
				}
				m.confirm("Switch off these properties?\n"+strings.Join(ls, "\n")+"\nChanges are journalled; setprop may need root on some builds.", false, func(m *Model) tea.Cmd {
					return m.job("disable loggers", false, func() cont {
						core.EnsureBaseline(p)
						ok, fail, last := 0, 0, ""
						group := fmt.Sprintf("loggers@%d", time.Now().Unix())
						for _, c := range plan {
							e := core.ApplyOp(p, group, it.Title, core.Op{Kind: "prop", Key: c.Key}, c.New, false)
							if e.Err != "" {
								fail++
								last = e.Err
							} else {
								ok++
							}
						}
						return func(m *Model) tea.Cmd {
							m.report(fail == 0, fmt.Sprintf("loggers: %d changed, %d failed %s", ok, fail, last))
							return nil
						}
					})
				})
				return nil
			}
		})
	case it.Action:
		return confirmThen("on", false)
	case len(it.Presets) > 0:
		var names []string
		for _, pr := range it.Presets {
			names = append(names, pr.Label)
		}
		m.choose(it.Title+"   (now: "+orStr(it.cur, "unset")+")", names, func(m *Model, i int) tea.Cmd { return t.run(m, it, it.Presets[i].Value) })
	default:
		mode := "on"
		if it.state == "on" {
			mode = "off"
		}
		return confirmThen(mode, false)
	}
	return nil
}

func (t *tweaksTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if t.nav(s) {
		return true, nil
	}
	it := t.cur_()
	switch s {
	case "/":
		t.askFilter(m)
	case "r":
		t.Reload()
	case "a":
		t.showAll = !t.showAll
	case "esc":
		if t.flt != "" {
			t.flt = ""
			return true, nil
		}
		return false, nil
	case "enter", "p", "i", "v", " ":
		if it == nil {
			return true, nil
		}
		if !it.OK && s != "i" {
			m.say(it.Title + ": " + it.Reason)
			return true, nil
		}
		switch s {
		case "enter":
			return true, t.activate(m, *it)
		case "i":
			t.info(m, it)
		case "p":
			mode := "on"
			if it.state == "on" {
				mode = "off"
			}
			if len(it.Presets) > 0 {
				mode = it.Presets[0].Value
			}
			t.preview(m, it, mode)
		case " ":
			t.marks[it.ID] = !t.marks[it.ID]
			t.step(1)
		case "v":
			serial, title := m.tgt().serial(), it.Title
			return true, m.job("revert "+title, false, func() cont {
				n, msg := core.RevertTweak(serial, title)
				return func(m *Model) tea.Cmd { m.report(n > 0, title+": "+msg); t.items = nil; return nil }
			})
		}
	case "A":
		var batch []twRow
		for _, x := range t.items {
			if t.marks[x.ID] && x.OK && !x.Action && len(x.Presets) == 0 && x.Special == "" {
				batch = append(batch, x)
			}
		}
		if len(batch) == 0 {
			m.say("mark toggle-type tweaks with Space first")
			return true, nil
		}
		m.confirm(fmt.Sprintf("Apply %d marked tweak(s)? Each is journalled and revertible.", len(batch)), false, func(m *Model) tea.Cmd {
			p := m.profile()
			return m.job("apply marked", false, func() cont {
				core.EnsureBaseline(p)
				bad := 0
				for _, x := range batch {
					for _, e := range core.ApplyTweak(p, x.Tweak, "on") {
						if e.Err != "" {
							bad++
						}
					}
				}
				return func(m *Model) tea.Cmd {
					t.marks, t.items = map[string]bool{}, nil
					m.report(bad == 0, fmt.Sprintf("applied %d tweak(s), %d error(s)", len(batch), bad))
					return nil
				}
			})
		})
	default:
		return false, nil
	}
	return true, nil
}
