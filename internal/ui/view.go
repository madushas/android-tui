package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/madushas/android-tui/internal/core"
)

// narrow terminals (<90 cols) show one column at a time: the lists, or the main panel once it has focus.
func (m *Model) narrow() bool { return m.w < 90 }

// mainOnly: the main panel owns the whole screen (zoom, or narrow terminal with focus in the main panel).
func (m *Model) mainOnly() bool { return m.zoom || (m.narrow() && m.focus == fMain) }

func (m *Model) layout() map[string]rect {
	bh := m.h - 1
	if m.mainOnly() {
		return map[string]rect{"main": {0, 0, m.w, bh}}
	}
	lw := max(30, min(42, m.w*3/10))
	if m.w < 100 {
		lw = max(26, m.w/3)
	}
	listsOnly := m.narrow() // focus is in a list pane here (mainOnly handled above)
	if listsOnly {
		lw = m.w
	}
	hostH := 0
	if bh >= 30 {
		hostH = 7
	}
	snapH := 6
	if bh >= 34 {
		snapH = 8
	}
	rest := bh - hostH - snapH
	if rest < 10 {
		hostH, rest = 0, bh-snapH
	}
	emuH := rest * 55 / 100
	connH := rest - emuH
	logH := 0
	switch {
	case bh < 24 || m.logMode == 0:
	case m.logMode == 1:
		logH = 8
	default:
		logH = bh * 45 / 100
	}
	L := map[string]rect{
		"emu": {0, 0, lw, emuH}, "conn": {0, emuH, lw, connH}, "snap": {0, emuH + connH, lw, snapH},
	}
	if hostH > 0 {
		L["host"] = rect{0, emuH + connH + snapH, lw, hostH}
	}
	if !listsOnly {
		L["main"] = rect{lw, 0, m.w - lw, bh - logH}
		if logH > 0 {
			L["log"] = rect{lw, bh - logH, m.w - lw, logH}
		}
	}
	return L
}

func (m *Model) View() string {
	if m.w < 60 || m.h < 14 {
		return sErr.Render(fmt.Sprintf("terminal too small (%dx%d) — need 60x14", m.w, m.h))
	}
	L := m.layout()
	m.rects = L
	var lines []string
	if m.mainOnly() {
		lines = m.panelMain(L["main"])
	} else {
		left := m.panelEmus(L["emu"])
		left = append(left, m.panelConns(L["conn"])...)
		left = append(left, m.panelSnaps(L["snap"])...)
		if r, ok := L["host"]; ok {
			left = append(left, m.panelHost(r)...)
		}
		if mr, ok := L["main"]; ok {
			right := m.panelMain(mr)
			if r, ok := L["log"]; ok {
				right = append(right, m.panelLog(r)...)
			}
			lines = hjoin(left, right)
		} else {
			lines = left
		}
	}
	for len(lines) < m.h-1 {
		lines = append(lines, "")
	}
	lines = append(lines[:m.h-1], m.bar())
	m.topBox = rect{}
	for i, md := range m.modals {
		b := md.view(m.w, m.h)
		x, y := centerXY(b, m.w, m.h)
		if p, ok := md.(placedModal); ok {
			x, y = p.place(b, m.w, m.h)
		}
		if i == len(m.modals)-1 && len(b) > 0 {
			m.topBox = rect{x, y, sw(b[0]), len(b)}
		}
		lines = overlay(lines, b, x, y)
	}
	return strings.Join(lines, "\n")
}

// ─────────────── list panels ───────────────

type lrow struct{ plain, styled, right string }

func listBody(rows []lrow, sel int, top *int, w, h int, focused bool) []string {
	if sel >= *top+h {
		*top = sel - h + 1
	}
	if sel < *top {
		*top = sel
	}
	*top = max(0, min(*top, max(0, len(rows)-h)))
	var out []string
	for i := 0; i < h; i++ {
		k := *top + i
		if k >= len(rows) {
			break
		}
		r := rows[k]
		gap := w - sw(r.plain) - sw(r.right) - 1
		if k == sel {
			l := r.plain + strings.Repeat(" ", max(1, gap)) + r.right
			if focused {
				out = append(out, sSel.Render(fit(l, w)))
			} else {
				out = append(out, sBold.Render(fit("▸"+strings.TrimPrefix(l, " "), w)))
			}
			continue
		}
		out = append(out, r.styled+strings.Repeat(" ", max(1, gap))+sDim.Render(r.right))
	}
	return out
}

func (m *Model) glyph(state string, emu bool) (string, string) {
	switch state {
	case "running":
		if emu {
			return "●", "ok"
		}
		return "◆", "ok"
	case "device":
		return "◆", "ok"
	case "booting":
		return string([]rune("◐◓◑◒")[(m.frame/4)%4]), "warn"
	case "stopped":
		return "○", "dim"
	case "saved":
		return "○", "dim"
	}
	return "◇", "err"
}

func (m *Model) panelEmus(r rect) []string {
	var rows []lrow
	for _, e := range m.sc.Emus {
		g, st := m.glyph(e.State, true)
		name := e.Name
		if e.Custom {
			name += " ★"
		}
		nm := sty("")
		if e.State == "stopped" {
			nm = sDim
		}
		right := e.Info.Short()
		if sw(name)+sw(right)+5 > r.w-2 {
			right = ""
		}
		rows = append(rows, lrow{" " + g + " " + name, " " + sty(st).Render(g) + " " + nm.Render(name), right})
	}
	foot := ""
	if len(rows) > 0 {
		foot = fmt.Sprintf("%d of %d", m.emuSel+1, len(rows))
	} else if m.scanned {
		rows = append(rows, lrow{" no AVDs — press n", sDim.Render(" no AVDs — press n"), ""})
	} else {
		rows = append(rows, lrow{" scanning…", sDim.Render(" scanning…"), ""})
	}
	sel := m.emuSel
	if len(m.sc.Emus) == 0 {
		sel = -1
	}
	title := sBold.Render("[1] Emulators")
	if m.focus == fEmu {
		title = sAccB.Render("[1] Emulators")
	}
	return box(title, listBody(rows, sel, &m.emuTop, r.w-2, r.h-2, m.focus == fEmu), r.w, r.h, m.focus == fEmu, foot)
}

func (m *Model) panelConns(r rect) []string {
	var rows []lrow
	for _, c := range m.sc.Conns {
		g, st := m.glyph(c.State, false)
		name := c.Name()
		tr := c.Transport
		if c.State != "device" && c.State != "saved" {
			tr = c.State
		}
		nm := sty("")
		if c.State == "saved" {
			nm = sDim
		}
		rows = append(rows, lrow{" " + g + " " + name, " " + sty(st).Render(g) + " " + nm.Render(name), tr})
	}
	foot := ""
	if len(rows) > 0 {
		foot = fmt.Sprintf("%d of %d", m.connSel+1, len(rows))
	} else {
		rows = append(rows, lrow{" none — p pair · c connect", sDim.Render(" none — p pair · c connect"), ""})
	}
	sel := m.connSel
	if len(m.sc.Conns) == 0 {
		sel = -1
	}
	title := sBold.Render("[2] adb connections")
	if m.focus == fConn {
		title = sAccB.Render("[2] adb connections")
	}
	return box(title, listBody(rows, sel, &m.connTop, r.w-2, r.h-2, m.focus == fConn), r.w, r.h, m.focus == fConn, foot)
}

func (m *Model) panelSnaps(r rect) []string {
	t := m.tgt()
	title := "[3] AVD snapshots"
	var rows []lrow
	if m.grp == gEmu && t.isEmu() {
		title += " · " + t.emu.Name
		for _, s := range m.snaps {
			n := s.name
			if n == "default_boot" {
				n += " ⚡"
			}
			rows = append(rows, lrow{" " + n, " " + n, s.size})
		}
		if len(rows) == 0 {
			h := " no snapshots"
			if t.online() {
				h += " — n to save"
			}
			rows = append(rows, lrow{h, sDim.Render(h), ""})
		}
	} else {
		rows = append(rows, lrow{" (emulators only)", sDim.Render(" (emulators only)"), ""})
	}
	f := m.focus == fSnap
	if f {
		title = sAccB.Render(title)
	} else {
		title = sBold.Render(title)
	}
	sel := m.snapSel
	if len(m.snaps) == 0 {
		sel = -1
	}
	foot := ""
	if len(m.snaps) > 0 {
		foot = fmt.Sprintf("%d of %d", m.snapSel+1, len(m.snaps))
	}
	return box(title, listBody(rows, sel, &m.snapTop, r.w-2, r.h-2, f), r.w, r.h, f, foot)
}

func (m *Model) panelHost(r rect) []string {
	h := m.host
	bw := max(6, r.w-2-17)
	memP := 0.0
	if h.MemTotal > 0 {
		memP = 100 * float64(h.MemUsed) / float64(h.MemTotal)
	}
	active, rss := 0, int64(0)
	for _, e := range m.sc.Emus {
		if e.Active() {
			active++
			rss += e.RSS
		}
	}
	kvm := sOK.Render("✓ ok")
	if h.KVM != "ok" && h.KVM != "" {
		kvm = sErr.Render("✗ " + h.KVM)
	}
	upd := ""
	if m.upd > 0 {
		upd = sWarn.Render(fmt.Sprintf("   ⬆ %d SDK update(s): u", m.upd))
	}
	body := []string{
		" " + sBold.Render("CPU ") + loadStyle(h.CPU).Render(bar(h.CPU/100, bw)) + fmt.Sprintf(" %3.0f%%", h.CPU),
		" " + sBold.Render("MEM ") + loadStyle(memP).Render(bar(memP/100, bw)) + fmt.Sprintf(" %3.0f%%", memP),
		fmt.Sprintf(" EMU %d active · %s RSS", active, human(float64(rss)*1024)),
		fmt.Sprintf(" DSK %s free (AVD volume)", human(float64(h.DiskFree))),
		" KVM " + kvm + upd,
	}
	return box(sBold.Render("Host"), body, r.w, r.h, false, "")
}

func (m *Model) panelLog(r rect) []string {
	ents := core.CmdLog.Last(r.h - 2)
	var body []string
	for _, e := range ents {
		st := map[string]string{"cmd": "bold", "ok": "ok", "err": "err", "info": "blue"}[e.Kind]
		body = append(body, " "+sDim.Render(e.T.Format("15:04:05"))+" "+sty(st).Render(strings.ReplaceAll(e.Text, "\n", " ")))
	}
	return box(sBold.Render("Command log"), body, r.w, r.h, false, "")
}

func (m *Model) panelMain(r rect) []string {
	tabs := m.curTabs()
	cur := m.curIdx()
	mi, lo, hi := m.modeRange()
	f := m.focus == fMain
	// title row: modes
	var sb strings.Builder
	m.modeHits = nil
	x := r.x + 3
	for i, md := range m.modes[m.grp] {
		lbl := " " + md.name + " "
		st := sDim
		if i == mi {
			st = sRev
			if f {
				st = sSel
			}
		}
		sb.WriteString(st.Render(lbl))
		m.modeHits = append(m.modeHits, [3]int{x, x + sw(lbl), i})
		x += sw(lbl)
		if i < len(m.modes[m.grp])-1 {
			sb.WriteString(sDim.Render("│"))
			x++
		}
	}
	if m.zoom {
		sb.WriteString(sWarn.Render("   ZOOM · Z or Esc to exit"))
	} else if m.narrow() {
		sb.WriteString(sDim.Render("  Esc: lists"))
	} else if r.w > 90 {
		sb.WriteString(sDim.Render("   { } mode · [ ] tab · . menu"))
	}
	// first body row: tabs of the current mode
	var tb strings.Builder
	m.tabHits = nil
	x = r.x + 1
	for i := lo; i < hi; i++ {
		lbl := " " + tabs[i].Name() + " "
		st := sDim
		if i == cur {
			st = sAccB.Underline(true)
		}
		tb.WriteString(st.Render(lbl))
		m.tabHits = append(m.tabHits, [3]int{x, x + sw(lbl), i})
		x += sw(lbl)
		if i < hi-1 {
			tb.WriteString(sDim.Render("│"))
			x++
		}
	}
	t := m.tgt()
	foot := m.modes[m.grp][mi].name + " › " + tabs[cur].Name() // breadcrumb
	if t.valid() {
		kind := "adb"
		if t.isEmu() {
			kind = "emulator"
		}
		foot += " · " + kind + " · " + t.name()
	}
	if tf := tabs[cur].Footer(); tf != "" {
		foot += " · " + tf
	}
	body := append([]string{fit(tb.String(), r.w-2)}, tabs[cur].View(m, r.w-2, r.h-3, f)...)
	return box(sb.String(), body, r.w, r.h, f, foot)
}

func (m *Model) bar() string {
	var hs []hint
	switch m.focus {
	case fEmu:
		hs = []hint{{"s", "start"}, {"x", "stop"}, {"r", "restart"}, {"c", "cold"}, {"S", "shot"}, {"I", "install"}, {"t", "shell"}, {"n", "new"}, {"d", "del"}, {"e", "edit"}, {":", "actions"}, {"?", "help"}}
	case fConn:
		hs = []hint{{"⏎", "open/connect"}, {"p", "QR pair"}, {"c", "connect"}, {"d", "disconnect"}, {"s", "save"}, {"S", "shot"}, {"I", "install"}, {"t", "shell"}, {":", "actions"}, {"?", "help"}}
	case fSnap:
		hs = []hint{{"⏎", "boot"}, {"l", "load live"}, {"n", "save new"}, {"d", "delete"}, {"Tab", "next"}, {"?", "help"}}
	default:
		hs = append(m.tab().Hints(), hint{"Esc", "back"}, hint{"?", "help"})
	}
	var left strings.Builder
	for _, h := range hs {
		left.WriteString(sAccB.Render(" "+h.k) + sDim.Render(" "+h.l+" "))
	}
	right := ""
	switch {
	case m.toast != "":
		st := toastStyle(m.toastK)
		right = st.Render(" " + clipS(m.toast, m.w/2+10) + " ")
	case m.busy > 0:
		right = sAcc.Render(fmt.Sprintf(" %s %d working ", spinner(m.frame), m.busy))
	}
	if m.rec != nil {
		el := int(time.Since(m.rec.t0).Seconds())
		right = sErrB.Render(fmt.Sprintf(" ● REC %d:%02d ", el/60, el%60)) + right
	}
	return fit(fit(left.String(), m.w-sw(right))+right, m.w)
}

func toastStyle(k string) lipglossStyle {
	switch k {
	case "ok":
		return sOKB
	case "err":
		return sErrB
	}
	return sWarnB
}
