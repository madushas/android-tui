package ui

import ()

func (m *Model) tgt() target {
	if m.grp == gEmu {
		if m.emuSel >= 0 && m.emuSel < len(m.sc.Emus) {
			return target{emu: &m.sc.Emus[m.emuSel]}
		}
		return target{}
	}
	if m.connSel >= 0 && m.connSel < len(m.sc.Conns) {
		return target{conn: &m.sc.Conns[m.connSel]}
	}
	return target{}
}

func (m *Model) curTabs() []Tab {
	var ts []Tab
	for _, id := range m.order[m.grp] {
		ts = append(ts, m.tabs[id])
	}
	return ts
}

// curIdx is the current flat tab index (clamped).
func (m *Model) curIdx() int { return min(m.tabIdx[m.grp], len(m.order[m.grp])-1) }

// modeRange returns the current mode's index and the [lo,hi) range of its tabs in m.order[m.grp].
func (m *Model) modeRange() (mi, lo, hi int) {
	idx, off := m.curIdx(), 0
	for i, md := range m.modes[m.grp] {
		if idx < off+len(md.ids) {
			return i, off, off + len(md.ids)
		}
		off += len(md.ids)
	}
	return 0, 0, len(m.order[m.grp])
}

// selectMode jumps to mode ni, resuming the tab last used there.
func (m *Model) selectMode(ni int) {
	ms := m.modes[m.grp]
	if ni < 0 || ni >= len(ms) {
		return
	}
	mi, _, _ := m.modeRange()
	m.lastTab[m.grp][mi] = m.curIdx()
	start := 0
	for i := 0; i < ni; i++ {
		start += len(ms[i].ids)
	}
	to := start
	if l := m.lastTab[m.grp][ni]; l >= start && l < start+len(ms[ni].ids) {
		to = l
	}
	m.tabIdx[m.grp] = to
	m.checkSelect()
}

func (m *Model) switchMode(d int) {
	mi, _, _ := m.modeRange()
	n := len(m.modes[m.grp])
	m.selectMode((mi + d + n) % n)
}

func (m *Model) clickMode(x int) {
	for _, h := range m.modeHits {
		if x >= h[0] && x < h[1] {
			m.selectMode(h[2])
		}
	}
}

func (m *Model) tab() Tab {
	return m.tabs[m.order[m.grp][m.curIdx()]]
}

func (m *Model) gotoTab(id string) {
	groups := []groupT{m.grp, gEmu, gConn} // prefer the group the user is in
	for _, g := range groups {
		for i, x := range m.order[g] {
			if x == id {
				m.grp, m.tabIdx[g] = g, i
				m.focus = fMain
				m.checkSelect()
				return
			}
		}
	}
}

func (m *Model) clickTab(x int) {
	for _, h := range m.tabHits {
		if x >= h[0] && x < h[1] {
			m.tabIdx[m.grp] = h[2]
		}
	}
}
