package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

const helpText = `android-tui %s — Android device & emulator suite

 LAYOUT      [1] Emulators (AVDs)   [2] adb connections (USB / Wi-Fi / remote)   [3] AVD snapshots   Host   Command log
             The main panel follows what you selected: pick an emulator and you get AVD + Images tabs,
             pick an adb connection and you get the Connect tab. Both share Overview/Apps/Files/…/System.
 PANELS      Tab / Shift-Tab cycle · 1 emulators · 2 connections · 3 AVD snapshots · →/l into the main panel · Z zoom main panel · Esc back
             { } switch mode · [ ] switch tab inside the mode · L cycle command log (normal/large/hidden) · F5 refresh
 MOUSE       click select · double-click = Enter · right-click, . or Ctrl-O = context menu on any list or tab · wheel scrolls · click modes/tabs
 MODES       Device [Overview Apps Files Screen Shell] · Optimize [Tweaks Debloat Procs Control Journal] · Inspect [Logcat Props System Diff Tools]
             plus Emulator [AVD Images] for an emulator, or Connect for adb connections. { } remembers the last tab used in each mode.
 EVERYWHERE  : or Ctrl-P action palette (type to filter) · ? help · q quit

 EMULATORS   s/Enter start · x stop · r restart · c cold boot · H headless · W wipe data · n new · d delete · R rename · C clone
             e edit hardware (AVD tab) · E edit config.ini · i change image · F launch flags · o emulator log
             S screenshot · I install APK · t adb shell · M scrcpy · u SDK updates · D doctor
 CONNECTIONS Enter open/connect · p pair with QR (Android 11+) · c connect host:port · a save · d disconnect/remove
             S screenshot · I install APK · t adb shell · M scrcpy
 SNAPSHOTS   Enter boot from snapshot · l load into the running emulator (no restart) · n save new · d delete

 OVERVIEW    live CPU / memory / network / battery / disk graphs + device & AVD facts
 AVD         hardware, system image, launch flags — Enter edits a field inline
 APPS        package manager: Space marks (a = all) → batch launch/stop/clear/uninstall/disable/enable/pull · p permission toggles
             m more (standby bucket, doze, appops, compile…) · f scope user/system/enabled/disabled/removed/all · s sort · x export
             i install APK · U install from URL · Enter on a 'removed' system app restores it · l logcat for the app · / filter
 FILES       Enter/l open · h up · P pull · u push (Tab completes) · v view · m mkdir · D delete · R rename · g goto · y copy path
 PROPS       / filter · y copy value · c copy key=value · e setprop · s save all
 LOGCAT      v d i w e min level · / regex filter · p pause · c clear · a one app · s save · G follow · Enter full line
 SHELL       runs adb shell <cmd> · ↑/↓ history · Ctrl-T real shell · Ctrl-L clear · Ctrl-N/B change tab (in this mode) · Esc back
 ROOT AVD     power users: palette › "Root AVD with rootAVD…" (needs your own copy of the third-party rootAVD script: set ROOTAVD in the config)
 OPTIMIZE    TWEAKS Enter apply/preview · Space mark · A apply marked · DEBLOAT d disable (snapshot first) · e restore · JOURNAL Enter revert · R revert all
             every change is journalled with the value it replaced, and an 'auto' settings snapshot is taken before the first change
 CONTROL     dark mode, Wi-Fi, airplane, proxy, density/size, animations, battery, GPS, SMS, calls, developer options
 TOOLS       the whole palette as a grouped toolbox
 SYSTEM      m cycles: Android settings editor · processes (x kills) · dumpsys services
 IMAGES      installed images (official + custom ROMs) · official downloads · /e/OS builds · import .zip/URL/folder · n new AVD
 CONNECT     devices, mDNS discovery, QR/code pairing, connect by IP, LAN scan, USB→Wi-Fi, saved connections, remote adb server

 Every adb command android-tui runs appears in the Command log. Config: android-tui config (bash syntax; v1/v2 settings still honoured).`

func (m *Model) showHelp() {
	m.push(newViewer("Help", strings.Split(fmt.Sprintf(helpText, core.Version), "\n")))
}

func (m *Model) setFocus(f focusT) {
	m.focus = f
	if f != fMain {
		m.zoom = false
	}
	switch f {
	case fEmu, fSnap:
		m.grp = gEmu
	case fConn:
		m.grp = gConn
	}
	m.checkSelect()
}

func (m *Model) cycle(d int) {
	order := []focusT{fEmu, fConn, fSnap, fMain}
	for i, f := range order {
		if f == m.focus {
			m.setFocus(order[(i+d+len(order))%len(order)])
			return
		}
	}
}

// switchTab cycles through the tabs of the current mode only (≤5); { } change mode.
func (m *Model) switchTab(d int) {
	_, lo, hi := m.modeRange()
	n := hi - lo
	m.tabIdx[m.grp] = lo + (m.curIdx()-lo+d+n)%n
}

func move(s string, sel *int, n, page int) bool {
	switch s {
	case "up", "k":
		*sel--
	case "down", "j":
		*sel++
	case "pgup":
		*sel -= page
	case "pgdown":
		*sel += page
	case "home", "g":
		*sel = 0
	case "end", "G":
		*sel = n - 1
	default:
		return false
	}
	*sel = max(0, min(*sel, n-1))
	return true
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	s := k.String()
	if s == "ctrl+o" { // context menu that also works inside text-input tabs, where "." is just a character
		return m.openContextAtSelection()
	}
	typing := m.focus == fMain && m.tab().Typing()
	switch {
	case s == "ctrl+c":
		m.quitting = true
		return nil
	case typing && (s == "ctrl+n" || s == "ctrl+b"):
		m.switchTab(map[bool]int{true: 1, false: -1}[s == "ctrl+n"])
		return nil
	case s == "ctrl+z":
		return m.undoLast()
	case s == "ctrl+p" || (!typing && s == ":"):
		return m.palette()
	case s == "tab":
		m.cycle(1)
		return nil
	case s == "shift+tab":
		m.cycle(-1)
		return nil
	}
	if !typing {
		switch s {
		case "q":
			m.quitting = true
			return nil
		case "?":
			m.showHelp()
			return nil
		case ".", "shift+f10":
			return m.openContextAtSelection()
		case "Z":
			m.zoom = !m.zoom
			if m.zoom {
				m.focus = fMain
			}
			return nil
		case "1":
			m.setFocus(fEmu)
			return nil
		case "2":
			m.setFocus(fConn)
			return nil
		case "3":
			m.setFocus(fSnap)
			return nil
		case "[":
			m.switchTab(-1)
			return nil
		case "]":
			m.switchTab(1)
			return nil
		case "{":
			m.switchMode(-1)
			return nil
		case "}":
			m.switchMode(1)
			return nil
		case "L":
			m.logMode = (m.logMode + 1) % 3
			return nil
		case "f5", "ctrl+r":
			m.tab().Reload()
			m.say("refreshing…")
			return m.refreshNow()
		}
	}
	switch m.focus {
	case fEmu:
		return m.keyEmu(s)
	case fConn:
		return m.keyConn(s)
	case fSnap:
		return m.keySnap(s)
	}
	handled, cmd := m.tab().Key(m, k)
	if !handled && (s == "esc" || (s == "h" && m.tab().id() != "files") || (s == "left" && m.tab().id() != "files")) {
		if m.grp == gEmu {
			m.setFocus(fEmu)
		} else {
			m.setFocus(fConn)
		}
	}
	return cmd
}

func (m *Model) keyEmu(s string) tea.Cmd {
	page := max(1, m.rects["emu"].h-2)
	if move(s, &m.emuSel, len(m.sc.Emus), page) {
		m.checkSelect()
		return nil
	}
	e := m.emuTarget()
	switch s {
	case "right", "l":
		m.setFocus(fMain)
	case "n":
		return m.newAVD()
	case "u":
		return m.maintenance()
	case "D":
		return m.doctor()
	case "a":
		return m.palette()
	}
	if e == nil {
		return nil
	}
	switch s {
	case "s", "enter":
		if e.State == "stopped" {
			return m.startEmu(e.Name, "warm")
		} else if s == "enter" {
			m.setFocus(fMain)
		} else {
			m.say(e.Name + " is already " + e.State)
		}
	case "H":
		if !e.Active() {
			return m.startEmu(e.Name, "headless")
		}
		m.say(e.Name + " is already " + e.State)
	case "x":
		return m.stopEmu()
	case "r":
		return m.startEmu(e.Name, "warm")
	case "c":
		return m.startEmu(e.Name, "cold")
	case "W":
		return m.wipeEmu()
	case "d":
		return m.deleteEmu()
	case "e":
		m.gotoTab("avd")
	case "E":
		return m.rawEdit()
	case "i":
		return m.changeImage(e.Name)
	case "F":
		return m.editFlags(e.Name)
	case "R":
		return m.renameEmu()
	case "C":
		return m.cloneEmu()
	case "o":
		return m.emuLog()
	case "S":
		return m.screenshot("save")
	case "I":
		return m.installAPK("")
	case "t":
		if m.tgt().online() {
			return m.ext(false, "adb", "-s", e.Serial, "shell")
		}
	case "M":
		return m.scrcpy()
	}
	return nil
}

func (m *Model) keyConn(s string) tea.Cmd {
	page := max(1, m.rects["conn"].h-2)
	if move(s, &m.connSel, len(m.sc.Conns), page) {
		m.checkSelect()
		return nil
	}
	conn := m.tabs["connect"].(*connectTab)
	switch s {
	case "p":
		return conn.qrPair(m)
	case "c":
		return conn.connectPrompt(m)
	case "a":
		m.ask("Name", "My phone", func(m *Model, n string) tea.Cmd {
			m.ask("host:port", "192.0.2.", func(m *Model, a string) tea.Cmd {
				if n != "" && a != "" {
					core.AddSaved(n, a)
				}
				return m.refreshNow()
			})
			return nil
		})
	case "right", "l":
		m.setFocus(fMain)
	}
	if m.connSel < 0 || m.connSel >= len(m.sc.Conns) {
		return nil
	}
	c := m.sc.Conns[m.connSel]
	switch s {
	case "enter":
		if c.State == "saved" {
			return conn.msgJob(m, "connect", func() (bool, string) { return core.AdbConnect(c.Addr) })
		}
		m.setFocus(fMain)
	case "d":
		if c.State == "saved" {
			core.RemoveSaved(c.Addr)
			return m.refreshNow()
		}
		if strings.HasPrefix(c.Transport, "wifi") {
			return conn.msgJob(m, "disconnect", func() (bool, string) {
				r := core.Run([]string{"adb", "disconnect", c.Serial}, core.Opt{})
				return r.OK(), core.AdbMsg(r)
			})
		}
		m.say("USB devices can't be disconnected — unplug the cable")
	case "s":
		conn.saveDev(m, core.Device{Serial: c.Serial, Model: c.Model, Transport: c.Transport})
		return m.refreshNow()
	case "S":
		return m.screenshot("save")
	case "I":
		return m.installAPK("")
	case "t":
		if c.Online() {
			return m.ext(false, "adb", "-s", c.Serial, "shell")
		}
	case "M":
		return m.scrcpy()
	}
	return nil
}

func (m *Model) keySnap(s string) tea.Cmd {
	if move(s, &m.snapSel, len(m.snaps), max(1, m.rects["snap"].h-2)) {
		return nil
	}
	e := m.emuTarget()
	if s == "esc" || s == "h" || s == "left" {
		m.setFocus(fEmu)
		return nil
	}
	if e == nil {
		return nil
	}
	sel := ""
	if m.snapSel >= 0 && m.snapSel < len(m.snaps) {
		sel = m.snaps[m.snapSel].name
	}
	switch {
	case s == "n":
		if !e.Online() {
			m.say(e.Name + " must be running to save a snapshot")
			return nil
		}
		m.ask("Snapshot name", "snap-"+time.Now().Format("20060102-1504"), func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}
			m.snapsFor = ""
			return m.act("snapshot "+v, func() (bool, string) { return core.SaveSnapshot(*e, v) })
		})
	case s == "enter" && sel != "":
		return m.act("boot from "+sel, func() (bool, string) { return core.StartMode(e.Name, "snap", sel) })
	case s == "l" && sel != "":
		if e.Online() {
			return m.act("load "+sel, func() (bool, string) { return core.LoadSnapshotLive(*e, sel) })
		}
		m.say("needs a running emulator — Enter boots from it instead")
	case s == "d" && sel != "":
		m.confirm("Delete snapshot '"+sel+"'?", true, func(m *Model) tea.Cmd {
			m.snapsFor = ""
			var cur *core.Emu
			if e.Online() {
				cur = e
			}
			return m.act("delete "+sel, func() (bool, string) {
				if cur != nil {
					return core.DeleteSnapshot(*cur, sel)
				}
				return core.DeleteSnapshot(core.Emu{Name: e.Name}, sel)
			})
		})
	}
	return nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	ev, ok := m.normMouse(msg)
	if !ok {
		return nil
	}
	R := m.rects
	var cmd tea.Cmd
	switch {
	case R["emu"].in(ev.x, ev.y):
		cmd = m.mouseList(ev, fEmu, R["emu"], &m.emuSel, m.emuTop, len(m.sc.Emus))
	case R["conn"].in(ev.x, ev.y):
		cmd = m.mouseList(ev, fConn, R["conn"], &m.connSel, m.connTop, len(m.sc.Conns))
	case R["snap"].in(ev.x, ev.y):
		cmd = m.mouseList(ev, fSnap, R["snap"], &m.snapSel, m.snapTop, len(m.snaps))
	case R["main"].in(ev.x, ev.y):
		cmd = m.mouseMain(ev, R["main"])
	}
	m.checkSelect()
	return cmd
}

// mouseList handles a gesture on one of the left-hand lists: wheel scrolls, click selects, double-click activates
// (same as Enter), right-click selects and opens the context menu.
func (m *Model) mouseList(ev mouseEv, f focusT, r rect, sel *int, top, n int) tea.Cmd {
	m.setFocus(f)
	if ev.kind == mWheel {
		*sel = max(0, min(n-1, *sel+ev.dy*wheelStep))
		return nil
	}
	onRow := false
	if i := top + ev.y - r.y - 1; i >= 0 && i < n {
		*sel, onRow = i, true
	}
	m.checkSelect()
	switch ev.kind {
	case mDouble:
		if onRow {
			return m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	case mRight:
		return m.openContext(ev.x, ev.y, onRow)
	}
	return nil
}

// mouseMain handles a gesture in the main panel: the title row switches mode, the first body row switches tab,
// everything below belongs to the current tab.
func (m *Model) mouseMain(ev mouseEv, r rect) tea.Cmd {
	if ev.kind == mWheel {
		m.tab().Scroll(ev.dy * wheelStep)
		return nil
	}
	m.focus = fMain
	row := ev.y - r.y
	if row <= 1 {
		if ev.kind == mRight {
			return m.openContext(ev.x, ev.y, false)
		}
		if row == 0 {
			m.clickMode(ev.x)
		} else {
			m.clickTab(ev.x)
		}
		return nil
	}
	tab, y := m.tab(), row-2
	tab.Click(m, y)
	onRow := true
	if h, ok := tab.(rowHitter); ok {
		onRow = h.hitRow(y)
	}
	switch ev.kind {
	case mDouble:
		if onRow && !tab.Typing() {
			return m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	case mRight:
		return m.openContext(ev.x, ev.y, onRow)
	}
	return nil
}
