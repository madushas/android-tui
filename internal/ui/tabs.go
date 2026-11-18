package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

// Tab is one page of the main panel.
type Tab interface {
	id() string
	Name() string
	Global() bool
	Tick(m *Model) tea.Cmd
	Bg(m *Model)
	Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd)
	View(m *Model, w, h int, focused bool) []string
	Hints() []hint
	Footer() string
	Typing() bool
	Click(m *Model, row int)
	Scroll(d int)
	Reload()
}

type baseTab struct {
	ident, name string
	global      bool
	sig         string
	gen         int
	listTop     int // first screen row of the list inside the tab area (for mouse clicks)
}

func (b *baseTab) id() string                                 { return b.ident }
func (b *baseTab) Name() string                               { return b.name }
func (b *baseTab) Global() bool                               { return b.global }
func (b *baseTab) Tick(m *Model) tea.Cmd                      { return nil }
func (b *baseTab) Bg(m *Model)                                {}
func (b *baseTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) { return false, nil }
func (b *baseTab) Hints() []hint                              { return nil }
func (b *baseTab) Footer() string                             { return "" }
func (b *baseTab) Typing() bool                               { return false }
func (b *baseTab) Click(m *Model, row int)                    {}
func (b *baseTab) Scroll(d int)                               {}
func (b *baseTab) Reload()                                    { b.sig = "·" }

// stale reports (once) that the target changed and the tab must reload.
func (b *baseTab) stale(m *Model) bool {
	t := m.tgt()
	sig := ""
	if t.online() {
		sig = t.serial() + "|" + t.state()
	}
	if sig != b.sig && m.settled() {
		b.sig = sig
		b.gen++
		return true
	}
	return false
}

// offline draws the standard "nothing to show" guidance; returns true if it did.
func offline(m *Model, w, h int) ([]string, bool) {
	t := m.tgt()
	if t.online() {
		return nil, false
	}
	out := []string{""}
	for i, l := range offlineHelp(t, m.haveAdb) {
		st := sDim
		if i == 0 {
			st = sBold
		}
		out = append(out, "  "+st.Render(clipS(l, max(10, w-4))))
	}
	return out, true
}

// offlineHelp explains why a tab is empty and what to do about it (first line = the state, rest = actions).
func offlineHelp(t target, adbOK bool) []string {
	switch {
	case !adbOK && !t.valid():
		return []string{"adb was not found on your PATH.", core.AdbInstallHint, "Then restart android-tui. Press D for Doctor to check the whole setup."}
	case t.isEmu():
		if t.state() == "booting" {
			return []string{t.name() + " is booting…", "Tabs unlock when Android finishes starting (usually under a minute)."}
		}
		return []string{t.name() + " is " + t.state() + ".",
			"s start · c cold boot · H headless — in the Emulators panel (press 1)",
			"AVD hardware and images: Emulator mode ( } ) → AVD / Images"}
	case t.valid():
		switch t.state() {
		case "unauthorized":
			return []string{t.name() + " is unauthorized.",
				"Unlock the phone and accept “Allow USB debugging?” (tick “Always allow”), then press F5.",
				"Still stuck? Developer options → Revoke USB debugging authorisations, then replug."}
		case "offline":
			return []string{t.name() + " is offline: adb sees it but it does not answer.",
				"Replug the cable or toggle USB debugging. Wi-Fi device: reconnect from Connect mode ( } ).",
				"Last resort: adb kill-server && adb start-server"}
		case "saved":
			return []string{t.name() + " is a saved connection (not connected).",
				"Select it in the connections panel (press 2) and press Enter to connect."}
		}
		return []string{t.name() + " is " + t.state() + "."}
	}
	return []string{"No target selected.",
		"Press 1 to pick an emulator or 2 to pick an adb connection.",
		"No device yet? Plug in a phone with USB debugging on, or pair by QR (Android 11+) in Connect mode.",
		"No emulator yet? Press 1, then n to create an AVD."}
}

// ───────────── rows-based tabs ─────────────

type row struct {
	hdr  bool
	text string // plain (used when selected)
	sty  string // styled version (used when not selected)
	d    any
}

type rowsTab struct {
	baseTab
	rows     []row
	sel, top int
	flt      string
	build    func(m *Model) []row
	head     func(m *Model) string
	page     int
}

func hdrRow(t string) row { return row{hdr: true, text: t} }
func itemRow(plain, styled string, d any) row {
	if styled == "" {
		styled = plain
	}
	return row{text: plain, sty: styled, d: d}
}

func (r *rowsTab) cur() *row {
	if r.sel >= 0 && r.sel < len(r.rows) && !r.rows[r.sel].hdr {
		return &r.rows[r.sel]
	}
	return nil
}

func (r *rowsTab) step(d int) {
	for i := r.sel + d; i >= 0 && i < len(r.rows); i += d {
		if !r.rows[i].hdr {
			r.sel = i
			return
		}
	}
}

func (r *rowsTab) nav(k string) bool {
	switch k {
	case "up", "k":
		r.step(-1)
	case "down", "j":
		r.step(1)
	case "pgup":
		for i := 0; i < max(1, r.page); i++ {
			r.step(-1)
		}
	case "pgdown":
		for i := 0; i < max(1, r.page); i++ {
			r.step(1)
		}
	case "home", "g":
		r.sel = -1
		r.step(1)
	case "end", "G":
		r.sel = len(r.rows)
		r.step(-1)
	default:
		return false
	}
	return true
}

func (r *rowsTab) Click(m *Model, y int) {
	i := r.top + y - r.listTop
	if y >= r.listTop && i >= 0 && i < len(r.rows) && !r.rows[i].hdr {
		r.sel = i
	}
}

// hitRow reports whether y (a row inside the tab area) is on a selectable row.
func (r *rowsTab) hitRow(y int) bool {
	i := r.top + y - r.listTop
	return y >= r.listTop && i >= 0 && i < len(r.rows) && !r.rows[i].hdr
}

// anchorRow is the tab-area row of the selection (for menus opened from the keyboard), or -1 when it is not visible.
func (r *rowsTab) anchorRow() int {
	if r.sel < r.top || r.sel >= len(r.rows) {
		return -1
	}
	return r.sel - r.top + r.listTop
}

func (r *rowsTab) Scroll(d int) {
	for i := 0; i < abs(d); i++ {
		r.step(d / abs(d))
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func (r *rowsTab) viewRows(m *Model, w, h int, focused bool) []string {
	var out []string
	hl := 0
	if r.head != nil {
		out = append(out, " "+r.head(m))
		hl = 1
	}
	r.rows = r.build(m)
	n := len(r.rows)
	if r.sel >= n {
		r.sel = n - 1
	}
	if r.sel < 0 || (n > 0 && r.rows[max(r.sel, 0)].hdr) {
		r.sel = max(r.sel, 0)
		r.step(1)
		if n > 0 && r.rows[r.sel].hdr {
			r.step(-1)
		}
	}
	ph := h - hl
	r.page, r.listTop = ph, hl
	if r.sel >= r.top+ph {
		r.top = r.sel - ph + 1
	}
	if r.sel < r.top {
		r.top = r.sel
	}
	r.top = max(0, min(r.top, max(0, n-ph)))
	for i := 0; i < ph; i++ {
		k := r.top + i
		if k >= n {
			break
		}
		rw := r.rows[k]
		switch {
		case rw.hdr:
			out = append(out, sDim.Render(" ─ "+rw.text+" "+strings.Repeat("─", max(0, w-sw(rw.text)-5))))
		case k == r.sel && focused:
			out = append(out, sSel.Render(fit("  "+rw.text, w)))
		case k == r.sel:
			out = append(out, sBold.Render(fit("▸ "+rw.text, w)))
		default:
			out = append(out, "  "+rw.sty)
		}
	}
	return out
}

func (r *rowsTab) askFilter(m *Model) {
	m.ask("Filter (empty clears)", r.flt, func(m *Model, v string) tea.Cmd {
		r.flt = strings.TrimSpace(v)
		r.sel, r.top = 0, 0
		return nil
	})
}

func (r *rowsTab) match(s string) bool {
	return r.flt == "" || strings.Contains(strings.ToLower(s), strings.ToLower(r.flt))
}

// ───────────── Model helpers used by tabs ─────────────

func (m *Model) ask(title, def string, ok func(m *Model, v string) tea.Cmd) {
	m.push(&inputModal{title: title, ed: newEdit(def), ok: ok, ci: -1})
}

func (m *Model) askPath(title, def, hint string, ok func(m *Model, v string) tea.Cmd) {
	m.push(&inputModal{title: title, hint: hint, ed: newEdit(def), ok: ok, complete: completePath, ci: -1})
}

func (m *Model) confirm(msg string, danger bool, yes cont) {
	m.push(&confirmModal{msg: msg, danger: danger, yes: yes})
}

// confirmTyped asks the user to type a word first: reserved for catastrophic actions (wipe / delete / revert everything).
func (m *Model) confirmTyped(msg, word string, yes cont) {
	m.push(&typedConfirmModal{msg: msg, word: word, yes: yes})
}

func (m *Model) choose(title string, items []string, pick func(m *Model, i int) tea.Cmd) *chooseModal {
	var its []item
	for _, s := range items {
		its = append(its, item{text: s})
	}
	c := &chooseModal{title: title, items: its, flt: newEdit(""), pick: pick, marks: map[int]bool{}}
	m.push(c)
	return c
}

func (m *Model) multi(title string, items []string, preset []int, allowEmpty bool, picks func(m *Model, idx []int) tea.Cmd) {
	var its []item
	for _, s := range items {
		its = append(its, item{text: s})
	}
	c := &chooseModal{title: title, items: its, flt: newEdit(""), multi: true, marks: map[int]bool{}, picks: picks, allowEmpty: allowEmpty}
	for _, i := range preset {
		c.marks[i] = true
	}
	m.push(c)
}

func (m *Model) viewText(title string, lines []string) { m.push(newViewer(title, lines)) }

// adbDo runs one device shell script and reports the outcome.
func (m *Model) adbDo(label, script string, then func(m *Model) tea.Cmd) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s := t.serial()
	return m.job(label, false, func() cont {
		r := core.Sh(s, script, core.Opt{Timeout: 60e9})
		out := strings.TrimSpace(r.Out)
		bad := !r.OK() || strings.HasPrefix(out, "Error") || strings.HasPrefix(out, "Failure") || strings.HasPrefix(out, "Exception")
		msg := label
		if bad {
			msg = label + ": " + r.Why()
			if out != "" {
				l := strings.Split(out, "\n")
				msg = label + ": " + l[len(l)-1]
			}
		}
		return func(m *Model) tea.Cmd {
			m.report(!bad, msg)
			if !bad && then != nil {
				return then(m)
			}
			return nil
		}
	})
}

// showAdb runs a device shell script and shows its output in a viewer.
func (m *Model) showAdb(title, script string) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s := t.serial()
	return m.job(title, false, func() cont {
		r := core.Sh(s, script, core.Opt{Timeout: 60e9})
		txt := r.Out
		if strings.TrimSpace(txt) == "" {
			txt = r.Err
		}
		lines := strings.Split(strings.TrimRight(txt, "\n"), "\n")
		if len(lines) > 20000 {
			lines = lines[:20000]
		}
		return func(m *Model) tea.Cmd { m.viewText(title, lines); return nil }
	})
}

func (m *Model) showHost(title string, argv ...string) tea.Cmd {
	return m.job(title, false, func() cont {
		r := core.Run(argv, core.Opt{Timeout: 60e9})
		lines := strings.Split(strings.TrimRight(r.Both(), "\n"), "\n")
		return func(m *Model) tea.Cmd { m.viewText(title, lines); return nil }
	})
}

func splitLines(s string) []string { return strings.Split(s, "\n") }
func trimS(s string) string        { return strings.TrimSpace(s) }

// fieldsN splits into at most n fields; the last field keeps the remainder (spaces included).
func fieldsN(s string, n int) []string {
	var out []string
	s = strings.TrimSpace(s)
	for len(out) < n-1 {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			break
		}
		out = append(out, s[:i])
		s = strings.TrimSpace(s[i:])
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
