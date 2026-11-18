package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type propsTab struct {
	rowsTab
	items []core.KV
}

func newProps() Tab {
	p := &propsTab{rowsTab: rowsTab{baseTab: baseTab{ident: "props", name: "Props"}}}
	p.build = p.buildRows
	return p
}

func (p *propsTab) Tick(m *Model) tea.Cmd {
	if p.stale(m) {
		p.items, p.sel, p.top = nil, 0, 0
		if m.tgt().online() {
			s, gen := m.tgt().serial(), p.gen
			return m.job("getprop", false, func() cont {
				r := core.Sh(s, "getprop", core.Opt{Timeout: 15 * time.Second})
				return func(m *Model) tea.Cmd {
					if gen == p.gen {
						p.items = core.ParseProps(r.Out)
					}
					return nil
				}
			})
		}
	}
	return nil
}

func (p *propsTab) buildRows(m *Model) []row {
	var rows []row
	kw := 44
	for _, kv := range p.items {
		if p.match(kv.K) || p.match(kv.V) {
			k := clipS(kv.K, kw)
			rows = append(rows, row{text: padR(k, kw) + "  " + kv.V, sty: sAcc.Render(padR(k, kw)) + "  " + kv.V, d: kv})
		}
	}
	return rows
}

func (p *propsTab) Hints() []hint {
	return []hint{{"/", "filter"}, {"⏎", "view"}, {"y", "copy value"}, {"c", "copy key=value"}, {"e", "setprop"}, {"s", "save all"}, {"r", "reload"}}
}
func (p *propsTab) Footer() string {
	return fmt.Sprintf("%d of %d", min(p.sel+1, len(p.rows)), len(p.rows))
}

func (p *propsTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	p.head = func(m *Model) string {
		s := sDim.Render(fmt.Sprintf("%d/%d properties", len(p.rows), len(p.items)))
		if p.flt != "" {
			s += sWarn.Render("   filter: " + p.flt)
		}
		return s
	}
	return p.viewRows(m, w, h, focused)
}

func (p *propsTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if p.nav(s) {
		return true, nil
	}
	var kv *core.KV
	if r := p.cur(); r != nil {
		x := r.d.(core.KV)
		kv = &x
	}
	switch s {
	case "/":
		p.askFilter(m)
	case "r":
		p.Reload()
	case "s":
		dir := core.Cfg.ShotDir
		core.MkdirP(dir)
		f := filepath.Join(dir, fmt.Sprintf("props-%s-%s.txt", core.SafeName(m.tgt().name()), time.Now().Format("20060102-150405")))
		var sb strings.Builder
		for _, x := range p.items {
			sb.WriteString(x.K + "=" + x.V + "\n")
		}
		_ = core.WritePrivate(f, []byte(sb.String()))
		m.report(true, fmt.Sprintf("saved %d props to %s", len(p.items), f))
	case "enter":
		if kv != nil {
			m.viewText(kv.K, wrap(orStr(kv.V, "(empty)"), 100))
		}
	case "y":
		if kv != nil {
			m.say(map[bool]string{true: "copied value", false: "no clipboard tool found"}[core.CopyText(kv.V)])
		}
	case "c":
		if kv != nil {
			m.say(map[bool]string{true: "copied", false: "no clipboard tool found"}[core.CopyText(kv.K+"="+kv.V)])
		}
	case "e":
		if kv != nil {
			k := *kv
			m.ask("setprop "+k.K, k.V, func(m *Model, v string) tea.Cmd {
				if v == k.V {
					return nil
				}
				return m.adbDo("setprop "+k.K, "setprop "+core.Quote(k.K)+" "+core.Quote(v), func(m *Model) tea.Cmd { p.Reload(); return nil })
			})
		}
	case "esc":
		if p.flt != "" {
			p.flt = ""
			return true, nil
		}
		return false, nil
	default:
		return false, nil
	}
	return true, nil
}
