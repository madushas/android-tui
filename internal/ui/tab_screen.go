package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type screenAct struct {
	label string
	fn    func(m *Model) tea.Cmd
}

type screenTab struct {
	rowsTab
	png     []byte
	pngFile string
	prevKey string
	prev    []string
	tree    *core.UINode
	treeAt  time.Time
}

func newScreen() Tab {
	s := &screenTab{rowsTab: rowsTab{baseTab: baseTab{ident: "screen", name: "Screen"}}}
	s.build = s.buildRows
	return s
}

func (s *screenTab) acts() []screenAct {
	return []screenAct{
		{"Capture screenshot (inline preview)", s.capture},
		{"Full-resolution preview (Kitty · chafa · Sixel)", s.fullPreview},
		{"Copy last screenshot to clipboard", s.copyShot},
		{"Open last screenshot in your viewer", func(m *Model) tea.Cmd {
			if s.pngFile == "" || !core.OpenPath(s.pngFile) {
				m.say("capture a screenshot first (and install xdg-open)")
			}
			return nil
		}},
		{"UI dump → tree (compact)", func(m *Model) tea.Cmd {
			return s.dump(m, func(m *Model, n *core.UINode) { m.viewText("UI tree", n.RenderTree(true, false)) })
		}},
		{"UI dump → full tree with bounds", func(m *Model) tea.Cmd {
			return s.dump(m, func(m *Model, n *core.UINode) { m.viewText("UI tree (full)", n.RenderTree(false, true)) })
		}},
		{"UI dump → copy as Markdown (LLM-ready)", func(m *Model) tea.Cmd {
			return s.dump(m, func(m *Model, n *core.UINode) {
				m.say(map[bool]string{true: "copied UI tree as Markdown", false: "no clipboard tool found"}[core.CopyText(n.RenderMarkdown(true))])
			})
		}},
		{"UI dump → copy as JSON", func(m *Model) tea.Cmd {
			return s.dump(m, func(m *Model, n *core.UINode) {
				m.say(map[bool]string{true: "copied UI tree as JSON", false: "no clipboard tool found"}[core.CopyText(n.RenderJSON(true))])
			})
		}},
		{"UI dump → save .txt / .md / .json", func(m *Model) tea.Cmd { return s.dump(m, s.save) }},
		{"Tap an element from the UI dump…", func(m *Model) tea.Cmd { return s.dump(m, s.tap) }},
		{"Record screen… (size, bitrate, time limit)", s.recordOpts},
		{"Stop recording", func(m *Model) tea.Cmd {
			if m.rec == nil {
				m.say("not recording")
				return nil
			}
			return m.record()
		}},
	}
}

func (s *screenTab) buildRows(m *Model) []row {
	var rows []row
	for i, a := range s.acts() {
		rows = append(rows, row{text: a.label, sty: a.label, d: i})
	}
	return rows
}

func (s *screenTab) Hints() []hint {
	return []hint{{"⏎", "run"}, {"c", "capture"}, {"f", "full-res"}, {"u", "UI tree"}}
}

func (s *screenTab) capture(m *Model) tea.Cmd {
	t := m.tgt()
	ser, name := t.serial(), t.name()
	return m.job("screenshot", false, func() cont {
		b, err := core.Binary([]string{"adb", "-s", ser, "exec-out", "screencap", "-p"}, 30*time.Second)
		var f string
		if err == nil && len(b) > 100 {
			core.MkdirP(core.Cfg.ShotDir)
			f = filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.png", core.SafeName(name), time.Now().Format("20060102-150405")))
			_ = core.WritePrivate(f, b)
		}
		return func(m *Model) tea.Cmd {
			if f == "" {
				m.report(false, "screenshot failed: "+fmt.Sprint(err))
				return nil
			}
			s.png, s.pngFile, s.prevKey = b, f, ""
			m.report(true, "saved "+f)
			return nil
		}
	})
}

func (s *screenTab) fullPreview(m *Model) tea.Cmd {
	if s.pngFile == "" {
		m.say("capture a screenshot first")
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		m.report(false, err.Error())
		return nil
	}
	return m.ext(false, exe, "__show", s.pngFile)
}

func (s *screenTab) copyShot(m *Model) tea.Cmd {
	if s.png == nil {
		m.say("capture a screenshot first")
		return nil
	}
	m.say(map[bool]string{true: "screenshot copied to the clipboard", false: "no clipboard tool (wl-copy / xclip)"}[core.CopyImage(s.png)])
	return nil
}

func (s *screenTab) dump(m *Model, then func(m *Model, n *core.UINode)) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	ser := t.serial()
	return m.job("uiautomator dump", false, func() cont {
		n, err := core.UIDump(ser)
		return func(m *Model) tea.Cmd {
			if err != nil {
				m.report(false, "UI dump failed: "+err.Error())
				return nil
			}
			s.tree, s.treeAt = n, time.Now()
			then(m, n)
			return nil
		}
	})
}

func (s *screenTab) save(m *Model, n *core.UINode) {
	core.MkdirP(core.Cfg.ShotDir)
	base := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("uidump-%s-%s", core.SafeName(m.tgt().name()), time.Now().Format("20060102-150405")))
	_ = core.WritePrivate(base+".txt", []byte(strings.Join(n.RenderTree(true, true), "\n")+"\n"))
	_ = core.WritePrivate(base+".md", []byte(n.RenderMarkdown(true)))
	_ = core.WritePrivate(base+".json", []byte(n.RenderJSON(true)))
	m.report(true, "saved "+base+".{txt,md,json}")
}

func (s *screenTab) tap(m *Model, n *core.UINode) {
	tg := n.Targets()
	if len(tg) == 0 {
		m.say("no tappable elements found")
		return
	}
	var its []item
	for _, x := range tg {
		d := orStr(x.Text, orStr(x.Desc, x.ID))
		its = append(its, item{text: fmt.Sprintf("%-8s %-34s %s", x.Class, clipS(d, 34), x.ID)})
	}
	ser := m.tgt().serial()
	m.push(&chooseModal{title: "Tap which element?", items: its, flt: newEdit(""), typeahead: true, filtering: true, marks: map[int]bool{},
		pick: func(m *Model, i int) tea.Cmd {
			x, y := tg[i].Center()
			return m.job("tap", false, func() cont {
				r := core.Sh(ser, fmt.Sprintf("input tap %d %d", x, y), core.Opt{Timeout: 10 * time.Second})
				return func(m *Model) tea.Cmd { m.report(r.OK(), fmt.Sprintf("tapped (%d,%d)", x, y)); return nil }
			})
		}})
}

func (s *screenTab) recordOpts(m *Model) tea.Cmd {
	if m.rec != nil {
		return m.record()
	}
	m.ask("Size WxH (empty = native; smaller = smaller files)", "", func(m *Model, size string) tea.Cmd {
		m.ask("Bit rate in Mbps (empty = device default)", "8", func(m *Model, br string) tea.Cmd {
			m.ask("Time limit in seconds (max 180)", "180", func(m *Model, lim string) tea.Cmd {
				var args []string
				if size = strings.TrimSpace(size); size != "" {
					args = append(args, "--size", size)
				}
				if br = strings.TrimSpace(br); br != "" {
					args = append(args, "--bit-rate", fmt.Sprint(atoi(br)*1000000))
				}
				if lim = strings.TrimSpace(lim); lim != "" {
					args = append(args, "--time-limit", fmt.Sprint(min(180, max(1, atoi(lim)))))
				}
				return m.record(args...)
			})
			return nil
		})
		return nil
	})
	return nil
}

// atoi parses a leading integer and returns 0 when there is none.
func atoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func (s *screenTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	lw := min(46, w/2)
	s.head = func(m *Model) string { return sDim.Render("screenshots, UI hierarchy → text, recorder") }
	left := s.viewRows(m, lw, h, focused)
	pw, ph := w-lw-3, h-3
	var right []string
	if s.png == nil {
		right = []string{"", sDim.Render("No screenshot yet — press c to capture."), sDim.Render("The preview uses half-block truecolor; f shows it at full resolution.")}
	} else {
		key := fmt.Sprintf("%d×%d", pw, ph)
		if s.prevKey != key {
			s.prev, _ = core.RenderHalfBlocks(s.png, pw, ph)
			s.prevKey = key
		}
		iw, ih := core.ImageSize(s.png)
		right = append([]string{sDim.Render(clipS(fmt.Sprintf("%s  (%d×%d)", filepath.Base(s.pngFile), iw, ih), pw))}, s.prev...)
	}
	out := make([]string, h)
	for i := range out {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = fit(l, lw) + sDim.Render(" │ ") + r
	}
	return out
}

func (s *screenTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	key := k.String()
	if s.nav(key) {
		return true, nil
	}
	switch key {
	case "enter":
		if r := s.cur(); r != nil {
			return true, s.acts()[r.d.(int)].fn(m)
		}
	case "c":
		return true, s.capture(m)
	case "f":
		return true, s.fullPreview(m)
	case "u":
		return true, s.acts()[4].fn(m)
	default:
		return false, nil
	}
	return true, nil
}
