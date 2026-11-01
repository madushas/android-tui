package ui

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type filesTab struct {
	rowsTab
	cwd     string
	ents    []core.FileEnt
	loading bool
}

func newFiles() Tab {
	f := &filesTab{rowsTab: rowsTab{baseTab: baseTab{ident: "files", name: "Files"}}, cwd: "/sdcard"}
	f.build = f.buildRows
	return f
}

func (f *filesTab) reset() { f.ents = nil }

func (f *filesTab) Tick(m *Model) tea.Cmd {
	if f.stale(m) {
		f.reset()
		if m.tgt().online() {
			return f.go_(m, f.cwd)
		}
	}
	return nil
}

func (f *filesTab) go_(m *Model, p string) tea.Cmd {
	s, gen, prev := m.tgt().serial(), f.gen, f.cwd
	p = path.Clean(p)
	f.loading = true
	return m.job("ls "+p, false, func() cont {
		r := core.Sh(s, "ls -lA "+core.Quote(strings.TrimRight(p, "/")+"/"), core.Opt{Timeout: 20 * time.Second})
		ents := core.ParseLs(r.Out)
		return func(m *Model) tea.Cmd {
			if gen != f.gen {
				return nil
			}
			f.loading = false
			if !r.OK() && len(ents) == 0 {
				m.report(false, p+": "+r.Why())
				f.cwd = prev
				return nil
			}
			f.cwd, f.ents, f.sel, f.top = p, ents, 0, 0
			return nil
		}
	})
}

func (f *filesTab) buildRows(m *Model) []row {
	var rows []row
	for _, e := range f.ents {
		if !f.match(e.Name) {
			continue
		}
		name := e.Name
		if e.Dir {
			name += "/"
		}
		if e.Link != "" {
			name += " → " + e.Link
		}
		sz := ""
		if !e.Dir {
			sz = human(float64(e.Size))
		}
		meta := fmt.Sprintf("%s %7s %s ", e.Perm, sz, e.Date)
		st := sNone
		if e.Dir {
			st = sBlue
		}
		rows = append(rows, row{text: meta + name, sty: sDim.Render(meta) + st.Render(name), d: e})
	}
	return rows
}

func (f *filesTab) ent() *core.FileEnt {
	if r := f.cur(); r != nil {
		e := r.d.(core.FileEnt)
		return &e
	}
	return nil
}

func (f *filesTab) rp(e *core.FileEnt) string { return strings.TrimRight(f.cwd, "/") + "/" + e.Name }

func (f *filesTab) Hints() []hint {
	return []hint{{"⏎/l", "open"}, {"h", "up"}, {"P", "pull"}, {"u", "push"}, {"v", "view"}, {"m", "mkdir"}, {"D", "delete"}, {"R", "rename"}, {"y", "copy path"}, {"g", "goto"}, {"/", "filter"}}
}

func (f *filesTab) Footer() string {
	return fmt.Sprintf("%d of %d", min(f.sel+1, len(f.rows)), len(f.rows))
}

func (f *filesTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	f.head = func(m *Model) string {
		s := sAccB.Render(f.cwd)
		if f.flt != "" {
			s += sWarn.Render("   filter: " + f.flt)
		}
		if f.loading {
			s += sDim.Render("   " + spinner(m.frame))
		}
		return s
	}
	out := f.viewRows(m, w, h, focused)
	if len(f.rows) == 0 && !f.loading {
		out = append(out, "  "+sDim.Render("(empty)"))
	}
	return out
}

func (f *filesTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if f.nav(s) {
		return true, nil
	}
	e, serial := f.ent(), m.tgt().serial()
	switch s {
	case "h", "backspace", "left":
		return true, f.go_(m, path.Dir(strings.TrimRight(f.cwd, "/")))
	case "enter", "l", "right":
		if e != nil {
			if e.Dir {
				return true, f.go_(m, f.rp(e))
			}
			return true, f.view(m, e)
		}
	case "v":
		if e != nil && !e.Dir {
			return true, f.view(m, e)
		}
	case "/":
		f.askFilter(m)
	case "~":
		return true, f.go_(m, "/sdcard")
	case "g":
		m.ask("Go to path", f.cwd, func(m *Model, v string) tea.Cmd {
			if v != "" {
				return f.go_(m, v)
			}
			return nil
		})
	case "r":
		return true, f.go_(m, f.cwd)
	case "y":
		if e != nil {
			m.say(map[bool]string{true: "copied path", false: "no clipboard tool found"}[core.CopyText(f.rp(e))])
		}
	case "P", "d":
		if e != nil {
			core.MkdirP(core.Cfg.PullDir)
			m.push(newTask("Pull — "+e.Name, [][]string{{"adb", "-s", serial, "pull", f.rp(e), core.Cfg.PullDir + "/"}}, func(m *Model, rc int, _ []string) tea.Cmd {
				m.report(rc == 0, "pulled "+e.Name+" to "+core.Cfg.PullDir)
				return nil
			}))
		}
	case "u", "p":
		m.askPath("Push local file or folder to "+f.cwd, "", "", func(m *Model, v string) tea.Cmd {
			src := core.NormPath(v)
			if _, err := os.Stat(src); err != nil {
				m.report(false, "not found: "+src)
				return nil
			}
			m.push(newTask("Push", [][]string{{"adb", "-s", serial, "push", src, strings.TrimRight(f.cwd, "/") + "/"}}, func(m *Model, rc int, _ []string) tea.Cmd {
				return f.go_(m, f.cwd)
			}))
			return nil
		})
	case "m":
		m.ask("New folder name", "", func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}
			return m.adbDo("mkdir", "mkdir -p "+core.Quote(strings.TrimRight(f.cwd, "/")+"/"+v), func(m *Model) tea.Cmd { return f.go_(m, f.cwd) })
		})
	case "D":
		if e != nil {
			m.confirm("Delete "+f.rp(e)+map[bool]string{true: " (recursively)"}[e.Dir]+"?", true, func(m *Model) tea.Cmd {
				return m.adbDo("delete", "rm -rf "+core.Quote(f.rp(e)), func(m *Model) tea.Cmd { return f.go_(m, f.cwd) })
			})
		}
	case "R":
		if e != nil {
			m.ask("Rename "+e.Name+" to", e.Name, func(m *Model, v string) tea.Cmd {
				if v == "" || v == e.Name {
					return nil
				}
				return m.adbDo("rename", "mv "+core.Quote(f.rp(e))+" "+core.Quote(strings.TrimRight(f.cwd, "/")+"/"+v), func(m *Model) tea.Cmd { return f.go_(m, f.cwd) })
			})
		}
	case "esc":
		if f.flt != "" {
			f.flt = ""
			return true, nil
		}
		return false, nil
	default:
		return false, nil
	}
	return true, nil
}

func (f *filesTab) view(m *Model, e *core.FileEnt) tea.Cmd {
	s := m.tgt().serial()
	if e.Size > 2_000_000 {
		m.report(false, fmt.Sprintf("%s is %s — pull it instead (P)", e.Name, human(float64(e.Size))))
		return nil
	}
	p := f.rp(e)
	return m.job("cat", false, func() cont {
		r := core.Sh(s, "cat "+core.Quote(p), core.Opt{Timeout: 30 * time.Second})
		return func(m *Model) tea.Cmd {
			if strings.Contains(r.Out, "\x00") {
				m.report(false, "binary file — pull it instead (P)")
				return nil
			}
			ls := strings.Split(strings.TrimRight(r.Out, "\n"), "\n")
			m.viewText(filepath.ToSlash(p), ls)
			return nil
		}
	})
}
